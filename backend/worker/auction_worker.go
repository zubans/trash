package worker

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// AuctionWorker автоматически отменяет аукционные заказы, не нашедшие пары за 7 дней.
type AuctionWorker struct {
	db           *sql.DB
	orderService *service.OrderService
	guard func(func() error) error
}

// NewAuctionWorker создаёт новый AuctionWorker.
func NewAuctionWorker(db *sql.DB, orderService *service.OrderService) *AuctionWorker {
	return &AuctionWorker{db: db, orderService: orderService}
}

// Start периодически выполняет цикл воркера.
func (w *AuctionWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("auction", func() error { return w.runGuarded(w.CheckExpiredAuctions) }); err != nil {
				log.Printf("[AuctionWorker] Error checking expired auctions: %v", err)
			}
		}
	}()
	log.Printf("[AuctionWorker] Background worker started every %v", interval)
}

type expiredAuction struct {
	ID uuid.UUID
}

// CheckExpiredAuctions выбирает и отменяет истёкшие аукционные заказы.
func (w *AuctionWorker) CheckExpiredAuctions() error {
	query := `
		SELECT o.id
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.status = 'SEARCHING' 
		  AND sn.is_auction = TRUE 
		  AND o.created_at < now() - INTERVAL '7 days'`

	rows, err := w.db.QueryContext(context.Background(), query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []expiredAuction
	for rows.Next() {
		var a expiredAuction
		err := rows.Scan(&a.ID)
		if err != nil {
			return err
		}
		list = append(list, a)
	}

	for _, a := range list {
		// Один правильный путь отмены, под блокировкой строки, вместо собственного
		// сырого SQL этого воркера, который зачислял заказчику, ни разу не списав
		// с эскроу, и оставлял hold_amount на месте.
		//
		// CancelUnclaimedAuction, а не CancelOrder: аукцион не держит денег, пока
		// не принята ставка, а принятие — ровно то, что переводит его в ASSIGNED.
		// Заявка, дошедшая до ASSIGNED между сканом выше и этой строкой, уже
		// забрана и больше не истёкшее дело — она принадлежит выигравшему её
		// исполнителю.
		err := w.orderService.CancelUnclaimedAuction(context.Background(), a.ID)
		if err != nil {
			log.Printf("[AuctionWorker] Failed to cancel auction %s: %v", a.ID, err)
		} else {
			log.Printf("[AuctionWorker] Canceled expired auction %s.", a.ID)
		}
	}

	return nil
}

// guard выполняет один тик под advisory-блокировкой задачи, когда подключён
// Leader, чтобы вторая реплика пропустила тик, а не продублировала его. Без
// него выполняется напрямую — так и делают однопроцессный деплой и тесты.
func (w *AuctionWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *AuctionWorker) WithLeader(leader *Leader, name string) *AuctionWorker {
	w.guard = leader.Guard(name)
	return w
}
