package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// booksTotal — инвариант, ради удержания которого и существует весь реестр:
// каждый баланс пользователя плюс каждый баланс системного счёта в сумме дают
// ноль, потому что ни одно движение не трогает только одну сторону.
// ReconciliationRepository проверяет то же самое по настоящей базе еженощно.
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

// Админское зачисление пользователю раньше выполняло сырой SQL: оно добавляло к
// балансу и писало строку TOP_UP, но не списывало ни с одного системного счёта.
// Собственная история пользователя всё ещё сходилась, поэтому пользовательская
// сверка продолжала проходить, а книги платформы расходились чуть сильнее с
// каждым пополнением — ровно то, что показал прод-отчёт: расхождений нет, книги открыты.
func TestAdminTopUpKeepsBooksClosed(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}
	srv := NewAdminService(newMockUserRepo(), adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(txRepo, accounts))

	userID := uuid.New()
	// Трогаем баланс, чтобы стартовый итог включил этого пользователя.
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
	// Деньги, входящие извне, записываются как требование к счёту депозитов, а
	// не наколдовываются на баланс.
	if got := accounts.balances[repository.AccountDeposits]; got != amount.Neg() {
		t.Errorf("deposits account = %s, expected %s", got, amount.Neg())
	}
}

// Путь, которому теперь делегирует воркер аукционов. Раньше он отменял истёкшие
// аукционы собственным SQL: зачислял заказчику, писал строку REFUND, никогда не
// списывал с эскроу и не обнулял hold_amount. Любой отменённый аукцион после
// этого выглядел так, будто всё ещё держит деньги, — аномалии «завершённый заказ
// всё ещё держит деньги», — а на эскроу оставался баланс, на который никто не претендовал.
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

// Форма возврата при понижении SLA: часть удержания возвращается заказчику,
// остальное остаётся удержанным. Она обязана выйти из эскроу, а не появиться на
// балансе из ниоткуда.
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

// Аукцион не держит денег, пока не принята ставка: заявка публикуется без цены,
// и именно принятие ставки двигает деньги в эскроу, а заказ — в ASSIGNED.
// Поэтому семидневная зачистка отменяет заказы, которые ничего не держат, и
// обязана не трогать тот, который забрали между
// сканом и отменой: тот заказ принадлежит выигравшему его исполнителю.
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

	// Ставку принимают ровно тогда, когда зачистка вот-вот дойдёт до этого заказа.
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
	// Заказчику отменить тот же заказ по-прежнему можно: ограничена только
	// зачистка.
	if err := orders.CancelOrder(context.Background(), order.ID); err != nil {
		t.Errorf("an ordinary cancel must still work: %v", err)
	}
}
