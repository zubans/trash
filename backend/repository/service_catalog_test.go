package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLocalizedText_ValueAndScan(t *testing.T) {
	lt := LocalizedText{"ru": "Вынос", "en": "Trash"}
	v, err := lt.Value()
	if err != nil {
		t.Fatalf("value error: %v", err)
	}
	var scanned LocalizedText
	if err := scanned.Scan(v); err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if scanned["ru"] != "Вынос" || scanned["en"] != "Trash" {
		t.Errorf("unexpected scanned value: %v", scanned)
	}
}

func TestLocalizedText_ScanNil(t *testing.T) {
	var lt LocalizedText
	if err := lt.Scan(nil); err != nil {
		t.Fatalf("scan nil error: %v", err)
	}
	if lt != nil {
		t.Errorf("expected nil, got %v", lt)
	}
}

func TestServiceNode_IsCategoryAndVariant(t *testing.T) {
	cat := &ServiceNode{NodeType: ServiceNodeTypeCategory}
	if !cat.IsCategory() || cat.IsVariant() {
		t.Error("category node type mismatch")
	}

	variant := &ServiceNode{NodeType: ServiceNodeTypeVariant}
	if !variant.IsVariant() || variant.IsCategory() {
		t.Error("variant node type mismatch")
	}
}

func TestServiceNode_DeletedIsNotOrderable(t *testing.T) {
	now := time.Now()
	live := &ServiceNode{NodeType: ServiceNodeTypeVariant, IsActive: true}
	if live.IsDeleted() || !live.IsOrderable() {
		t.Error("a live active variant must be orderable")
	}

	deleted := &ServiceNode{NodeType: ServiceNodeTypeVariant, IsActive: true, DeletedAt: &now}
	if !deleted.IsDeleted() || deleted.IsOrderable() {
		t.Error("a deleted variant must never be orderable")
	}

	category := &ServiceNode{NodeType: ServiceNodeTypeCategory, IsActive: true}
	if category.IsOrderable() {
		t.Error("a category is not something you can order")
	}
}

// TestServiceNodeFilter_Where фиксирует предикаты, на которые опираются запросы
// по дереву: удалённые узлы выпадают, пока их не запросят явно.
func TestServiceNodeFilter_Where(t *testing.T) {
	cases := map[string]struct {
		filter ServiceNodeFilter
		want   string
	}{
		"live":            {FilterLive, " AND deleted_at IS NULL"},
		"active":          {FilterActive, " AND deleted_at IS NULL AND is_active = TRUE"},
		"include deleted": {ServiceNodeFilter{IncludeDeleted: true}, ""},
	}
	for name, tc := range cases {
		if got := tc.filter.where(""); got != tc.want {
			t.Errorf("%s: expected %q, got %q", name, tc.want, got)
		}
	}

	if got := FilterLive.where("sn."); got != " AND sn.deleted_at IS NULL" {
		t.Errorf("expected the alias to be applied, got %q", got)
	}
}

// Проверка цикла при смене родителя.
//
// Дерево теста повторяет настоящее: категория «Мойка окон» с вариантом
// «2-4 окна» внутри. Именно на нём ломалась правка цены — узел слали с его же
// настоящим родителем, а проверка отвечала «cannot set parent to descendant».
func TestCheckParentCycle(t *testing.T) {
	category := uuid.New()
	variant := uuid.New()

	// Кто внутри чьего поддерева. Ровно то, что отвечает закрытая таблица путей.
	subtree := map[uuid.UUID][]uuid.UUID{
		category: {variant},
	}
	isDescendantOf := func(_ context.Context, ancestor, descendant uuid.UUID) (bool, error) {
		for _, id := range subtree[ancestor] {
			if id == descendant {
				return true, nil
			}
		}
		return false, nil
	}

	ctx := context.Background()

	t.Run("вариант остаётся в своей категории", func(t *testing.T) {
		if err := checkParentCycle(ctx, variant, category, isDescendantOf); err != nil {
			t.Fatalf("правка узла с его настоящим родителем должна проходить, получено: %v", err)
		}
	})

	t.Run("категорию нельзя увести под собственный вариант", func(t *testing.T) {
		err := checkParentCycle(ctx, category, variant, isDescendantOf)
		if !errors.Is(err, ErrServiceNodeParentChild) {
			t.Errorf("ожидался отказ по циклу, получено: %v", err)
		}
	})

	t.Run("узел не может быть родителем самому себе", func(t *testing.T) {
		err := checkParentCycle(ctx, category, category, isDescendantOf)
		if !errors.Is(err, ErrServiceNodeParentSelf) {
			t.Errorf("ожидался отказ «родитель — сам узел», получено: %v", err)
		}
	})

	t.Run("сбой чтения дерева не выдаётся за цикл", func(t *testing.T) {
		boom := errors.New("closure table unavailable")
		err := checkParentCycle(ctx, variant, category, func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
			return false, boom
		})
		if !errors.Is(err, boom) {
			t.Errorf("ожидалась исходная ошибка, получено: %v", err)
		}
	})

	// Отказ по циклу — ошибка запроса, и обработчик отличает её от сбоя сервера
	// по этому признаку: без него правка отвечала 500 вместо 400.
	t.Run("оба отказа опознаются как цикл", func(t *testing.T) {
		if !errors.Is(ErrServiceNodeParentSelf, ErrServiceNodeParentCycle) ||
			!errors.Is(ErrServiceNodeParentChild, ErrServiceNodeParentCycle) {
			t.Error("оба отказа должны опознаваться как ErrServiceNodeParentCycle")
		}
	})
}
