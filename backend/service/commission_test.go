package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// confirmWithCommission runs one order end to end at the given rate and hands
// back the books so a test can look at both sides of the split, along with the
// books total from before the order — the mock balances do not start from zero,
// so what matters is that a confirmation leaves the total exactly where it was.
func confirmWithCommission(t *testing.T, percent string) (*mockTransactionRepo, *mockAccounts, uuid.UUID, money.Amount) {
	t.Helper()

	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	orderRepo := &mockOrderRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{
		SettingOrderCommissionPercent: percent,
	}}
	srv := NewOrderService(orderRepo, NewLedger(txRepo, accounts), settings,
		newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	ctx := context.Background()
	customerID, executorID := uuid.New(), uuid.New()
	// Touch both balances so the opening total covers everyone involved.
	if _, err := txRepo.GetBalance(ctx, customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if _, err := txRepo.GetBalance(ctx, executorID); err != nil {
		t.Fatalf("balance: %v", err)
	}

	opening := booksTotal(txRepo, accounts)

	order, err := srv.CreateOrder(ctx, customerID, standardVariantID, false, false, "", nil, nil)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := orderRepo.AssignOrder(ctx, order.ID, executorID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := orderRepo.Execute(ctx, nil, order.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := srv.ConfirmOrder(ctx, order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	return txRepo, accounts, executorID, opening
}

// A completed order is the platform's only source of commission, and taking it
// must not invent or destroy money: escrow holds exactly what the customer paid
// and drains to zero across the executor's reward and the commission account.
func TestConfirmOrderSplitsPaymentBetweenExecutorAndCommission(t *testing.T) {
	txRepo, accounts, executorID, opening := confirmWithCommission(t, "15")

	price := money.FromRubles(100)
	commission := money.FromRubles(15)
	reward := price.Sub(commission)

	if got := accounts.balances[repository.AccountCommission]; got != commission {
		t.Errorf("commission account = %s, expected %s", got, commission)
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != reward {
		t.Errorf("executor was rewarded %s, expected %s", got, reward)
	}
	// The customer paid the full price either way: the commission comes out of
	// the executor's reward, not out of a second charge.
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("confirming changed the books total: %s, expected %s", got, opening)
	}
}

// With no rate configured nothing is taken, so deploying the feature does not
// quietly start shrinking payouts.
func TestConfirmOrderTakesNoCommissionWhenRateIsUnset(t *testing.T) {
	txRepo, accounts, executorID, _ := confirmWithCommission(t, "")

	if got := accounts.balances[repository.AccountCommission]; !got.IsZero() {
		t.Errorf("commission account = %s, expected nothing collected", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != money.FromRubles(100) {
		t.Errorf("executor was rewarded %s, expected the whole payment", got)
	}
}

// A rate above 100% would otherwise pay a negative reward and take money escrow
// is not holding. It is clamped, so the worst case is a zero reward.
func TestCommissionNeverExceedsThePayment(t *testing.T) {
	txRepo, accounts, executorID, opening := confirmWithCommission(t, "150")

	if got := accounts.balances[repository.AccountCommission]; got != money.FromRubles(100) {
		t.Errorf("commission account = %s, expected the whole payment", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); !got.IsZero() {
		t.Errorf("executor was rewarded %s, expected nothing", got)
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("confirming changed the books total: %s, expected %s", got, opening)
	}
}

// A fractional share still lands on whole kopecks, and the rounding remainder
// stays with the executor rather than disappearing.
func TestCommissionRoundingLeavesNothingBehind(t *testing.T) {
	txRepo, accounts, executorID, _ := confirmWithCommission(t, "3.333")

	price := money.FromRubles(100)
	commission := accounts.balances[repository.AccountCommission]
	reward := txRepo.balances[executorID].Sub(mockDefaultBalance)

	if commission.Add(reward) != price {
		t.Errorf("commission %s + reward %s does not add up to %s", commission, reward, price)
	}
	if commission != money.FromKopecks(333) {
		t.Errorf("commission = %s, expected 3.33", commission)
	}
}

// newCommissionAdmin wires an admin service over books that already hold some
// collected commission.
func newCommissionAdmin(collected money.Amount) (*AdminService, *mockTransactionRepo, *mockAccounts) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	// Money collected as commission came in from outside at some point, which is
	// what keeps the opening books closed.
	accounts.balances[repository.AccountCommission] = collected
	accounts.balances[repository.AccountDeposits] = collected.Neg()

	srv := NewAdminService(newMockUserRepo(), &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)},
		&mockSettingsRepo{settings: map[string]string{SettingOrderCommissionPercent: "15"}}, "secret", nil).
		WithLedger(NewLedger(txRepo, accounts))
	return srv, txRepo, accounts
}

func TestPayoutCommissionDrainsTheAccountAndKeepsBooksClosed(t *testing.T) {
	collected := money.FromRubles(500)
	srv, txRepo, accounts := newCommissionAdmin(collected)
	opening := booksTotal(txRepo, accounts)

	result, err := srv.PayoutCommission(context.Background(), uuid.New(), money.FromRubles(200))
	if err != nil {
		t.Fatalf("payout: %v", err)
	}
	if want := money.FromRubles(300); result.Balance != want {
		t.Errorf("commission balance after payout = %s, expected %s", result.Balance, want)
	}
	if result.Percent != 15 {
		t.Errorf("reported rate = %v, expected 15", result.Percent)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("paying out changed the books total: %s, expected %s", got, opening)
	}
}

// The account may not be overdrawn: a payout is bounded by what was actually
// collected, checked in the same guarded statement that moves the money.
func TestPayoutCommissionRefusesMoreThanCollected(t *testing.T) {
	collected := money.FromRubles(500)
	srv, _, accounts := newCommissionAdmin(collected)

	if _, err := srv.PayoutCommission(context.Background(), uuid.New(), money.FromRubles(501)); err == nil {
		t.Fatal("expected a payout larger than the balance to be refused")
	}
	if got := accounts.balances[repository.AccountCommission]; got != collected {
		t.Errorf("refused payout still moved money: account = %s, expected %s", got, collected)
	}

	if _, err := srv.PayoutCommission(context.Background(), uuid.New(), money.Zero); err == nil {
		t.Error("expected a zero payout to be refused")
	}
}

// The settings screen is the only place the rate is set, so the bounds have to
// hold there: a share over 100% would pay a negative reward.
func TestUpdateSettingsBoundsTheCommissionRate(t *testing.T) {
	settings := &mockSettingsRepo{settings: map[string]string{}}
	srv := NewAdminService(newMockUserRepo(), &mockAdminRepo{}, settings, "secret", nil)
	ctx := context.Background()

	for _, bad := range []string{"-1", "101", "abc"} {
		if err := srv.UpdateSettings(ctx, map[string]string{SettingOrderCommissionPercent: bad}); err == nil {
			t.Errorf("expected rate %q to be rejected", bad)
		}
	}
	if err := srv.UpdateSettings(ctx, map[string]string{SettingOrderCommissionPercent: "12.5"}); err != nil {
		t.Errorf("expected rate 12.5 to be accepted, got %v", err)
	}
	if got := settings.settings[SettingOrderCommissionPercent]; got != "12.5" {
		t.Errorf("stored rate = %q, expected 12.5", got)
	}
}

// Guard against the payout path being wired to the unguarded Settle by mistake:
// the error a caller sees for an overdraw has to be the insufficient-funds one,
// because that is what the handler turns into a 400 rather than a 500.
func TestPayoutCommissionOverdrawReportsInsufficientFunds(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.Payout(context.Background(), tx, repository.AccountCommission, uuid.New(),
			money.FromRubles(1), repository.TransactionTypeCommissionPayout, nil)
	})
	if !errors.Is(err, repository.ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}
