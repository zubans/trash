package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func opsMux(t *testing.T, o OpsHandlers) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	o.register(mux)
	return mux
}

// A shared secret that was never configured must not become an open door: the
// routes are not registered at all rather than registered without a check.
func TestOpsRoutesAreAbsentWithoutASecret(t *testing.T) {
	mux := opsMux(t, OpsHandlers{Reconcile: func() (any, error) { return "ran", nil }})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/internal/reconcile", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d without a secret, want 404 (route not registered)", rec.Code)
	}
}

func TestOpsRouteRejectsAWrongKey(t *testing.T) {
	var ran bool
	mux := opsMux(t, OpsHandlers{Secret: "right", Reconcile: func() (any, error) {
		ran = true
		return "ran", nil
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/reconcile", nil)
	req.Header.Set("X-Ops-Key", "wrong")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d for a wrong key, want 403", rec.Code)
	}
	if ran {
		t.Fatal("the action ran despite a rejected key")
	}
}

// GET must not trigger a privileged action: anything that can be reached by a
// link, a crawler or a prefetch is not a safe way to run one.
func TestOpsRouteRejectsGet(t *testing.T) {
	var ran bool
	mux := opsMux(t, OpsHandlers{Secret: "right", Reconcile: func() (any, error) {
		ran = true
		return "ran", nil
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/reconcile", nil)
	req.Header.Set("X-Ops-Key", "right")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || ran {
		t.Fatalf("GET returned %d (ran=%v), want 405 and no action", rec.Code, ran)
	}
}

func TestOpsRouteRunsWithTheRightKey(t *testing.T) {
	mux := opsMux(t, OpsHandlers{Secret: "right", Reconcile: func() (any, error) {
		return map[string]bool{"ok": true}, nil
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/reconcile", nil)
	req.Header.Set("X-Ops-Key", "right")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d with the right key, want 200", rec.Code)
	}
}

func TestOpsRouteReportsAFailedRun(t *testing.T) {
	mux := opsMux(t, OpsHandlers{Secret: "right", Reconcile: func() (any, error) {
		return nil, errors.New("database is unreachable")
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/reconcile", nil)
	req.Header.Set("X-Ops-Key", "right")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d for a failed run, want 500", rec.Code)
	}
}
