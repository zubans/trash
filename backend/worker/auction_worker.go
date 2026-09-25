package worker

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/service"
)

// AuctionWorker автоматически отменяет аукционные заказы, не нашедшие пары за 7 дней.
type AuctionWorker struct {
	db           *sql.DB
	orderService *service.OrderService
	guard        Guard
}

// NewAuctionWorker создаёт новый AuctionWorker.
func NewAuctionWorker(db *sql.DB, orderService *service.OrderService) *AuctionWorker {
	return &AuctionWorker{db: db, orderService: orderService}
}

// Start периодически выполняет цикл воркера.
func (w *AuctionWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "AuctionWorker", metric: "auction", guard: w.guard}.
		Start(ctx, interval, w.CheckExpiredAuctions)
}

// CheckExpiredAuctions выбирает и отменяет истёкшие аукционные заказы.
func (w *AuctionWorker) CheckExpiredAuctions(ctx context.Context) error {
	query := `
		SELECT o.id
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.status = 'SEARCHING' 
		  AND sn.is_auction = TRUE 
		  AND o.created_at < now() - INTERVAL '7 days'`

	rows, err := w.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		list = append(list, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range list {
		// Один правильный путь отмены, под блокировкой строки, вместо собственного
		// сырого SQL этого воркера, который зачислял заказчику, ни разу не списав
		// с эскроу, и оставлял hold_amount на месте.
		//
		// CancelUnclaimedAuction, а не CancelOrder: аукцион не держит денег, пока
		// не принята ставка, а принятие — ровно то, что переводит его в ASSIGNED.
		// Заявка, дошедшая до ASSIGNED между сканом выше и этой строкой, уже
		// забрана и больше не истёкшее дело — она принадлежит выигравшему её
		// исполнителю.
		if err := w.orderService.CancelUnclaimedAuction(ctx, id); err != nil {
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
