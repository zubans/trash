package service

import (
	"context"
	"errors"
	"testing"

	"healthlogin/backend/behavior"
	"healthlogin/backend/script"
)

// fakeScriptEngine считает вызовы компиляции: реестр обязан пропускать
// скрипт, чей текст не менялся, а не компилировать каждый проход заново.
type fakeScriptEngine struct {
	compiled map[string]int
	removed  map[string]int
	broken   map[string]bool
}

func newFakeScriptEngine() *fakeScriptEngine {
	return &fakeScriptEngine{compiled: map[string]int{}, removed: map[string]int{}, broken: map[string]bool{}}
}

func (e *fakeScriptEngine) compile(code string, _ []script.SourceFile) error {
	e.compiled[code]++
	if e.broken[code] {
		return errors.New("syntax error")
	}
	return nil
}

func (e *fakeScriptEngine) remove(code string) { e.removed[code]++ }

func (e *fakeScriptEngine) codes() []string {
	out := []string{}
	for code, n := range e.compiled {
		if n > e.removed[code] {
			out = append(out, code)
		}
	}
	return out
}

func registryOver(e *fakeScriptEngine) *scriptRegistry {
	return newScriptRegistry("test", e.compile, e.remove, e.codes, func(string) bool { return true })
}

func src(code, text string) scriptSource {
	return scriptSource{code: code, label: code, files: []script.SourceFile{{Name: "s.star", Src: []byte(text)}}}
}

func TestSyncAllCompilesOnlyChangedScripts(t *testing.T) {
	engine := newFakeScriptEngine()
	r := registryOver(engine)

	list := []scriptSource{src("a", "x = 1"), src("b", "y = 2")}
	for i := 0; i < 3; i++ {
		if err := r.syncAll(list); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if engine.compiled["a"] != 1 || engine.compiled["b"] != 1 {
		t.Fatalf("unchanged scripts recompiled: %v, want once each", engine.compiled)
	}

	// Правка одного скрипта компилирует только его.
	list[1] = src("b", "y = 3")
	if err := r.syncAll(list); err != nil {
		t.Fatalf("after edit: %v", err)
	}
	if engine.compiled["a"] != 1 || engine.compiled["b"] != 2 {
		t.Errorf("after editing b: %v, want a once and b twice", engine.compiled)
	}

	// Исчезнувший скрипт снимается с регистрации и забывается.
	if err := r.syncAll(list[1:]); err != nil {
		t.Fatalf("after removal: %v", err)
	}
	if engine.removed["a"] != 1 {
		t.Errorf("vanished script removed %d times, want 1", engine.removed["a"])
	}
	if _, ok := r.state["a"]; ok {
		t.Error("vanished script is still remembered")
	}
}

// Сломанный скрипт снимается, а его текст запоминается: он не компилируется
// заново каждый проход, но каждый проход о нём сообщает.
func TestSyncAllRemembersBrokenScripts(t *testing.T) {
	engine := newFakeScriptEngine()
	engine.broken["bad"] = true
	r := registryOver(engine)

	list := []scriptSource{src("bad", "oops")}
	for i := 0; i < 2; i++ {
		if err := r.syncAll(list); err == nil {
			t.Fatalf("pass %d: broken script must be reported", i)
		}
	}
	if engine.compiled["bad"] != 1 {
		t.Errorf("broken script compiled %d times, want 1", engine.compiled["bad"])
	}
	if engine.removed["bad"] != 1 {
		t.Errorf("broken script removed %d times, want 1", engine.removed["bad"])
	}
	// Исправленный текст компилируется снова.
	engine.broken["bad"] = false
	if err := r.syncAll([]scriptSource{src("bad", "fixed")}); err != nil {
		t.Fatalf("fixed script: %v", err)
	}
	if engine.compiled["bad"] != 2 {
		t.Errorf("fixed script compiled %d times, want 2", engine.compiled["bad"])
	}
}

// Sync после сохранения запоминает текст: следующий SyncAll его не трогает.
func TestSyncThenSyncAllDoesNotRecompile(t *testing.T) {
	engine := newFakeScriptEngine()
	r := registryOver(engine)
	one := src("a", "x = 1")
	if err := r.sync(one.code, one.files, true); err != nil {
		t.Fatal(err)
	}
	if err := r.syncAll([]scriptSource{one}); err != nil {
		t.Fatal(err)
	}
	if engine.compiled["a"] != 1 {
		t.Errorf("compiled %d times, want 1", engine.compiled["a"])
	}
}

// То же на настоящем движке поведений: узел, чей скрипт не менялся, не
// компилируется повторно, а библиотечные поведения реестр не трогает.
func TestBehaviorsSyncAllSkipsUnchangedNodes(t *testing.T) {
	node := nodeWithScript(nodeScriptConstants, nodeScriptSource)
	catalog := newMockCatalogRepo()
	catalog.nodes[node.ID] = node
	behaviors := newBehaviorsForTest().WithCatalog(catalog)

	calls := 0
	compile := behaviors.registry.compile
	behaviors.registry.compile = func(code string, files []script.SourceFile) error {
		calls++
		return compile(code, files)
	}
	for i := 0; i < 3; i++ {
		if err := behaviors.SyncAll(context.Background()); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Errorf("node script compiled %d times over three passes, want 1", calls)
	}
	if !behaviors.Engine().Has(behavior.NodeCode(node.ID.String())) {
		t.Error("node script is not registered")
	}
}
