package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/service"
)

type fakeSLAOrders struct {
	ids     []uuid.UUID
	results map[uuid.UUID]service.DowngradeResult
	fail    map[uuid.UUID]bool
	calls   []uuid.UUID
}

func (f *fakeSLAOrders) OverdueUrgentOrders(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return f.ids, nil
}

func (f *fakeSLAOrders) DowngradeOverdue(ctx context.Context, id uuid.UUID) (service.DowngradeResult, error) {
	f.calls = append(f.calls, id)
	if f.fail[id] {
		return service.DowngradeResult{}, errors.New("boom")
	}
	return f.results[id], nil
}

type fakeBroadcaster struct {
	sent map[uuid.UUID]interface{}
	ctxs []context.Context
}

func (f *fakeBroadcaster) BroadcastSystemMessage(ctx context.Context, id uuid.UUID, msg interface{}) {
	f.sent[id] = msg
	f.ctxs = append(f.ctxs, ctx)
}

// Воркер только выбирает заказы и оповещает чат: каждый понижается через
// сервис, сбой одного не останавливает проход, в чат уходят только
// действительно пониженные.
func TestSLAWorkerDelegatesToService(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	orders := &fakeSLAOrders{
		ids:     []uuid.UUID{a, b, c},
		results: map[uuid.UUID]service.DowngradeResult{a: {Downgraded: true, FinalAmount: money.FromRubles(100)}, c: {}},
		fail:    map[uuid.UUID]bool{b: true},
	}
	chat := &fakeBroadcaster{sent: map[uuid.UUID]interface{}{}}
	if err := NewSLAWorker(orders, chat).CheckSLAOverdue(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if len(orders.calls) != 3 {
		t.Fatalf("downgrade called for %d orders, want all 3 despite a failure", len(orders.calls))
	}
	if len(chat.sent) != 1 || chat.sent[a] == nil {
		t.Fatalf("broadcasts = %v, want exactly the downgraded order", chat.sent)
	}
	if msg := chat.sent[a].(map[string]interface{}); msg["final_amount"] != money.FromRubles(100) || msg["action"] != "downgrade" {
		t.Errorf("broadcast payload = %v", msg)
	}
}

// Выключение останавливает проход между заказами, а уведомление о уже
// сделанном понижении уходит на контексте, который выключение не отменяет.
func TestSLAWorkerStopsBetweenOrdersOnShutdown(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	orders := &fakeSLAOrders{ids: []uuid.UUID{a, b}, results: map[uuid.UUID]service.DowngradeResult{a: {Downgraded: true}, b: {Downgraded: true}}}
	chat := &fakeBroadcaster{sent: map[uuid.UUID]interface{}{}}
	w := NewSLAWorker(&cancelAfterFirst{fakeSLAOrders: orders, cancel: cancel}, chat)
	if err := w.CheckSLAOverdue(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pass err = %v, want context.Canceled", err)
	}
	if len(orders.calls) != 1 {
		t.Fatalf("downgraded %d orders after shutdown began, want 1", len(orders.calls))
	}
	if len(chat.ctxs) != 1 || chat.ctxs[0].Err() != nil {
		t.Errorf("broadcast for the finished downgrade must use a context shutdown does not cancel")
	}
}

type cancelAfterFirst struct {
	*fakeSLAOrders
	cancel context.CancelFunc
}

func (c *cancelAfterFirst) DowngradeOverdue(ctx context.Context, id uuid.UUID) (service.DowngradeResult, error) {
	defer c.cancel()
	return c.fakeSLAOrders.DowngradeOverdue(ctx, id)
}

type fakeAuctionOrders struct {
	ids      []uuid.UUID
	gotNow   time.Time
	canceled []uuid.UUID
	fail     map[uuid.UUID]bool
}

func (f *fakeAuctionOrders) ExpiredAuctionOrders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	f.gotNow = now
	return f.ids, nil
}

func (f *fakeAuctionOrders) CancelUnclaimedAuction(ctx context.Context, id uuid.UUID) error {
	f.canceled = append(f.canceled, id)
	if f.fail[id] {
		return errors.New("claimed meanwhile")
	}
	return nil
}

// Воркер аукционов отменяет каждый истёкший аукцион через сервис, не
// останавливаясь на отказе по одному из них.
func TestAuctionWorkerCancelsEveryExpiredAuction(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	orders := &fakeAuctionOrders{ids: []uuid.UUID{a, b}, fail: map[uuid.UUID]bool{a: true}}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	w := NewAuctionWorker(orders)
	w.now = func() time.Time { return now }
	if err := w.CheckExpiredAuctions(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !orders.gotNow.Equal(now) {
		t.Errorf("expiry measured from %v, want %v", orders.gotNow, now)
	}
	if len(orders.canceled) != 2 {
		t.Errorf("canceled %v, want both auctions tried", orders.canceled)
	}
}
