package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// seedPlainVariant заводит активный вариант без ограничений допуска: тесты
// взятия проверяют лимиты и смены, а не правила каталога.
func seedPlainVariant(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, '{"ru": "Обычный вывоз"}'::jsonb, 'VARIANT', 100, true)`,
		id, "plain-"+id.String()[:8]); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM service_nodes WHERE id = $1`, id) })
	return id
}

// Лимит активных заказов считается внутри транзакции взятия под блокировкой
// исполнителя: два параллельных взятия разных заказов одним исполнителем не
// могут оба пройти под одним и тем же счётчиком. Раньше лимит читался до
// транзакции, и оба видели ноль.
func TestAcceptLimitHoldsUnderConcurrencyIntegration(t *testing.T) {
	db := openTestDB(t)
	srv := newIntegrationOrderService(db)
	srv.settingsRepo = settingsOverride{srv.settingsRepo, map[string]string{
		"max_active_orders":             "1",
		SettingAutoShiftOnAcceptEnabled: "0",
	}}
	ctx := context.Background()

	customerID, _ := seedCustomer(t, db, money.FromRubles(5000))
	variantID := seedPlainVariant(t, db)
	executorID := seedExecutor(t, db)
	if _, err := repository.NewShiftRepository(db).StartShift(ctx, executorID, 3); err != nil {
		t.Fatalf("start shift: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM shifts WHERE executor_id = $1`, executorID) })

	const attempts = 4
	orderIDs := make([]uuid.UUID, 0, attempts)
	for i := 0; i < attempts; i++ {
		order, err := srv.Create(ctx, customerID, CreateOrderRequest{ServiceVariantID: variantID, Address: "Россия, Москва, Тверская улица, д. 1"})
		if err != nil {
			t.Fatalf("create order %d: %v", i, err)
		}
		orderIDs = append(orderIDs, order.ID)
	}

	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i, id := range orderIDs {
		wg.Add(1)
		go func(i int, id uuid.UUID) {
			defer wg.Done()
			results[i] = srv.Accept(ctx, id, executorID)
		}(i, id)
	}
	wg.Wait()

	accepted := 0
	for _, err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrRule):
		default:
			t.Errorf("unexpected accept error: %v", err)
		}
	}
	if accepted != 1 {
		t.Errorf("accepted %d orders with max_active_orders=1, want exactly 1", accepted)
	}
	var assigned int
	if err := db.QueryRow(`SELECT count(*) FROM orders WHERE executor_id = $1 AND status = 'ASSIGNED'`, executorID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != 1 {
		t.Errorf("%d orders assigned in the database, want 1", assigned)
	}
}

// Досрочный выход из смены: штраф, снятие назначений и закрытие смены — одна
// транзакция. Возвращается записанная смена, а вторая попытка выйти видит, что
// смены уже нет.
func TestShiftEarlyEndIsAtomicIntegration(t *testing.T) {
	db := openTestDB(t)
	orders := newIntegrationOrderService(db)
	orders.settingsRepo = settingsOverride{orders.settingsRepo, map[string]string{SettingAutoShiftOnAcceptEnabled: "0"}}
	shiftRepo := repository.NewShiftRepository(db)
	shifts := NewShiftService(shiftRepo, orders.ledger, settingsOverride{orders.settingsRepo, map[string]string{
		"shift_early_exit_penalty": "50",
	}}, orders.orderRepo)
	ctx := context.Background()

	customerID, _ := seedCustomer(t, db, money.FromRubles(5000))
	variantID := seedPlainVariant(t, db)
	executorID := seedExecutor(t, db)
	if _, err := shiftRepo.StartShift(ctx, executorID, 3); err != nil {
		t.Fatalf("start shift: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM shifts WHERE executor_id = $1`, executorID) })

	order, err := orders.Create(ctx, customerID, CreateOrderRequest{ServiceVariantID: variantID, Address: "Россия, Москва, Тверская улица, д. 1"})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := orders.Accept(ctx, order.ID, executorID); err != nil {
		t.Fatalf("accept: %v", err)
	}

	ended, err := shifts.End(ctx, executorID)
	if err != nil {
		t.Fatalf("end shift: %v", err)
	}
	wantFine := money.FromRubles(50).Scale(2).Add(order.HoldAmount)
	if ended.Status != repository.ShiftStatusPenalized || ended.FineAmount != wantFine || ended.ActualEndAt == nil {
		t.Errorf("returned shift = %+v, want PENALIZED with fine %s", ended, wantFine)
	}

	var status string
	var fine money.Amount
	if err := db.QueryRow(`SELECT status::text, fine_amount FROM shifts WHERE id = $1`, ended.ID).Scan(&status, &fine); err != nil {
		t.Fatal(err)
	}
	if status != string(repository.ShiftStatusPenalized) || fine != wantFine {
		t.Errorf("stored shift: %s %s, want PENALIZED %s", status, fine, wantFine)
	}
	var orderStatus string
	var executor sql.NullString
	if err := db.QueryRow(`SELECT status::text, executor_id::text FROM orders WHERE id = $1`, order.ID).Scan(&orderStatus, &executor); err != nil {
		t.Fatal(err)
	}
	if orderStatus != string(repository.OrderStatusSearching) || executor.Valid {
		t.Errorf("order after early end: %s executor=%v, want SEARCHING without executor", orderStatus, executor)
	}
	var fines int
	if err := db.QueryRow(`SELECT count(*) FROM transactions WHERE user_id = $1 AND type = 'FINE' AND amount = $2`, executorID, wantFine).Scan(&fines); err != nil {
		t.Fatal(err)
	}
	if fines != 1 {
		t.Errorf("fine transactions: %d, want 1", fines)
	}

	if _, err := shifts.End(ctx, executorID); !errors.Is(err, ErrNoActiveShift) {
		t.Errorf("second end: %v, want ErrNoActiveShift", err)
	}
}
