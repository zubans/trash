package worker

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"healthlogin/backend/metrics"
)

// Guard — защита одного прохода: выполняет job под advisory-блокировкой
// задачи или пропускает его, когда блокировку держит другой процесс. Выдаёт
// её Leader.Guard.
type Guard func(ctx context.Context, job func() error) error

// periodic — общий цикл периодической задачи. Раньше каждый воркер держал
// свою копию: тикер без остановки, `for range ticker.C` без контекста и
// восемь одинаковых runGuarded. Здесь всё это один раз: остановка по
// контексту, остановка тикера, защита лидера, метрика прохода и защита от
// паники.
type periodic struct {
	// name — имя воркера в журнале.
	name string
	// metric — лейбл worker в метриках прохода; пустой — проход не считается.
	metric string
	// guard — защита лидера; nil — задача выполняется на каждом процессе.
	guard Guard
	// runAtStart выполняет первый проход сразу, не дожидаясь первого тика.
	runAtStart bool
}

// Start запускает цикл в горутине. Возвращённый канал закрывается, когда цикл
// остановлен и последний начатый проход завершён, — по нему main ждёт воркеры
// при выключении.
func (p periodic) Start(ctx context.Context, interval time.Duration, job func(context.Context) error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx, interval, job)
	}()
	log.Printf("[%s] background worker started every %v", p.name, interval)
	return done
}

// Run выполняет проходы по тикеру до отмены ctx и возвращается, когда цикл
// остановлен. Проход, начатый до отмены, доводится до конца: ctx у него уже
// отменён, и обращения к базе внутри обрываются сами, но цикл не бросает
// проход посередине — он ждёт, пока тот вернётся.
func (p periodic) Run(ctx context.Context, interval time.Duration, job func(context.Context) error) {
	if p.runAtStart {
		p.tick(ctx, job)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// Тик и отмена могли прийти одновременно; после отмены проход не начинается.
		if ctx.Err() != nil {
			return
		}
		p.tick(ctx, job)
	}
}

// tick — один проход: под защитой лидера, с метрикой и с перехватом паники.
func (p periodic) tick(ctx context.Context, job func(context.Context) error) {
	run := func() error {
		safe := func() error { return recovering(func() error { return job(ctx) }) }
		if p.guard == nil {
			return safe()
		}
		return p.guard(ctx, safe)
	}
	var err error
	if p.metric != "" {
		err = metrics.TrackWorker(p.metric, run)
	} else {
		err = run()
	}
	if err != nil {
		log.Printf("[%s] pass failed: %v", p.name, err)
	}
}

// recovering превращает панику прохода в его ошибку: сломанный проход
// считается и логируется как неудавшийся, а не роняет процесс вместе с
// остальными воркерами и HTTP-сервером.
func recovering(job func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	return job()
}

// stopped — «уже остановлен»: его возвращает Start воркера, которому нечего
// делать, чтобы ждущему его main не приходилось это различать.
var stopped = func() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()

// Group — запущенные воркеры, которых main ждёт при выключении.
type Group struct {
	done []<-chan struct{}
}

// Add регистрирует канал остановки воркера.
func (g *Group) Add(done <-chan struct{}) {
	g.done = append(g.done, done)
}

// Wait ждёт остановки всех воркеров. Истёкший ctx прекращает ожидание и
// возвращает его ошибку: процесс выключается и без тех, кто не успел.
func (g *Group) Wait(ctx context.Context) error {
	for _, done := range g.done {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
