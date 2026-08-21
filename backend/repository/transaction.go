package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// TransactionType represents the type of a financial transaction.
type TransactionType string

const (
	TransactionTypeHold    TransactionType = "HOLD"
	TransactionTypePayment TransactionType = "PAYMENT"
	TransactionTypeReward  TransactionType = "REWARD"
	TransactionTypeRefund  TransactionType = "REFUND"
	TransactionTypeFine       TransactionType = "FINE"
	TransactionTypeTopUp      TransactionType = "TOP_UP"
	TransactionTypeWithdrawal TransactionType = "WITHDRAWAL"
)

// TransactionRepository defines storage operations for financial transactions and balance.
type TransactionRepository interface {
	GetBalance(userID uuid.UUID) (float64, error)
	UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error
	CreateTransaction(tx *sql.Tx, t *Transaction) error
	GetTransactionsByUserID(userID uuid.UUID) ([]*Transaction, error)
	RunInTx(fn func(*sql.Tx) error) error
}

// transactionRepo implements TransactionRepository using *sql.DB.
type transactionRepo struct {
	db *sql.DB
}

// NewTransactionRepository creates a new TransactionRepository.
func NewTransactionRepository(db *sql.DB) TransactionRepository {
	return &transactionRepo{db: db}
}

func (r *transactionRepo) GetBalance(userID uuid.UUID) (float64, error) {
	var balance float64
	err := r.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance)
	if err != nil {
		return 0, err
	}
	return balance, nil
}

func (r *transactionRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error {
	query := `UPDATE users SET balance = balance + $1 WHERE id = $2`
	if tx != nil {
		_, err := tx.Exec(query, delta, userID)
		return err
	}
	_, err := r.db.Exec(query, delta, userID)
	return err
}

func (r *transactionRepo) RunInTx(fn func(*sql.Tx) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *transactionRepo) CreateTransaction(tx *sql.Tx, t *Transaction) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	query := `INSERT INTO transactions (id, user_id, order_id, type, amount, admin_id, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if tx != nil {
		_, err := tx.Exec(query, t.ID, t.UserID, t.OrderID, t.Type, t.Amount, t.AdminID, t.CreatedAt)
		return err
	}
	_, err := r.db.Exec(query, t.ID, t.UserID, t.OrderID, t.Type, t.Amount, t.AdminID, t.CreatedAt)
	return err
}

func (r *transactionRepo) GetTransactionsByUserID(userID uuid.UUID) ([]*Transaction, error) {
	rows, err := r.db.Query(
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
