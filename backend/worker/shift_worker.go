package worker

import (
	"context"
	"time"

	"healthlogin/backend/service"
)

// ShiftWorker периодически ищет и автоматически закрывает истёкшие смены.
type ShiftWorker struct {
	shiftService *service.ShiftService
	guard        Guard
}

// NewShiftWorker создаёт новый ShiftWorker.
func NewShiftWorker(shiftService *service.ShiftService) *ShiftWorker {
	return &ShiftWorker{shiftService: shiftService}
}

// Start выполняет фоновый цикл автозавершения смен.
//
// Под защитой лидера: закрытие смены списывает штраф за ранний уход, поэтому
// оно обязано произойти один раз, сколько бы процессов ни работало.
func (w *ShiftWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "ShiftWorker", metric: "shift_autoclose", guard: w.guard}.
		Start(ctx, interval, w.shiftService.AutoEndExpiredShifts)
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *ShiftWorker) WithLeader(leader *Leader, name string) *ShiftWorker {
	w.guard = leader.Guard(name)
	return w
}
