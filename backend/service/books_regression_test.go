package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// booksTotal is the invariant the whole ledger exists to hold: every user
// balance plus every system account balance sums to zero, because no movement
// touches only one side. ReconciliationRepository checks the same thing against
// the real database nightly.
func booksTotal(txRepo *mockTransactionRepo, accounts *mockAccounts) money.Amount {
	sum := money.Zero
	for _, b := range txRepo.balances {
		sum = sum.Add(b)
	}
	for _, b := range accounts.balances {
		sum = sum.Add(b)
	}
	return sum
}

// An admin crediting a user used to run raw SQL: it added to the balance and
// wrote a TOP_UP row, but debited no system account. The user's own history
// still added up, so per-user reconciliation kept passing while the platform
// books drifted a little further open with every top-up — which is exactly the
// shape the production report showed: zero balance mismatches, books open.
func TestAdminTopUpKeepsBooksClosed(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	srv := NewAdminService(newMockUserRepo(), adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(txRepo, accounts))

	userID := uuid.New()
	// Touch the balance so the opening total includes this user.
	if _, err := txRepo.GetBalance(context.Background(), userID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	amount := money.FromRubles(500)
	if err := srv.TopUpUserBalance(context.Background(), userID, uuid.New(), amount); err != nil {
		t.Fatalf("top up: %v", err)
	}

	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("topping up changed the books total: %s, expected %s", got, opening)
	}
	// Money entering from outside is recorded as a claim on the deposits
	// account, not conjured onto the balance.
	if got := accounts.balances[repository.AccountDeposits]; got != amount.Neg() {
		t.Errorf("deposits account = %s, expected %s", got, amount.Neg())
	}
}

// The path the auction worker now delegates to. It used to cancel expired
// auctions with its own SQL: credit the customer, write a REFUND row, never
// debit escrow, never zero hold_amount. Every cancelled auction then looked
// like it still held money — the "finished order still holds money" anomalies —
// while escrow kept a balance no order claimed.
func TestCancellingSearchingOrderDrainsEscrowAndHold(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	orderRepo := &mockOrderRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	orders := NewOrderService(orderRepo, NewLedger(txRepo, accounts), settings,
		newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	customerID := uuid.New()
	if _, err := txRepo.GetBalance(context.Background(), customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	lat, lon := 55.75, 37.61
	order, err := orders.CreateOrder(context.Background(), customerID, standardVariantID, false, false,
		"Россия, Москва, Тверская улица, д. 3", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if accounts.balances[repository.AccountEscrow] != order.HoldAmount {
		t.Fatalf("escrow should hold %s, holds %s", order.HoldAmount, accounts.balances[repository.AccountEscrow])
	}

	if err := orders.CancelOrder(context.Background(), order.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow still holds %s after cancelling", got)
	}
	cancelled, err := orderRepo.GetOrderByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if !cancelled.HoldAmount.IsZero() {
		t.Errorf("cancelled order still holds %s — this is the reconciliation anomaly", cancelled.HoldAmount)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("cancelling changed the books total: %s, expected %s", got, opening)
	}
}

// The shape of the SLA downgrade refund: part of a hold goes back to the
// customer while the rest stays held. It must come out of escrow, not appear
// on the balance from nowhere.
func TestPartialRefundOutOfEscrowKeepsBooksClosed(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	customerID := uuid.New()
	if _, err := txRepo.GetBalance(context.Background(), customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	hold := money.FromRubles(300)
	refund := money.FromRubles(120)
	orderID := uuid.New()

	if err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.Reserve(context.Background(), tx, customerID, repository.AccountEscrow, hold, repository.TransactionTypeHold, &orderID)
	}); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.Release(context.Background(), tx, repository.AccountEscrow, customerID, refund, repository.TransactionTypeRefund, &orderID, nil)
	}); err != nil {
		t.Fatalf("refund: %v", err)
	}

	if got := accounts.balances[repository.AccountEscrow]; got != hold.Sub(refund) {
		t.Errorf("escrow holds %s, expected %s", got, hold.Sub(refund))
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("partial refund changed the books total: %s, expected %s", got, opening)
	}
}

// An auction holds no money until a bid is accepted — the request is published
// without a price, and accepting a bid is what moves the money into escrow and
// the order into ASSIGNED. The seven-day sweep therefore cancels orders that
// hold nothing, and it must keep its hands off one that was claimed between the
// scan and the cancel: that order belongs to the executor who won it.
func TestExpiredAuctionSweepWillNotCancelAClaimedOrder(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	orderRepo := &mockOrderRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	orders := NewOrderService(orderRepo, NewLedger(txRepo, accounts), settings,
		newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	customerID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := orders.CreateOrder(context.Background(), customerID, standardVariantID, false, false,
		"Россия, Москва, Тверская улица, д. 4", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// A bid is accepted just as the sweep is about to reach this order.
	if err := orderRepo.AssignOrder(context.Background(), order.ID, uuid.New()); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := orders.CancelUnclaimedAuction(context.Background(), order.ID); err == nil {
		t.Fatal("the sweep cancelled an order that had already been claimed")
	}

	claimed, err := orderRepo.GetOrderByID(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if claimed.Status != repository.OrderStatusAssigned {
		t.Errorf("order status is %s, expected it to stay ASSIGNED", claimed.Status)
	}
	if claimed.HoldAmount != order.HoldAmount {
		t.Errorf("hold changed to %s, expected it to stay %s", claimed.HoldAmount, order.HoldAmount)
	}
	// A customer cancelling the same order is still allowed: only the sweep is
	// restricted.
	if err := orders.CancelOrder(context.Background(), order.ID); err != nil {
		t.Errorf("an ordinary cancel must still work: %v", err)
	}
}
