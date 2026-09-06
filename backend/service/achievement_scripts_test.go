package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"healthlogin/backend/achievement"
	"healthlogin/backend/achievements"
	"healthlogin/backend/repository"
)

// Ачивку теперь можно написать в админ-панели, и тесты ниже проверяют границу
// между такой ачивкой и поставляемой: чью строку можно удалить, чей скрипт
// можно переписать и что происходит с движком, когда строка исчезает.

// scriptCatalog — каталог ачивок в памяти, которому важен только скрипт.
type scriptCatalog struct {
	repository.AchievementRepository
	rows []*repository.Achievement
}

func (c *scriptCatalog) ListWithScript(ctx context.Context) ([]*repository.Achievement, error) {
	out := make([]*repository.Achievement, 0, len(c.rows))
	for _, row := range c.rows {
		if row.HasOwnScript() && row.DeletedAt == nil {
			out = append(out, row)
		}
	}
	return out, nil
}

const customScript = `
MANIFEST = {
    "title": "Своя ачивка",
    "audience": "EXECUTOR",
    "events": ["order.confirmed"],
    "once_per_user": True,
    "weight": 15,
}

def check(f):
    return None
`

func libraryEngine(t *testing.T) *achievement.Engine {
	t.Helper()
	engine := achievement.New(achievement.DefaultLimits)
	if err := engine.Load(achievements.FS, "embedded"); err != nil {
		t.Fatalf("load library: %v", err)
	}
	return engine
}

func TestCustomAchievementCompilesAndDisappearsWhenArchived(t *testing.T) {
	engine := libraryEngine(t)
	row := &repository.Achievement{Code: "my_award", Source: customScript}
	catalog := &scriptCatalog{rows: []*repository.Achievement{row}}
	scripts := NewAchievements(engine, catalog)

	if err := scripts.SyncAll(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	manifest, ok := engine.Manifest("my_award")
	if !ok {
		t.Fatal("a custom achievement was not compiled")
	}
	if manifest.Title != "Своя ачивка" || manifest.Weight != 15 {
		t.Errorf("manifest = %+v, want the title and weight from the script", manifest)
	}

	// Заархивированная ачивка должна исчезнуть и из движка, иначе она
	// продолжила бы срабатывать на этом процессе до перезапуска.
	now := time.Now()
	row.DeletedAt = &now
	if err := scripts.SyncAll(context.Background()); err != nil {
		t.Fatalf("sync after archiving: %v", err)
	}
	if engine.Has("my_award") {
		t.Error("an archived achievement is still loaded")
	}
}

// Поставляемая ачивка живёт в бинарнике, и синхронизация из базы не должна её
// трогать: иначе первый же проход выгружал бы всё, чего в базе нет.
func TestSyncKeepsLibraryAchievements(t *testing.T) {
	engine := libraryEngine(t)
	scripts := NewAchievements(engine, &scriptCatalog{})

	if err := scripts.SyncAll(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	for _, code := range []string{"first_order", "fastest_gun", "marathon_month"} {
		if !engine.Has(code) {
			t.Errorf("library achievement %q was unloaded by a sync", code)
		}
		if !scripts.IsLibrary(code) {
			t.Errorf("%q is not recognised as a library achievement", code)
		}
	}
	if scripts.IsLibrary("my_award") {
		t.Error("a code that ships with nothing is reported as library")
	}
}

// Сломанный скрипт отклоняется до сохранения: ачивка, чей скрипт не
// компилируется, молча перестала бы выдаваться, и заметили бы это по её
// отсутствию через неделю.
func TestBrokenScriptIsRefusedBeforeItIsSaved(t *testing.T) {
	scripts := NewAchievements(libraryEngine(t), &scriptCatalog{})

	err := scripts.Validate(&repository.Achievement{Code: "broken", Source: `
MANIFEST = {"title": "x", "audience": "EXECUTOR", "events": ["order.confirmed"]}

def check(f)
    return None
`})
	if err == nil {
		t.Fatal("a script with a syntax error validated")
	}
	// Ошибка Starlark называет файл и строку — её и показывают админу.
	if !strings.Contains(err.Error(), "achievement.star") {
		t.Errorf("error = %v, want it to name the file", err)
	}
}

// Ачивка без аудитории или без событий никогда бы не сработала — молча.
// Проверка живёт в компиляции, поэтому её видно на сохранении.
func TestValidateRejectsAnAchievementThatCouldNeverFire(t *testing.T) {
	scripts := NewAchievements(libraryEngine(t), &scriptCatalog{})

	noEvents := scripts.Validate(&repository.Achievement{Code: "silent", Source: `
MANIFEST = {"title": "x", "audience": "EXECUTOR"}

def check(f):
    return None
`})
	if noEvents == nil {
		t.Error("an achievement without events validated")
	}

	noAudience := scripts.Validate(&repository.Achievement{Code: "nameless", Source: `
MANIFEST = {"title": "x", "events": ["order.confirmed"]}

def check(f):
    return None
`})
	if noAudience == nil {
		t.Error("an achievement without an audience validated")
	}
}

// Правка применяется к следующему событию на этом процессе, не дожидаясь
// таймера: иначе админ сохраняет и не понимает, почему ничего не изменилось.
func TestSyncAppliesAnEditImmediately(t *testing.T) {
	engine := libraryEngine(t)
	scripts := NewAchievements(engine, &scriptCatalog{})

	row := &repository.Achievement{Code: "my_award", Source: customScript}
	if err := scripts.Sync(row); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if manifest, _ := engine.Manifest("my_award"); manifest.Weight != 15 {
		t.Fatalf("weight = %d, want 15", manifest.Weight)
	}

	row.Source = strings.Replace(customScript, `"weight": 15`, `"weight": 40`, 1)
	if err := scripts.Sync(row); err != nil {
		t.Fatalf("sync after edit: %v", err)
	}
	if manifest, _ := engine.Manifest("my_award"); manifest.Weight != 40 {
		t.Errorf("weight = %d after the edit, want 40", manifest.Weight)
	}
}
