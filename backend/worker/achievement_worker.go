package worker

import (
	"context"
	"time"

	"healthlogin/backend/service"
)

// AchievementWorker вычерпывает outbox доменных событий в скрипты ачивок.
//
// Он работает реже диспетчера поведений: там события несут то, чего кто-то
// ждёт прямо сейчас — закрытие заказа и вознаграждение, — а здесь значок,
// который вполне может появиться минутой позже. Реже — значит дешевле:
// каждый тик читает агрегаты и сводки по каждому субъекту события.
type AchievementWorker struct {
	dispatcher *service.AchievementDispatcher
	scripts    *service.Achievements
	guard      Guard
}

// NewAchievementWorker создаёт AchievementWorker.
func NewAchievementWorker(dispatcher *service.AchievementDispatcher) *AchievementWorker {
	return &AchievementWorker{dispatcher: dispatcher}
}

// WithScriptSync заставляет воркер по таймеру перекомпилировать ачивки,
// написанные в админ-панели. Правка админа применяется к обслужившему
// сохранение процессу сразу; так она доходит до остальных, и так вообще
// подхватывается изменение, сделанное прямо в базе.
//
// Он работает на каждом процессе и намеренно не охраняется блокировкой лидера:
// компиляция — локальная работа, и каждому процессу нужна своя копия результата.
func (w *AchievementWorker) WithScriptSync(scripts *service.Achievements) *AchievementWorker {
	w.scripts = scripts
	return w
}

// StartScriptSync выполняет цикл пересинхронизации скриптов ачивок.
func (w *AchievementWorker) StartScriptSync(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.scripts == nil {
		return stopped
	}
	return periodic{name: "AchievementScriptSync"}.Start(ctx, interval, w.scripts.SyncAll)
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех
// процессов. Он выдаёт подарки, а значит платит деньги: ключи выдач поймали бы
// второй процесс, но защита, останавливающая работу, лучше.
func (w *AchievementWorker) WithLeader(leader *Leader, name string) *AchievementWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start выполняет цикл диспетчеризации.
func (w *AchievementWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.dispatcher == nil {
		return stopped
	}
	return periodic{name: "AchievementWorker", metric: "achievement_dispatch", guard: w.guard}.
		Start(ctx, interval, w.dispatcher.Tick)
}
