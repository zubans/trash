package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

// Заказ в поиске исполнителя не имеет, и его телефон приходит из LEFT JOIN как
// NULL. Проверяется, переживает ли это чтение списка заказов: NULL, прочитанный
// в обычную строку, — ошибка драйвера, а не пустое значение. Заодно — что
// заказ в поиске попадает в группу активных и не попадает в «на проверке».
func TestGetOrdersHandlesUnassignedOrder(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := repository.NewAdminRepository(db)

	customerID := createTestUser(t, db, "CUSTOMER")

	variantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 100, true)`,
		variantID, "active-order-"+uuid.New().String()[:8],
		`{"ru": "Вывоз", "en": "Pickup"}`,
	); err != nil {
		t.Fatalf("insert variant: %v", err)
	}

	orderID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount, address)
		 VALUES ($1, $2, $3, 'SEARCHING', 100, 'Россия, г. Москва, ул. Арбат, д. 10')`,
		orderID, customerID, variantID,
	); err != nil {
		t.Fatalf("insert order: %v", err)
	}

	orders, _, err := repo.GetOrders(ctx, repository.OrdersFilter{
		Statuses: repository.OrderStatusGroups[repository.OrderGroupActive], Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetOrders вернул ошибку на заказе без исполнителя: %v", err)
	}

	var found bool
	for _, o := range orders {
		if o.ID == orderID {
			found = true
			if o.ExecutorPhone != "" {
				t.Errorf("у заказа без исполнителя телефон исполнителя = %q, ожидалась пустая строка", o.ExecutorPhone)
			}
		}
	}
	if !found {
		t.Error("заказ в поиске не попал в список активных")
	}

	review, _, err := repo.GetOrders(ctx, repository.OrdersFilter{
		Statuses: repository.OrderStatusGroups[repository.OrderGroupReview], Search: orderID.String(), Limit: 50,
	})
	if err != nil {
		t.Fatalf("GetOrders(review): %v", err)
	}
	if len(review) != 0 {
		t.Error("заказ в поиске не должен попадать в группу «на проверке»")
	}
}
