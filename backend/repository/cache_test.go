package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- кэш настроек ---

type countingSettingsRepo struct {
	values map[string]string
	reads  int
}

func (r *countingSettingsRepo) GetSettings(ctx context.Context) (map[string]string, error) {
	r.reads++
	out := make(map[string]string, len(r.values))
	for k, v := range r.values {
		out[k] = v
	}
	return out, nil
}

func (r *countingSettingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.values[k] = v
	}
	return nil
}

func TestSettingsCacheServesRepeatReadsFromMemory(t *testing.T) {
	inner := &countingSettingsRepo{values: map[string]string{"fine_amount": "500"}}
	repo := NewCachedSettingsRepository(inner, time.Minute)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		got, err := repo.GetSettings(ctx)
		if err != nil {
			t.Fatalf("GetSettings: %v", err)
		}
		if got["fine_amount"] != "500" {
			t.Fatalf("fine_amount = %q, want 500", got["fine_amount"])
		}
	}
	if inner.reads != 1 {
		t.Errorf("inner repository read %d times, want 1", inner.reads)
	}
}

// Админ, меняющий тариф, должен увидеть его действие уже на следующем заказе, а
// не когда случайно истечёт TTL.
func TestSettingsCacheUpdateIsVisibleImmediately(t *testing.T) {
	inner := &countingSettingsRepo{values: map[string]string{"fine_amount": "500"}}
	repo := NewCachedSettingsRepository(inner, time.Minute)
	ctx := context.Background()

	if _, err := repo.GetSettings(ctx); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if err := repo.UpdateSettings(ctx, map[string]string{"fine_amount": "900"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	got, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got["fine_amount"] != "900" {
		t.Errorf("fine_amount = %q after update, want 900", got["fine_amount"])
	}
}

// Кэш выдаёт копию: вызывающий, правящий возвращённую карту, не должен уметь
// изменить то, что видят все прочие читатели.
func TestSettingsCacheReturnsCopy(t *testing.T) {
	inner := &countingSettingsRepo{values: map[string]string{"fine_amount": "500"}}
	repo := NewCachedSettingsRepository(inner, time.Minute)
	ctx := context.Background()

	first, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	first["fine_amount"] = "tampered"
	delete(first, "fine_amount")

	second, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if second["fine_amount"] != "500" {
		t.Errorf("fine_amount = %q after a caller mutated its copy, want 500", second["fine_amount"])
	}
}

func TestSettingsCacheDisabledWithZeroTTL(t *testing.T) {
	inner := &countingSettingsRepo{values: map[string]string{"a": "1"}}
	repo := NewCachedSettingsRepository(inner, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := repo.GetSettings(ctx); err != nil {
			t.Fatalf("GetSettings: %v", err)
		}
	}
	if inner.reads != 3 {
		t.Errorf("inner read %d times with caching disabled, want 3", inner.reads)
	}
}

// --- кэш каталога ---

// countingCatalogRepo реализует только те методы, которые задействует кэш.
// Встроенный nil-интерфейс заставляет любой другой вызов громко паниковать, а
// не тихо возвращать нулевое значение — именно этого тест ждёт от такого вызова.
type countingCatalogRepo struct {
	ServiceCatalogRepository

	nodes     map[uuid.UUID]*ServiceNode
	byIDCalls int
	batchIDs  [][]uuid.UUID
}

func (r *countingCatalogRepo) GetNodeByID(ctx context.Context, id uuid.UUID) (*ServiceNode, error) {
	r.byIDCalls++
	return r.nodes[id], nil
}

func (r *countingCatalogRepo) GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*ServiceNode, error) {
	r.batchIDs = append(r.batchIDs, ids)
	out := make(map[uuid.UUID]*ServiceNode, len(ids))
	for _, id := range ids {
		if n, ok := r.nodes[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (r *countingCatalogRepo) UpdateNode(ctx context.Context, node *ServiceNode) error {
	r.nodes[node.ID] = node
	return nil
}

func TestCatalogCacheServesRepeatLookupsFromMemory(t *testing.T) {
	id := uuid.New()
	inner := &countingCatalogRepo{nodes: map[uuid.UUID]*ServiceNode{id: {ID: id, Code: "wash"}}}
	repo := NewCachedServiceCatalogRepository(inner, time.Minute)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		node, err := repo.GetNodeByID(ctx, id)
		if err != nil {
			t.Fatalf("GetNodeByID: %v", err)
		}
		if node == nil || node.Code != "wash" {
			t.Fatalf("unexpected node %+v", node)
		}
	}
	if inner.byIDCalls != 1 {
		t.Errorf("inner GetNodeByID called %d times, want 1", inner.byIDCalls)
	}
}

// Пакетное чтение должно спрашивать у базы только те id, которых у него ещё
// нет, — в этом весь смысл связки пакета с кэшем.
func TestCatalogCacheBatchAsksOnlyForMissingIDs(t *testing.T) {
	cached, missing := uuid.New(), uuid.New()
	inner := &countingCatalogRepo{nodes: map[uuid.UUID]*ServiceNode{
		cached:  {ID: cached, Code: "cached"},
		missing: {ID: missing, Code: "missing"},
	}}
	repo := NewCachedServiceCatalogRepository(inner, time.Minute)
	ctx := context.Background()

	if _, err := repo.GetNodeByID(ctx, cached); err != nil {
		t.Fatalf("warm cache: %v", err)
	}

	got, err := repo.GetNodesByIDs(ctx, []uuid.UUID{cached, missing})
	if err != nil {
		t.Fatalf("GetNodesByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d nodes, want 2", len(got))
	}
	if len(inner.batchIDs) != 1 {
		t.Fatalf("inner batch called %d times, want 1", len(inner.batchIDs))
	}
	if len(inner.batchIDs[0]) != 1 || inner.batchIDs[0][0] != missing {
		t.Errorf("inner batch asked for %v, want only the uncached id", inner.batchIDs[0])
	}
}

func TestCatalogCacheUpdateFlushes(t *testing.T) {
	id := uuid.New()
	inner := &countingCatalogRepo{nodes: map[uuid.UUID]*ServiceNode{id: {ID: id, Code: "before"}}}
	repo := NewCachedServiceCatalogRepository(inner, time.Minute)
	ctx := context.Background()

	if _, err := repo.GetNodeByID(ctx, id); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if err := repo.UpdateNode(ctx, &ServiceNode{ID: id, Code: "after"}); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	node, err := repo.GetNodeByID(ctx, id)
	if err != nil {
		t.Fatalf("GetNodeByID: %v", err)
	}
	if node.Code != "after" {
		t.Errorf("code = %q after update, want \"after\" — the cache was not flushed", node.Code)
	}
}

func TestCatalogCacheExpires(t *testing.T) {
	id := uuid.New()
	inner := &countingCatalogRepo{nodes: map[uuid.UUID]*ServiceNode{id: {ID: id, Code: "wash"}}}
	repo := NewCachedServiceCatalogRepository(inner, 10*time.Millisecond)
	ctx := context.Background()

	if _, err := repo.GetNodeByID(ctx, id); err != nil {
		t.Fatalf("first read: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := repo.GetNodeByID(ctx, id); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if inner.byIDCalls != 2 {
		t.Errorf("inner called %d times across the TTL boundary, want 2", inner.byIDCalls)
	}
}
