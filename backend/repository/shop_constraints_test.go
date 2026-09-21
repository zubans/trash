package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"healthlogin/backend/repository"
)

// Критерий готовности B1.2: «CHECK не пускают … — проверено вставкой». Это те
// самые вставки, плюс согласованность полей рода, денежные границы и правила
// user_perks, которым ручная выдача привилегии пишет напрямую.
//
// Каждый случай — строка, которую база обязана отклонить: молча принятая
// означала бы, что Go придётся отбрасывать её денежным инцидентом при каждом
// чтении.
func TestShopConstraints(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	seedShopGift(t, db, "shop-constraints-shirt", repository.GiftKindPhysical, nil)
	userID := createTestUser(t, db, "EXECUTOR")

	productID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO shop_products (id, kind, category, title, price, perk_kind, perk_days)
		VALUES ($1, 'PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_FREE', 1)`, productID); err != nil {
		t.Fatalf("control product: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM shop_products WHERE id = $1`, productID) })

	rejected := []struct {
		name  string
		query string
		args  []interface{}
	}{
		{"привилегия без срока",
			`INSERT INTO shop_products (kind, category, title, price, perk_kind, perk_value)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_MULTIPLIER', 0.5)`, nil},
		{"вещь без подарка",
			`INSERT INTO shop_products (kind, category, title, price)
			 VALUES ('PHYSICAL', 'merch', '{"ru":"x"}', 100)`, nil},
		{"множитель 1.5",
			`INSERT INTO shop_products (kind, category, title, price, perk_kind, perk_value, perk_days)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_MULTIPLIER', 1.5, 30)`, nil},
		{"пункты −1",
			`INSERT INTO shop_products (kind, category, title, price, perk_kind, perk_value, perk_days)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_DISCOUNT_PP', -1, 30)`, nil},
		{"«без комиссии» со значением",
			`INSERT INTO shop_products (kind, category, title, price, perk_kind, perk_value, perk_days)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_FREE', 0.5, 1)`, nil},
		{"привилегия с подарком",
			`INSERT INTO shop_products (kind, category, title, price, gift_code, perk_kind, perk_days)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'shop-constraints-shirt', 'COMMISSION_FREE', 1)`, nil},
		{"вещь с видом привилегии",
			`INSERT INTO shop_products (kind, category, title, price, gift_code, perk_kind)
			 VALUES ('PHYSICAL', 'merch', '{"ru":"x"}', 100, 'shop-constraints-shirt', 'COMMISSION_FREE')`, nil},
		{"старая цена ниже цены",
			`INSERT INTO shop_products (kind, category, title, price, compare_at_price, perk_kind, perk_days)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 99, 'COMMISSION_FREE', 1)`, nil},
		{"ноль единиц в заказе",
			`INSERT INTO shop_products (kind, category, title, price, perk_kind, perk_days, max_qty_per_order)
			 VALUES ('PERK', 'perks', '{"ru":"x"}', 100, 'COMMISSION_FREE', 1, 0)`, nil},
		{"итог не сходится с ценой и количеством",
			`INSERT INTO shop_orders (user_id, request_id, product_id, product_snapshot, quantity, unit_price, total, status, offer_version)
			 VALUES ($1, $2, $3, '{}', 2, 100, 150, 'PAID', 1)`,
			[]interface{}{userID, uuid.New(), productID}},
		{"возвращено больше уплаченного",
			`INSERT INTO shop_orders (user_id, request_id, product_id, product_snapshot, quantity, unit_price, total, status, offer_version, refunded_amount)
			 VALUES ($1, $2, $3, '{}', 1, 100, 100, 'PAID', 1, 101)`,
			[]interface{}{userID, uuid.New(), productID}},
		{"выданная вручную привилегия с множителем 1.5",
			`INSERT INTO user_perks (user_id, kind, value, starts_at, expires_at)
			 VALUES ($1, 'COMMISSION_MULTIPLIER', 1.5, now(), now() + interval '1 day')`,
			[]interface{}{userID}},
		{"привилегия неизвестного вида",
			`INSERT INTO user_perks (user_id, kind, value, starts_at, expires_at)
			 VALUES ($1, 'HALF_OFF', 0.5, now(), now() + interval '1 day')`,
			[]interface{}{userID}},
		{"беспроцентный период со значением",
			`INSERT INTO user_perks (user_id, kind, value, starts_at, expires_at)
			 VALUES ($1, 'COMMISSION_FREE', 0.5, now(), now() + interval '1 day')`,
			[]interface{}{userID}},
		{"срок задом наперёд",
			`INSERT INTO user_perks (user_id, kind, starts_at, expires_at)
			 VALUES ($1, 'COMMISSION_FREE', now() + interval '1 day', now())`,
			[]interface{}{userID}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, tc.query, tc.args...)
			if err == nil {
				t.Fatal("the database accepted a row it must reject")
			}
			var pqErr *pq.Error
			if !asPQError(err, &pqErr) || pqErr.Code != "23514" {
				t.Errorf("expected a CHECK violation (23514), got %v", err)
			}
		})
	}

	// Контроль: согласованные строки проходят — иначе запреты выше проверяли
	// бы не правила, а всеобщий отказ.
	orderID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO shop_orders (id, user_id, request_id, product_id, product_snapshot, quantity, unit_price, total, status, offer_version, refunded_amount)
		VALUES ($1, $2, $3, $4, '{}', 2, 100, 200, 'PAID', 1, 50)`,
		orderID, userID, uuid.New(), productID); err != nil {
		t.Errorf("control order: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO transactions (user_id, shop_order_id, type, amount, counterparty)
		VALUES ($1, $2, 'SHOP_PURCHASE', 200, 'SHOP')`, userID, orderID); err != nil {
		t.Errorf("transaction linked to the purchase: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO user_perks (user_id, kind, value, starts_at, expires_at)
		VALUES ($1, 'COMMISSION_MULTIPLIER', 0.5, now(), now() + interval '30 days')`, userID); err != nil {
		t.Errorf("control perk: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE shop_order_id = $1`, orderID)
		_, _ = db.Exec(`DELETE FROM user_perks WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM shop_orders WHERE id = $1`, orderID)
	})
}

// asPQError достаёт ошибку драйвера, чтобы тест отличал нарушение CHECK от
// любой другой ошибки вставки.
func asPQError(err error, target **pq.Error) bool {
	return errors.As(err, target)
}
