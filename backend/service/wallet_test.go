package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

func newWalletTestService() (*WalletService, *mockAdminRepo, *mockRepo, *mockTransactionRepo) {
	userRepo := newMockRepo()
	payouts := &mockAdminRepo{
		requests:    make(map[uuid.UUID]*repository.TopUpRequest),
		withdrawals: make(map[uuid.UUID]*repository.WithdrawalRequest),
	}
	txRepo := &mockTransactionRepo{}
	return NewWalletService(userRepo, payouts, NewLedger(txRepo, newMockAccounts())), payouts, userRepo, txRepo
}

// Вторая заявка на вывод при открытой первой — отказ по состоянию: проверка
// идёт внутри транзакции, и деньги не резервируются дважды.
func TestWalletRefusesSecondPendingWithdrawal(t *testing.T) {
	svc, _, userRepo, txRepo := newWalletTestService()
	user := &repository.User{ID: uuid.New(), Phone: "+79990000020", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user
	ctx := context.Background()

	if _, err := svc.CreateWithdrawalRequest(ctx, user.ID, money.FromRubles(100)); err != nil {
		t.Fatalf("first request: %v", err)
	}
	_, err := svc.CreateWithdrawalRequest(ctx, user.ID, money.FromRubles(100))
	if !errors.Is(err, ErrOrderState) {
		t.Fatalf("second request: expected ErrOrderState, got %v", err)
	}
	if balance, _ := txRepo.GetBalance(ctx, user.ID); balance != mockDefaultBalance.Sub(money.FromRubles(100)) {
		t.Errorf("refused request must not reserve money again, balance %s", balance)
	}
}

// Классы ошибок кошелька: заблокированный — отказ в доступе, неизвестный —
// «не найдено», нулевая сумма — негодный ввод, нехватка — ErrInsufficientFunds.
func TestWalletErrorClasses(t *testing.T) {
	svc, _, userRepo, txRepo := newWalletTestService()
	ctx := context.Background()
	banned := &repository.User{ID: uuid.New(), Phone: "+79990000021", Role: "CUSTOMER", Status: repository.UserStatusBanned}
	poor := &repository.User{ID: uuid.New(), Phone: "+79990000022", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[banned.Phone] = banned
	userRepo.users[poor.Phone] = poor
	txRepo.balances = map[uuid.UUID]money.Amount{poor.ID: money.FromRubles(10)}

	if _, err := svc.CreateTopUpRequest(ctx, banned.ID, money.FromRubles(10)); !errors.Is(err, ErrForbidden) {
		t.Errorf("banned top-up: expected ErrForbidden, got %v", err)
	}
	if _, err := svc.CreateWithdrawalRequest(ctx, banned.ID, money.FromRubles(10)); !errors.Is(err, ErrForbidden) {
		t.Errorf("banned withdrawal: expected ErrForbidden, got %v", err)
	}
	if _, err := svc.CreateTopUpRequest(ctx, uuid.New(), money.FromRubles(10)); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("unknown user: expected ErrNotFound, got %v", err)
	}
	if _, err := svc.CreateTopUpRequest(ctx, poor.ID, money.Zero); !errors.Is(err, ErrValidation) {
		t.Errorf("zero amount: expected ErrValidation, got %v", err)
	}
	if _, err := svc.CreateWithdrawalRequest(ctx, poor.ID, money.FromRubles(500)); !errors.Is(err, repository.ErrInsufficientFunds) {
		t.Errorf("insufficient: expected ErrInsufficientFunds, got %v", err)
	}
}
