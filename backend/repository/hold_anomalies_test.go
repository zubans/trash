package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
)

// Бесплатные услуги — поддерживаемое продуктовое решение, а не дефект, поэтому
// заказ такой услуги по замыслу ничего не удерживает и не должен попадать в
// отчёт. Заказ, который деньги взял и больше их не удерживает, — должен: это
// возврат или выплата, прошедшие без своего перехода состояния, и именно ради
// этого случая проверка существует. Различают их по реестру, а не по текущей
// цене, потому что услугу могут переоценить после размещения заказа.
func TestHoldAnomaliesAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("RECONCILE_TEST_DSN")
	if dsn == "" {
		t.Skip("set RECONCILE_TEST_DSN to run the hold anomaly checks against a real database")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()

	customer := uuid.New()
	freeVariant, paidVariant, auctionVariant := uuid.New(), uuid.New(), uuid.New()
	freeOrder, brokenOrder, healthyOrder, auctionOrder, finishedOrder := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE order_id = ANY($1)`,
			pgUUIDs(freeOrder, brokenOrder, healthyOrder, auctionOrder, finishedOrder))
		_, _ = db.Exec(`DELETE FROM orders WHERE id = ANY($1)`,
			pgUUIDs(freeOrder, brokenOrder, healthyOrder, auctionOrder, finishedOrder))
		_, _ = db.Exec(`DELETE FROM service_nodes WHERE id = ANY($1)`,
			pgUUIDs(freeVariant, paidVariant, auctionVariant))
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, customer)
	})

	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'CUSTOMER', $2, 'x', 0, 'ACTIVE')`,
		customer, "+7997"+customer.String()[:7]); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	variant := func(id uuid.UUID, price money.Amount, auction bool) {
		if _, err := db.Exec(
			`INSERT INTO service_nodes (id, code, node_type, name, base_price, is_auction) VALUES ($1, $2, 'VARIANT', $3, $4, $5)`,
			id, "test-"+id.String()[:8], `{"ru":"test"}`, price, auction); err != nil {
			t.Fatalf("seed variant: %v", err)
		}
	}
	order := func(id, vid uuid.UUID, status string, hold money.Amount) {
		if _, err := db.Exec(
			`INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount)
			 VALUES ($1, $2, $3, $4::order_status_type, $5)`,
			id, customer, vid, status, hold); err != nil {
			t.Fatalf("seed order %s: %v", status, err)
		}
	}
	held := func(orderID uuid.UUID, amount money.Amount) {
		if _, err := db.Exec(
			`INSERT INTO transactions (user_id, order_id, type, amount) VALUES ($1, $2, 'HOLD', $3)`,
			customer, orderID, amount); err != nil {
			t.Fatalf("seed hold: %v", err)
		}
	}

	variant(freeVariant, money.Zero, false)
	variant(paidVariant, money.FromRubles(150), false)
	variant(auctionVariant, money.Zero, true)

	// Поддерживается: бесплатная услуга ничего не удерживает и никогда ничего не брала.
	order(freeOrder, freeVariant, "ASSIGNED", money.Zero)
	// Поддерживается: аукцион ничего не удерживает, пока не принята ставка.
	order(auctionOrder, auctionVariant, "ASSIGNED", money.Zero)
	// Верно: платный заказ удерживает то, что взял.
	order(healthyOrder, paidVariant, "ASSIGNED", money.FromRubles(150))
	held(healthyOrder, money.FromRubles(150))
	// Сломано: деньги взяли, и их нет, а заказ ещё жив.
	order(brokenOrder, paidVariant, "EXECUTED", money.Zero)
	held(brokenOrder, money.FromRubles(150))
	// Сломано в другую сторону: завершён, но всё ещё удерживает.
	order(finishedOrder, paidVariant, "COMPLETED", money.FromRubles(150))
	held(finishedOrder, money.FromRubles(150))

	repo := NewReconciliationRepository(db).(*reconcileRepo)
	anomalies, err := repo.holdAnomalies(ctx, money.FromRubles(0.01))
	if err != nil {
		t.Fatalf("holdAnomalies: %v", err)
	}

	reported := make(map[uuid.UUID]string, len(anomalies))
	for _, a := range anomalies {
		reported[a.OrderID] = a.Reason
	}

	for name, id := range map[string]uuid.UUID{
		"free service":  freeOrder,
		"auction":       auctionOrder,
		"healthy order": healthyOrder,
	} {
		if reason, found := reported[id]; found {
			t.Errorf("%s was reported as an anomaly (%q); it is correct data", name, reason)
		}
	}

	if _, found := reported[brokenOrder]; !found {
		t.Error("a live order that lost the money it held was not reported — this is the case the check exists for")
	}
	if _, found := reported[finishedOrder]; !found {
		t.Error("a finished order still holding money was not reported")
	}
}

func pgUUIDs(ids ...uuid.UUID) string {
	out := "{"
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id.String()
	}
	return out + "}"
}
