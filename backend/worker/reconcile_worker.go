package worker

import (
	"log"
	"time"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ReconcileWorker periodically checks that stored balances still agree with the
// transaction log. It only reports: repairing a balance automatically would
// paper over the bug that caused the drift, and the difference is somebody's
// money either way.
type ReconcileWorker struct {
	repo      repository.ReconciliationRepository
	tolerance money.Amount
}

// NewReconcileWorker creates a ReconcileWorker.
func NewReconcileWorker(repo repository.ReconciliationRepository, tolerance money.Amount) *ReconcileWorker {
	return &ReconcileWorker{repo: repo, tolerance: tolerance}
}

// Start runs a pass immediately and then on every interval.
func (w *ReconcileWorker) Start(interval time.Duration) {
	go func() {
		w.Run()
		ticker := time.NewTicker(interval)
		for range ticker.C {
			w.Run()
		}
	}()
	log.Printf("[ReconcileWorker] Balance reconciliation scheduled every %v", interval)
}

// Run performs one pass and logs the outcome.
func (w *ReconcileWorker) Run() {
	report, err := w.repo.Reconcile(w.tolerance)
	if err != nil {
		log.Printf("[ReconcileWorker] reconciliation failed: %v", err)
		return
	}

	if report.OK() {
		log.Printf("[ReconcileWorker] %s", report.Summary())
		return
	}

	// Loud, and with enough detail to act on without opening a database client.
	log.Printf("[ALERT] %s", report.Summary())
	if report.BooksOpen {
		log.Printf("[ALERT] users hold %s, platform accounts hold %s: the two sides differ by %s",
			report.Books.UserTotal, report.Books.AccountTotal, report.Books.Difference)
	}
	if report.EscrowMismatch {
		log.Printf("[ALERT] escrow holds %s but live orders account for %s (%s)",
			report.Books.EscrowHeld, report.Books.LiveOrderSum, report.Books.EscrowDrift)
	}
	for _, t := range report.UnknownTypes {
		log.Printf("[ALERT] transaction type %q is not covered by the ledger sign convention; every sum below is unreliable", t)
	}
	for _, d := range report.Discrepancies {
		log.Printf("[ALERT] user %s (%s): balance %s vs ledger %s, difference %s",
			d.UserID, d.Phone, d.Balance, d.Ledger, d.Difference)
	}
	for _, a := range report.HoldAnomalies {
		log.Printf("[ALERT] order %s (%s): hold %s — %s", a.OrderID, a.Status, a.HoldAmount, a.Reason)
	}
}
