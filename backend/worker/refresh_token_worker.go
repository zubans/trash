package worker

import (
	"context"
	"time"
)

// RefreshTokenCleaner удаляет refresh-токены, которые уже нельзя обменять. Ему
// удовлетворяет *service.AuthService.
type RefreshTokenCleaner interface {
	CleanupExpiredRefreshTokens(ctx context.Context)
}

// RefreshTokenWorker раз в сутки удаляет истёкшие refresh-токены.
// Использованные хранятся до истечения срока, потому что обнаружение повторов
// должно их узнавать.
//
// Раньше это был `time.Tick` в main, мимо воркеров и защиты лидера, — на
// каждой реплике. Под защитой уборку делает один процесс; проход — один
// DELETE, поэтому в метриках воркеров он не считается: суточный воркер
// пришлось бы вносить в исключения правила BackgroundWorkerStalled.
type RefreshTokenWorker struct {
	cleaner RefreshTokenCleaner
	guard   Guard
}

// NewRefreshTokenWorker создаёт RefreshTokenWorker.
func NewRefreshTokenWorker(cleaner RefreshTokenCleaner) *RefreshTokenWorker {
	return &RefreshTokenWorker{cleaner: cleaner}
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех процессов.
func (w *RefreshTokenWorker) WithLeader(leader *Leader, name string) *RefreshTokenWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start выполняет уборку сразу, а затем на каждом интервале.
func (w *RefreshTokenWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.cleaner == nil {
		return stopped
	}
	return periodic{name: "RefreshTokenWorker", guard: w.guard, runAtStart: true}.
		Start(ctx, interval, w.Run)
}

// Run — один проход. Ошибку уборки сервис логирует сам.
func (w *RefreshTokenWorker) Run(ctx context.Context) error {
	w.cleaner.CleanupExpiredRefreshTokens(ctx)
	return nil
}
