package service

import (
	"errors"
	"testing"
	"time"
)

// Кэш фасетов: значение живёт TTL, разные ключи независимы, ошибка загрузки
// не запоминается.
func TestFacetCache(t *testing.T) {
	now := time.Now()
	cache := newFacetCache[[]string](time.Minute)
	cache.now = func() time.Time { return now }

	calls := 0
	load := func() ([]string, error) {
		calls++
		return []string{"a"}, nil
	}
	for i := 0; i < 3; i++ {
		if got, err := cache.get("k", load); err != nil || len(got) != 1 {
			t.Fatalf("get: %v %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("loaded %d times within TTL, want 1", calls)
	}

	if _, err := cache.get("other", load); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("another key must load separately, loaded %d times", calls)
	}

	now = now.Add(2 * time.Minute)
	if _, err := cache.get("k", load); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("expired value must reload, loaded %d times", calls)
	}

	failing := func() ([]string, error) { return nil, errors.New("db down") }
	if _, err := cache.get("bad", failing); err == nil {
		t.Fatal("load error must surface")
	}
	if _, err := cache.get("bad", load); err != nil || calls != 4 {
		t.Fatalf("failed load must not be cached: err %v, calls %d", err, calls)
	}
}
