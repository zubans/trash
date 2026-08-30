package service

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Ledger is the only way money moves.
//
// Every operation below touches two sides: a user balance and a system account,
// or two system accounts. That is the point — before this type existed, a fine
// left an executor's balance and simply stopped existing, a hold left the
// customer and lived only as a number on the order, and a top-up appeared from
// nowhere. Services no longer have a raw balance mutator to reach for, so a
// one-sided movement cannot be written by accident.
//
// The invariant this buys: the sum of every user balance plus the sum of every
// system account balance is zero. ReconciliationRepository checks it.
type Ledger struct {
	transactions repository.TransactionRepository
	accounts     repository.SystemAccountRepository
}

// NewLedger creates a Ledger over the balance and account stores.
func NewLedger(transactions repository.TransactionRepository, accounts repository.SystemAccountRepository) *Ledger {
	return &Ledger{transactions: transactions, accounts: accounts}
}

// RunInTx runs fn in a database transaction. Every paired operation below must
// be called inside one.
func (l *Ledger) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return l.transactions.RunInTx(ctx, fn)
}

// GetBalance reads a user's balance.
func (l *Ledger) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	return l.transactions.GetBalance(ctx, userID)
}

// AccountBalance reads a system account. Admin surfaces show the commission
// account this way; services never need it to move money, because every
// movement already names the account it faces.
func (l *Ledger) AccountBalance(ctx context.Context, code string) (*repository.SystemAccount, error) {
	return l.accounts.Get(ctx, code)
}

// History returns a user's ledger entries.
func (l *Ledger) History(ctx context.Context, userID uuid.UUID) ([]*repository.Transaction, error) {
	return l.transactions.GetTransactionsByUserID(ctx, userID)
}

// HasTip reports whether an order was already tipped. Called inside the tip
// transaction so the guard and the charge commit together.
func (l *Ledger) HasTip(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) (bool, error) {
	return l.transactions.HasTip(ctx, tx, orderID)
}

// entry describes one side of a movement as it is recorded in the log.
type entry struct {
	UserID  uuid.UUID
	OrderID *uuid.UUID
	AdminID *uuid.UUID
	Type    repository.TransactionType
	Account string
	Amount  money.Amount
}

func (l *Ledger) record(ctx context.Context, tx *sql.Tx, e entry) error {
	err := l.transactions.CreateTransaction(ctx, tx, &repository.Transaction{
		UserID:       e.UserID,
		OrderID:      e.OrderID,
		AdminID:      e.AdminID,
		Type:         string(e.Type),
		Amount:       e.Amount,
		Counterparty: e.Account,
	})
	// Counted here rather than at each call site: this is the one funnel every
	// movement passes through, so the totals cannot drift from the log. The
	// entry may still be rolled back with its transaction, which is why the
	// authoritative number stays the reconciliation pass and this is a rate.
	if err != nil {
		metrics.LedgerError(string(e.Type))
		return err
	}
	metrics.LedgerEntry(string(e.Type), e.Account, e.Amount.Rubles())
	return nil
}

// Reserve moves money from a user to a system account, but only if the balance
// covers it. Used for order holds and withdrawal reservations, where spending
// money the user does not have is never acceptable.
//
// Returns repository.ErrInsufficientFunds when the balance is too small.
func (l *Ledger) Reserve(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.transactions.Debit(ctx, tx, userID, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, account, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Charge moves money from a user to a system account without checking the
// balance. Used for penalties: an executor's balance is allowed to go negative,
// which is what min_balance_limit is for.
func (l *Ledger) Charge(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.transactions.UpdateBalance(ctx, tx, userID, -amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, account, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Release moves money from a system account to a user: a refund out of escrow,
// an executor's reward, a returned withdrawal reservation.
func (l *Ledger) Release(ctx context.Context, tx *sql.Tx, account string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, orderID, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, account, amount); err != nil {
		return err
	}
	if err := l.transactions.UpdateBalance(ctx, tx, userID, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, AdminID: adminID, Type: kind, Account: account, Amount: amount})
}

// Deposit brings money in from outside: an approved top-up. DEPOSITS goes
// negative by the same amount, which is how an external source is represented.
func (l *Ledger) Deposit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount, adminID *uuid.UUID) error {
	return l.Release(ctx, tx, repository.AccountDeposits, userID, amount, repository.TransactionTypeTopUp, nil, adminID)
}

// Settle moves money between two system accounts, recording the entry against
// the user it concerns. Used when a payout leaves the system: the reservation
// goes out through DEPOSITS, the account that represents the outside world.
func (l *Ledger) Settle(ctx context.Context, tx *sql.Tx, from, to string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, from, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, to, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, AdminID: adminID, Type: kind, Account: from, Amount: amount})
}

// Commission moves the platform's share of a completed order from escrow to the
// commission account, inside the confirmation transaction. It is recorded
// against the executor and the order, so the entry can be traced back to the
// payout it was taken out of. Escrow is debited unguarded like every other
// order movement: the money is already held for this order, and the caller
// splits exactly what it holds between the executor and this account.
func (l *Ledger) Commission(ctx context.Context, tx *sql.Tx, executorID uuid.UUID, amount money.Amount, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.Debit(ctx, tx, repository.AccountEscrow, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, repository.AccountCommission, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{
		UserID:  executorID,
		OrderID: orderID,
		Type:    repository.TransactionTypeCommission,
		Account: repository.AccountEscrow,
		Amount:  amount,
	})
}

// Payout sends money out of a system account to the outside world, but only if
// the account actually holds it. Settle is the unguarded version, used where
// the money was reserved earlier and is known to be there; this one is for
// paying out an account on request, where the amount is whatever the caller
// asked for and an unguarded debit would let the account go negative.
//
// Returns repository.ErrInsufficientFunds when the account holds less.
func (l *Ledger) Payout(ctx context.Context, tx *sql.Tx, from string, userID uuid.UUID, amount money.Amount, kind repository.TransactionType, adminID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.accounts.DebitAvailable(ctx, tx, from, amount); err != nil {
		return err
	}
	if err := l.accounts.Credit(ctx, tx, repository.AccountDeposits, amount); err != nil {
		return err
	}
	return l.record(ctx, tx, entry{UserID: userID, AdminID: adminID, Type: kind, Account: from, Amount: amount})
}

// Note records an entry that moves no money, for a step that is worth seeing in
// the log: PAYMENT marks a hold being spent, and the balance already changed
// when the hold was taken.
func (l *Ledger) Note(ctx context.Context, tx *sql.Tx, userID uuid.UUID, account string, amount money.Amount, kind repository.TransactionType, orderID *uuid.UUID) error {
	return l.record(ctx, tx, entry{UserID: userID, OrderID: orderID, Type: kind, Account: account, Amount: amount})
}

// Tip moves a tip from a customer to an executor. The money passes through
// ESCROW in the caller's transaction — debited from the customer only if the
// balance covers it, then released to the executor — so it never exists outside
// an account and reconciliation stays balanced. Returns
// repository.ErrInsufficientFunds when the customer cannot cover the tip.
func (l *Ledger) Tip(ctx context.Context, tx *sql.Tx, customerID, executorID uuid.UUID, amount money.Amount, orderID *uuid.UUID) error {
	if !amount.IsPositive() {
		return nil
	}
	if err := l.Reserve(ctx, tx, customerID, repository.AccountEscrow, amount, repository.TransactionTypeTip, orderID); err != nil {
		return err
	}
	return l.Release(ctx, tx, repository.AccountEscrow, executorID, amount, repository.TransactionTypeTipReward, orderID, nil)
}
