package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrationFilesAreOrderedByName охраняет порядок, на который опирается
// раннер. Две миграции делят префикс 010, поэтому порядок обязан браться из
// полного имени файла, а не из разобранного числа.
func TestMigrationFilesAreOrderedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"010_ratings_and_reviews.sql",
		"002_create_tables.sql",
		"010_create_chats_for_existing_orders.sql",
		"readme.md",
		"024_security_hardening.sql",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	files, err := migrationFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"002_create_tables.sql",
		"010_create_chats_for_existing_orders.sql",
		"010_ratings_and_reviews.sql",
		"024_security_hardening.sql",
	}
	if len(files) != len(want) {
		t.Fatalf("expected %d migrations, got %d: %v", len(want), len(files), files)
	}
	for i, f := range files {
		if got := filepath.Base(f); got != want[i] {
			t.Errorf("position %d: expected %s, got %s", i, want[i], got)
		}
	}
}

// TestBaselineCutoff фиксирует, какие миграции можно применять к базе, уже
// несущей схему: всё до отсечки применили старые init-скрипты или DDL времени
// выполнения, а 001/002 не идемпотентны.
func TestBaselineCutoff(t *testing.T) {
	cases := map[string]bool{
		"001_create_enums.sql":               true,
		"023_pending_email_verification.sql": true,
		"024_security_hardening.sql":         false,
		"025_consolidate_runtime_ddl.sql":    false,
	}
	for version, shouldBaseline := range cases {
		if got := version < baselineBefore; got != shouldBaseline {
			t.Errorf("%s: expected baseline=%v, got %v", version, shouldBaseline, got)
		}
	}
}

// TestRealMigrationsAreDiscoverable убеждается, что поставляемый каталог — это
// то, что раннер и прочитает.
func TestRealMigrationsAreDiscoverable(t *testing.T) {
	files, err := migrationFiles("../migrations")
	if err != nil {
		t.Fatalf("unexpected error reading migrations: %v", err)
	}
	if len(files) < 25 {
		t.Errorf("expected the full migration set, found %d files", len(files))
	}
	if base := filepath.Base(files[0]); base != "001_create_enums.sql" {
		t.Errorf("expected the enum migration first, got %s", base)
	}
}
