package achievement_test

import (
	"strings"
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

func TestFastestGunIgnoresSlowAndCheapOrders(t *testing.T) {
	e := engineWithLibrary(t)
	now := time.Now()

	slow := executorFacts("order.confirmed", now)
	slow.Order.CreatedAt = now.Add(-40 * time.Minute)
	if grant, err := e.Check("fastest_gun", slow); err != nil || grant != nil {
		t.Errorf("a forty-minute order was granted: grant=%v err=%v", grant, err)
	}

	cheap := executorFacts("order.confirmed", now)
	cheap.Order.Amount = 100
	if grant, err := e.Check("fastest_gun", cheap); err != nil || grant != nil {
		t.Errorf("a 100 ruble order was granted: grant=%v err=%v", grant, err)
	}

	// Заказ, где этот человек — заказчик, а не исполнитель.
	foreign := executorFacts("order.confirmed", now)
	foreign.Order.ExecutorID = "executor-2"
	if grant, err := e.Check("fastest_gun", foreign); err != nil || grant != nil {
		t.Errorf("somebody else's order was granted: grant=%v err=%v", grant, err)
	}
}

func TestFirstOrderGrantsOnlyOnTheFirst(t *testing.T) {
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

	second := executorFacts("order.confirmed", now)
	second.Stats.OrdersCompleted = 2
	if grant, err := e.Check("first_order", second); err != nil || grant != nil {
		t.Errorf("the second order was granted: grant=%v err=%v", grant, err)
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
	err := e.Compile("greedy", "achievement.star", []byte(`
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
	err := e.Compile("nameless", "achievement.star", []byte(`
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
	err := e.Compile("silent", "achievement.star", []byte(`
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
	if err := e.Compile("eager", "achievement.star", []byte(`
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
