package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/service"
)

// BehaviorWorker вычерпывает outbox доменных событий в скрипты поведений.
//
// Он работает часто: события, которые он несёт, — то, чего ждёт пользователь.
// Заказ, закрывающий себя сам, когда заказчик верифицирован, и идущее с этим
// вознаграждение.
type BehaviorWorker struct {
	dispatcher *service.BehaviorDispatcher
	behaviors  *service.Behaviors
	guard      func(func() error) error
}

// NewBehaviorWorker создаёт BehaviorWorker.
func NewBehaviorWorker(dispatcher *service.BehaviorDispatcher) *BehaviorWorker {
	return &BehaviorWorker{dispatcher: dispatcher}
}

// WithScriptSync заставляет воркер по таймеру перекомпилировать скрипты,
// хранящиеся на узлах каталога. Правка админа применяется к обслужившему
// сохранение процессу сразу; так она доходит до остальных, и так вообще
// подхватывается изменение, сделанное прямо в базе.
//
// Он работает на каждом процессе и намеренно не охраняется блокировкой лидера:
// компиляция — локальная работа, и каждому процессу нужна своя копия результата.
func (w *BehaviorWorker) WithScriptSync(behaviors *service.Behaviors) *BehaviorWorker {
	w.behaviors = behaviors
	return w
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех
// процессов. Он платит деньги, поэтому второй процесс, обрабатывающий ту же
// пачку, — ровно то дублирование, ради предотвращения которого блокировка и
// существует: ключи эффектов поймали бы его, но защита, останавливающая работу, лучше.
func (w *BehaviorWorker) WithLeader(leader *Leader, name string) *BehaviorWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start выполняет цикл диспетчеризации.
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

// StartScriptSync выполняет цикл пересинхронизации скриптов узлов.
func (w *BehaviorWorker) StartScriptSync(interval time.Duration) {
	if w.behaviors == nil {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := w.behaviors.SyncAll(context.Background()); err != nil {
				log.Printf("[BehaviorWorker] Error compiling node scripts: %v", err)
			}
		}
	}()
	log.Printf("[BehaviorWorker] Node script sync started every %v", interval)
}

func (w *BehaviorWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}
