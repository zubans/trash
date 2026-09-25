package worker

import (
	"context"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// GaugeWorker публикует датчики, которые читаются из базы, а не считаются в
// процессе: сейчас это число неразобранных денежных инцидентов.
//
// Раньше датчик инцидентов публиковал воркер ачивок из своего тика — под
// своей блокировкой лидера и только если сам тик прошёл. На реплике, не
// держащей блокировку, датчик замирал на старом значении, а падение тика
// ачивок молча гасило алерт про деньги. Здесь свой тик, на каждом процессе и
// без защиты лидера: датчик — это то, что видит именно этот процесс, и
// Prometheus снимает его с каждого. Цена — один count(*) в полминуты.
type GaugeWorker struct {
	incidents repository.MoneyIncidentRepository
}

// NewGaugeWorker создаёт GaugeWorker.
func NewGaugeWorker() *GaugeWorker {
	return &GaugeWorker{}
}

// WithIncidents подключает датчик открытых денежных инцидентов. Он читается
// из таблицы, потому что на него повешен алерт: алерт про деньги обязан
// говорить только о том, что закоммичено.
func (w *GaugeWorker) WithIncidents(incidents repository.MoneyIncidentRepository) *GaugeWorker {
	w.incidents = incidents
	return w
}

// Start периодически обновляет датчики.
func (w *GaugeWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	if w.incidents == nil {
		return stopped
	}
	return periodic{name: "GaugeWorker", metric: "ops_gauges"}.Start(ctx, interval, w.Run)
}

// Run — один проход. При ошибке чтения датчик остаётся при прежнем значении:
// не сумев прочитать таблицу, сказать «инцидентов нет» было бы хуже, чем не
// сказать ничего.
func (w *GaugeWorker) Run(ctx context.Context) error {
	open, err := w.incidents.CountOpen(ctx)
	if err != nil {
		return err
	}
	metrics.SetMoneyIncidentsOpen(open)
	return nil
}
