package repository

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// migrationsTable records which migration files have been applied.
const migrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// baselineBefore is the first migration this runner is allowed to apply to a
// database that already contains the application schema. Everything before it
// was applied either by the docker-entrypoint init scripts or by the ad-hoc DDL
// that used to live in the repository constructors, and files 001 and 002 are
// not idempotent, so re-running them on a populated database would fail.
const baselineBefore = "024_"

// noTransactionMarker lets a migration opt out of the wrapping transaction for
// statements Postgres refuses to run inside one.
const noTransactionMarker = "-- +migrate no-transaction"

// Migrate applies every .sql file in dir that has not been applied yet, in file
// name order, and records each one. It replaces both the manual `make migrate`
// step and the DDL that used to run from repository constructors, where errors
// were discarded and the process started against a half-built schema.
func Migrate(db *sql.DB, dir string) error {
	if _, err := db.Exec(migrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := migrationFiles(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		log.Printf("[migrate] no migration files found in %s", dir)
		return nil
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		baselined, err := baselineExistingSchema(db, files)
		if err != nil {
			return err
		}
		for _, v := range baselined {
			applied[v] = struct{}{}
		}
	}

	for _, file := range files {
		version := filepath.Base(file)
		if _, done := applied[version]; done {
			continue
		}
		if err := applyMigration(db, file, version); err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
		log.Printf("[migrate] applied %s", version)
	}
	return nil
}

func migrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func appliedVersions(db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = struct{}{}
	}
	return applied, rows.Err()
}

// baselineExistingSchema marks the historical migrations as applied when the
// schema is already in place but has never been tracked. A brand new database
// has no users table and gets every migration applied from the first one.
func baselineExistingSchema(db *sql.DB, files []string) ([]string, error) {
	var hasSchema bool
	if err := db.QueryRow(`SELECT to_regclass('public.users') IS NOT NULL`).Scan(&hasSchema); err != nil {
		return nil, err
	}
	if !hasSchema {
		return nil, nil
	}

	var baselined []string
	for _, file := range files {
		version := filepath.Base(file)
		if version >= baselineBefore {
			continue
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version); err != nil {
			return nil, err
		}
		baselined = append(baselined, version)
	}
	if len(baselined) > 0 {
		log.Printf("[migrate] existing schema detected: baselined %d migrations up to %s", len(baselined), baselineBefore)
	}
	return baselined, nil
}

func applyMigration(db *sql.DB, path, version string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	statements := string(content)

	if strings.Contains(statements, noTransactionMarker) {
		if _, err := db.Exec(statements); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(statements); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit()
}
