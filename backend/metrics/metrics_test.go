package metrics

import (
	"strings"
	"testing"
	"time"
)

// A collector that panics or clashes on gather only shows up when something
// scrapes it, which in production means the first time anyone looks — usually
// during an incident.
func TestRegistryGathers(t *testing.T) {
	// Touch every family so the exposition is not trivially empty.
	ObserveHTTP("GET", "/api/health", 200, 5*time.Millisecond)
	AuthEvent("login", "ok")
	OrderEvent("created")
	BidEvent("placed")
	ShiftEvent("started")
	MatchingAssignment("assigned")
	SetMarketplaceDepth(3, 1)
	LedgerEntry("HOLD", "escrow", 250)
	LedgerError("HOLD")
	ReconcileReport(true, 0, 0, 0, 0, 0)
	WorkerRun("matching", time.Second, nil)
	UpstreamCall("dadata", "suggest", 80*time.Millisecond, nil)
	MailSend("password_reset", time.Second, nil)
	ChatConnected("order")
	ChatMessage("order")
	AppEndpointsRequest("ok")
	AppEndpointsFile(3, time.Now())
	SetBuildInfo("1.2.3", "abc123")

	families, err := Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("registry exposed nothing")
	}

	// An empty address disables the listener; it must return, not panic.
	Serve("")

	body := renderForTest(t)
	for _, want := range []string{
		"healthlogin_http_requests_total",
		"healthlogin_ledger_amount_rubles_total",
		"healthlogin_reconcile_ok",
		"healthlogin_app_endpoints_configs",
		"healthlogin_build_info",
		// Registered by default, and the reason a leaking goroutine or a
		// saturated pool is diagnosable at all.
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition is missing %s", want)
		}
	}
}

// SetBuildInfo registers a collector on every call. Calling it twice must not
// take the registry down — main calls it once, but a test binary or a future
// hot reload could call it again.
func TestSetBuildInfoIsIdempotent(t *testing.T) {
	SetBuildInfo("1.0.0", "aaa")
	SetBuildInfo("2.0.0", "bbb")
}
