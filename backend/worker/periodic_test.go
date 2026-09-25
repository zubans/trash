package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor ждёт условия не дольше пяти секунд.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Отмена контекста останавливает цикл: канал закрывается, проходов больше нет.
func TestPeriodicStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ticks atomic.Int64
	done := periodic{name: "test"}.Start(ctx, time.Millisecond, func(context.Context) error {
		ticks.Add(1)
		return nil
	})
	waitFor(t, "a few ticks", func() bool { return ticks.Load() >= 3 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop after cancel")
	}
	after := ticks.Load()
	time.Sleep(20 * time.Millisecond)
	if ticks.Load() != after {
		t.Errorf("ticks kept coming after the loop stopped: %d -> %d", after, ticks.Load())
	}
}

// Начатый проход доводится до конца: канал закрывается только после него.
func TestPeriodicWaitsForTheRunningPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inside := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	done := periodic{name: "test"}.Start(ctx, time.Millisecond, func(context.Context) error {
		select {
		case inside <- struct{}{}:
			<-release
			finished.Store(true)
		default:
		}
		return nil
	})
	<-inside
	cancel()
	select {
	case <-done:
		t.Fatal("the loop reported itself stopped while a pass was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-done
	if !finished.Load() {
		t.Error("the running pass did not finish before the loop stopped")
	}
}

// Проход идёт через защиту лидера: пропуск защитой — пропуск прохода, а
// ошибка задачи доходит из-под защиты наружу.
func TestPeriodicRunsThroughTheGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var guarded, ran atomic.Int64
	skip := atomic.Bool{}
	skip.Store(true)
	guard := func(_ context.Context, job func() error) error {
		guarded.Add(1)
		if skip.Load() {
			return nil
		}
		return job()
	}
	periodic{name: "test", guard: guard}.Start(ctx, time.Millisecond, func(context.Context) error {
		ran.Add(1)
		return nil
	})
	waitFor(t, "guarded passes", func() bool { return guarded.Load() >= 3 })
	if ran.Load() != 0 {
		t.Fatalf("the job ran %d times while the guard skipped it", ran.Load())
	}
	skip.Store(false)
	waitFor(t, "the job to run once the guard lets it", func() bool { return ran.Load() >= 1 })
}

// Паника прохода не роняет цикл: следующий проход выполняется.
func TestPeriodicRecoversFromPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ticks atomic.Int64
	periodic{name: "test"}.Start(ctx, time.Millisecond, func(context.Context) error {
		if ticks.Add(1) == 1 {
			panic("boom")
		}
		return nil
	})
	waitFor(t, "passes after the panic", func() bool { return ticks.Load() >= 3 })
}

// recovering превращает панику в ошибку и пропускает ошибку задачи как есть.
func TestRecovering(t *testing.T) {
	if err := recovering(func() error { panic("boom") }); err == nil {
		t.Error("a panic did not become an error")
	}
	want := errors.New("job")
	if err := recovering(func() error { return want }); !errors.Is(err, want) {
		t.Errorf("err = %v, want the job's error", err)
	}
}

// runAtStart выполняет первый проход сразу, не дожидаясь интервала.
func TestPeriodicRunsAtStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ticks atomic.Int64
	periodic{name: "test", runAtStart: true}.Start(ctx, time.Hour, func(context.Context) error {
		ticks.Add(1)
		return nil
	})
	waitFor(t, "the immediate pass", func() bool { return ticks.Load() == 1 })
}

// Проход получает контекст цикла, а не фоновый.
func TestPeriodicPassesTheContext(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "marker"))
	defer cancel()
	seen := make(chan any, 1)
	periodic{name: "test", runAtStart: true}.Start(ctx, time.Hour, func(ctx context.Context) error {
		select {
		case seen <- ctx.Value(key{}):
		default:
		}
		return nil
	})
	select {
	case v := <-seen:
		if v != "marker" {
			t.Errorf("the pass got a context without the loop's value: %v", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pass never ran")
	}
}

// Group ждёт всех и сдаётся по своему контексту.
func TestGroupWait(t *testing.T) {
	var g Group
	first := make(chan struct{})
	second := make(chan struct{})
	g.Add(first)
	g.Add(second)
	g.Add(stopped)

	close(first)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Wait with a worker still running returned %v, want the deadline", err)
	}
	close(second)
	if err := g.Wait(context.Background()); err != nil {
		t.Errorf("Wait with every worker stopped returned %v", err)
	}
}
