package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// newMoneyTestService wires an OrderService with in-memory repositories and a
// balance-tracking transaction repository.
func newMoneyTestService() (*OrderService, *mockOrderRepo, *mockTransactionRepo) {
	orderRepo := &mockOrderRepo{}
	txRepo := &mockTransactionRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	srv := NewOrderService(orderRepo, txRepo, settings, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	return srv, orderRepo, txRepo
}

// TestCancelAssignedOrderRefundsOnce covers the duplicate-refund bug: cancelling
// an order that an executor had already accepted refunded the hold on every
// call, because the guarded UPDATE matched no rows but the refund still ran.
func TestCancelAssignedOrderRefundsOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}

	afterHold, _ := txRepo.GetBalance(customerID)
	if afterHold != mockDefaultBalance-order.HoldAmount {
		t.Fatalf("expected hold of %.2f, balance is %.2f", order.HoldAmount, afterHold)
	}

	// An executor takes the order.
	if err := orderRepo.AssignOrder(order.ID, uuid.New()); err != nil {
		t.Fatalf("failed to assign order: %v", err)
	}

	if err := srv.Cancel(customerID, order.ID); err != nil {
		t.Fatalf("first cancel should succeed: %v", err)
	}

	// Every further cancel must be refused, not silently refunded again.
	for i := 0; i < 3; i++ {
		if err := srv.Cancel(customerID, order.ID); err == nil {
			t.Fatalf("repeat cancel #%d was accepted", i+1)
		}
	}

	final, _ := txRepo.GetBalance(customerID)
	if final != mockDefaultBalance {
		t.Errorf("expected balance restored to %.2f exactly once, got %.2f", mockDefaultBalance, final)
	}
}

// TestConfirmOrderPaysExecutorOnce ensures a second confirmation is refused
// instead of rewarding the executor twice.
func TestConfirmOrderPaysExecutorOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	executorID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}

	// Capture the hold before confirmation: confirming zeroes it.
	holdAmount := order.HoldAmount

	if err := orderRepo.AssignOrder(order.ID, executorID); err != nil {
		t.Fatalf("failed to assign order: %v", err)
	}
	if err := srv.ExecuteOrder(order.ID, executorID); err != nil {
		t.Fatalf("failed to mark order executed: %v", err)
	}

	if err := srv.Confirm(customerID, order.ID); err != nil {
		t.Fatalf("first confirm should succeed: %v", err)
	}
	if err := srv.Confirm(customerID, order.ID); err == nil {
		t.Fatal("second confirm was accepted")
	}

	executorBalance, _ := txRepo.GetBalance(executorID)
	expected := mockDefaultBalance + holdAmount
	if executorBalance != expected {
		t.Errorf("expected executor to be paid once (%.2f), got %.2f", expected, executorBalance)
	}
}

// TestCreateOrderRejectsOverdraft checks that the balance is debited atomically
// and that a rejected order is not persisted.
func TestCreateOrderRejectsOverdraft(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	txRepo.balances = map[uuid.UUID]float64{customerID: 50.0} // variant costs 100

	lat, lon := 55.75, 37.61
	if _, err := srv.CreateOrder(customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon); err == nil {
		t.Fatal("expected order creation to fail on insufficient balance")
	}

	if len(orderRepo.orders) != 0 {
		t.Errorf("no order may be persisted when the hold fails, got %d", len(orderRepo.orders))
	}
	balance, _ := txRepo.GetBalance(customerID)
	if balance != 50.0 {
		t.Errorf("balance must be untouched, got %.2f", balance)
	}
}

// TestAcceptRejectsIneligibleExecutor verifies that the age and verification
// rules are enforced when an order is taken, not only when it is listed.
func TestAcceptRejectsIneligibleExecutor(t *testing.T) {
	minor := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "ACTIVE", EmailVerified: true}
	birth := time.Now().AddDate(-16, 0, 0)
	minor.BirthDate = &birth

	restricted := &repository.ServiceNode{MinAge: 18, RequiresVerification: true}
	if err := canExecutorTakeOrder(minor, restricted); err == nil {
		t.Error("expected an underage executor to be rejected")
	}

	unverified := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "ACTIVE"}
	adult := time.Now().AddDate(-30, 0, 0)
	unverified.BirthDate = &adult
	if err := canExecutorTakeOrder(unverified, restricted); err == nil {
		t.Error("expected an unverified executor to be rejected")
	}

	verified := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "ACTIVE", EmailVerified: true, BirthDate: &adult}
	if err := canExecutorTakeOrder(verified, restricted); err != nil {
		t.Errorf("expected a verified adult to be accepted, got %v", err)
	}

	banned := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "BANNED", EmailVerified: true, BirthDate: &adult}
	if err := canExecutorTakeOrder(banned, nil); err == nil {
		t.Error("expected a banned executor to be rejected")
	}
}

// TestEndShiftEarlyChargesPenalty covers the bypass where calling /shifts/end
// instead of /shifts/early-end skipped the penalty entirely.
func TestEndShiftEarlyChargesPenalty(t *testing.T) {
	shiftRepo := &mockShiftRepo{}
	txRepo := &mockShiftTransactionRepo{}
	settings := &mockSettingsRepo{settings: map[string]string{"shift_early_exit_penalty": "50"}}
	srv := NewShiftService(shiftRepo, nil, txRepo, settings, &mockOrderRepo{}, nil, nil)

	executorID := uuid.New()
	if _, err := shiftRepo.StartShift(executorID, 3); err != nil {
		t.Fatalf("failed to start shift: %v", err)
	}

	if err := srv.End(executorID); err != nil {
		t.Fatalf("ending the shift should succeed: %v", err)
	}

	var fined float64
	for _, tx := range txRepo.txs {
		if tx.Type == string(repository.TransactionTypeFine) {
			fined += tx.Amount
		}
	}
	if fined != 50 {
		t.Errorf("expected a 50 penalty for leaving a 3h shift early, got %.2f", fined)
	}
}

// TestAcceptBidChecksExecutorAtAcceptTime covers the rules that were missing
// while the whole accept flow lived in the repository: an executor had to be
// eligible when the bid was placed, but nothing was re-checked when the
// customer accepted it.
func TestAcceptBidChecksExecutorAtAcceptTime(t *testing.T) {
	bidRepo := &mockBidRepo{}
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	txRepo := &mockTransactionRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, txRepo, newMockUserRepo(), newMockCatalogRepo(), nil)

	customerID := uuid.New()
	executorID := uuid.New()
	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)

	bid, err := bidRepo.CreateBid(order.ID, executorID, 350.0)
	if err != nil {
		t.Fatalf("failed to seed bid: %v", err)
	}

	// The executor placed the bid but is no longer on shift.
	if err := srv.AcceptBid(bid.ID, customerID); err == nil {
		t.Error("expected accept to fail while the executor has no active shift")
	}
	if bid.Status != "PENDING" {
		t.Errorf("bid must stay pending after a failed accept, got %s", bid.Status)
	}

	// Back on shift: the bid can be accepted, and the hold is taken.
	shiftRepo.shifts = append(shiftRepo.shifts, &repository.Shift{
		ID:           uuid.New(),
		ExecutorID:   executorID,
		Status:       repository.ShiftStatusActive,
		PlannedEndAt: time.Now().Add(time.Hour),
	})
	if err := srv.AcceptBid(bid.ID, customerID); err != nil {
		t.Fatalf("expected accept to succeed: %v", err)
	}
	if balance, _ := txRepo.GetBalance(customerID); balance != mockDefaultBalance-350.0 {
		t.Errorf("expected the offer to be held, balance is %.2f", balance)
	}
	if order.HoldAmount != 350.0 || order.Status != repository.OrderStatusAssigned {
		t.Errorf("expected the order assigned at 350.00, got %s / %.2f", order.Status, order.HoldAmount)
	}

	// A second accept must not double-charge.
	if err := srv.AcceptBid(bid.ID, customerID); err == nil {
		t.Error("expected a repeated accept to be refused")
	}
	if balance, _ := txRepo.GetBalance(customerID); balance != mockDefaultBalance-350.0 {
		t.Errorf("balance must be charged once, got %.2f", balance)
	}
}

// TestAcceptBidRejectsForeignCustomer keeps ownership enforcement in place after
// the move out of the repository.
func TestAcceptBidRejectsForeignCustomer(t *testing.T) {
	bidRepo := &mockBidRepo{}
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, &mockTransactionRepo{}, newMockUserRepo(), newMockCatalogRepo(), nil)

	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       uuid.New(),
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)
	bid, _ := bidRepo.CreateBid(order.ID, uuid.New(), 100.0)

	if err := srv.AcceptBid(bid.ID, uuid.New()); err == nil {
		t.Error("expected a stranger to be refused")
	}
}
