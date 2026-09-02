package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// ReconcileWorker периодически проверяет, что сохранённые балансы всё ещё
// согласуются с журналом транзакций. Он только сообщает: автоматический ремонт
// баланса замазал бы баг, вызвавший расхождение, а разница — в любом случае
// чьи-то деньги.
type ReconcileWorker struct {
	repo      repository.ReconciliationRepository
	tolerance money.Amount
	guard func(func() error) error
}

// NewReconcileWorker создаёт ReconcileWorker.
func NewReconcileWorker(repo repository.ReconciliationRepository, tolerance money.Amount) *ReconcileWorker {
	return &ReconcileWorker{repo: repo, tolerance: tolerance}
}

// Start выполняет проход сразу, а затем на каждом интервале.
func (w *ReconcileWorker) Start(interval time.Duration) {
	go func() {
		w.runGuarded()
		ticker := time.NewTicker(interval)
		for range ticker.C {
			w.runGuarded()
		}
	}()
	log.Printf("[ReconcileWorker] Balance reconciliation scheduled every %v", interval)
}

// Run выполняет один проход и логирует исход.
func (w *ReconcileWorker) Run() {
	started := time.Now()
	report, err := w.repo.Reconcile(context.Background(), w.tolerance)
	metrics.WorkerRun("reconcile", time.Since(started), err)
	if err != nil {
		metrics.ReconcileFailed()
		log.Printf("[ReconcileWorker] reconciliation failed: %v", err)
		return
	}

	metrics.ReconcileReport(
		report.OK(),
		len(report.Discrepancies),
		len(report.HoldAnomalies),
		len(report.UnknownTypes),
		report.Books.Difference.Rubles(),
		report.Books.EscrowDrift.Rubles(),
	)

	if report.OK() {
		log.Printf("[ReconcileWorker] %s", report.Summary())
		return
	}

	// Громко и с достаточной детализацией, чтобы действовать, не открывая клиент базы.
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

// runGuarded выполняет один проход под advisory-блокировкой задачи, когда
// подключён Leader. Проход только читает и сообщает, поэтому дубль безвреден —
// но он поднял бы тот же алерт дважды, а этот шум никому не нужен.
func (w *ReconcileWorker) runGuarded() {
	if w.guard == nil {
		w.Run()
		return
	}
	_ = w.guard(func() error {
		w.Run()
		return nil
	})
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *ReconcileWorker) WithLeader(leader *Leader, name string) *ReconcileWorker {
	w.guard = leader.Guard(name)
	return w
}
