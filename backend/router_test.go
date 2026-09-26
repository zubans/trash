package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/handler"
	"healthlogin/backend/middleware"
	"healthlogin/backend/photoproof"
)

// testHandlers собирает обработчики без сервисов: регистрация маршрутов их не
// вызывает, а тесту нужна только таблица маршрутов.
func testHandlers() appHandlers {
	return appHandlers{
		public:               handler.NewPublicHandler(nil),
		passport:             handler.NewPassportHandler(nil),
		admin:                handler.NewAdminHandler(nil),
		profile:              handler.NewProfileHandler(nil),
		wallet:               handler.NewWalletHandler(nil),
		roles:                handler.NewRoleHandler(nil),
		orders:               handler.NewOrderHandler(nil),
		executorVerification: handler.NewExecutorVerificationHandler(nil),
		shifts:               handler.NewShiftHandler(nil),
		bids:                 handler.NewBidHandler(nil, nil),
		chat:                 handler.NewChatHandler(nil, ""),
		shop:                 handler.NewShopHandler(nil, nil, nil, nil, nil, ""),
		geo:                  handler.NewGeoHandler(nil),
		serviceCatalog:       handler.NewServiceCatalogHandler(nil, nil),
		appReleases:          handler.NewAppReleaseHandler(nil),
		reviews:              handler.NewReviewHandler(nil),
		executorGeo:          handler.NewExecutorGeoHandler(nil),
		behavior:             handler.NewBehaviorHandler(nil),
		disputes:             handler.NewDisputeHandler(nil),
		penalties:            handler.NewPenaltyHandler(nil),
		photoProof:           photoproof.NewHandler(nil, handler.CallerID),
		mail:                 handler.NewMailHandler(nil),
		achievements:         handler.NewAchievementHandler(nil, nil, nil, nil),
	}
}

func testRouterConfig(legacy bool) routerConfig {
	limiter := func() *middleware.RateLimiter { return middleware.NewRateLimiter(1, time.Minute) }
	return routerConfig{
		auth:         middleware.NewAuthMiddleware(nil, nil, "test", 0),
		allowsOrigin: func(string) bool { return false },
		limits: limiters{
			login: limiter(), passwordReset: limiter(), register: limiter(),
			geo: limiter(), refresh: limiter(), shopPurchase: limiter(),
		},
		releasesDir:  "releases",
		legacyRoot:   legacy,
		maxBodyBytes: 1 << 20,
	}
}

// routeTable — «метод путь число_middleware» на строку, отсортировано.
func routeTable(r chi.Router) []string {
	var lines []string
	_ = chi.Walk(r, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		lines = append(lines, fmt.Sprintf("%s %s %d", method, route, len(mws)))
		return nil
	})
	sort.Strings(lines)
	return lines
}

// Таблица маршрутов снята до переноса регистрации в обработчики
// (testdata/routes.txt): метод, путь и число middleware у каждого маршрута
// обязаны совпасть. Перенос меняет, где маршрут описан, а не что он значит.
func TestRouteTableMatchesSnapshot(t *testing.T) {
	raw, err := os.ReadFile("testdata/routes.txt")
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	want := strings.Split(strings.TrimSpace(string(raw)), "\n")
	sort.Strings(want)

	got := routeTable(newRouter(testHandlers(), testRouterConfig(false)))
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("route table differs from testdata/routes.txt:\n%s", diffLines(want, got))
	}
}

// С LEGACY_ROOT_ROUTES каждый маршрут /api/X доступен и как X — с тем же
// методом и той же цепочкой middleware, включая раздачу файлов витрины.
func TestLegacyRootRoutesMirrorAPI(t *testing.T) {
	got := routeTable(newRouter(testHandlers(), testRouterConfig(true)))
	set := make(map[string]struct{}, len(got))
	for _, line := range got {
		set[line] = struct{}{}
	}
	for _, line := range got {
		parts := strings.SplitN(line, " ", 3)
		if !strings.HasPrefix(parts[1], "/api/") {
			continue
		}
		twin := parts[0] + " " + strings.TrimPrefix(parts[1], "/api") + " " + parts[2]
		if _, ok := set[twin]; !ok {
			t.Errorf("no root twin for %s", line)
		}
	}
	for _, must := range []string{"GET /uploads/shop/{name} 7", "GET /api/uploads/shop/{name} 7"} {
		if _, ok := set[must]; !ok {
			t.Errorf("missing %s", must)
		}
	}
}

func diffLines(want, got []string) string {
	wantSet := map[string]struct{}{}
	for _, l := range want {
		wantSet[l] = struct{}{}
	}
	gotSet := map[string]struct{}{}
	for _, l := range got {
		gotSet[l] = struct{}{}
	}
	var b strings.Builder
	for _, l := range want {
		if _, ok := gotSet[l]; !ok {
			b.WriteString("- " + l + "\n")
		}
	}
	for _, l := range got {
		if _, ok := wantSet[l]; !ok {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}
