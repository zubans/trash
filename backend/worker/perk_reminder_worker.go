package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/service"
)

// PerkReminderWorker напоминает о конце привилегии магазина за три дня, если
// за ней в очереди ничего нет.
//
// Проход идёт раз в десять минут, а не раз в час: запрос дешёвый, а общее
// правило BackgroundWorkerStalled ждёт от короткого воркера успеха раз в
// пятнадцать минут. Почасовой пришлось бы вносить в исключения правила, иначе
// он будил бы дежурного после каждого здорового прохода.
type PerkReminderWorker struct {
	shop  *service.PerkGrants
	guard Guard
}

// NewPerkReminderWorker создаёт PerkReminderWorker.
func NewPerkReminderWorker(shop *service.PerkGrants) *PerkReminderWorker {
	return &PerkReminderWorker{shop: shop}
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех процессов.
func (w *PerkReminderWorker) WithLeader(leader *Leader, name string) *PerkReminderWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start периодически выполняет проход.
func (w *PerkReminderWorker) Start(ctx context.Context, interval time.Duration) <-chan struct{} {
	return periodic{name: "PerkReminderWorker", metric: "perk_reminder", guard: w.guard}.
		Start(ctx, interval, w.Run)
}

// Run — один проход.
func (w *PerkReminderWorker) Run(ctx context.Context) error {
	if w.shop == nil {
		return nil
	}
	sent, err := w.shop.SendPerkReminders(ctx)
	if sent > 0 {
		log.Printf("[PerkReminderWorker] %d perk reminders sent", sent)
	}
	return err
}
