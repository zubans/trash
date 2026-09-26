package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Просроченный срочный заказ понижается до обычного тарифа: удержание и сумма
// становятся базовой ценой, разница возвращается заказчику из эскроу. Повтор
// ничего не возвращает второй раз, а заказ, которому понижаться не положено,
// не трогается.
func TestDowngradeOverdue(t *testing.T) {
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)
	customerID := uuid.New()

	mk := func(status repository.OrderStatus, urgent bool, deadline *time.Time) *repository.Order {
		return &repository.Order{
			ID: uuid.New(), CustomerID: customerID, ServiceVariantID: standardVariantID, Status: status,
			IsUrgent: urgent, HoldAmount: money.FromRubles(150), FinalAmount: money.FromRubles(150), DeadlineAt: deadline,
		}
	}
	overdue := mk(repository.OrderStatusAssigned, true, &past)
	notYet := mk(repository.OrderStatusAssigned, true, &future)
	regular := mk(repository.OrderStatusAssigned, false, &past)
	done := mk(repository.OrderStatusCompleted, true, &past)
	repo := &mockOrderRepo{orders: []*repository.Order{overdue, notYet, regular, done}}
	txRepo := &mockTransactionRepo{}
	srv := NewOrderService(repo, NewLedger(txRepo, newMockAccounts()), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	ids, err := srv.OverdueUrgentOrders(ctx, 10)
	if err != nil || len(ids) != 1 || ids[0] != overdue.ID {
		t.Fatalf("overdue list = %v, %v; want only the overdue urgent order", ids, err)
	}

	before, _ := txRepo.GetBalance(ctx, customerID)
	res, err := srv.DowngradeOverdue(ctx, overdue.ID)
	if err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	base, _ := srv.CalculatePrice(ctx, standardVariantID, false, false, false)
	if !res.Downgraded || res.FinalAmount != base || res.Refund != money.FromRubles(150).Sub(base) {
		t.Fatalf("result = %+v, want downgraded to %s with refund %s", res, base, money.FromRubles(150).Sub(base))
	}
	if !overdue.IsDowngraded || overdue.IsUrgent || overdue.HoldAmount != base || overdue.FinalAmount != base {
		t.Errorf("order after downgrade = %+v", overdue)
	}
	if after, _ := txRepo.GetBalance(ctx, customerID); after != before.Add(res.Refund) {
		t.Errorf("customer balance %s → %s, want +%s", before, after, res.Refund)
	}

	again, err := srv.DowngradeOverdue(ctx, overdue.ID)
	if err != nil || again.Downgraded {
		t.Errorf("second downgrade = %+v, %v; want a no-op", again, err)
	}
	if after, _ := txRepo.GetBalance(ctx, customerID); after != before.Add(res.Refund) {
		t.Errorf("second downgrade refunded again: balance %s", after)
	}

	for name, o := range map[string]*repository.Order{"deadline ahead": notYet, "not urgent": regular, "completed": done} {
		r, err := srv.DowngradeOverdue(ctx, o.ID)
		if err != nil || r.Downgraded || o.IsDowngraded || o.HoldAmount != money.FromRubles(150) {
			t.Errorf("%s: result %+v, err %v, order %+v — must stay untouched", name, r, err, o)
		}
	}
}

// Истёкшими считаются аукционы в поиске старше AuctionTTL.
func TestExpiredAuctionOrders(t *testing.T) {
	now := time.Now()
	old := &repository.Order{ID: uuid.New(), ServiceVariantID: constructionVariantID, Status: repository.OrderStatusSearching, CreatedAt: now.Add(-AuctionTTL - time.Hour)}
	fresh := &repository.Order{ID: uuid.New(), ServiceVariantID: constructionVariantID, Status: repository.OrderStatusSearching, CreatedAt: now.Add(-time.Hour)}
	taken := &repository.Order{ID: uuid.New(), ServiceVariantID: constructionVariantID, Status: repository.OrderStatusAssigned, CreatedAt: now.Add(-AuctionTTL - time.Hour)}
	srv := NewOrderService(&mockOrderRepo{orders: []*repository.Order{old, fresh, taken}}, testLedger(), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	ids, err := srv.ExpiredAuctionOrders(context.Background(), now, 10)
	if err != nil || len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("expired auctions = %v, %v; want only the old unclaimed one", ids, err)
	}
}
