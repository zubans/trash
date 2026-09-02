package repository

import (
	"context"
	"sync"
	"time"
)

// cachedSettingsRepo кэширует таблицу настроек на короткий TTL.
//
// system_settings — несколько строк, меняющихся, когда их правит админ, и
// читаемых на постоянно работающих путях: ценообразование на каждом заказе,
// пределы допуска на каждом принятии, радиус воркера подбора на каждом цикле —
// и часть этого внутри циклов. Каждое чтение было полным сканом таблицы.
//
// Обновление через этот репозиторий немедленно освежает кэш, поэтому админ,
// меняющий тариф, видит его действие на следующем заказе. TTL ограничивает
// устаревание от записей, которых этот процесс не выполнял.
type cachedSettingsRepo struct {
	inner SettingsRepository
	ttl   time.Duration

	mu      sync.RWMutex
	values  map[string]string
	expires time.Time
}

// NewCachedSettingsRepository оборачивает репозиторий настроек короткоживущим
// кэшем. Нулевой ttl выключает кэширование и возвращает внутренний репозиторий.
func NewCachedSettingsRepository(inner SettingsRepository, ttl time.Duration) SettingsRepository {
	if ttl <= 0 {
		return inner
	}
	return &cachedSettingsRepo{inner: inner, ttl: ttl}
}

func (r *cachedSettingsRepo) GetSettings(ctx context.Context) (map[string]string, error) {
	r.mu.RLock()
	cached := r.values
	fresh := cached != nil && time.Now().Before(r.expires)
	r.mu.RUnlock()

	if !fresh {
		loaded, err := r.inner.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.values = loaded
		r.expires = time.Now().Add(r.ttl)
		r.mu.Unlock()
		cached = loaded
	}

	// Вызывающие обходят и индексируют результат, а как минимум один (loadSettings
	// в OrderService) строит из него производную карту. Отдача самой кэшированной
	// карты позволила бы любому из них изменить то, что видят все прочие читатели,
	// поэтому копия здесь не роскошь.
	out := make(map[string]string, len(cached))
	for k, v := range cached {
		out[k] = v
	}
	return out, nil
}

func (r *cachedSettingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	if err := r.inner.UpdateSettings(ctx, settings); err != nil {
		return err
	}
	// Инвалидируем, а не подправляем кэшированную карту только что записанным:
	// UpdateSettings — это upsert подмножества, и перечитывание — единственный
	// способ убедиться, что кэш соответствует таблице.
	r.mu.Lock()
	r.values = nil
	r.mu.Unlock()
	return nil
}
