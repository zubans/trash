package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AchievementWorker вычерпывает outbox доменных событий в скрипты ачивок и
// публикует число неразобранных денежных инцидентов.
//
// Он работает реже диспетчера поведений: там события несут то, чего кто-то
// ждёт прямо сейчас — закрытие заказа и вознаграждение, — а здесь значок,
// который вполне может появиться минутой позже. Реже — значит дешевле:
// каждый тик читает агрегаты и сводки по каждому субъекту события.
type AchievementWorker struct {
	dispatcher *service.AchievementDispatcher
	incidents  repository.MoneyIncidentRepository
	scripts    *service.Achievements
	guard      func(func() error) error
}

// NewAchievementWorker создаёт AchievementWorker.
func NewAchievementWorker(dispatcher *service.AchievementDispatcher) *AchievementWorker {
	return &AchievementWorker{dispatcher: dispatcher}
}

// WithIncidents заставляет воркер публиковать датчик открытых денежных
// инцидентов. Датчик читается из таблицы, а не считается в процессе, потому что
// на него повешен алерт: алерт про деньги обязан говорить только о том, что
// закоммичено.
func (w *AchievementWorker) WithIncidents(incidents repository.MoneyIncidentRepository) *AchievementWorker {
	w.incidents = incidents
	return w
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
func (w *AchievementWorker) StartScriptSync(interval time.Duration) {
	if w.scripts == nil {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := w.scripts.SyncAll(context.Background()); err != nil {
				log.Printf("[AchievementWorker] Error compiling achievement scripts: %v", err)
			}
		}
	}()
	log.Printf("[AchievementWorker] Achievement script sync started every %v", interval)
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех
// процессов. Он выдаёт подарки, а значит платит деньги: ключи выдач поймали бы
// второй процесс, но защита, останавливающая работу, лучше.
func (w *AchievementWorker) WithLeader(leader *Leader, name string) *AchievementWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start выполняет цикл диспетчеризации.
func (w *AchievementWorker) Start(interval time.Duration) {
	if w.dispatcher == nil {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			tick := func() error {
				return w.runGuarded(func() error {
					ctx := context.Background()
					if err := w.dispatcher.Tick(ctx); err != nil {
						return err
					}
					w.publishIncidents(ctx)
					return nil
				})
			}
			if err := metrics.TrackWorker("achievement_dispatch", tick); err != nil {
				log.Printf("[AchievementWorker] Error dispatching domain events: %v", err)
			}
		}
	}()
	log.Printf("[AchievementWorker] Background worker started every %v", interval)
}

func (w *AchievementWorker) publishIncidents(ctx context.Context) {
	if w.incidents == nil {
		return
	}
	open, err := w.incidents.CountOpen(ctx)
	if err != nil {
		// Датчик остаётся при прежнем значении: не сумев прочитать таблицу,
		// сказать «инцидентов нет» было бы хуже, чем не сказать ничего.
		log.Printf("[AchievementWorker] cannot count open money incidents: %v", err)
		return
	}
	metrics.SetMoneyIncidentsOpen(open)
}

func (w *AchievementWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}
