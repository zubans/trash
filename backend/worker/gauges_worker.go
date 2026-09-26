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
	// backlogs — очереди потребителей outbox: имя датчика и откуда его читать.
	backlogs []backlogGauge
}

// pendingCounter — очередь одного потребителя outbox. Ему удовлетворяют
// диспетчеры поведений и ачивок.
type pendingCounter interface {
	CountPending(ctx context.Context) (int, error)
}

type backlogGauge struct {
	source pendingCounter
	set    func(pending int)
}

// NewGaugeWorker создаёт GaugeWorker.
func NewGaugeWorker() *GaugeWorker {
	return &GaugeWorker{}
}

// WithBehaviorBacklog подключает датчик очереди поведений. Раньше его
// публиковал тик диспетчера под блокировкой лидера — реплика без блокировки
// показывала замёрзшее значение.
func (w *GaugeWorker) WithBehaviorBacklog(source pendingCounter) *GaugeWorker {
	w.backlogs = append(w.backlogs, backlogGauge{source: source, set: metrics.SetBehaviorBacklog})
	return w
}

// WithAchievementBacklog — то же для очереди ачивок.
func (w *GaugeWorker) WithAchievementBacklog(source pendingCounter) *GaugeWorker {
	w.backlogs = append(w.backlogs, backlogGauge{source: source, set: metrics.SetAchievementBacklog})
	return w
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
	if w.incidents == nil && len(w.backlogs) == 0 {
		return stopped
	}
	return periodic{name: "GaugeWorker", metric: "ops_gauges"}.Start(ctx, interval, w.Run)
}

// Run — один проход. При ошибке чтения датчик остаётся при прежнем значении:
// не сумев прочитать таблицу, сказать «инцидентов нет» было бы хуже, чем не
// сказать ничего. Первая ошибка не мешает остальным датчикам обновиться.
func (w *GaugeWorker) Run(ctx context.Context) error {
	var first error
	if w.incidents != nil {
		if open, err := w.incidents.CountOpen(ctx); err != nil {
			first = err
		} else {
			metrics.SetMoneyIncidentsOpen(open)
		}
	}
	for _, g := range w.backlogs {
		pending, err := g.source.CountPending(ctx)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		g.set(pending)
	}
	return first
}
