package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// newMoneyTestService собирает OrderService с репозиториями в памяти и
// репозиторием транзакций, отслеживающим баланс.
func newMoneyTestService() (*OrderService, *mockOrderRepo, *mockTransactionRepo) {
	orderRepo := &mockOrderRepo{}
	txRepo := &mockTransactionRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{}}
	srv := NewOrderService(orderRepo, NewLedger(txRepo, newMockAccounts()), settings, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	return srv, orderRepo, txRepo
}

// TestCancelAssignedOrderRefundsOnce покрывает баг двойного возврата: отмена
// заказа, который исполнитель уже принял, возвращала удержание на каждом
// вызове, потому что охраняемый UPDATE не задевал строк, а возврат всё равно шёл.
func TestCancelAssignedOrderRefundsOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}

	afterHold, _ := txRepo.GetBalance(context.Background(), customerID)
	if afterHold != mockDefaultBalance-order.HoldAmount {
		t.Fatalf("expected hold of %s, balance is %s", order.HoldAmount, afterHold)
	}

	// Исполнитель берёт заказ.
	if err := orderRepo.AssignOrder(context.Background(), order.ID, uuid.New()); err != nil {
		t.Fatalf("failed to assign order: %v", err)
	}

	if err := srv.Cancel(context.Background(), customerID, order.ID); err != nil {
		t.Fatalf("first cancel should succeed: %v", err)
	}

	// Любая дальнейшая отмена должна отклоняться, а не молча возвращать снова.
	for i := 0; i < 3; i++ {
		if err := srv.Cancel(context.Background(), customerID, order.ID); err == nil {
			t.Fatalf("repeat cancel #%d was accepted", i+1)
		}
	}

	final, _ := txRepo.GetBalance(context.Background(), customerID)
	if final != mockDefaultBalance {
		t.Errorf("expected balance restored to %s exactly once, got %s", mockDefaultBalance, final)
	}
}

// TestConfirmOrderPaysExecutorOnce проверяет, что второе подтверждение
// отклоняется, а не награждает исполнителя дважды.
func TestConfirmOrderPaysExecutorOnce(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	executorID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}

	// Запоминаем удержание до подтверждения: подтверждение его обнуляет.
	holdAmount := order.HoldAmount

	if err := orderRepo.AssignOrder(context.Background(), order.ID, executorID); err != nil {
		t.Fatalf("failed to assign order: %v", err)
	}
	if err := srv.ExecuteOrder(context.Background(), order.ID, executorID); err != nil {
		t.Fatalf("failed to mark order executed: %v", err)
	}

	if err := srv.Confirm(context.Background(), customerID, order.ID); err != nil {
		t.Fatalf("first confirm should succeed: %v", err)
	}
	if err := srv.Confirm(context.Background(), customerID, order.ID); err == nil {
		t.Fatal("second confirm was accepted")
	}

	executorBalance, _ := txRepo.GetBalance(context.Background(), executorID)
	expected := mockDefaultBalance + holdAmount
	if executorBalance != expected {
		t.Errorf("expected executor to be paid once (%s), got %s", expected, executorBalance)
	}
}

// TestConfirmAssignedOrderPaysExecutor покрывает раннее одобрение: заказчик
// может подтвердить, пока заказ ещё ASSIGNED (исполнитель не нажимал
// «Исполнил»), и это обязано выплатить исполнителю удержанную сумму ровно так
// же, как это делает путь EXECUTED, — не больше и не меньше.
func TestConfirmAssignedOrderPaysExecutor(t *testing.T) {
	srv, orderRepo, txRepo := newMoneyTestService()

	customerID := uuid.New()
	executorID := uuid.New()
	lat, lon := 55.75, 37.61
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}
	holdAmount := order.HoldAmount

	if err := orderRepo.AssignOrder(context.Background(), order.ID, executorID); err != nil {
		t.Fatalf("failed to assign order: %v", err)
	}

	// Намеренно пропускаем ExecuteOrder: подтверждаем прямо из ASSIGNED.
	if err := srv.Confirm(context.Background(), customerID, order.ID); err != nil {
		t.Fatalf("early confirm from ASSIGNED should succeed: %v", err)
	}
	// Второе подтверждение всё равно обязано отклоняться (заказ уже COMPLETED).
	if err := srv.Confirm(context.Background(), customerID, order.ID); err == nil {
		t.Fatal("second confirm was accepted")
	}

	executorBalance, _ := txRepo.GetBalance(context.Background(), executorID)
	expected := mockDefaultBalance + holdAmount
	if executorBalance != expected {
		t.Errorf("expected executor to be paid the held amount once (%s), got %s", expected, executorBalance)
	}
}

// TestCreateOrderRejectsOverdraft проверяет, что баланс списывается атомарно,
// поэтому гонка «проверил-записал» не потратит деньги, которых у заказчика нет.
func TestCreateOrderRejectsOverdraft(t *testing.T) {
	srv, _, txRepo := newMoneyTestService()

	customerID := uuid.New()
	txRepo.balances = map[uuid.UUID]money.Amount{customerID: money.FromRubles(50.0)} // вариант стоит 100

	lat, lon := 55.75, 37.61
	if _, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon); err == nil {
		t.Fatal("expected order creation to fail on insufficient balance")
	}

	// Выживет ли строка заказа — свойство транзакции базы, которую делят два
	// оператора, а моки его не моделируют; это проверяется на настоящем Postgres
	// в TestCreateOrderRollsBackOnInsufficientFunds.
	balance, _ := txRepo.GetBalance(context.Background(), customerID)
	if balance != money.FromRubles(50) {
		t.Errorf("balance must be untouched, got %s", balance)
	}
}

// TestAcceptRejectsIneligibleExecutor проверяет, что правила возраста и
// верификации применяются при взятии заказа, а не только при показе в списке.
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

// TestEndShiftEarlyChargesPenalty покрывает обход, при котором вызов
// /shifts/end вместо /shifts/early-end полностью пропускал штраф.
func TestEndShiftEarlyChargesPenalty(t *testing.T) {
	shiftRepo := &mockShiftRepo{}
	txRepo := &mockShiftTransactionRepo{}
	settings := &mockSettingsRepo{settings: map[string]string{"shift_early_exit_penalty": "50"}}
	srv := NewShiftService(shiftRepo, NewLedger(txRepo, newMockAccounts()), settings, &mockOrderRepo{}, nil, nil)

	executorID := uuid.New()
	if _, err := shiftRepo.StartShift(context.Background(), executorID, 3); err != nil {
		t.Fatalf("failed to start shift: %v", err)
	}

	if err := srv.End(context.Background(), executorID); err != nil {
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

// TestAcceptBidChecksExecutorAtAcceptTime покрывает правила, которых не хватало,
// пока весь поток принятия жил в репозитории: исполнитель должен был быть
// допущен в момент подачи ставки, но ничего не перепроверялось, когда заказчик
// её принимал.
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

	bid, err := bidRepo.CreateBid(context.Background(), order.ID, executorID, money.FromRubles(350))
	if err != nil {
		t.Fatalf("failed to seed bid: %v", err)
	}

	// Исполнитель подал ставку, но больше не на смене.
	if err := srv.AcceptBid(context.Background(), bid.ID, customerID); err == nil {
		t.Error("expected accept to fail while the executor has no active shift")
	}
	if bid.Status != "PENDING" {
		t.Errorf("bid must stay pending after a failed accept, got %s", bid.Status)
	}

	// Снова на смене: ставку можно принять, и удержание берётся.
	shiftRepo.shifts = append(shiftRepo.shifts, &repository.Shift{
		ID:           uuid.New(),
		ExecutorID:   executorID,
		Status:       repository.ShiftStatusActive,
		PlannedEndAt: time.Now().Add(time.Hour),
	})
	if err := srv.AcceptBid(context.Background(), bid.ID, customerID); err != nil {
		t.Fatalf("expected accept to succeed: %v", err)
	}
	if balance, _ := txRepo.GetBalance(context.Background(), customerID); balance != mockDefaultBalance.Sub(money.FromRubles(350)) {
		t.Errorf("expected the offer to be held, balance is %s", balance)
	}
	if order.HoldAmount != money.FromRubles(350) || order.Status != repository.OrderStatusAssigned {
		t.Errorf("expected the order assigned at 350.00, got %s / %s", order.Status, order.HoldAmount)
	}

	// Второе принятие не должно списать дважды.
	if err := srv.AcceptBid(context.Background(), bid.ID, customerID); err == nil {
		t.Error("expected a repeated accept to be refused")
	}
	if balance, _ := txRepo.GetBalance(context.Background(), customerID); balance != mockDefaultBalance.Sub(money.FromRubles(350)) {
		t.Errorf("balance must be charged once, got %s", balance)
	}
}

// TestAcceptBidRejectsForeignCustomer сохраняет проверку владения на месте
// после переезда из репозитория.
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
	bid, _ := bidRepo.CreateBid(context.Background(), order.ID, uuid.New(), money.FromRubles(100))

	if err := srv.AcceptBid(context.Background(), bid.ID, uuid.New()); err == nil {
		t.Error("expected a stranger to be refused")
	}
}

// newWithdrawalTestService собирает AdminService с реестром, отслеживающим баланс.
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

// TestWithdrawalReservesFunds покрывает M-06: заявка раньше лишь смотрела на
// баланс и оставляла деньги тратимыми.
func TestWithdrawalReservesFunds(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000010", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	if _, err := svc.CreateWithdrawalRequest(context.Background(), user.ID, money.FromRubles(400)); err != nil {
		t.Fatalf("unexpected error requesting withdrawal: %v", err)
	}

	balance, _ := txRepo.GetBalance(context.Background(), user.ID)
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

// TestWithdrawalCannotExceedBalance проверяет охраняемое списание, а не
// «проверил-записал» по устаревшему чтению.
func TestWithdrawalCannotExceedBalance(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000011", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user
	txRepo.balances = map[uuid.UUID]money.Amount{user.ID: money.FromRubles(100)}

	if _, err := svc.CreateWithdrawalRequest(context.Background(), user.ID, money.FromRubles(500)); err == nil {
		t.Error("expected a request larger than the balance to be refused")
	}
	if balance, _ := txRepo.GetBalance(context.Background(), user.ID); balance != money.FromRubles(100) {
		t.Errorf("a refused request must not touch the balance, got %s", balance)
	}
}

// TestRejectedWithdrawalReturnsTheMoney убеждается, что отказ не превращается в
// тихую конфискацию теперь, когда средства уходят с баланса заранее.
func TestRejectedWithdrawalReturnsTheMoney(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000012", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	req, err := svc.CreateWithdrawalRequest(context.Background(), user.ID, money.FromRubles(250))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.RejectWithdrawalRequest(context.Background(), req.ID, uuid.New()); err != nil {
		t.Fatalf("unexpected error rejecting: %v", err)
	}
	if balance, _ := txRepo.GetBalance(context.Background(), user.ID); balance != mockDefaultBalance {
		t.Errorf("rejecting must return the reserved money, balance is %s", balance)
	}

	// Второе решение по той же заявке не должно вернуть деньги дважды.
	if err := svc.RejectWithdrawalRequest(context.Background(), req.ID, uuid.New()); err == nil {
		t.Error("expected a second decision to be refused")
	}
	if balance, _ := txRepo.GetBalance(context.Background(), user.ID); balance != mockDefaultBalance {
		t.Errorf("balance must be restored once, got %s", balance)
	}
}

// TestApprovedWithdrawalDoesNotDebitTwice: деньги ушли с баланса ещё при
// создании заявки, поэтому одобрение не должно ничего двигать.
func TestApprovedWithdrawalDoesNotDebitTwice(t *testing.T) {
	svc, _, userRepo, txRepo := newWithdrawalTestService()

	user := &repository.User{ID: uuid.New(), Phone: "+79990000013", Role: "EXECUTOR", Status: "ACTIVE"}
	userRepo.users[user.Phone] = user

	req, err := svc.CreateWithdrawalRequest(context.Background(), user.ID, money.FromRubles(300))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.ApproveWithdrawalRequest(context.Background(), req.ID, uuid.New()); err != nil {
		t.Fatalf("unexpected error approving: %v", err)
	}

	if balance, _ := txRepo.GetBalance(context.Background(), user.ID); balance != mockDefaultBalance.Sub(money.FromRubles(300)) {
		t.Errorf("approval must not debit again, balance is %s", balance)
	}
	if err := svc.ApproveWithdrawalRequest(context.Background(), req.ID, uuid.New()); err == nil {
		t.Error("expected a repeated approval to be refused")
	}
}

// TestMoneyIsNeverCreatedOrDestroyed — смысл системных счетов: каждое движение
// трогает две стороны, поэтому сумма балансов пользователей и счетов платформы
// остаётся там, где была. До появления реестра штраф просто уходил с баланса
// исполнителя и переставал существовать.
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

	// Даём обоим участникам стартовый баланс, как это бывает в жизни.
	customerStart, _ := txRepo.GetBalance(context.Background(), customerID)
	executorStart, _ := txRepo.GetBalance(context.Background(), executorID)
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

	// Доводим заказ до конца: эскроу перетекает исполнителю.
	if err := orderRepo.AssignOrder(context.Background(), order.ID, executorID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := orders.ExecuteOrder(context.Background(), order.ID, executorID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := orders.Confirm(context.Background(), customerID, order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !accounts.balances[repository.AccountEscrow].IsZero() {
		t.Errorf("escrow must drain on completion, holds %s", accounts.balances[repository.AccountEscrow])
	}
	if got := total(); got != opening {
		t.Errorf("completing the order changed the total: %s, expected %s", got, opening)
	}

	// Штраф собирают, а не уничтожают.
	second, err := orders.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 2", &lat, &lon)
	if err != nil {
		t.Fatalf("create second order: %v", err)
	}
	if err := orderRepo.AssignOrder(context.Background(), second.ID, executorID); err != nil {
		t.Fatalf("assign second: %v", err)
	}
	if err := orders.RejectAssignedOrder(context.Background(), second.ID, executorID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if !accounts.balances[repository.AccountFines].IsPositive() {
		t.Error("the penalty should have landed on the fines account")
	}
	if got := total(); got != opening {
		t.Errorf("fining an executor changed the total: %s, expected %s", got, opening)
	}
}
