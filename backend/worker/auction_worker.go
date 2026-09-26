package worker

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
)

// auctionBatchSize — сколько истёкших аукционов воркер отменяет за проход.
const auctionBatchSize = 100

// auctionOrders — то, что воркеру аукционов нужно от сервиса заказов.
type auctionOrders interface {
	ExpiredAuctionOrders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	CancelUnclaimedAuction(ctx context.Context, orderID uuid.UUID) error
}

// AuctionWorker автоматически отменяет аукционные заказы, не нашедшие пары за
// service.AuctionTTL.
type AuctionWorker struct {
	orders auctionOrders
	now    func() time.Time
	guard  Guard
}

// NewAuctionWorker создаёт новый AuctionWorker.
func NewAuctionWorker(orders auctionOrders) *AuctionWorker {
	return &AuctionWorker{orders: orders, now: time.Now}
}

// Start периодически выполняет цикл воркера.
func (w *AuctionWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "AuctionWorker", metric: "auction", guard: w.guard}.
		Start(ctx, interval, w.CheckExpiredAuctions)
}

// CheckExpiredAuctions выбирает и отменяет истёкшие аукционные заказы.
func (w *AuctionWorker) CheckExpiredAuctions(ctx context.Context) error {
	ids, err := w.orders.ExpiredAuctionOrders(ctx, w.now(), auctionBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		// CancelUnclaimedAuction, а не CancelOrder: аукцион не держит денег, пока
		// не принята ставка, а принятие — ровно то, что переводит его в ASSIGNED.
		// Заявка, дошедшая до ASSIGNED между выборкой и этой строкой, уже
		// забрана и принадлежит выигравшему исполнителю: отмена её не тронет.
		if err := w.orders.CancelUnclaimedAuction(ctx, id); err != nil {
			log.Printf("[AuctionWorker] Failed to cancel auction %s: %v", id, err)
		} else {
			log.Printf("[AuctionWorker] Canceled expired auction %s.", id)
		}
	}
	return nil
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *AuctionWorker) WithLeader(leader *Leader, name string) *AuctionWorker {
	w.guard = leader.Guard(name)
	return w
}
