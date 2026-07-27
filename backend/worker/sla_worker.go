package worker

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/service"
)

// SLAWorker automatically downgrades delayed ASAP/URGENT orders.
type SLAWorker struct {
	db           *sql.DB
	orderService *service.OrderService
	chatService  *service.ChatService
}

// NewSLAWorker creates a new SLAWorker.
func NewSLAWorker(db *sql.DB, orderService *service.OrderService, chatService *service.ChatService) *SLAWorker {
	return &SLAWorker{
		db:           db,
		orderService: orderService,
		chatService:  chatService,
	}
}

// Start runs the worker loop periodically.
func (w *SLAWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := w.CheckSLAOverdue(); err != nil {
				log.Printf("[SLAWorker] Error checking overdue orders: %v", err)
			}
		}
	}()
	log.Printf("[SLAWorker] Background worker started every %v", interval)
}

type overdueOrder struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	VolumeType  string
	SpeedTariff string
	HoldAmount  float64
}

// CheckSLAOverdue scans for overdue orders and updates them.
func (w *SLAWorker) CheckSLAOverdue() error {
	query := `
		SELECT id, customer_id, volume_type, speed_tariff, hold_amount 
		FROM orders 
		WHERE status = 'ASSIGNED' 
		  AND is_downgraded = FALSE 
		  AND deadline_at < now() 
		  AND speed_tariff IN ('ASAP', 'URGENT')`

	rows, err := w.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []overdueOrder
	for rows.Next() {
		var o overdueOrder
		err := rows.Scan(&o.ID, &o.CustomerID, &o.VolumeType, &o.SpeedTariff, &o.HoldAmount)
		if err != nil {
			return err
		}
		list = append(list, o)
	}

	for _, o := range list {
		err := w.downgradeOrder(o)
		if err != nil {
			log.Printf("[SLAWorker] Failed to downgrade order %s: %v", o.ID, err)
		} else {
			log.Printf("[SLAWorker] Downgraded order %s to REGULAR due to delay. Customer refunded.", o.ID)
		}
	}

	return nil
}

func (w *SLAWorker) downgradeOrder(o overdueOrder) error {
	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Calculate price for standard (REGULAR) speed
	basePrice, err := w.orderService.CalculatePrice(o.VolumeType, "REGULAR")
	if err != nil {
		return err
	}

	refund := o.HoldAmount - basePrice
	if refund < 0 {
		refund = 0
	}

	// 2. Update order columns
	_, err = tx.Exec(`
		UPDATE orders 
		SET speed_tariff = 'REGULAR', 
		    final_amount = $1, 
		    is_downgraded = TRUE 
		WHERE id = $2`, basePrice, o.ID)
	if err != nil {
		return err
	}

	// 3. Issue refund if applicable
	if refund > 0 {
		_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, refund, o.CustomerID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			INSERT INTO transactions (user_id, order_id, type, amount, created_at)
			VALUES ($1, $2, 'REFUND', $3, now())`,
			o.CustomerID, o.ID, refund)
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	// 4. Send websocket notification to active rooms
	w.chatService.BroadcastSystemMessage(o.ID, map[string]interface{}{
		"type":         "system",
		"action":       "downgrade",
		"speed_tariff": "REGULAR",
		"final_amount": basePrice,
	})

	return nil
}
