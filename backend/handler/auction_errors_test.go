package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"healthlogin/backend/service"
)

// Отказ по неподтверждённому заказчику аукциона — нарушение правила площадки
// (409) с текстом, который можно показать исполнителю и заказчику как есть.
func TestAuctionCustomerNotVerifiedMapsToConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeDomainError(w, service.ErrAuctionCustomerNotVerified)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", w.Code)
	}
	if !strings.Contains(w.Body.String(), "подтверждённой личностью") {
		t.Errorf("body %q must carry the user-facing reason", w.Body.String())
	}
}
