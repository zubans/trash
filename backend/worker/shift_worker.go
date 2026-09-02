package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// ShiftWorker периодически ищет и автоматически закрывает истёкшие смены.
type ShiftWorker struct {
	shiftService *service.ShiftService
	guard        func(func() error) error
}

// NewShiftWorker создаёт новый ShiftWorker.
func NewShiftWorker(shiftService *service.ShiftService) *ShiftWorker {
	return &ShiftWorker{shiftService: shiftService}
}

// Start выполняет фоновый цикл автозавершения смен.
func (w *ShiftWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			tick := func() error {
				return w.runGuarded(func() error {
					return w.shiftService.AutoEndExpiredShifts(context.Background())
				})
			}
			if err := metrics.TrackWorker("shift_autoclose", tick); err != nil {
				log.Printf("[ShiftWorker] Error auto-ending expired shifts: %v", err)
			}
		}
	}()
	log.Printf("[ShiftWorker] Background worker started every %v", interval)
}

// runGuarded выполняет один тик под advisory-блокировкой задачи, когда
// подключён Leader. Закрытие смены списывает штраф за ранний уход, поэтому оно
// обязано произойти один раз, сколько бы процессов ни работало.
func (w *ShiftWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *ShiftWorker) WithLeader(leader *Leader, name string) *ShiftWorker {
	w.guard = leader.Guard(name)
	return w
}
