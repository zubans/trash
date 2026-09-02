package metrics

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// WebSocket чата живёт на том же роутере, что и API, а gorilla/websocket
// приводит переданный ему ResponseWriter напрямую к http.Hijacker. Обёртка,
// которая лишь пробрасывает Hijack через Unwrap, это приведение проваливает, и
// каждый апгрейд отвечает 500, поэтому интерфейс проверяется здесь.
func TestMiddlewarePreservesHijacker(t *testing.T) {
	var hijackable bool
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, hijackable = w.(http.Hijacker)
	}))

	h.ServeHTTP(hijackRecorder{httptest.NewRecorder()}, httptest.NewRequest(http.MethodGet, "/api/chats/1/ws", nil))

	if !hijackable {
		t.Fatal("handler did not receive an http.Hijacker; WebSocket upgrades would fail")
	}
}

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	before := testutil.ToFloat64(httpRequests.WithLabelValues("GET", "/api/orders/{id}", "200"))

	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/api/orders/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Два разных заказа должны попасть в один ряд: лейбл — это шаблон, а не путь,
	// иначе каждый идентификатор сущности штамповал бы собственный временной ряд.
	for _, path := range []string{"/api/orders/aaa", "/api/orders/bbb"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	if got := testutil.ToFloat64(httpRequests.WithLabelValues("GET", "/api/orders/{id}", "200")) - before; got != 2 {
		t.Fatalf("counter for the route pattern = %v, want 2", got)
	}
}

func TestMiddlewareFoldsUnmatchedPaths(t *testing.T) {
	before := testutil.ToFloat64(httpRequests.WithLabelValues("GET", "not_found", "404"))

	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {})

	for _, path := range []string{"/wp-admin", "/.env", "/phpmyadmin"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	if got := testutil.ToFloat64(httpRequests.WithLabelValues("GET", "not_found", "404")) - before; got != 3 {
		t.Fatalf("counter for unmatched paths = %v, want 3 on a single series", got)
	}
}

// hijackRecorder — это httptest.ResponseRecorder, который к тому же выдаёт себя
// за http.Hijacker, заменяя настоящий writer ответа из net/http.
type hijackRecorder struct{ *httptest.ResponseRecorder }

func (hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
