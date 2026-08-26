package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Middleware records one time series per route pattern, not per URL.
//
// The pattern is read from chi *after* the handler has run, because that is
// when routing has resolved: before it, every request looks like its raw path
// and /uploads/<uuid>/<filename> would mint a new label set per file. Requests
// that match nothing collapse into a single "not_found" label so a scanner
// walking random paths cannot grow the metric unbounded.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		IncInFlight()
		defer DecInFlight()

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		ObserveHTTP(r.Method, routePattern(r), rec.status, time.Since(start))
	})
}

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "not_found"
	}
	pattern := rctx.RoutePattern()
	// chi leaves a trailing wildcard on mounted sub-routers that did not match.
	if pattern == "" || pattern == "/*" {
		return "not_found"
	}
	// The legacy root mount serves the same handlers without the /api prefix.
	// Folding it onto the same label keeps one series per endpoint and still
	// leaves the split visible in the nginx and access-log views.
	return strings.TrimSuffix(pattern, "/*")
}

// statusRecorder captures the status code and keeps the optional interfaces the
// rest of the stack relies on: the chat WebSocket needs Hijack, and dropping
// Flush would break any streaming response.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer, which covers
// the deadline setters and anything else added later.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Hijack must be a real method, not just an Unwrap away: gorilla/websocket
// type-asserts the ResponseWriter it is handed directly to http.Hijacker, and
// chi's own logger wrapper decides what to implement by asserting on this
// wrapper in turn. Without it every chat WebSocket upgrade would answer 500.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("metrics: underlying ResponseWriter is not an http.Hijacker")
	}
	// A hijacked connection writes its own status line, so whatever was
	// recorded here stops being meaningful; 101 is what the upgrade returns.
	s.status = http.StatusSwitchingProtocols
	s.wroteHeader = true
	return h.Hijack()
}

// Flush keeps streaming responses streaming.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
