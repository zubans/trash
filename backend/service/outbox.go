package service

import (
	"context"
	"log"
	"sync"
	"time"

	"healthlogin/backend/repository"
)

// outboxConsumer — общий цикл потребителя outbox доменных событий. Раньше
// диспетчеры поведений и ачивок держали по копии: та же пачка в 50, те же 10
// попыток, тот же порядок «обработать → пометить», и копии уже разошлись
// (подрезка истории была только у поведений). Здесь цикл один, а диспетчеру
// остаётся ровно его дело — что делать с одним событием.
type outboxConsumer struct {
	events repository.EventRepository
	// consumer — имя курсора в outbox: у каждого потребителя свой.
	consumer string
	// tag — префикс в журнале.
	tag string
	// batchSize ограничивает один тик; maxAttempts ограничивает жизнь одного
	// события, чтобы постоянно падающее событие перестало занимать пачку, а не
	// блокировало навсегда все события за собой.
	batchSize   int
	maxAttempts int
	// handle применяет одно событие. Ошибка оставляет событие необработанным
	// до следующего тика, вплоть до maxAttempts.
	handle func(ctx context.Context, event *repository.DomainEvent) error
	// observe считает исход события в метриках: "processed" или "failed".
	observe func(eventType, outcome string)
	// purge — подрезка обработанной истории; nil — история не подрезается
	// этим потребителем.
	purge *historyPurge
}

// historyPurge подрезает обработанные события не чаще, чем every, оставляя
// последние retention. Окно намного длиннее любой переотправки, поэтому ключ
// идемпотентности не исчезает, пока его событие ещё может вернуться.
type historyPurge struct {
	retention time.Duration
	every     time.Duration
	mu        sync.Mutex
	last      time.Time
}

// Tick обрабатывает одну пачку ожидающих событий. Вызывается по таймеру
// воркером под защитой лидера.
func (c *outboxConsumer) Tick(ctx context.Context) error {
	if c == nil || c.events == nil {
		return nil
	}
	events, err := c.events.ClaimPending(ctx, c.consumer, c.batchSize, c.maxAttempts)
	if err != nil {
		return err
	}
	for _, event := range events {
		// Ошибка одного события не останавливает пачку: она записана в outbox,
		// и следующий тик повторит только это событие.
		_ = c.settle(ctx, event, c.handle(ctx, event))
	}
	if c.purge != nil {
		c.purge.run(ctx, c.events, c.tag)
	}
	return nil
}

// settle помечает исход обработки одного события и возвращает ошибку
// обработки как есть. Помечает и успех, и неудачу: событие, оставшееся
// занятым без отметки, вернётся в пачку по таймауту, а не по решению.
func (c *outboxConsumer) settle(ctx context.Context, event *repository.DomainEvent, err error) error {
	if err != nil {
		c.observe(event.Type, "failed")
		log.Printf("[%s] event %s (%s) failed: %v", c.tag, event.ID, event.Type, err)
		// Причина сохраняется, чтобы её можно было прочитать, не копаясь в логах.
		_ = c.events.MarkFailed(ctx, c.consumer, event.ID, err.Error())
		return err
	}
	c.observe(event.Type, "processed")
	if err := c.events.MarkProcessed(ctx, c.consumer, event.ID); err != nil {
		log.Printf("[%s] event %s applied but not marked processed: %v", c.tag, event.ID, err)
	}
	return nil
}

// CountPending — сколько событий ждут этого потребителя. Читается датчиком
// на каждом процессе, а не из тика под блокировкой лидера: реплика без
// блокировки иначе публиковала бы замёрзшее значение.
func (c *outboxConsumer) CountPending(ctx context.Context) (int, error) {
	if c == nil || c.events == nil {
		return 0, nil
	}
	return c.events.CountPending(ctx, c.consumer)
}

// run подрезает историю, если пришло время. Сбой логируется, и больше ничего:
// медленно растущая таблица — не повод прекращать диспетчеризацию.
func (p *historyPurge) run(ctx context.Context, events repository.EventRepository, tag string) {
	p.mu.Lock()
	due := time.Since(p.last) >= p.every
	if due {
		p.last = time.Now()
	}
	p.mu.Unlock()
	if !due {
		return
	}
	if removed, err := events.PurgeProcessed(ctx, p.retention); err != nil {
		log.Printf("[%s] cannot trim processed events: %v", tag, err)
	} else if removed > 0 {
		log.Printf("[%s] trimmed %d processed events older than %s", tag, removed, p.retention)
	}
}
