package photoproof_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	skip := t.Skipf
	if os.Getenv("TEST_DB_REQUIRED") != "" {
		skip = t.Fatalf
	}
	if dsn == "" {
		skip("database test: DATABASE_URL / TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Ping(); err != nil {
		skip("cannot ping test db: %v", err)
	}
	if err := repository.Migrate(db, "../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Справочник жестов: правка, мягкое удаление, восстановление, постоянный номер
// и уникальность кода среди действующих.
func TestSymbolCRUD(t *testing.T) {
	db := testDB(t)
	svc := photoproof.NewService(photoproof.NewSymbolRepository(db))
	ctx := context.Background()

	code := "test_" + uuid.New().String()[:8]
	symbol := &photoproof.Symbol{Code: "  " + code + "  ", Title: " Тестовый жест ", Description: "Покажите что-нибудь", SortOrder: 100}
	if err := svc.CreateSymbol(ctx, symbol); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM watermark_symbols WHERE id = $1`, symbol.ID) })
	if symbol.Code != code || symbol.Title != "Тестовый жест" || symbol.Number <= 0 {
		t.Fatalf("created: %+v", symbol)
	}
	number := symbol.Number

	// Код уникален среди действующих.
	if err := svc.CreateSymbol(ctx, &photoproof.Symbol{Code: code, Title: "Другой"}); !errors.Is(err, photoproof.ErrSymbolCodeTaken) {
		t.Fatalf("duplicate code: %v", err)
	}

	symbol.Title = "Переименованный"
	symbol.FitsInSelfie = false
	if err := svc.UpdateSymbol(ctx, symbol); err != nil {
		t.Fatalf("update: %v", err)
	}
	if symbol.Number != number {
		t.Fatalf("number changed on update: %d → %d", number, symbol.Number)
	}

	// Мягкое удаление: из выдачи ушёл, по id читается.
	if err := svc.DeleteSymbol(ctx, symbol.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	live, err := svc.ListSymbols(ctx, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range live {
		if s.ID == symbol.ID {
			t.Fatal("deleted symbol is still offered")
		}
	}
	all, err := svc.ListSymbols(ctx, true)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	var found *photoproof.Symbol
	for i := range all {
		if all[i].ID == symbol.ID {
			found = &all[i]
		}
	}
	if found == nil || found.Live() {
		t.Fatalf("deleted symbol in the full list: %+v", found)
	}
	// Освободившийся код можно занять заново, и номер будет другой.
	replacement := &photoproof.Symbol{Code: code, Title: "Замена"}
	if err := svc.CreateSymbol(ctx, replacement); err != nil {
		t.Fatalf("reuse of a freed code: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM watermark_symbols WHERE id = $1`, replacement.ID) })
	if replacement.Number == number {
		t.Fatalf("number %d reused: old photos would point at the wrong gesture", number)
	}

	if err := svc.RestoreSymbol(ctx, symbol.ID); !errors.Is(err, photoproof.ErrSymbolCodeTaken) {
		t.Fatalf("restore over a taken code: %v", err)
	}
	if err := svc.DeleteSymbol(ctx, replacement.ID); err != nil {
		t.Fatalf("delete replacement: %v", err)
	}
	if err := svc.RestoreSymbol(ctx, symbol.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := svc.DeleteSymbol(ctx, uuid.New()); !errors.Is(err, photoproof.ErrSymbolNotFound) {
		t.Fatalf("delete of a missing symbol: %v", err)
	}
}

// Кривой жест не сохраняется: код по шаблону, название обязательно.
func TestSymbolValidation(t *testing.T) {
	db := testDB(t)
	svc := photoproof.NewService(photoproof.NewSymbolRepository(db))
	ctx := context.Background()

	cases := []struct {
		name   string
		symbol photoproof.Symbol
		want   error
	}{
		{"пустой код", photoproof.Symbol{Title: "Жест"}, photoproof.ErrSymbolCode},
		{"код с пробелом", photoproof.Symbol{Code: "two words", Title: "Жест"}, photoproof.ErrSymbolCode},
		{"код с кириллицей", photoproof.Symbol{Code: "жест", Title: "Жест"}, photoproof.ErrSymbolCode},
		{"без названия", photoproof.Symbol{Code: "valid_code", Title: "   "}, photoproof.ErrSymbolTitle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			symbol := c.symbol
			if err := svc.CreateSymbol(ctx, &symbol); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// Заказу выдаётся случайный действующий жест, и удалённый среди них не
// появляется.
func TestPickSymbol(t *testing.T) {
	db := testDB(t)
	svc := photoproof.NewService(photoproof.NewSymbolRepository(db))
	ctx := context.Background()

	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		symbol, err := svc.PickSymbol(ctx, nil)
		if err != nil {
			t.Fatalf("pick: %v", err)
		}
		if !symbol.Live() {
			t.Fatalf("a deleted gesture was picked: %+v", symbol)
		}
		seen[symbol.Code] = true
	}
	if len(seen) < 2 {
		t.Fatalf("40 draws gave %d distinct gestures: %v", len(seen), seen)
	}
}
