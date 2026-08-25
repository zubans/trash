package worker

import (
	"log"
	"time"

	"healthlogin/backend/repository"
)

// ReconcileWorker periodically checks that stored balances still agree with the
// transaction log. It only reports: repairing a balance automatically would
// paper over the bug that caused the drift, and the difference is somebody's
// money either way.
type ReconcileWorker struct {
	repo      repository.ReconciliationRepository
	tolerance float64
}

// NewReconcileWorker creates a ReconcileWorker.
func NewReconcileWorker(repo repository.ReconciliationRepository, tolerance float64) *ReconcileWorker {
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
	for _, t := range report.UnknownTypes {
		log.Printf("[ALERT] transaction type %q is not covered by the ledger sign convention; every sum below is unreliable", t)
	}
	for _, d := range report.Discrepancies {
		log.Printf("[ALERT] user %s (%s): balance %.2f vs ledger %.2f, difference %+.2f",
			d.UserID, d.Phone, d.Balance, d.Ledger, d.Difference)
	}
	for _, a := range report.HoldAnomalies {
		log.Printf("[ALERT] order %s (%s): hold %.2f — %s", a.OrderID, a.Status, a.HoldAmount, a.Reason)
	}
}
