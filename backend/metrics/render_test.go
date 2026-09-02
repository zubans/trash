package metrics

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// renderForTest возвращает текст выкладки, который увидел бы сборщик.
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
