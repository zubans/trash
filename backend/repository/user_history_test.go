package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// История на карточке пользователя показывает его заказы в обеих ролях и только
// его проводки. Оба свойства проверяются на настоящей базе: первое — это OR по
// двум столбцам, второе — то, ради чего отбор идёт по user_id, а не по
// нестрогому поиску телефона, каким пользуется общий журнал.
func TestUserHistoryCoversBothRolesAndOnlyThisUser(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := repository.NewAdminRepository(db)

	subject := createTestUser(t, db, "CUSTOMER")
	other := createTestUser(t, db, "EXECUTOR")
	stranger := createTestUser(t, db, "CUSTOMER")

	variantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO service_nodes (id, code, name, node_type, base_price, is_active)
		 VALUES ($1, $2, $3::jsonb, 'VARIANT', 100, true)`,
		variantID, "history-"+uuid.New().String()[:8], `{"ru": "Вывоз", "en": "Pickup"}`,
	); err != nil {
		t.Fatalf("insert variant: %v", err)
	}

	// Заказ, где наш пользователь — заказчик, и ещё не взят: телефон
	// исполнителя придёт из LEFT JOIN как NULL.
	asCustomer := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount, address)
		 VALUES ($1, $2, $3, 'SEARCHING', 100, 'Россия, г. Москва, ул. Арбат, д. 10')`,
		asCustomer, subject, variantID,
	); err != nil {
		t.Fatalf("insert order as customer: %v", err)
	}

	// Заказ, где он же — исполнитель.
	asExecutor := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO orders (id, customer_id, executor_id, service_variant_id, status, hold_amount, address)
		 VALUES ($1, $2, $3, $4, 'ASSIGNED', 200, 'Россия, г. Москва, ул. Тверская, д. 1')`,
		asExecutor, other, subject, variantID,
	); err != nil {
		t.Fatalf("insert order as executor: %v", err)
	}

	// Чужой заказ, к которому наш пользователь отношения не имеет.
	foreign := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO orders (id, customer_id, service_variant_id, status, hold_amount, address)
		 VALUES ($1, $2, $3, 'SEARCHING', 300, 'Россия, г. Москва, ул. Чужая, д. 3')`,
		foreign, stranger, variantID,
	); err != nil {
		t.Fatalf("insert foreign order: %v", err)
	}

	orders, total, err := repo.GetUserOrders(ctx, subject, 50, 0)
	if err != nil {
		t.Fatalf("GetUserOrders: %v", err)
	}
	if total != 2 {
		t.Fatalf("заказов у пользователя = %d, ожидалось 2 (в обеих ролях)", total)
	}
	seen := map[uuid.UUID]bool{}
	for _, o := range orders {
		seen[o.ID] = true
		if o.ID == foreign {
			t.Error("в историю попал чужой заказ")
		}
	}
	if !seen[asCustomer] {
		t.Error("заказ, где пользователь заказчик, не попал в историю")
	}
	if !seen[asExecutor] {
		t.Error("заказ, где пользователь исполнитель, не попал в историю")
	}

	// Проводки: свои видны, чужие — нет.
	for _, row := range []struct {
		user   uuid.UUID
		amount float64
	}{{subject, 100}, {subject, 50}, {stranger, 999}} {
		if _, err := db.Exec(
			`INSERT INTO transactions (id, user_id, type, amount) VALUES ($1, $2, 'TOP_UP', $3)`,
			uuid.New(), row.user, money.FromRubles(row.amount),
		); err != nil {
			t.Fatalf("insert transaction: %v", err)
		}
	}

	txs, txTotal, err := repo.GetUserTransactions(ctx, subject, 50, 0)
	if err != nil {
		t.Fatalf("GetUserTransactions: %v", err)
	}
	if txTotal != 2 {
		t.Fatalf("проводок у пользователя = %d, ожидалось 2", txTotal)
	}
	for _, tx := range txs {
		if tx.UserID != subject {
			t.Errorf("в историю попала чужая проводка пользователя %s", tx.UserID)
		}
		// Направление приходит из общего соглашения о знаках, а не считается
		// клиентом заново.
		if tx.Direction == 0 {
			t.Errorf("у проводки типа %s не проставлено направление", tx.Type)
		}
	}
}

// Страница ограничена сверху: без предела один запрос мог бы попросить всю
// историю пользователя целиком.
func TestUserHistoryPagesAndClamps(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := repository.NewAdminRepository(db)
	subject := createTestUser(t, db, "CUSTOMER")

	for i := 0; i < 5; i++ {
		if _, err := db.Exec(
			`INSERT INTO transactions (id, user_id, type, amount) VALUES ($1, $2, 'TOP_UP', $3)`,
			uuid.New(), subject, money.FromRubles(float64(i+1)),
		); err != nil {
			t.Fatalf("insert transaction: %v", err)
		}
	}

	first, total, err := repo.GetUserTransactions(ctx, subject, 2, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 5 || len(first) != 2 {
		t.Fatalf("страница 1: %d строк при total=%d, ожидалось 2 при 5", len(first), total)
	}

	second, _, err := repo.GetUserTransactions(ctx, subject, 2, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(second) != 2 || second[0].ID == first[0].ID {
		t.Error("вторая страница повторяет первую")
	}

	// Запрошенная тысяча зажимается, а не выполняется.
	huge, _, err := repo.GetUserTransactions(ctx, subject, 100000, 0)
	if err != nil {
		t.Fatalf("huge page: %v", err)
	}
	if len(huge) != 5 {
		t.Fatalf("вернулось %d строк, ожидалось 5", len(huge))
	}
}
