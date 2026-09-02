package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// TransactionType представляет тип финансовой транзакции.
type TransactionType string

const (
	TransactionTypeHold       TransactionType = "HOLD"
	TransactionTypePayment    TransactionType = "PAYMENT"
	TransactionTypeReward     TransactionType = "REWARD"
	TransactionTypeRefund     TransactionType = "REFUND"
	TransactionTypeFine       TransactionType = "FINE"
	TransactionTypeTopUp      TransactionType = "TOP_UP"
	TransactionTypeWithdrawal TransactionType = "WITHDRAWAL"
	// TransactionTypeWithdrawalHold резервирует деньги при запросе вывода;
	// TransactionTypeWithdrawalPaid фиксирует выплату этого резерва. Вместе они
	// зеркалят HOLD/PAYMENT на стороне заказа.
	TransactionTypeWithdrawalHold TransactionType = "WITHDRAWAL_HOLD"
	TransactionTypeWithdrawalPaid TransactionType = "WITHDRAWAL_PAID"
	// TransactionTypeTip списывает с заказчика, дающего чаевые исполнителю после
	// завершённого заказа; TransactionTypeTipReward зачисляет исполнителю. Чаевые
	// проходят через ESCROW одной транзакцией, поэтому пара там сводится в ноль.
	TransactionTypeTip       TransactionType = "TIP"
	TransactionTypeTipReward TransactionType = "TIP_REWARD"
	// TransactionTypeCommission фиксирует переход доли платформы с завершённого
	// заказа из эскроу на счёт комиссии;
	// TransactionTypeCommissionPayout фиксирует вывод этого счёта админом из
	// системы. Ни один из них не трогает баланс пользователя.
	TransactionTypeCommission       TransactionType = "COMMISSION"
	TransactionTypeCommissionPayout TransactionType = "COMMISSION_PAYOUT"
	// TransactionTypeBonus зачисляет пользователю из собственного кармана
	// платформы: вознаграждение, которое скрипт поведения платит за услугу, не
	// оплаченную заказчиком. Он смотрит на счёт BONUSES (см. миграцию 043).
	TransactionTypeBonus TransactionType = "BONUS"
)

// ledgerSigns объявляет, как каждый тип транзакции двигает баланс пользователя.
// Это соглашение о знаках в реестре, и оно намеренно записано один раз: суммы в
// таблице все положительные, а направление живёт в типе, поэтому без
// единственного объявления правило пришлось бы заново выводить из кода сервисов
// всякий раз, когда кому-то надо сложить журнал.
//
// PAYMENT равен 0 намеренно. Деньги заказчика ушли с его баланса, когда бралось
// удержание; PAYMENT фиксирует расход этого удержания и ничего не двигает.
var ledgerSigns = map[TransactionType]int{
	TransactionTypeTopUp:      +1,
	TransactionTypeReward:     +1,
	TransactionTypeRefund:     +1,
	TransactionTypeHold:       -1,
	TransactionTypeFine:       -1,
	TransactionTypeWithdrawal: -1,
	// Списание — это резервирование денег; выплата ничего не двигает, потому что
	// они ушли с баланса ещё при создании заявки.
	TransactionTypeWithdrawalHold: -1,
	TransactionTypePayment:        0,
	TransactionTypeWithdrawalPaid: 0,
	// Чаевые списывают с заказчика и зачисляют исполнителю ту же сумму, одной
	// транзакцией через ESCROW.
	TransactionTypeTip:       -1,
	TransactionTypeTipReward: +1,
	// Комиссия перемещается между двумя системными счетами. Пользователь, против
	// которого она записана, — исполнитель, с чьего заказа она пришла, админ,
	// который её вывел, — нужен, чтобы запись находилась, а не чтобы двигать баланс.
	TransactionTypeCommission:       0,
	TransactionTypeCommissionPayout: 0,
	// Бонус зачисляет пользователю; счёт платформы BONUSES уходит в минус на ту же
	// сумму, поэтому книги всё равно сходятся.
	TransactionTypeBonus: +1,
}

// LedgerSign сообщает, как тип транзакции двигает баланс и известен ли тип
// вообще. Неизвестный тип означает, что соглашение выше не обновили вместе с
// новым видом транзакции, а это делает любой результат сверки бессмысленным —
// вызывающие обязаны трактовать это как ошибку, а не пропускать.
func LedgerSign(t TransactionType) (int, bool) {
	sign, ok := ledgerSigns[t]
	return sign, ok
}

// KnownTransactionTypes перечисляет типы, покрытые соглашением о знаках.
func KnownTransactionTypes() []TransactionType {
	types := make([]TransactionType, 0, len(ledgerSigns))
	for t := range ledgerSigns {
		types = append(types, t)
	}
	return types
}

// TransactionRepository описывает операции хранения финансовых транзакций и баланса.
type TransactionRepository interface {
	GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error)
	// UpdateBalance применяет безусловную дельту. Используйте Debit всякий раз,
	// когда баланс обязан остаться неотрицательным.
	UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error
	// Debit вычитает amount, только если баланс это покрывает, атомарно.
	// Возвращает ErrInsufficientFunds, когда не покрывает.
	Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error
	CreateTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error
	// GetTransactionsByUserID возвращает проводки пользователя, сначала новые, не
	// более limit. Limit, равный нулю или меньше, означает DefaultHistoryPageSize:
	// это питает экран истории, и учётка с годами активности не должна уметь
	// заставить один запрос прочитать всё её прошлое.
	GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*Transaction, error)
	// HasTip сообщает, давал ли заказчик чаевые по этому заказу, чтобы чаевые
	// списывались не более одного раза. Выполняется внутри транзакции вызывающего,
	// поэтому проверка и запись — один атомарный шаг.
	HasTip(ctx context.Context, q Querier, orderID uuid.UUID) (bool, error)
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
}

// transactionRepo реализует TransactionRepository поверх *sql.DB.
type transactionRepo struct {
	db *sql.DB
}

// NewTransactionRepository создаёт новый TransactionRepository.
func NewTransactionRepository(db *sql.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	var balance money.Amount
	err := r.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (r *transactionRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	return execExpectingOne(ctx, r.querier(ctx, tx),
		`UPDATE users SET balance = balance + $1 WHERE id = $2`, delta, userID)
}

// Debit вычитает amount одним охраняемым оператором, поэтому гонка
// «проверил-записал» не может увести баланс ниже нуля.
func (r *transactionRepo) Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	if amount.IsNegative() {
		return errors.New("debit amount must not be negative")
	}
	err := execExpectingOne(ctx, r.querier(ctx, tx),
		`UPDATE users SET balance = balance - $1 WHERE id = $2 AND balance >= $1`, amount, userID)
	if errors.Is(err, ErrConflict) {
		return ErrInsufficientFunds
	}
	return err
}

func (r *transactionRepo) querier(ctx context.Context, tx *sql.Tx) Querier {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *transactionRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *transactionRepo) CreateTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	query := `INSERT INTO transactions (id, user_id, order_id, type, amount, admin_id, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if tx != nil {
		_, err := tx.ExecContext(ctx, query, t.ID, t.UserID, t.OrderID, t.Type, t.Amount, t.AdminID, t.CreatedAt)
		return err
	}
	_, err := r.db.ExecContext(ctx, query, t.ID, t.UserID, t.OrderID, t.Type, t.Amount, t.AdminID, t.CreatedAt)
	return err
}

// HasTip проверяет наличие записи TIP по заказу. Списание с заказчика — это
// строка TIP; TIP_REWARD лежит на исполнителе, поэтому искать достаточно по
// одному типу.
func (r *transactionRepo) HasTip(ctx context.Context, q Querier, orderID uuid.UUID) (bool, error) {
	var exists bool
	err := r.querierAny(ctx, q).QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM transactions WHERE order_id = $1 AND type = $2)`,
		orderID, TransactionTypeTip,
	).Scan(&exists)
	return exists, err
}

func (r *transactionRepo) querierAny(ctx context.Context, q Querier) Querier {
	if q != nil {
		return q
	}
	return r.db
}

func (r *transactionRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*Transaction, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, order_id, type, amount, admin_id, created_at
		 FROM transactions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, historyLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Transaction
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.OrderID, &t.Type, &t.Amount, &t.AdminID, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, &t)
	}
	return result, rows.Err()
}
