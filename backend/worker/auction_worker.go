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

// AuctionWorker automatically cancels auction orders that remain unmatched for 7 days.
type AuctionWorker struct {
	db           *sql.DB
	orderService *service.OrderService
}

// NewAuctionWorker creates a new AuctionWorker.
func NewAuctionWorker(db *sql.DB, orderService *service.OrderService) *AuctionWorker {
	return &AuctionWorker{db: db, orderService: orderService}
}

// Start runs the worker loop periodically.
func (w *AuctionWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("auction", w.CheckExpiredAuctions); err != nil {
				log.Printf("[AuctionWorker] Error checking expired auctions: %v", err)
			}
		}
	}()
	log.Printf("[AuctionWorker] Background worker started every %v", interval)
}

type expiredAuction struct {
	ID uuid.UUID
}

// CheckExpiredAuctions selects and cancels expired auction orders.
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
		// One correct cancel path, under a row lock, instead of this worker's
		// own raw SQL — which credited the customer without ever debiting
		// escrow and left hold_amount standing.
		//
		// CancelUnclaimedAuction rather than CancelOrder: an auction holds no
		// money until a bid is accepted, and accepting one is exactly what
		// moves it to ASSIGNED. A request that reached ASSIGNED between the
		// scan above and this line has been claimed and is no longer expired
		// business — it belongs to the executor who won it.
		err := w.orderService.CancelUnclaimedAuction(context.Background(), a.ID)
		if err != nil {
			log.Printf("[AuctionWorker] Failed to cancel auction %s: %v", a.ID, err)
		} else {
			log.Printf("[AuctionWorker] Canceled expired auction %s.", a.ID)
		}
	}

	return nil
}
