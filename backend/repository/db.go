package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
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

const (
	// DefaultHistoryPageSize bounds the history lists — an executor's past
	// orders, a user's ledger entries — that a client asks for without saying
	// how much it wants. These are screens, not exports: they render a recent
	// window, and an account with years of activity should not be able to make
	// one request read all of it.
	DefaultHistoryPageSize = 200
	// MaxHistoryPageSize caps what an explicit limit can ask for.
	MaxHistoryPageSize = 1000
)

// historyLimit resolves a caller-supplied limit against those bounds.
func historyLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultHistoryPageSize
	case limit > MaxHistoryPageSize:
		return MaxHistoryPageSize
	default:
		return limit
	}
}

// idList renders a deduplicated id set as a positional placeholder list
// ("$1, $2, $3") together with the arguments to bind to it, for the batch
// lookups that replace per-row queries in list endpoints.
//
// Deduplication is the point as much as the batching: a page of orders placed
// by the same customer would otherwise bind that id once per row. An empty
// input yields an empty list, which callers must treat as "nothing to fetch"
// rather than passing to a query — "IN ()" is not valid SQL.
func idList(ids []uuid.UUID) (string, []interface{}) {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	placeholders := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	return strings.Join(placeholders, ", "), args
}

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
