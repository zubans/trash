package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// TransactionType represents the type of a financial transaction.
type TransactionType string

const (
	TransactionTypeHold       TransactionType = "HOLD"
	TransactionTypePayment    TransactionType = "PAYMENT"
	TransactionTypeReward     TransactionType = "REWARD"
	TransactionTypeRefund     TransactionType = "REFUND"
	TransactionTypeFine       TransactionType = "FINE"
	TransactionTypeTopUp      TransactionType = "TOP_UP"
	TransactionTypeWithdrawal TransactionType = "WITHDRAWAL"
	// TransactionTypeWithdrawalHold reserves money when a withdrawal is
	// requested; TransactionTypeWithdrawalPaid records that reservation being
	// paid out. Together they mirror HOLD/PAYMENT on the order side.
	TransactionTypeWithdrawalHold TransactionType = "WITHDRAWAL_HOLD"
	TransactionTypeWithdrawalPaid TransactionType = "WITHDRAWAL_PAID"
	// TransactionTypeTip debits a customer who tips the executor after a
	// completed order; TransactionTypeTipReward credits the executor. The tip
	// passes through ESCROW in one transaction, so the pair nets to zero there.
	TransactionTypeTip       TransactionType = "TIP"
	TransactionTypeTipReward TransactionType = "TIP_REWARD"
	// TransactionTypeCommission records the platform's share of a completed
	// order moving from escrow to the commission account;
	// TransactionTypeCommissionPayout records an admin paying that account out
	// of the system. Neither touches a user balance.
	TransactionTypeCommission       TransactionType = "COMMISSION"
	TransactionTypeCommissionPayout TransactionType = "COMMISSION_PAYOUT"
)

// ledgerSigns declares how each transaction type moves a user's balance. This is
// the ledger's sign convention, and it is deliberately written down once:
// amounts in the table are all positive and the direction lives in the type, so
// without a single declaration the rule has to be re-derived from the service
// code every time somebody wants to add up the log.
//
// PAYMENT is 0 on purpose. The customer's money left their balance when the hold
// was taken; PAYMENT records that hold being spent and moves nothing.
var ledgerSigns = map[TransactionType]int{
	TransactionTypeTopUp:      +1,
	TransactionTypeReward:     +1,
	TransactionTypeRefund:     +1,
	TransactionTypeHold:       -1,
	TransactionTypeFine:       -1,
	TransactionTypeWithdrawal: -1,
	// Reserving the money is the debit; paying it out moves nothing, because it
	// already left the balance when the request was created.
	TransactionTypeWithdrawalHold: -1,
	TransactionTypePayment:        0,
	TransactionTypeWithdrawalPaid: 0,
	// A tip debits the customer and credits the executor by the same amount, in
	// one transaction through ESCROW.
	TransactionTypeTip:       -1,
	TransactionTypeTipReward: +1,
	// Commission moves between two system accounts. The user it is recorded
	// against — the executor whose order it came from, the admin who paid it
	// out — is there to make the entry findable, not to move their balance.
	TransactionTypeCommission:       0,
	TransactionTypeCommissionPayout: 0,
}

// LedgerSign reports how a transaction type moves the balance, and whether the
// type is known at all. An unknown type means the convention above was not
// updated alongside a new kind of transaction, which makes every reconciliation
// result meaningless — callers must treat it as an error, not skip it.
func LedgerSign(t TransactionType) (int, bool) {
	sign, ok := ledgerSigns[t]
	return sign, ok
}

// KnownTransactionTypes lists the types covered by the sign convention.
func KnownTransactionTypes() []TransactionType {
	types := make([]TransactionType, 0, len(ledgerSigns))
	for t := range ledgerSigns {
		types = append(types, t)
	}
	return types
}

// TransactionRepository defines storage operations for financial transactions and balance.
type TransactionRepository interface {
	GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error)
	// UpdateBalance applies an unconditional delta. Use Debit instead whenever
	// the balance must stay non-negative.
	UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error
	// Debit subtracts amount only if the balance covers it, atomically.
	// Returns ErrInsufficientFunds when it does not.
	Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error
	CreateTransaction(ctx context.Context, tx *sql.Tx, t *Transaction) error
	GetTransactionsByUserID(ctx context.Context, userID uuid.UUID) ([]*Transaction, error)
	// HasTip reports whether the customer already tipped this order, so a tip is
	// charged at most once. Runs inside the caller's transaction so the check
	// and the write are one atomic step.
	HasTip(ctx context.Context, q Querier, orderID uuid.UUID) (bool, error)
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
}

// transactionRepo implements TransactionRepository using *sql.DB.
type transactionRepo struct {
	db *sql.DB
}

// NewTransactionRepository creates a new TransactionRepository.
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

// Debit subtracts amount in a single guarded statement, so a check-then-write
// race cannot push the balance below zero.
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

// HasTip checks for an existing TIP entry on the order. The customer's debit is
// the TIP row; TIP_REWARD sits on the executor, so one type is enough to look
// for.
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

func (r *transactionRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID) ([]*Transaction, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, order_id, type, amount, admin_id, created_at
		 FROM transactions WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
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
