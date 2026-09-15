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

// disputeFixture — заказ, исполненный исполнителем и ждущий подтверждения.
type disputeFixture struct {
	db         *sql.DB
	srv        *OrderService
	disputes   repository.DisputeRepository
	customerID uuid.UUID
	executorID uuid.UUID
	order      *repository.Order
}

func seedExecutor(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	phone := "+7998" + id.String()[:7]
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'EXECUTOR', $2, 'x', 0, 'ACTIVE')`,
		id, phone); err != nil {
		t.Fatalf("seed executor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func newDisputeFixture(t *testing.T) *disputeFixture {
	t.Helper()
	db := openTestDB(t)
	disputes := repository.NewDisputeRepository(db)
	srv := newIntegrationOrderService(db).WithDisputes(disputes)

	customerID, variantID := seedCustomer(t, db, money.FromRubles(5000))
	executorID := seedExecutor(t, db)

	lat, lon := 55.7558, 37.6173
	order, err := srv.CreateOrder(context.Background(), customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	// Назначение и отметка «Исполнил» — не предмет этих тестов; их путь
	// проверяют свои тесты, здесь заказ просто приводится в EXECUTED.
	if _, err := db.Exec(
		`UPDATE orders SET executor_id = $1, status = 'EXECUTED', assigned_at = now() WHERE id = $2`,
		executorID, order.ID); err != nil {
		t.Fatalf("mark executed: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM order_disputes WHERE order_id = $1`, order.ID) })
	order.ExecutorID = &executorID
	return &disputeFixture{db: db, srv: srv, disputes: disputes, customerID: customerID, executorID: executorID, order: order}
}

func (f *disputeFixture) status(t *testing.T) repository.OrderStatus {
	t.Helper()
	var status string
	if err := f.db.QueryRow(`SELECT status::text FROM orders WHERE id = $1`, f.order.ID).Scan(&status); err != nil {
		t.Fatalf("read order status: %v", err)
	}
	return repository.OrderStatus(status)
}

func TestOpenDisputeIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	ctx := context.Background()

	if _, err := f.srv.OpenDispute(ctx, uuid.New(), f.order.ID, "не вывезли"); err == nil {
		t.Fatal("a stranger opened a dispute")
	}
	if _, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "   "); !errors.Is(err, ErrDisputeClaimRequired) {
		t.Fatalf("empty claim: %v", err)
	}

	// Заказ, ещё не отмеченный исполнителем, оспаривать рано.
	if _, err := f.db.Exec(`UPDATE orders SET status = 'ASSIGNED' WHERE id = $1`, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "не вывезли"); !errors.Is(err, ErrDisputeNotAllowed) {
		t.Fatalf("assigned order: %v", err)
	}
	if _, err := f.db.Exec(`UPDATE orders SET status = 'EXECUTED' WHERE id = $1`, f.order.ID); err != nil {
		t.Fatal(err)
	}

	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "  мешки стоят у подъезда  ")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	if dispute.Status != repository.DisputeStatusOpen || dispute.ExecutorID != f.executorID || dispute.Claim != "мешки стоят у подъезда" {
		t.Fatalf("dispute: %+v", dispute)
	}
	if got := f.status(t); got != repository.OrderStatusDisputed {
		t.Fatalf("order status %s, want DISPUTED", got)
	}
	if _, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "ещё раз"); !errors.Is(err, ErrDisputeAlreadyOpen) {
		t.Fatalf("second dispute: %v", err)
	}

	// Оспоренный заказ остаётся в списке исполнителя: разбирательство — его дело.
	assigned, err := f.srv.orderRepo.GetExecutorAssignedOrders(ctx, f.executorID)
	if err != nil {
		t.Fatalf("executor orders: %v", err)
	}
	found := false
	for _, o := range assigned {
		found = found || o.ID == f.order.ID
	}
	if !found {
		t.Fatal("disputed order dropped from the executor's list")
	}

	// Общие пути закрытия не проходят мимо открытого спора.
	if err := f.srv.ConfirmOrder(ctx, f.order.ID); !errors.Is(err, ErrOrderHasOpenDispute) {
		t.Fatalf("confirm bypassing the dispute: %v", err)
	}
	err = f.srv.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return f.srv.cancelTx(ctx, tx, f.order.ID, repository.OrderStatusDisputed)
	})
	if !errors.Is(err, ErrOrderHasOpenDispute) {
		t.Fatalf("cancel bypassing the dispute: %v", err)
	}
	if got := f.status(t); got != repository.OrderStatusDisputed {
		t.Fatalf("order status %s after refused bypasses", got)
	}
}

// Заказчик подтверждает оспоренный заказ: исполнитель получает деньги как
// обычно, спор закрывается этим подтверждением.
func TestConfirmDisputedOrderIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	ctx := context.Background()

	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "не вывезли")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}

	if err := f.srv.Confirm(ctx, uuid.New(), f.order.ID); err == nil {
		t.Fatal("a stranger confirmed the order")
	}
	if err := f.srv.Confirm(ctx, f.customerID, f.order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if got := f.status(t); got != repository.OrderStatusCompleted {
		t.Fatalf("order status %s, want COMPLETED", got)
	}

	var status, closure string
	var closedBy uuid.NullUUID
	if err := f.db.QueryRow(`SELECT status, closure, closed_by FROM order_disputes WHERE id = $1`, dispute.ID).
		Scan(&status, &closure, &closedBy); err != nil {
		t.Fatalf("read dispute: %v", err)
	}
	if status != repository.DisputeStatusClosed || closure != repository.DisputeClosureCustomerConfirmed || closedBy.UUID != f.customerID {
		t.Fatalf("dispute after confirm: %s %s %v", status, closure, closedBy)
	}

	var reward, hold money.Amount
	if err := f.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, f.executorID).Scan(&reward); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT hold_amount FROM orders WHERE id = $1`, f.order.ID).Scan(&hold); err != nil {
		t.Fatal(err)
	}
	if !reward.IsPositive() || !hold.IsZero() {
		t.Fatalf("executor balance %s, order hold %s after confirm", reward, hold)
	}

	// Спор закрыт — повторное подтверждение ничего не платит.
	if err := f.srv.Confirm(ctx, f.customerID, f.order.ID); err == nil {
		t.Fatal("confirmed a completed order twice")
	}
}

// Исполнитель признаёт вину: заказ отменён, заказчику вернулось всё
// удержанное, спор закрыт признанием, событие для ачивки записано.
func TestConcedeDisputeIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	ctx := context.Background()
	events := repository.NewEventRepository(f.db)
	f.srv.WithBehaviors(nil, nil, events)

	if err := f.srv.ConcedeDispute(ctx, f.executorID, f.order.ID); !errors.Is(err, ErrDisputeNotOpen) {
		t.Fatalf("concede without a dispute: %v", err)
	}
	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "не вывезли")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	if err := f.srv.ConcedeDispute(ctx, uuid.New(), f.order.ID); err == nil {
		t.Fatal("another executor conceded")
	}

	if err := f.srv.ConcedeDispute(ctx, f.executorID, f.order.ID); err != nil {
		t.Fatalf("concede: %v", err)
	}
	if got := f.status(t); got != repository.OrderStatusCanceled {
		t.Fatalf("order status %s, want CANCELED", got)
	}

	var customerBalance, executorBalance money.Amount
	if err := f.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, f.customerID).Scan(&customerBalance); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT balance FROM users WHERE id = $1`, f.executorID).Scan(&executorBalance); err != nil {
		t.Fatal(err)
	}
	if customerBalance != money.FromRubles(5000) || !executorBalance.IsZero() {
		t.Fatalf("balances after concession: customer %s, executor %s", customerBalance, executorBalance)
	}

	var closure string
	var closedBy uuid.NullUUID
	if err := f.db.QueryRow(`SELECT closure, closed_by FROM order_disputes WHERE id = $1`, dispute.ID).Scan(&closure, &closedBy); err != nil {
		t.Fatal(err)
	}
	if closure != repository.DisputeClosureExecutorConceded || closedBy.UUID != f.executorID {
		t.Fatalf("dispute after concession: %s %v", closure, closedBy)
	}

	var conceded int
	if err := f.db.QueryRow(`SELECT count(*) FROM domain_events WHERE type = $1 AND subject_id = $2 AND actor_id = $3`,
		repository.EventDisputeConceded, f.order.ID, f.executorID).Scan(&conceded); err != nil {
		t.Fatal(err)
	}
	if conceded != 1 {
		t.Fatalf("dispute.conceded events: %d, want 1", conceded)
	}
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM domain_events WHERE subject_id = $1`, f.order.ID) })

	var points int
	if err := f.db.QueryRow(`SELECT count(*) FROM penalty_points WHERE order_id = $1`, f.order.ID).Scan(&points); err != nil {
		t.Fatal(err)
	}
	if points != 0 {
		t.Fatalf("concession awarded %d penalty points", points)
	}

	// Второй раз признавать нечего.
	if err := f.srv.ConcedeDispute(ctx, f.executorID, f.order.ID); !errors.Is(err, ErrDisputeNotOpen) {
		t.Fatalf("second concession: %v", err)
	}
}
