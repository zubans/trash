package service

import (
	"sync"
	"time"
)

// facetCacheTTL — сколько живут значения фильтров админских списков (типы и
// месяцы проводок, услуги и месяцы заказов). Считаются они DISTINCT-проходом
// по всей таблице, а меняются раз в месяц или при появлении нового типа —
// минуту устаревания на выпадающем списке никто не заметит, а проход по
// таблице на каждой странице замечали.
const facetCacheTTL = time.Minute

// facetCache держит один результат на ключ до истечения TTL. Инвалидации нет
// намеренно: новое значение появится через минуту само, а кто и когда пишет
// в таблицу, кэшу знать незачем.
type facetCache[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]facetEntry[T]
}

type facetEntry[T any] struct {
	value   T
	expires time.Time
}

func newFacetCache[T any](ttl time.Duration) *facetCache[T] {
	return &facetCache[T]{ttl: ttl, now: time.Now, entries: map[string]facetEntry[T]{}}
}

// get отдаёт значение по ключу, вызывая load, когда его нет или оно устарело.
// Ошибка load не кэшируется: следующий запрос попробует снова.
func (c *facetCache[T]) get(key string, load func() (T, error)) (T, error) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && c.now().Before(entry.expires) {
		return entry.value, nil
	}
	value, err := load()
	if err != nil {
		return value, err
	}
	c.mu.Lock()
	c.entries[key] = facetEntry[T]{value: value, expires: c.now().Add(c.ttl)}
	c.mu.Unlock()
	return value, nil
}
