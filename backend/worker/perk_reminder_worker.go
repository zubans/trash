package worker

import (
	"context"
	"log"
	"time"

	"healthlogin/backend/metrics"
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
	shop  *service.ShopService
	guard func(func() error) error
}

// NewPerkReminderWorker создаёт PerkReminderWorker.
func NewPerkReminderWorker(shop *service.ShopService) *PerkReminderWorker {
	return &PerkReminderWorker{shop: shop}
}

// WithLeader заставляет воркер выполняться не более одного раза среди всех процессов.
func (w *PerkReminderWorker) WithLeader(leader *Leader, name string) *PerkReminderWorker {
	w.guard = leader.Guard(name)
	return w
}

// Start периодически выполняет проход.
func (w *PerkReminderWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			job := w.Run
			if w.guard != nil {
				job = func() error { return w.guard(w.Run) }
			}
			if err := metrics.TrackWorker("perk_reminder", job); err != nil {
				log.Printf("[PerkReminderWorker] pass failed: %v", err)
			}
		}
	}()
	log.Printf("[PerkReminderWorker] Background worker started every %v", interval)
}

// Run — один проход.
func (w *PerkReminderWorker) Run() error {
	if w.shop == nil {
		return nil
	}
	sent, err := w.shop.SendPerkReminders(context.Background())
	if sent > 0 {
		log.Printf("[PerkReminderWorker] %d perk reminders sent", sent)
	}
	return err
}
