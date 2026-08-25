package repository

import (
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
	Credit(q Querier, code string, amount money.Amount) error
	Debit(q Querier, code string, amount money.Amount) error
	List() ([]SystemAccount, error)
	Get(code string) (*SystemAccount, error)
}

type systemAccountRepo struct {
	db *sql.DB
}

// NewSystemAccountRepository creates a SystemAccountRepository.
func NewSystemAccountRepository(db *sql.DB) SystemAccountRepository {
	return &systemAccountRepo{db: db}
}

func (r *systemAccountRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *systemAccountRepo) Credit(q Querier, code string, amount money.Amount) error {
	return r.move(q, code, amount)
}

func (r *systemAccountRepo) Debit(q Querier, code string, amount money.Amount) error {
	return r.move(q, code, amount.Neg())
}

// move applies a delta. A zero amount is a no-op rather than an error: several
// call sites legitimately move nothing (a refund of zero, a fine of zero).
func (r *systemAccountRepo) move(q Querier, code string, delta money.Amount) error {
	if delta.IsZero() {
		return nil
	}
	err := execExpectingOne(r.exec(q),
		`UPDATE system_accounts SET balance = balance + $1, updated_at = now() WHERE code = $2`,
		delta, code)
	if errors.Is(err, ErrConflict) {
		return ErrUnknownSystemAccount
	}
	return err
}

func (r *systemAccountRepo) Get(code string) (*SystemAccount, error) {
	var a SystemAccount
	err := r.db.QueryRow(
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

func (r *systemAccountRepo) List() ([]SystemAccount, error) {
	rows, err := r.db.Query(`SELECT code, name, balance FROM system_accounts ORDER BY code`)
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
