package repository

import (
	"testing"
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
