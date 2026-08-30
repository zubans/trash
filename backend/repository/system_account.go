package repository

import (
	"context"
	"database/sql"
	"errors"

	"healthlogin/backend/money"
)

// System account codes. Every movement of user money faces one of these, so the
// money always has somewhere to come from and somewhere to go.
const (
	// AccountEscrow holds what has been taken from customers for orders that are
	// still running. It is released to the executor on completion or back to the
	// customer on cancellation.
	AccountEscrow = "ESCROW"
	// AccountFines collects penalties charged to executors. Before this account
	// existed a fine simply left the executor's balance and vanished.
	AccountFines = "FINES"
	// AccountDeposits is the outside world: money enters the system from here.
	AccountDeposits = "DEPOSITS"
	// AccountPayouts holds money reserved by pending withdrawal requests and
	// releases it outward when a payout is approved.
	AccountPayouts = "PAYOUTS"
	// AccountCommission collects the platform's share of every completed order.
	// It is the one account whose balance is the platform's own money rather
	// than money it is holding for somebody, and only an admin may pay it out.
	AccountCommission = "COMMISSION"
)

// ErrUnknownSystemAccount is returned for a code with no account.
var ErrUnknownSystemAccount = errors.New("unknown system account")

// SystemAccount is one side of the platform's books.
type SystemAccount struct {
	Code    string       `json:"code"`
	Name    string       `json:"name"`
	Balance money.Amount `json:"balance"`
}

// SystemAccountRepository moves money on the platform's own accounts.
type SystemAccountRepository interface {
	// Credit adds to an account; Debit subtracts. Both take the caller's
	// transaction, because an account movement is always the other half of a
	// user balance movement and the two must commit together.
	Credit(ctx context.Context, q Querier, code string, amount money.Amount) error
	Debit(ctx context.Context, q Querier, code string, amount money.Amount) error
	// DebitAvailable subtracts only if the account holds that much, atomically.
	// Returns ErrInsufficientFunds when it does not. Debit is unguarded because
	// its callers are the other half of a movement that already established the
	// money is there; this is for paying money out of an account on request.
	DebitAvailable(ctx context.Context, q Querier, code string, amount money.Amount) error
	List(ctx context.Context) ([]SystemAccount, error)
	Get(ctx context.Context, code string) (*SystemAccount, error)
}

type systemAccountRepo struct {
	db *sql.DB
}

// NewSystemAccountRepository creates a SystemAccountRepository.
func NewSystemAccountRepository(db *sql.DB) SystemAccountRepository {
	return &systemAccountRepo{db: db}
}

func (r *systemAccountRepo) exec(ctx context.Context, q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *systemAccountRepo) Credit(ctx context.Context, q Querier, code string, amount money.Amount) error {
	return r.move(ctx, q, code, amount)
}

func (r *systemAccountRepo) Debit(ctx context.Context, q Querier, code string, amount money.Amount) error {
	return r.move(ctx, q, code, amount.Neg())
}

// move applies a delta. A zero amount is a no-op rather than an error: several
// call sites legitimately move nothing (a refund of zero, a fine of zero).
func (r *systemAccountRepo) move(ctx context.Context, q Querier, code string, delta money.Amount) error {
	if delta.IsZero() {
		return nil
	}
	err := execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE system_accounts SET balance = balance + $1, updated_at = now() WHERE code = $2`,
		delta, code)
	if errors.Is(err, ErrConflict) {
		return ErrUnknownSystemAccount
	}
	return err
}

// DebitAvailable subtracts in a single guarded statement, so two concurrent
// payouts cannot both pass a balance check and push the account negative.
func (r *systemAccountRepo) DebitAvailable(ctx context.Context, q Querier, code string, amount money.Amount) error {
	if amount.IsNegative() {
		return errors.New("debit amount must not be negative")
	}
	if amount.IsZero() {
		return nil
	}
	err := execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE system_accounts SET balance = balance - $1, updated_at = now() WHERE code = $2 AND balance >= $1`,
		amount, code)
	if errors.Is(err, ErrConflict) {
		// Either the account does not exist or it does not hold enough. Tell the
		// two apart so an unknown code is not reported as a money problem.
		if _, getErr := r.Get(ctx, code); getErr != nil {
			return getErr
		}
		return ErrInsufficientFunds
	}
	return err
}

func (r *systemAccountRepo) Get(ctx context.Context, code string) (*SystemAccount, error) {
	var a SystemAccount
	err := r.db.QueryRowContext(ctx,
		`SELECT code, name, balance FROM system_accounts WHERE code = $1`, code,
	).Scan(&a.Code, &a.Name, &a.Balance)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnknownSystemAccount
		}
		return nil, err
	}
	return &a, nil
}

func (r *systemAccountRepo) List(ctx context.Context) ([]SystemAccount, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code, name, balance FROM system_accounts ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []SystemAccount
	for rows.Next() {
		var a SystemAccount
		if err := rows.Scan(&a.Code, &a.Name, &a.Balance); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}
