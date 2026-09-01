package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// BehaviorWorker drains the domain event outbox into the behaviour scripts.
//
// It runs often — the events it carries are things a user is waiting for: an
// order that closes by itself once the customer is verified, and the reward
// that goes with it.
type BehaviorWorker struct {
	dispatcher *service.BehaviorDispatcher
	guard      func(func() error) error
}

// NewBehaviorWorker creates a BehaviorWorker.
func NewBehaviorWorker(dispatcher *service.BehaviorDispatcher) *BehaviorWorker {
	return &BehaviorWorker{dispatcher: dispatcher}
}

// WithLeader makes this worker run at most once across every process. It pays
// money, so a second process running the same batch is exactly the duplication
// the lock exists to prevent — the effect keys would catch it, but a guard that
// stops the work is better than a constraint that undoes it.
func (w *BehaviorWorker) WithLeader(leader *Leader, name string) *BehaviorWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start runs the dispatch loop.
func (w *BehaviorWorker) Start(interval time.Duration) {
	if w.dispatcher == nil {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			tick := func() error {
				return w.runGuarded(func() error {
					return w.dispatcher.Tick(context.Background())
				})
			}
			if err := metrics.TrackWorker("behavior_dispatch", tick); err != nil {
				log.Printf("[BehaviorWorker] Error dispatching domain events: %v", err)
			}
		}
	}()
	log.Printf("[BehaviorWorker] Background worker started every %v", interval)
}

func (w *BehaviorWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}
