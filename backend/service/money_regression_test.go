package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// newMoneyTestService wires an OrderService with in-memory repositories and a
// balance-tracking transaction repository.
func newMoneyTestService() (*OrderService, *mockOrderRepo, *mockTransactionRepo) {
	orderRepo := &mockOrderRepo{}
	txRepo := &mockTransactionRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	srv := NewOrderService(orderRepo, NewLedger(txRepo, newMockAccounts()), settings, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	return srv, orderRepo, txRepo
}

// TestCancelAssignedOrderRefundsOnce covers the duplicate-refund bug: cancelling
// an order that an executor had already accepted refunded the hold on every
// call, because the guarded UPDATE matched no rows but the refund still ran.
func TestCancelAssignedOrderRefundsOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}

	afterHold, _ := txRepo.GetBalance(customerID)
	if afterHold != mockDefaultBalance-order.HoldAmount {
		t.Fatalf("expected hold of %s, balance is %s", order.HoldAmount, afterHold)
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
		t.Errorf("expected balance restored to %s exactly once, got %s", mockDefaultBalance, final)
	}
}

// TestConfirmOrderPaysExecutorOnce ensures a second confirmation is refused
// instead of rewarding the executor twice.
func TestConfirmOrderPaysExecutorOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	executorID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
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
		t.Errorf("expected executor to be paid once (%s), got %s", expected, executorBalance)
	}
}

// TestCreateOrderRejectsOverdraft checks that the balance is debited atomically,
// so a check-then-write race cannot spend money the customer does not have.
func TestCreateOrderRejectsOverdraft(t *testing.T) {
	srv, _, txRepo := newMoneyTestService()

	customerID := uuid.New()
	txRepo.balances = map[uuid.UUID]money.Amount{customerID: money.FromRubles(50.0)} // variant costs 100

	lat, lon := 55.75, 37.61
	if _, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon); err == nil {
		t.Fatal("expected order creation to fail on insufficient balance")
	}

	// Whether the order row survives is a property of the database transaction
	// the two statements share, which mocks do not model; that is asserted
	// against a real Postgres in TestCreateOrderRollsBackOnInsufficientFunds.
	balance, _ := txRepo.GetBalance(customerID)
	if balance != money.FromRubles(50) {
		t.Errorf("balance must be untouched, got %s", balance)
	}
}

// TestAcceptRejectsIneligibleExecutor verifies that the age and verification
// rules are enforced when an order is taken, not only when it is listed.
func TestAcceptRejectsIneligibleExecutor(t *testing.T) {
	minor := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "ACTIVE", Verified: true}
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

	verified := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "ACTIVE", Verified: true, BirthDate: &adult}
	if err := canExecutorTakeOrder(verified, restricted); err != nil {
		t.Errorf("expected a verified adult to be accepted, got %v", err)
	}

	banned := &repository.User{ID: uuid.New(), Role: "EXECUTOR", Status: "BANNED", Verified: true, BirthDate: &adult}
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
	srv := NewShiftService(shiftRepo, nil, NewLedger(txRepo, newMockAccounts()), settings, &mockOrderRepo{}, nil, nil)

	executorID := uuid.New()
	if _, err := shiftRepo.StartShift(executorID, 3); err != nil {
		t.Fatalf("failed to start shift: %v", err)
	}

	if err := srv.End(executorID); err != nil {
		t.Fatalf("ending the shift should succeed: %v", err)
	}

	fined := money.Zero
	for _, tx := range txRepo.txs {
		if tx.Type == string(repository.TransactionTypeFine) {
			fined = fined.Add(tx.Amount)
		}
	}
	if fined != money.FromRubles(50) {
		t.Errorf("expected a 50 penalty for leaving a 3h shift early, got %s", fined)
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
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, NewLedger(txRepo, newMockAccounts()), newMockUserRepo(), newMockCatalogRepo(), nil)

	customerID := uuid.New()
	executorID := uuid.New()
	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)

	bid, err := bidRepo.CreateBid(order.ID, executorID, money.FromRubles(350))
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
	if balance, _ := txRepo.GetBalance(customerID); balance != mockDefaultBalance.Sub(money.FromRubles(350)) {
		t.Errorf("expected the offer to be held, balance is %s", balance)
	}
	if order.HoldAmount != money.FromRubles(350) || order.Status != repository.OrderStatusAssigned {
		t.Errorf("expected the order assigned at 350.00, got %s / %s", order.Status, order.HoldAmount)
	}

	// A second accept must not double-charge.
	if err := srv.AcceptBid(bid.ID, customerID); err == nil {
		t.Error("expected a repeated accept to be refused")
	}
	if balance, _ := txRepo.GetBalance(customerID); balance != mockDefaultBalance.Sub(money.FromRubles(350)) {
		t.Errorf("balance must be charged once, got %s", balance)
	}
}

// TestAcceptBidRejectsForeignCustomer keeps ownership enforcement in place after
// the move out of the repository.
func TestAcceptBidRejectsForeignCustomer(t *testing.T) {
	bidRepo := &mockBidRepo{}
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, testLedger(), newMockUserRepo(), newMockCatalogRepo(), nil)

	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       uuid.New(),
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)
	bid, _ := bidRepo.CreateBid(order.ID, uuid.New(), money.FromRubles(100))

	if err := srv.AcceptBid(bid.ID, uuid.New()); err == nil {
		t.Error("expected a stranger to be refused")
	}
}

// newWithdrawalTestService wires AdminService with a balance-tracking ledger.
func newWithdrawalTestService() (*AdminService, *mockAdminRepo, *mockRepo, *mockTransactionRepo) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{
		requests:    make(map[uuid.UUID]*repository.TopUpRequest),
		withdrawals: make(map[uuid.UUID]*repository.WithdrawalRequest),
	}
	txRepo := &mockTransactionRepo{}
	svc := NewAdminService(userRepo, adminRepo, &mockSettingsRepo{settings: map[string]string{}}, "secret", nil).
		WithLedger(NewLedger(txRepo, newMockAccounts()))
	return svc, adminRepo, userRepo, txRepo
}

// TestWithdrawalReservesFunds covers M-06: a request used to only look at the
// balance and leave the money spendable.
func TestWithdrawalReservesFunds(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000010", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	if _, err := svc.CreateWithdrawalRequest(user.ID, money.FromRubles(400)); err != nil {
		t.Fatalf("unexpected error requesting withdrawal: %v", err)
	}

	balance, _ := txRepo.GetBalance(user.ID)
	if balance != mockDefaultBalance.Sub(money.FromRubles(400)) {
		t.Errorf("expected the money to be reserved at request time, balance is %s", balance)
	}

	held := money.Zero
	for _, tx := range txRepo.txs {
		if tx.Type == string(repository.TransactionTypeWithdrawalHold) {
			held = held.Add(tx.Amount)
		}
	}
	if held != money.FromRubles(400) {
		t.Errorf("expected a WITHDRAWAL_HOLD entry of 400.00, got %s", held)
	}
}

// TestWithdrawalCannotExceedBalance checks the guarded debit rather than a
// check-then-write on a stale read.
func TestWithdrawalCannotExceedBalance(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000011", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user
	txRepo.balances = map[uuid.UUID]money.Amount{user.ID: money.FromRubles(100)}

	if _, err := svc.CreateWithdrawalRequest(user.ID, money.FromRubles(500)); err == nil {
		t.Error("expected a request larger than the balance to be refused")
	}
	if balance, _ := txRepo.GetBalance(user.ID); balance != money.FromRubles(100) {
		t.Errorf("a refused request must not touch the balance, got %s", balance)
	}
}

// TestRejectedWithdrawalReturnsTheMoney makes sure a refusal is not a quiet
// confiscation now that funds leave the balance up front.
func TestRejectedWithdrawalReturnsTheMoney(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000012", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	req, err := svc.CreateWithdrawalRequest(user.ID, money.FromRubles(250))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.RejectWithdrawalRequest(req.ID, uuid.New()); err != nil {
		t.Fatalf("unexpected error rejecting: %v", err)
	}
	if balance, _ := txRepo.GetBalance(user.ID); balance != mockDefaultBalance {
		t.Errorf("rejecting must return the reserved money, balance is %s", balance)
	}

	// A second decision on the same request must not double-refund.
	if err := svc.RejectWithdrawalRequest(req.ID, uuid.New()); err == nil {
		t.Error("expected a second decision to be refused")
	}
	if balance, _ := txRepo.GetBalance(user.ID); balance != mockDefaultBalance {
		t.Errorf("balance must be restored once, got %s", balance)
	}
}

// TestApprovedWithdrawalDoesNotDebitTwice: the money already left the balance
// when the request was created, so approval must move nothing.
func TestApprovedWithdrawalDoesNotDebitTwice(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000013", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	req, err := svc.CreateWithdrawalRequest(user.ID, money.FromRubles(300))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.ApproveWithdrawalRequest(req.ID, uuid.New()); err != nil {
		t.Fatalf("unexpected error approving: %v", err)
	}

	if balance, _ := txRepo.GetBalance(user.ID); balance != mockDefaultBalance.Sub(money.FromRubles(300)) {
		t.Errorf("approval must not debit again, balance is %s", balance)
	}
	if err := svc.ApproveWithdrawalRequest(req.ID, uuid.New()); err == nil {
		t.Error("expected a repeated approval to be refused")
	}
}

// TestMoneyIsNeverCreatedOrDestroyed is the point of system accounts: every
// movement touches two sides, so the sum of user balances and platform accounts
// stays where it started. Before the ledger existed a fine simply left the
// executor's balance and stopped existing.
func TestMoneyIsNeverCreatedOrDestroyed(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	orderRepo := &mockOrderRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	orders := NewOrderService(orderRepo, ledger, settings, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	customerID := uuid.New()
	executorID := uuid.New()

	total := func() money.Amount {
		sum := money.Zero
		for _, b := range txRepo.balances {
			sum = sum.Add(b)
		}
		for _, b := range accounts.balances {
			sum = sum.Add(b)
		}
		return sum
	}

	// Give both participants a starting balance the way the world does.
	customerStart, _ := txRepo.GetBalance(customerID)
	executorStart, _ := txRepo.GetBalance(executorID)
	opening := customerStart.Add(executorStart)
	if total() != opening {
		t.Fatalf("fixture is not balanced: %s vs %s", total(), opening)
	}

	lat, lon := 55.75, 37.61
	order, err := orders.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if accounts.balances[repository.AccountEscrow] != order.HoldAmount {
		t.Errorf("escrow should hold %s, holds %s", order.HoldAmount, accounts.balances[repository.AccountEscrow])
	}
	if got := total(); got != opening {
		t.Errorf("holding money changed the total: %s, expected %s", got, opening)
	}

	// Run the order to completion: escrow drains into the executor.
	if err := orderRepo.AssignOrder(order.ID, executorID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := orders.ExecuteOrder(order.ID, executorID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := orders.Confirm(customerID, order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !accounts.balances[repository.AccountEscrow].IsZero() {
		t.Errorf("escrow must drain on completion, holds %s", accounts.balances[repository.AccountEscrow])
	}
	if got := total(); got != opening {
		t.Errorf("completing the order changed the total: %s, expected %s", got, opening)
	}

	// A fine is collected, not destroyed.
	second, err := orders.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 2", &lat, &lon)
	if err != nil {
		t.Fatalf("create second order: %v", err)
	}
	if err := orderRepo.AssignOrder(second.ID, executorID); err != nil {
		t.Fatalf("assign second: %v", err)
	}
	if err := orders.RejectAssignedOrder(second.ID, executorID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if !accounts.balances[repository.AccountFines].IsPositive() {
		t.Error("the penalty should have landed on the fines account")
	}
	if got := total(); got != opening {
		t.Errorf("fining an executor changed the total: %s, expected %s", got, opening)
	}
}
