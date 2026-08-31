package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// ShiftWorker periodically checks for and auto-closes expired shifts.
type ShiftWorker struct {
	shiftService *service.ShiftService
	guard        func(func() error) error
}

// NewShiftWorker creates a new ShiftWorker.
func NewShiftWorker(shiftService *service.ShiftService) *ShiftWorker {
	return &ShiftWorker{shiftService: shiftService}
}

// Start runs the shift auto-completion background loop.
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

// runGuarded runs one tick under the job's advisory lock when a Leader is
// wired. Closing a shift charges an early-exit penalty, so it must happen once
// no matter how many processes are running.
func (w *ShiftWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// WithLeader makes this worker run at most once across every process.
func (w *ShiftWorker) WithLeader(leader *Leader, name string) *ShiftWorker {
	w.guard = leader.Guard(name)
	return w
}
