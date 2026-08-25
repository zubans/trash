package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrationFilesAreOrderedByName guards the ordering the runner relies on.
// Two migrations share the 010 prefix, so ordering has to come from the full
// file name, not from a parsed number.
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

// TestBaselineCutoff pins which migrations may be applied to a database that
// already carries the schema: everything before the cutoff was applied by the
// old init scripts or by the runtime DDL, and 001/002 are not idempotent.
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

// TestRealMigrationsAreDiscoverable makes sure the shipped directory is what the
// runner will read.
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
