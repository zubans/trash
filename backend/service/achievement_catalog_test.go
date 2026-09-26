package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/achievement"
	"healthlogin/backend/repository"
)

const catalogTestScript = `
MANIFEST = {"title": "Тест", "audience": "EXECUTOR", "events": ["order.confirmed"], "weight": 10}

def check(f):
    return None
`

// newCatalogHarness — каталог с одной поставляемой ачивкой lib_award.
func newCatalogHarness(t *testing.T) (*AchievementCatalog, *dispatchAchievements) {
	t.Helper()
	engine := achievement.New(achievement.DefaultLimits)
	if err := compileAchievement(engine, "lib_award", "achievement.star", []byte(catalogTestScript)); err != nil {
		t.Fatalf("compile library: %v", err)
	}
	repo := &dispatchAchievements{rows: []*repository.Achievement{{Code: "lib_award", IsActive: true}}}
	scripts := NewAchievements(engine, repo)
	levels := NewLevels(repo, &orderMockSettingsRepo{settings: map[string]string{}})
	return NewAchievementCatalog(repo, nil, levels, engine, scripts), repo
}

// Правила создания живут в сервисе и отвечают классом: негодный код и
// отсутствующий скрипт — ErrValidation, чужой код — конфликт.
func TestAchievementCatalog_CreateRules(t *testing.T) {
	catalog, repo := newCatalogHarness(t)
	ctx := context.Background()
	admin := uuid.New()
	weight := 5

	cases := map[string]struct {
		form AchievementForm
		want error
	}{
		"bad code":       {AchievementForm{Code: "Bad-Code", Source: catalogTestScript}, ErrAchievementCodeInvalid},
		"library code":   {AchievementForm{Code: "lib_award", Source: catalogTestScript}, ErrAchievementCodeLibrary},
		"no script":      {AchievementForm{Code: "own_award"}, ErrAchievementScriptRequired},
		"weight too big": {AchievementForm{Code: "own_award", Source: catalogTestScript, Weight: intPtr(10001)}, ErrValidation},
		"broken script":  {AchievementForm{Code: "own_award", Source: "def check(f) return"}, ErrValidation},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := catalog.Create(ctx, admin, tc.form); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if len(repo.rows) != 1 {
		t.Fatalf("rejected forms reached the repository: %d rows", len(repo.rows))
	}

	row, err := catalog.Create(ctx, admin, AchievementForm{Code: "own_award", Source: catalogTestScript, Weight: &weight, IsActive: true})
	if err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if row.Code != "own_award" || row.Weight == nil || *row.Weight != 5 || len(repo.rows) != 2 {
		t.Fatalf("row not stored as sent: %+v (%d rows)", row, len(repo.rows))
	}
	if _, ok := catalog.engine.Manifest("own_award"); !ok {
		t.Fatal("own script was not compiled into the engine after create")
	}
}

// У поставляемой ачивки правится всё, кроме скрипта, и её нельзя удалить.
func TestAchievementCatalog_LibraryScriptIsProtected(t *testing.T) {
	catalog, repo := newCatalogHarness(t)
	ctx := context.Background()
	admin := uuid.New()

	row, err := catalog.Update(ctx, admin, "lib_award", AchievementForm{IsActive: true, Source: "def check(f): return None", SortOrder: 3})
	if err != nil {
		t.Fatalf("update library: %v", err)
	}
	if row.Source != "" || row.SortOrder != 3 {
		t.Fatalf("library script was overwritten from the form: %+v", row)
	}
	if err := catalog.Delete(ctx, admin, "lib_award"); !errors.Is(err, ErrAchievementLibraryDelete) {
		t.Fatalf("delete library: %v", err)
	}
	if _, err := catalog.Update(ctx, admin, "missing", AchievementForm{}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	_ = repo
}

func intPtr(v int) *int { return &v }
