package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// blockSilently ставит роли тихую блокировку напрямую: как её включает порог
// 2×N, но без начисления четырёх баллов.
func blockSilently(t *testing.T, f *disputeFixture, userID uuid.UUID, role string) {
	t.Helper()
	ends := time.Now().Add(30 * 24 * time.Hour)
	if _, err := f.db.Exec(`
		INSERT INTO user_penalty_status (user_id, role, active_points, silent_block_started_at, silent_block_ends_at)
		VALUES ($1, $2, 4, now(), $3)
		ON CONFLICT (user_id, role) DO UPDATE SET
			active_points = 4, silent_block_started_at = now(), silent_block_ends_at = EXCLUDED.silent_block_ends_at`,
		userID, role, ends); err != nil {
		t.Fatalf("block: %v", err)
	}
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM user_penalty_status WHERE user_id = $1`, userID) })
}

// Тихо заблокированный исполнитель не видит заказов и не может их взять, а
// заблокированный заказчик не может создать заказ. Отказ — тот же, что и у
// всякого, кому заказ недоступен: механика о себе не объявляет.
func TestSilentBlockHidesWorkIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	ctx := context.Background()
	penalties := newIntegrationPenaltyService(f.db, f.srv)
	f.srv.WithPenalties(penalties)

	// Свежий заказ в поиске, рядом с исполнителем.
	customerID, variantID := seedCustomer(t, f.db, money.FromRubles(5000))
	lat, lon := 55.7558, 37.6173
	order, err := f.srv.CreateOrder(ctx, customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	seesOrder := func(executorID uuid.UUID) bool {
		t.Helper()
		orders, err := f.srv.FindNearbyOrdersForExecutor(ctx, executorID, lat, lon)
		if err != nil {
			t.Fatalf("nearby: %v", err)
		}
		for _, o := range orders {
			if o.ID == order.ID {
				return true
			}
		}
		return false
	}

	freeExecutor := seedExecutor(t, f.db)
	if !seesOrder(freeExecutor) {
		t.Fatal("an unblocked executor does not see a nearby searching order")
	}

	blocked := seedExecutor(t, f.db)
	blockSilently(t, f, blocked, repository.RoleExecutor)
	if seesOrder(blocked) {
		t.Fatal("a silently blocked executor still sees orders")
	}
	if err := f.srv.Accept(ctx, order.ID, blocked); err == nil {
		t.Fatal("a silently blocked executor took an order")
	}

	// Блокировка в роли заказчика работы исполнителю не закрывает.
	blockSilently(t, f, freeExecutor, repository.RoleCustomer)
	if !seesOrder(freeExecutor) {
		t.Fatal("a customer-role block hid orders from the executor")
	}

	// Заказчик в тихой блокировке не может создать заказ.
	blockSilently(t, f, customerID, repository.RoleCustomer)
	if _, err := f.srv.CreateOrder(ctx, customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 2", &lat, &lon); err == nil {
		t.Fatal("a silently blocked customer created an order")
	}
}
