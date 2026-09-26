package worker

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/service"
)

// slaBatchSize — сколько просроченных заказов воркер понижает за проход.
const slaBatchSize = 100

// slaOrders — то, что SLA-воркеру нужно от сервиса заказов. Правило понижения
// живёт в сервисе; воркер только выбирает заказы и оповещает чат.
type slaOrders interface {
	OverdueUrgentOrders(ctx context.Context, limit int) ([]uuid.UUID, error)
	DowngradeOverdue(ctx context.Context, orderID uuid.UUID) (service.DowngradeResult, error)
}

// systemBroadcaster рассылает системное сообщение в комнату чата заказа.
type systemBroadcaster interface {
	BroadcastSystemMessage(ctx context.Context, orderID uuid.UUID, msg interface{})
}

// SLAWorker автоматически понижает просроченные заказы ASAP/URGENT.
type SLAWorker struct {
	orders slaOrders
	chat   systemBroadcaster
	guard  Guard
}

// NewSLAWorker создаёт SLAWorker. chat необязателен: без него понижение
// проходит, но открытые чаты об этом не узнают до перечитывания.
func NewSLAWorker(orders slaOrders, chat systemBroadcaster) *SLAWorker {
	return &SLAWorker{orders: orders, chat: chat}
}

// Start периодически выполняет цикл воркера.
func (w *SLAWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "SLAWorker", metric: "sla", guard: w.guard}.
		Start(ctx, interval, w.CheckSLAOverdue)
}

// CheckSLAOverdue понижает просроченные заказы. Каждый заказ — отдельная
// транзакция в сервисе, поэтому сбой одного не мешает остальным, а при
// выключении проход останавливается между заказами.
func (w *SLAWorker) CheckSLAOverdue(ctx context.Context) error {
	ids, err := w.orders.OverdueUrgentOrders(ctx, slaBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := w.orders.DowngradeOverdue(ctx, id)
		if err != nil {
			log.Printf("[SLAWorker] Failed to downgrade order %s: %v", id, err)
			continue
		}
		if !res.Downgraded {
			continue
		}
		log.Printf("[SLAWorker] Downgraded order %s to REGULAR due to delay. Refunded %s.", id, res.Refund)
		if w.chat == nil {
			continue
		}
		// Возврат уже зафиксирован, и уведомление о нём должно дойти, даже если
		// проход застало выключение процесса: отмена ctx его не отменяет.
		w.chat.BroadcastSystemMessage(context.WithoutCancel(ctx), id, map[string]interface{}{
			"type":         "system",
			"action":       "downgrade",
			"is_urgent":    false,
			"is_asap":      false,
			"final_amount": res.FinalAmount,
		})
	}
	return nil
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *SLAWorker) WithLeader(leader *Leader, name string) *SLAWorker {
	w.guard = leader.Guard(name)
	return w
}
