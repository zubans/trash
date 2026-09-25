package worker

import (
	"context"
	"time"

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
	guard      Guard
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
func (w *BehaviorWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.dispatcher == nil {
		return stopped
	}
	return periodic{name: "BehaviorWorker", metric: "behavior_dispatch", guard: w.guard}.
		Start(ctx, interval, w.dispatcher.Tick)
}

// StartScriptSync выполняет цикл пересинхронизации скриптов узлов.
func (w *BehaviorWorker) StartScriptSync(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.behaviors == nil {
		return stopped
	}
	return periodic{name: "BehaviorScriptSync"}.Start(ctx, interval, w.behaviors.SyncAll)
}
