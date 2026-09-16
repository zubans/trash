package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// PenaltyWorker обслуживает штрафное состояние: гасит баллы, по которым давно
// не приходило новых, и снимает тихие блокировки, чей срок вышел.
//
// Оба срока — время, а не событие, поэтому без воркера они не наступают ни для
// кого, кому больше не начисляют баллов, — то есть ровно для тех, кто
// исправился. Начисление балла делает то же самое для своей роли само, так что
// воркер здесь не единственный путь, а тот, что срабатывает в тишине.
type PenaltyWorker struct {
	penalties *service.PenaltyService
	guard     func(func() error) error
}

// NewPenaltyWorker создаёт PenaltyWorker.
func NewPenaltyWorker(penalties *service.PenaltyService) *PenaltyWorker {
	return &PenaltyWorker{penalties: penalties}
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех процессов.
func (w *PenaltyWorker) WithLeader(leader *Leader, name string) *PenaltyWorker {
	w.guard = leader.Guard(name)
	return w
}

func (w *PenaltyWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// Start периодически выполняет проход обслуживания.
func (w *PenaltyWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("penalty_sweep", func() error { return w.runGuarded(w.Run) }); err != nil {
				log.Printf("[PenaltyWorker] sweep failed: %v", err)
			}
		}
	}()
	log.Printf("[PenaltyWorker] Background worker started every %v", interval)
}

// Run — один проход.
func (w *PenaltyWorker) Run() error {
	if w.penalties == nil {
		return nil
	}
	result, err := w.penalties.Sweep(context.Background())
	if err != nil {
		return err
	}
	if result.PointsBurnt > 0 || result.BlocksLifted > 0 {
		log.Printf("[PenaltyWorker] %d penalty points expired, %d silent blocks lifted", result.PointsBurnt, result.BlocksLifted)
	}
	return nil
}
