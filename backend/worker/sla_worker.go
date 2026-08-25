package worker

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
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
	ID               uuid.UUID
	CustomerID       uuid.UUID
	ServiceVariantID uuid.UUID
	HoldAmount       money.Amount
}

// CheckSLAOverdue scans for overdue orders and updates them.
func (w *SLAWorker) CheckSLAOverdue() error {
	query := `
		SELECT id, customer_id, service_variant_id, hold_amount 
		FROM orders 
		WHERE status = 'ASSIGNED' 
		  AND is_downgraded = FALSE 
		  AND deadline_at < now() 
		  AND (is_urgent = TRUE OR is_asap = TRUE)`

	rows, err := w.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []overdueOrder
	for rows.Next() {
		var o overdueOrder
		err := rows.Scan(&o.ID, &o.CustomerID, &o.ServiceVariantID, &o.HoldAmount)
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

	// 1. Calculate base (non-urgent) price for the variant.
	basePrice, err := w.orderService.CalculatePrice(o.ServiceVariantID, false, false, false)
	if err != nil {
		return err
	}
	if basePrice > o.HoldAmount {
		// Never raise the hold retroactively; the customer only authorised the
		// amount that was taken at order time.
		basePrice = o.HoldAmount
	}

	refund := o.HoldAmount.Sub(basePrice)
	if refund.IsNegative() {
		refund = money.Zero
	}

	// 2. Update order columns. hold_amount must follow the refund: the payout at
	// confirmation time is derived from the hold, so leaving the original urgent
	// hold in place would pay the executor the full urgent price after the
	// customer has already been refunded the difference.
	res, err := tx.Exec(`
		UPDATE orders
		SET is_urgent = FALSE, is_asap = FALSE, final_amount = $1, hold_amount = $1, is_downgraded = TRUE
		WHERE id = $2 AND status = 'ASSIGNED' AND is_downgraded = FALSE`, basePrice, o.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Another worker or a confirmation already moved this order on.
		return nil
	}

	// 3. Issue refund if applicable
	if refund.IsPositive() {
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
		"is_urgent":    false,
		"is_asap":      false,
		"final_amount": basePrice,
	})

	return nil
}
