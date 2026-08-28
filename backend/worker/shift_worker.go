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
			if err := metrics.TrackWorker("shift_autoclose", func() error { return w.shiftService.AutoEndExpiredShifts(context.Background()) }); err != nil {
				log.Printf("[ShiftWorker] Error auto-ending expired shifts: %v", err)
			}
		}
	}()
	log.Printf("[ShiftWorker] Background worker started every %v", interval)
}
