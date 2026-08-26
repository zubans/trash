package metrics

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// renderForTest returns the exposition text the scraper would see.
func renderForTest(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}).
		ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 {
		t.Fatalf("/metrics returned %d", rec.Code)
	}
	return rec.Body.String()
}
