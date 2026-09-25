package achievement_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"healthlogin/backend/achievement"
	"healthlogin/backend/achievements"
)

// engineWithLibrary поднимает движок с поставляемыми скриптами — теми же, что
// поедут в продакшн. Тест на выдуманном скрипте проверял бы только сам себя.
func engineWithLibrary(t *testing.T) *achievement.Engine {
	t.Helper()
	e := achievement.New(achievement.DefaultLimits)
	if err := e.Load(achievements.FS, "embedded"); err != nil {
		t.Fatalf("load embedded achievements: %v", err)
	}
	return e
}

func executorFacts(event string, now time.Time) achievement.Facts {
	return achievement.Facts{
		Event: event,
		Now:   now,
		User:  &achievement.Actor{ID: "executor-1", Role: "EXECUTOR", Roles: []string{"EXECUTOR"}},
		Stats: &achievement.Stats{},
		Order: &achievement.OrderFacts{
			ID:          "order-1",
			Status:      "COMPLETED",
			CustomerID:  "customer-1",
			ExecutorID:  "executor-1",
			Amount:      1000,
			CreatedAt:   now.Add(-10 * time.Minute),
			ConfirmedAt: now,
		},
	}
}

func TestLibraryCompiles(t *testing.T) {
	e := engineWithLibrary(t)
	for _, code := range []string{"first_order", "fastest_gun", "marathon_month"} {
		m, ok := e.Manifest(code)
		if !ok {
			t.Fatalf("achievement %q is not loaded", code)
		}
		if m.Audience != achievement.AudienceExecutor {
			t.Errorf("%s: audience = %q, want EXECUTOR", code, m.Audience)
		}
		if len(m.Events) == 0 {
			t.Errorf("%s: declares no events", code)
		}
		if m.Weight <= 0 {
			t.Errorf("%s: weight = %d, want a positive default", code, m.Weight)
		}
	}
}

func TestFastestGunGrantsInsideTheWindow(t *testing.T) {
	e := engineWithLibrary(t)
	now := time.Now()
	f := executorFacts("order.confirmed", now)

	grant, err := e.Check("fastest_gun", f)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if grant == nil {
		t.Fatal("a ten-minute order should have been granted")
	}
	if grant.Points != 5 {
		t.Errorf("points = %d, want 5", grant.Points)
	}
	// Ключ выдачи обязан быть заказом: без него повторяемая ачивка начислила бы
	// баллы заново на каждой переотправке события.
	if grant.Key != "order-1" {
		t.Errorf("key = %q, want the order id", grant.Key)
	}
	if len(grant.Effects) != 1 || grant.Effects[0].Kind != achievement.EffectNotify {
		t.Errorf("effects = %+v, want a single notify", grant.Effects)
	}
}

func TestFastestGunIgnoresSlowOrders(t *testing.T) {
	e := engineWithLibrary(t)
	now := time.Now()

	slow := executorFacts("order.confirmed", now)
	slow.Order.CreatedAt = now.Add(-40 * time.Minute)
	if grant, err := e.Check("fastest_gun", slow); err != nil || grant != nil {
		t.Errorf("a forty-minute order was granted: grant=%v err=%v", grant, err)
	}

	// Порога суммы у поставляемой ачивки больше нет: единственный на платформе
	// живёт в настройке и проверяется ядром. Дешёвый заказ скрипт пропускает.
	cheap := executorFacts("order.confirmed", now)
	cheap.Order.Amount = 100
	if grant, err := e.Check("fastest_gun", cheap); err != nil || grant == nil {
		t.Errorf("a cheap order was refused by the script: grant=%v err=%v", grant, err)
	}

	// А вот админ вправе быть строже ядра — тем же ключом, каким настраивает
	// любую другую константу ачивки.
	guarded := executorFacts("order.confirmed", now)
	guarded.Order.Amount = 100
	guarded.Config = map[string]interface{}{"min_order_amount": 300}
	if grant, err := e.Check("fastest_gun", guarded); err != nil || grant != nil {
		t.Errorf("the configured floor was ignored: grant=%v err=%v", grant, err)
	}

	// Заказ, где этот человек — заказчик, а не исполнитель.
	foreign := executorFacts("order.confirmed", now)
	foreign.Order.ExecutorID = "executor-2"
	if grant, err := e.Check("fastest_gun", foreign); err != nil || grant != nil {
		t.Errorf("somebody else's order was granted: grant=%v err=%v", grant, err)
	}
}

func TestFirstOrderNeedsACompletedOrder(t *testing.T) {
	e := engineWithLibrary(t)
	now := time.Now()

	first := executorFacts("order.confirmed", now)
	first.Stats.OrdersCompleted = 1
	grant, err := e.Check("first_order", first)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if grant == nil {
		t.Fatal("the first completed order should have been granted")
	}
	// Разовая ачивка ключа не называет: его подставит ядро, и уникальный индекс
	// отклонит повтор.
	if grant.Key != "" {
		t.Errorf("key = %q, want empty for a once-per-user achievement", grant.Key)
	}

	// Счётчик, ушедший дальше единицы, ачивку не отменяет: ачивку могли включить
	// человеку с историей или пересчитать ему агрегаты. Один раз она всё равно
	// выдастся один — это решают ключ выдачи и уникальный индекс, а не правило.
	veteran := executorFacts("order.confirmed", now)
	veteran.Stats.OrdersCompleted = 42
	if grant, err := e.Check("first_order", veteran); err != nil || grant == nil {
		t.Errorf("a veteran's first grant was refused: grant=%v err=%v", grant, err)
	}

	// А вот заказов вовсе без единого выполненного не бывает: событие о
	// подтверждении уже учтено в агрегате, и ноль здесь означает, что считает
	// его кто-то другой.
	fresh := executorFacts("order.confirmed", now)
	fresh.Stats.OrdersCompleted = 0
	if grant, err := e.Check("first_order", fresh); err != nil || grant != nil {
		t.Errorf("granted without a completed order: grant=%v err=%v", grant, err)
	}
}

func TestMarathonKeyIsTheCalendarMonth(t *testing.T) {
	e := engineWithLibrary(t)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	f := executorFacts("order.confirmed", now)
	f.Stats.OrdersCompletedMonth = 50

	grant, err := e.Check("marathon_month", f)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if grant == nil {
		t.Fatal("fifty orders in a month should have been granted")
	}
	if grant.Key != "month:2026-09" {
		t.Errorf("key = %q, want month:2026-09", grant.Key)
	}
	// Подарок не выдаётся, пока заказчиков мало: пятьдесят заказов от одного
	// человека — это не марафон, а сговор.
	for _, effect := range grant.Effects {
		if effect.Kind == achievement.EffectGift {
			t.Error("a gift was granted with too few distinct customers")
		}
	}
}

// Скрипт не может назначить комиссию: такого конструктора в его окружении нет.
// Проверяется компиляцией, потому что именно так это и должно проявляться — на
// сохранении скрипта, а не при выдаче.
func TestScriptCannotTouchCommission(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	err := compileOne(e, "greedy", "achievement.star", []byte(`
MANIFEST = {"title": "x", "audience": "EXECUTOR", "events": ["order.confirmed"]}

def check(f):
    return grant(points = 5, effects = [commission_discount(points = 100)])
`))
	if err == nil {
		t.Fatal("a script calling commission_discount compiled")
	}
	if !strings.Contains(err.Error(), "commission_discount") {
		t.Errorf("error = %v, want it to name the undefined builtin", err)
	}
}

func TestAudienceIsRequired(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	err := compileOne(e, "nameless", "achievement.star", []byte(`
MANIFEST = {"title": "x", "events": ["order.confirmed"]}

def check(f):
    return None
`))
	if err == nil {
		t.Fatal("an achievement without an audience compiled")
	}
}

// Ачивка, не объявившая событий, никогда бы не сработала — молча. Отказ на
// компиляции переводит это в ошибку, которую видно на сохранении.
func TestEventsAreRequired(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	err := compileOne(e, "silent", "achievement.star", []byte(`
MANIFEST = {"title": "x", "audience": "EXECUTOR"}

def check(f):
    return None
`))
	if err == nil {
		t.Fatal("an achievement without events compiled")
	}
}

func TestProgressIsClamped(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	if err := compileOne(e, "eager", "achievement.star", []byte(`
MANIFEST = {"title": "x", "audience": "EXECUTOR", "events": ["order.confirmed"]}

def check(f):
    return None

def progress(f):
    return 7.0
`)); err != nil {
		t.Fatalf("compile: %v", err)
	}
	value, ok, err := e.Progress("eager", executorFacts("order.confirmed", time.Now()))
	if err != nil || !ok {
		t.Fatalf("progress: value=%v ok=%v err=%v", value, ok, err)
	}
	if value != 1 {
		t.Errorf("progress = %v, want it clamped to 1", value)
	}
}

// compileOne регистрирует однофайловую ачивку: тестам хватает одного файла,
// а загрузчик и админ-панель всегда идут через CompileFiles.
func compileOne(e *achievement.Engine, code, filename string, src []byte) error {
	return e.CompileFiles(code, []achievement.SourceFile{{Name: filename, Src: src}})
}

// Ачивка с неверным манифестом не остаётся в рантайме: иначе Has отвечал бы
// «да» на то, чего Manifest не находит.
func TestInvalidManifestIsNotRegistered(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	if err := compileOne(e, "silent", "achievement.star", []byte(`
MANIFEST = {"title": "x", "audience": "EXECUTOR"}

def check(f):
    return None
`)); err == nil {
		t.Fatal("an achievement without events compiled")
	}
	if e.Has("silent") {
		t.Error("a rejected achievement stayed registered")
	}
	if _, ok := e.Manifest("silent"); ok {
		t.Error("a rejected achievement has a manifest")
	}
}

// Регистрация, снятие и перечисление идут параллельно — как правка ачивки в
// админ-панели во время синхронизации и чтения списка. Манифест выводится из
// рантайма, поэтому перечисленное и найденное по коду не могут разойтись;
// гонок быть не должно (go test -race).
func TestManifestsStayConsistentUnderConcurrentUse(t *testing.T) {
	e := achievement.New(achievement.DefaultLimits)
	src := []byte(`
MANIFEST = {"title": "t", "audience": "EXECUTOR", "events": ["order.confirmed"], "weight": 7}

def check(f):
    return None
`)
	const workers = 4
	const rounds = 50

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(2)
		code := fmt.Sprintf("ach-%d", w)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := compileOne(e, code, "achievement.star", src); err != nil {
					t.Errorf("compile %s: %v", code, err)
					return
				}
				e.Remove(code)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				for _, m := range e.Manifests() {
					if m.Code == "" || m.Audience != achievement.AudienceExecutor || m.Weight != 7 {
						t.Errorf("listed manifest is not derived properly: %+v", m)
						return
					}
				}
				if m, ok := e.Manifest(code); ok && (m.Audience != achievement.AudienceExecutor || m.Weight != 7) {
					t.Errorf("manifest by code is not derived properly: %+v", m)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got := e.Manifests(); len(got) != 0 {
		t.Errorf("after every Remove the engine still lists %d achievements", len(got))
	}
	if err := compileOne(e, "kept", "achievement.star", src); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := e.Manifests(); len(got) != 1 || got[0].Code != "kept" {
		t.Errorf("Manifests() = %+v, want the one registered achievement", got)
	}
	if _, ok := e.Manifest("kept"); !ok {
		t.Error("Manifest() does not see what Manifests() lists")
	}
}
