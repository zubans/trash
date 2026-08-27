package worker

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// SLAWorker automatically downgrades delayed ASAP/URGENT orders.
type SLAWorker struct {
	db           *sql.DB
	orderService *service.OrderService
	chatService  *service.ChatService
	ledger       *service.Ledger
}

// NewSLAWorker creates a new SLAWorker. The ledger is required: the downgrade
// refunds part of the hold, and a refund that does not come out of the escrow
// account is exactly the one-sided movement the ledger exists to prevent.
func NewSLAWorker(db *sql.DB, orderService *service.OrderService, chatService *service.ChatService, ledger *service.Ledger) *SLAWorker {
	return &SLAWorker{
		db:           db,
		orderService: orderService,
		chatService:  chatService,
		ledger:       ledger,
	}
}

// Start runs the worker loop periodically.
func (w *SLAWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("sla", w.CheckSLAOverdue); err != nil {
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
			metrics.OrderEvent("downgraded")
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

	// 3. Issue refund if applicable.
	//
	// Out of escrow, through the ledger. The hold is reduced to basePrice just
	// above, so escrow must give up exactly the difference; the raw UPDATE this
	// replaces credited the customer and left escrow holding money that no
	// order claimed any more, which is one of the ways the platform books came
	// to differ from the sum of user balances.
	if refund.IsPositive() {
		if err := w.ledger.Release(tx, repository.AccountEscrow, o.CustomerID, refund, repository.TransactionTypeRefund, &o.ID, nil); err != nil {
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
