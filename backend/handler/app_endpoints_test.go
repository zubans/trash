package handler

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"healthlogin/backend/metrics"
)

const testEncKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// gaugeValue reads one gauge out of the process registry by name.
func gaugeValue(t *testing.T, name string) float64 {
	t.Helper()
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		if len(f.GetMetric()) == 0 {
			t.Fatalf("%s has no samples", name)
		}
		return f.GetMetric()[0].GetGauge().GetValue()
	}
	t.Fatalf("%s was never registered", name)
	return 0
}

// The endpoint-list gauge used to be written only when a client successfully
// fetched the list, which left it at zero on a backend nobody had polled yet —
// indistinguishable from an empty list, and enough to fire the alert that says
// the fallback channel has no servers left. It must describe the file from the
// moment the handler exists.
func TestEndpointCountIsPublishedBeforeAnyRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vless-endpoints.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"configs":[{"remarks":"a"},{"remarks":"b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	NewAppEndpointsHandler(path, "key", testEncKeyHex)

	if got := gaugeValue(t, "healthlogin_app_endpoints_configs"); got != 2 {
		t.Fatalf("configs gauge = %v before any request, want 2", got)
	}
}

// A file that cannot be read is an outage of the fallback channel: every client
// gets a 503. Reporting zero is what makes the alert fire, so it must not be
// quietly skipped.
func TestUnreadableEndpointListReportsZero(t *testing.T) {
	NewAppEndpointsHandler(filepath.Join(t.TempDir(), "absent.json"), "key", testEncKeyHex)

	if got := gaugeValue(t, "healthlogin_app_endpoints_configs"); got != 0 {
		t.Fatalf("configs gauge = %v for a missing file, want 0", got)
	}
}

// The access check must run before the file is read, so a wrong key can never
// be answered with a 5xx that looks like a server fault.
func TestWrongKeyIsRejectedBeforeReadingTheFile(t *testing.T) {
	h := NewAppEndpointsHandler(filepath.Join(t.TempDir(), "absent.json"), "right-key", testEncKeyHex)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/endpoints", nil)
	req.Header.Set("X-App-Key", "wrong-key")
	h.Serve(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d for a wrong key, want 403", rec.Code)
	}
}

// A correct key against a missing list is a 503 — the case that shows up both
// as a failed probe and as server errors in the HTTP metrics.
func TestMissingListAnswers503(t *testing.T) {
	h := NewAppEndpointsHandler(filepath.Join(t.TempDir(), "absent.json"), "right-key", testEncKeyHex)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/endpoints", nil)
	req.Header.Set("X-App-Key", "right-key")
	h.Serve(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d for a missing list, want 503", rec.Code)
	}
}

// Sanity: the key really is 32 bytes, so a handler built with it is enabled.
func TestEncryptionKeyLength(t *testing.T) {
	if k, err := hex.DecodeString(testEncKeyHex); err != nil || len(k) != 32 {
		t.Fatalf("test key is not 32 bytes: %v", err)
	}
}

// The failure that actually happened in production: Docker created a directory
// where the endpoint list should be, because the bind mount's source file was
// missing. Reading it fails with EISDIR, so every request answers 503 and the
// count must report zero — the handler must not mistake a directory for a list.
func TestDirectoryInPlaceOfTheListIsTreatedAsUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vless-endpoints.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	h := NewAppEndpointsHandler(path, "right-key", testEncKeyHex)

	if got := gaugeValue(t, "healthlogin_app_endpoints_configs"); got != 0 {
		t.Fatalf("configs gauge = %v for a directory, want 0", got)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/endpoints", nil)
	req.Header.Set("X-App-Key", "right-key")
	h.Serve(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d for a directory, want 503", rec.Code)
	}
}
