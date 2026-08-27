package repository

import (
	"testing"
	"time"
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

// TestServiceNodeFilter_Where pins the predicates the tree queries rely on:
// deleted nodes drop out unless they are asked for by name.
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
