package metrics

import (
	"strings"
	"testing"
	"time"
)

// Коллектор, который паникует или конфликтует при сборе, проявляется только
// когда его кто-то собирает, а в проде это значит — когда кто-то впервые
// посмотрит, обычно во время инцидента.
func TestRegistryGathers(t *testing.T) {
	// Трогаем каждое семейство, чтобы выкладка не оказалась тривиально пустой.
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
	SetBuildInfo("1.2.3", "abc123")

	families, err := Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("registry exposed nothing")
	}

	// Пустой адрес выключает слушатель; он должен вернуться, а не паниковать.
	Serve("", OpsHandlers{})

	body := renderForTest(t)
	for _, want := range []string{
		"healthlogin_http_requests_total",
		"healthlogin_ledger_amount_rubles_total",
		"healthlogin_reconcile_ok",
		"healthlogin_build_info",
		// Зарегистрированы по умолчанию, и именно поэтому утекающая горутина или
		// забитый пул вообще поддаются диагностике.
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("exposition is missing %s", want)
		}
	}
}

// SetBuildInfo регистрирует коллектор на каждом вызове. Двойной вызов не должен
// ронять реестр — main вызывает его один раз, но тестовый бинарник или будущая
// горячая перезагрузка могут вызвать снова.
func TestSetBuildInfoIsIdempotent(t *testing.T) {
	SetBuildInfo("1.0.0", "aaa")
	SetBuildInfo("2.0.0", "bbb")
}
