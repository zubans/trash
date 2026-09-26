package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Понижение просроченного срочного заказа на настоящей базе: выборка находит
// заказ, понижение ставит удержание на базовую цену, разница уходит заказчику
// из эскроу одной проводкой REFUND, повтор ничего не двигает, и заказ пропадает
// из выборки.
func TestDowngradeOverdueIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	srv := newIntegrationOrderService(db)
	customerID, variantID := seedCustomer(t, db, money.FromRubles(5000))

	lat, lon := 55.7558, 37.6173
	order, err := srv.Create(ctx, customerID, CreateOrderRequest{
		ServiceVariantID: variantID, IsUrgent: true, Address: "Россия, Москва, Тверская улица, д. 1", Lat: &lat, Lon: &lon,
	})
	if err != nil {
		t.Fatalf("create urgent order: %v", err)
	}
	if _, err := db.Exec(`UPDATE orders SET status = 'ASSIGNED', deadline_at = now() - interval '1 minute' WHERE id = $1`, order.ID); err != nil {
		t.Fatalf("make overdue: %v", err)
	}

	ids, err := srv.OverdueUrgentOrders(ctx, 1000)
	if err != nil || !containsID(ids, order.ID) {
		t.Fatalf("overdue list %v (%v) must contain the order", ids, err)
	}

	balance := func() money.Amount {
		var b money.Amount
		if err := db.QueryRow(`SELECT balance FROM users WHERE id = $1`, customerID).Scan(&b); err != nil {
			t.Fatalf("balance: %v", err)
		}
		return b
	}
	escrow := func() money.Amount {
		var b money.Amount
		if err := db.QueryRow(`SELECT balance FROM system_accounts WHERE code = 'ESCROW'`).Scan(&b); err != nil {
			t.Fatalf("escrow: %v", err)
		}
		return b
	}
	balanceBefore, escrowBefore := balance(), escrow()

	res, err := srv.DowngradeOverdue(ctx, order.ID)
	if err != nil || !res.Downgraded {
		t.Fatalf("downgrade = %+v, %v", res, err)
	}
	base, _ := srv.CalculatePrice(ctx, variantID, false, false, false)
	if res.FinalAmount != base || res.Refund != order.HoldAmount.Sub(base) || !res.Refund.IsPositive() {
		t.Fatalf("result %+v, want final %s and refund %s", res, base, order.HoldAmount.Sub(base))
	}

	var hold, final money.Amount
	var downgraded, urgent bool
	if err := db.QueryRow(`SELECT hold_amount, final_amount, is_downgraded, is_urgent FROM orders WHERE id = $1`, order.ID).
		Scan(&hold, &final, &downgraded, &urgent); err != nil {
		t.Fatalf("read order: %v", err)
	}
	if hold != base || final != base || !downgraded || urgent {
		t.Errorf("order row: hold %s final %s downgraded %v urgent %v", hold, final, downgraded, urgent)
	}
	if got := balance(); got != balanceBefore.Add(res.Refund) {
		t.Errorf("customer balance %s → %s, want +%s", balanceBefore, got, res.Refund)
	}
	if got := escrow(); got != escrowBefore.Sub(res.Refund) {
		t.Errorf("escrow %s → %s, want −%s", escrowBefore, got, res.Refund)
	}
	var refunds int
	if err := db.QueryRow(`SELECT count(*) FROM transactions WHERE order_id = $1 AND type = $2`, order.ID, repository.TransactionTypeRefund).Scan(&refunds); err != nil || refunds != 1 {
		t.Errorf("refund entries = %d (%v), want 1", refunds, err)
	}

	again, err := srv.DowngradeOverdue(ctx, order.ID)
	if err != nil || again.Downgraded {
		t.Errorf("second downgrade = %+v, %v; want a no-op", again, err)
	}
	if got := balance(); got != balanceBefore.Add(res.Refund) {
		t.Errorf("second downgrade moved money: balance %s", got)
	}
	if ids, _ := srv.OverdueUrgentOrders(ctx, 1000); containsID(ids, order.ID) {
		t.Error("a downgraded order is still listed as overdue")
	}
}

// Выборка истёкших аукционов на настоящей базе: старый аукцион в поиске
// попадает, свежий и уже забранный — нет.
func TestExpiredAuctionsIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	srv := newIntegrationOrderService(db)
	customerID, _ := seedCustomer(t, db, money.Zero)

	var auctionVariant uuid.UUID
	if err := db.QueryRow(`SELECT id FROM service_nodes WHERE node_type = 'VARIANT' AND is_auction = TRUE LIMIT 1`).Scan(&auctionVariant); err != nil {
		t.Fatalf("no auction variant in the catalog: %v", err)
	}
	insert := func(status string, age time.Duration) uuid.UUID {
		id := uuid.New()
		if _, err := db.Exec(`INSERT INTO orders (id, customer_id, service_variant_id, status, created_at) VALUES ($1, $2, $3, $4, now() - $5::interval)`,
			id, customerID, auctionVariant, status, age.String()); err != nil {
			t.Fatalf("seed auction: %v", err)
		}
		return id
	}
	old := insert("SEARCHING", AuctionTTL+time.Hour)
	fresh := insert("SEARCHING", time.Hour)
	taken := insert("ASSIGNED", AuctionTTL+time.Hour)

	ids, err := srv.ExpiredAuctionOrders(ctx, time.Now(), 1000)
	if err != nil {
		t.Fatalf("expired auctions: %v", err)
	}
	if !containsID(ids, old) || containsID(ids, fresh) || containsID(ids, taken) {
		t.Errorf("expired = %v; want old %s only among {old, fresh %s, taken %s}", ids, old, fresh, taken)
	}
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
