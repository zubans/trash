package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// earned_total — BIGINT в копейках, а final_amount заказа — NUMERIC в рублях.
// Оба пути к агрегату — накопление при подтверждении и пересчёт по заказам —
// обязаны давать одну и ту же сумму, и чтение не должно её масштабировать.
func TestExecutorStatsEarnedTotalKeepsScale(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewExecutorStatsRepository(db)

	customerID := createTestUser(t, db, "CUSTOMER")
	executorID := createTestUser(t, db, "EXECUTOR")
	variantID := uuid.New()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM orders WHERE executor_id = $1`, executorID)
		_, _ = db.Exec(`DELETE FROM executor_customers WHERE executor_id = $1`, executorID)
		_, _ = db.Exec(`DELETE FROM executor_stats WHERE user_id = $1`, executorID)
		_, _ = db.Exec(`DELETE FROM service_nodes WHERE id = $1`, variantID)
	})

	earned := money.FromRubles(1234.56)
	if err := repo.RecordCompletion(ctx, nil, repository.CompletedOrder{
		ExecutorID: executorID, CustomerID: customerID, Minutes: 10, Earned: earned,
	}); err != nil {
		t.Fatalf("record completion: %v", err)
	}
	stats, err := repo.Get(ctx, nil, executorID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stats.EarnedTotal != earned {
		t.Fatalf("earned_total after a completion = %s, want %s", stats.EarnedTotal, earned)
	}

	// Тот же заказ в журнале заказов: пересчёт обязан прийти к той же сумме.
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, '{"ru": "Вывоз"}'::jsonb, 'VARIANT', 100, true)`,
		variantID, "earned-"+uuid.New().String()[:8]); err != nil {
		t.Fatalf("insert variant: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO orders (id, customer_id, executor_id, service_variant_id, status, final_amount, completed_at, address)
		 VALUES ($1, $2, $3, $4, 'COMPLETED', $5, now(), 'Россия, г. Москва, ул. Арбат, д. 10')`,
		uuid.New(), customerID, executorID, variantID, earned); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if err := repo.Recalculate(ctx, executorID); err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	stats, err = repo.Get(ctx, nil, executorID)
	if err != nil {
		t.Fatalf("get after recalculate: %v", err)
	}
	if stats.EarnedTotal != earned {
		t.Fatalf("earned_total after recalculate = %s, want %s", stats.EarnedTotal, earned)
	}
}
