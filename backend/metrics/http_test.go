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

// The chat WebSocket lives on the same router as the API, and
// gorilla/websocket asserts the ResponseWriter it is given to http.Hijacker
// directly. A wrapper that only forwards Hijack through Unwrap fails that
// assertion and every upgrade answers 500, so the interface is asserted here.
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

	// Two different orders must land on one series: the label is the pattern,
	// not the path, or every entity id would mint its own time series.
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

// hijackRecorder is an httptest.ResponseRecorder that also claims to be an
// http.Hijacker, standing in for the real net/http response writer.
type hijackRecorder struct{ *httptest.ResponseRecorder }

func (hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }
