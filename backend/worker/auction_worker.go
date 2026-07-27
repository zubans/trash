package worker

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
)

// AuctionWorker automatically cancels construction orders that remain unmatched for 7 days.
type AuctionWorker struct {
	db *sql.DB
}

// NewAuctionWorker creates a new AuctionWorker.
func NewAuctionWorker(db *sql.DB) *AuctionWorker {
	return &AuctionWorker{db: db}
}

// Start runs the worker loop periodically.
func (w *AuctionWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := w.CheckExpiredAuctions(); err != nil {
				log.Printf("[AuctionWorker] Error checking expired auctions: %v", err)
			}
		}
	}()
	log.Printf("[AuctionWorker] Background worker started every %v", interval)
}

type expiredAuction struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	HoldAmount float64
}

// CheckExpiredAuctions selects and cancels expired construction orders.
func (w *AuctionWorker) CheckExpiredAuctions() error {
	query := `
		SELECT id, customer_id, hold_amount 
		FROM orders 
		WHERE status = 'SEARCHING' 
		  AND volume_type = 'CONSTRUCTION' 
		  AND created_at < now() - INTERVAL '7 days'`

	rows, err := w.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []expiredAuction
	for rows.Next() {
		var a expiredAuction
		err := rows.Scan(&a.ID, &a.CustomerID, &a.HoldAmount)
		if err != nil {
			return err
		}
		list = append(list, a)
	}

	for _, a := range list {
		err := w.cancelAuction(a)
		if err != nil {
			log.Printf("[AuctionWorker] Failed to cancel auction %s: %v", a.ID, err)
		} else {
			log.Printf("[AuctionWorker] Canceled expired construction auction %s.", a.ID)
		}
	}

	return nil
}

func (w *AuctionWorker) cancelAuction(a expiredAuction) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Set status to CANCELED
	_, err = tx.Exec(`UPDATE orders SET status = 'CANCELED' WHERE id = $1`, a.ID)
	if err != nil {
		return err
	}

	// 2. Refund client if holdAmount > 0
	if a.HoldAmount > 0 {
		_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, a.HoldAmount, a.CustomerID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			INSERT INTO transactions (user_id, order_id, type, amount, created_at)
			VALUES ($1, $2, 'REFUND', $3, now())`,
			a.CustomerID, a.ID, a.HoldAmount)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
