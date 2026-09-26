package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// WalletService — заявки пользователя на пополнение и вывод собственных денег.
// Решает по ним администратор (AdminService); здесь только то, что делает сам
// пользователь.
type WalletService struct {
	users   repository.UserRepository
	payouts repository.PayoutRepository
	ledger  *Ledger
}

// NewWalletService создаёт WalletService. ledger нужен выводу: заявка
// резервирует деньги в момент создания.
func NewWalletService(users repository.UserRepository, payouts repository.PayoutRepository, ledger *Ledger) *WalletService {
	return &WalletService{users: users, payouts: payouts, ledger: ledger}
}

var (
	// ErrWithdrawalPending — у пользователя уже есть заявка на вывод в обработке.
	ErrWithdrawalPending = stateError("у вас уже есть заявка на вывод в обработке")
	// ErrUserBlocked — заблокированный пользователь не двигает деньги.
	ErrUserBlocked = forbiddenError("учётная запись заблокирована")
	// ErrAmountNotPositive — сумма заявки или зачисления должна быть больше нуля.
	ErrAmountNotPositive = validationError("amount must be greater than zero")
)

// activeUser читает пользователя и отказывает заблокированному.
func (s *WalletService) activeUser(ctx context.Context, userID uuid.UUID) (*repository.User, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, userNotFound(err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if user.Status == repository.UserStatusBanned {
		return nil, ErrUserBlocked
	}
	return user, nil
}

// CreateTopUpRequest создаёт ожидающую заявку на пополнение баланса.
func (s *WalletService) CreateTopUpRequest(ctx context.Context, userID uuid.UUID, amount money.Amount) (*repository.TopUpRequest, error) {
	if !amount.IsPositive() {
		return nil, ErrAmountNotPositive
	}
	if s.payouts == nil {
		return nil, fmt.Errorf("%w: payouts", ErrNotConfigured)
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.payouts.CreateTopUpRequest(ctx, nil, userID, amount)
}

// CreateWithdrawalRequest резервирует запрошенную сумму и записывает ожидающую
// заявку на неё.
//
// Деньги уходят с баланса немедленно, ровно как удержание по заказу. Раньше
// заявка лишь проверяла баланс и оставляла средства тратимыми, поэтому
// пользователь мог поставить в очередь несколько заявок на одни и те же деньги
// и потратить их за время ожидания, а очередь выплат тогда содержала суммы,
// которые нельзя было выполнить все.
//
// Проверка «уже есть открытая заявка» идёт внутри той же транзакции под
// advisory-блокировкой по пользователю (см. PayoutRepository.HasPendingWithdrawal):
// до этого она стояла перед транзакцией, и две одновременные заявки проходили
// её обе.
func (s *WalletService) CreateWithdrawalRequest(ctx context.Context, userID uuid.UUID, amount money.Amount) (*repository.WithdrawalRequest, error) {
	if !amount.IsPositive() {
		return nil, ErrAmountNotPositive
	}
	if s.ledger == nil || s.payouts == nil {
		return nil, fmt.Errorf("%w: ledger", ErrNotConfigured)
	}
	if _, err := s.activeUser(ctx, userID); err != nil {
		return nil, err
	}

	var created *repository.WithdrawalRequest
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		pending, err := s.payouts.HasPendingWithdrawal(ctx, tx, userID)
		if err != nil {
			return err
		}
		if pending {
			return ErrWithdrawalPending
		}
		// Охраняемое списание: баланс обязан покрыть заявку в этот момент,
		// а не при каком-то более раннем чтении. Деньги уходят с баланса на
		// счёт выплат, где они ждут решения администратора.
		if err := s.ledger.Reserve(ctx, tx, userID, repository.AccountPayouts, amount, repository.TransactionTypeWithdrawalHold, nil); err != nil {
			return err
		}
		req, err := s.payouts.CreateWithdrawalRequest(ctx, tx, userID, amount)
		if err != nil {
			return err
		}
		created = req
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, ErrInsufficientBalance
		}
		return nil, err
	}
	return created, nil
}
