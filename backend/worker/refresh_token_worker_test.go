package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingCleaner struct {
	calls atomic.Int64
	// ctxCancelled запоминает, пришёл ли проход с уже отменённым контекстом.
	ctxCancelled atomic.Bool
}

func (c *countingCleaner) CleanupExpiredRefreshTokens(ctx context.Context) {
	c.calls.Add(1)
	if ctx.Err() != nil {
		c.ctxCancelled.Store(true)
	}
}

// Уборка идёт сразу при старте, через защиту лидера, и останавливается по контексту.
func TestRefreshTokenWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cleaner := &countingCleaner{}
	var guarded atomic.Int64
	w := NewRefreshTokenWorker(cleaner)
	w.guard = func(_ context.Context, job func() error) error {
		guarded.Add(1)
		return job()
	}
	done := w.Start(ctx, time.Hour)

	waitFor(t, "the first cleanup", func() bool { return cleaner.calls.Load() == 1 })
	if guarded.Load() != 1 {
		t.Errorf("the cleanup ran outside the leader guard: %d guarded passes", guarded.Load())
	}
	if cleaner.ctxCancelled.Load() {
		t.Error("the cleanup got a cancelled context")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not stop after cancel")
	}
	if cleaner.calls.Load() != 1 {
		t.Errorf("cleanup ran %d times, want the start-up pass only", cleaner.calls.Load())
	}
}

// Без сервиса воркер нечего делать: он сразу остановлен, и main его не ждёт.
func TestRefreshTokenWorkerWithoutCleaner(t *testing.T) {
	done := NewRefreshTokenWorker(nil).Start(context.Background(), time.Hour)
	select {
	case <-done:
	default:
		t.Error("a worker without a cleaner reported itself running")
	}
}
