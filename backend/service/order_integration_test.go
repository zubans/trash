package service

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Эти тесты выполняются на настоящем Postgres с применёнными миграциями. Они
// существуют потому, что отказ, от которого они охраняют, нельзя воспроизвести
// на моках: создание заказа пишет проводку, ссылающуюся на строку заказа, а
// transactions.order_id — внешний ключ, проверяемый немедленно. Неверный порядок
// этих двух операторов падает только на настоящей базе — именно так он и
// добрался до прода.
//
//	ORDER_TEST_DSN="postgres://postgres:x@localhost:55432/healthlogin?sslmode=disable" \
//	    go test ./service/ -run Integration
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("ORDER_TEST_DSN")
	if dsn == "" {
		t.Skip("set ORDER_TEST_DSN to run the order flow against a real database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedCustomer создаёт пользователя с балансом и вариант услуги для заказа.
func seedCustomer(t *testing.T, db *sql.DB, balance money.Amount) (uuid.UUID, uuid.UUID) {
	t.Helper()

	customerID := uuid.New()
	phone := "+7999" + customerID.String()[:7]
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'CUSTOMER', $2, 'x', $3, 'ACTIVE')`,
		customerID, phone, balance); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	var variantID uuid.UUID
	if err := db.QueryRow(
		`SELECT id FROM service_nodes WHERE node_type = 'VARIANT' AND is_auction = FALSE AND base_price > 0 LIMIT 1`,
	).Scan(&variantID); err != nil {
		t.Fatalf("no non-auction service variant in the catalog: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, customerID)
		_, _ = db.Exec(`DELETE FROM orders WHERE customer_id = $1`, customerID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, customerID)
	})
	return customerID, variantID
}

func newIntegrationOrderService(db *sql.DB) *OrderService {
	ledger := NewLedger(repository.NewTransactionRepository(db), repository.NewSystemAccountRepository(db))
	return NewOrderService(
		repository.NewOrderRepository(db),
		ledger,
		repository.NewSettingsRepository(db),
		repository.New(db),
		repository.NewShiftRepository(db),
		nil,
		repository.NewServiceCatalogRepository(db),
		nil,
	)
}

// TestCreateOrderIntegration — регрессионный тест на нарушение внешнего ключа:
// проводка называет заказ, поэтому к моменту её записи строка заказа обязана
// существовать.
func TestCreateOrderIntegration(t *testing.T) {
	db := openTestDB(t)
	srv := newIntegrationOrderService(db)

	customerID, variantID := seedCustomer(t, db, money.FromRubles(5000))

	lat, lon := 55.7558, 37.6173
	order, err := srv.CreateOrder(context.Background(), customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	if err != nil {
		t.Fatalf("creating an order must not fail: %v", err)
	}
	if !order.HoldAmount.IsPositive() {
		t.Fatalf("expected a hold, got %s", order.HoldAmount)
	}

	// Строка заказа на месте.
	var stored money.Amount
	if err := db.QueryRow(`SELECT hold_amount FROM orders WHERE id = $1`, order.ID).Scan(&stored); err != nil {
		t.Fatalf("the order was not persisted: %v", err)
	}
	if stored != order.HoldAmount {
		t.Errorf("stored hold %s does not match the returned %s", stored, order.HoldAmount)
	}

	// И проводка, указывающая на неё, тоже.
	var entries int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM transactions WHERE order_id = $1 AND type = 'HOLD'`, order.ID).Scan(&entries); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if entries != 1 {
		t.Errorf("expected exactly one HOLD entry for the order, got %d", entries)
	}

	// Заказчик заплатил за него ровно один раз.
	var balance money.Amount
	if err := db.QueryRow(`SELECT balance FROM users WHERE id = $1`, customerID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if want := money.FromRubles(5000).Sub(order.HoldAmount); balance != want {
		t.Errorf("expected a balance of %s, got %s", want, balance)
	}
}

// TestCreateOrderRollsBackOnInsufficientFunds покрывает то, чего не могут моки:
// строка заказа и проводка делят одну транзакцию, поэтому отклонённое удержание
// не оставляет после себя ничего.
func TestCreateOrderRollsBackOnInsufficientFunds(t *testing.T) {
	db := openTestDB(t)
	srv := newIntegrationOrderService(db)

	// Слишком мало, чтобы покрыть любой вариант в каталоге.
	customerID, variantID := seedCustomer(t, db, money.FromRubles(1))

	lat, lon := 55.7558, 37.6173
	if _, err := srv.CreateOrder(context.Background(), customerID, variantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon); err == nil {
		t.Fatal("expected the order to be refused for insufficient funds")
	}

	var orders, entries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = $1`, customerID).Scan(&orders); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE user_id = $1`, customerID).Scan(&entries); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if orders != 0 {
		t.Errorf("a rejected order must leave no row behind, found %d", orders)
	}
	if entries != 0 {
		t.Errorf("a rejected order must leave no ledger entry, found %d", entries)
	}

	var balance money.Amount
	if err := db.QueryRow(`SELECT balance FROM users WHERE id = $1`, customerID).Scan(&balance); err != nil {
		t.Fatalf("read balance: %v", err)
	}
	if balance != money.FromRubles(1) {
		t.Errorf("the balance must be untouched, got %s", balance)
	}
}
