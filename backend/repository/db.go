package repository

import (
	"context"
	"database/sql"
	"errors"
)

// Querier is the subset of *sql.DB / *sql.Tx used by the repositories.
// Accepting it lets a caller run a repository method inside an open
// transaction instead of on an implicit auto-commit connection. The
// context-aware methods are used so cancellation propagates into the driver.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

var (
	// ErrConflict is returned when a guarded UPDATE matched no rows, i.e. the
	// entity was not in the state the caller expected. Handlers map it to 409.
	ErrConflict = errors.New("state changed concurrently, operation not applied")

	// ErrInsufficientFunds is returned when a debit would drive a balance below zero.
	ErrInsufficientFunds = errors.New("insufficient balance")
)

// execExpectingOne runs a guarded statement and fails with ErrConflict when the
// guard in the WHERE clause did not match. Silent no-op updates are the root
// cause of the duplicate-refund class of bugs, so every state transition must
// go through this helper.
func execExpectingOne(ctx context.Context, q Querier, query string, args ...interface{}) error {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConflict
	}
	return nil
}
