package worker

import (
	"context"
	"time"

	"healthlogin/backend/service"
)

// MatchingWorker периодически запускает автоподбор заказов. Сам подбор живёт
// в service.MatchingService; здесь только цикл — как у всех остальных
// периодических задач, а не собственный `for range ticker.C` внутри сервиса.
type MatchingWorker struct {
	matching *service.MatchingService
	guard    Guard
}

// NewMatchingWorker создаёт MatchingWorker.
func NewMatchingWorker(matching *service.MatchingService) *MatchingWorker {
	return &MatchingWorker{matching: matching}
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех
// процессов. Без защиты два процесса назначали бы одни и те же ждущие заказы,
// а заказ можно назначить лишь однажды — проигравший писал бы ошибку каждый цикл.
func (w *MatchingWorker) WithLeader(leader *Leader, name string) *MatchingWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start периодически выполняет цикл подбора.
func (w *MatchingWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.matching == nil {
		return stopped
	}
	return periodic{name: "MatchingWorker", metric: "matching", guard: w.guard}.
		Start(ctx, interval, w.matching.MatchOrders)
}
