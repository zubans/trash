package repository

import (
	"context"
	"sync"
	"time"
)

// cachedSettingsRepo caches the settings table for a short TTL.
//
// system_settings is a handful of rows that change when an admin edits them,
// and it is read on paths that run constantly: pricing on every order, the
// eligibility limits on every accept, the matching worker's radius on every
// cycle — several of those inside loops. Each read was a full table scan.
//
// An update through this repository refreshes the cache immediately, so an
// admin who changes a tariff sees it apply to the next order. The TTL bounds
// staleness from writes this process did not perform.
type cachedSettingsRepo struct {
	inner SettingsRepository
	ttl   time.Duration

	mu      sync.RWMutex
	values  map[string]string
	expires time.Time
}

// NewCachedSettingsRepository wraps a settings repository with a short-lived
// cache. A ttl of zero disables caching and returns the inner repository.
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

	// Callers iterate and index the result, and at least one (OrderService's
	// loadSettings) builds a derived map from it. Handing out the cached map
	// itself would let any of them mutate what every other reader sees, so the
	// copy is not optional.
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
	// Invalidate rather than patch the cached map with what was just written:
	// UpdateSettings is an upsert of a subset, and re-reading is the only way to
	// be sure the cache matches the table.
	r.mu.Lock()
	r.values = nil
	r.mu.Unlock()
	return nil
}
