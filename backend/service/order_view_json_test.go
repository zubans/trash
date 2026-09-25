package service

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Имена JSON-полей карточки заказа — контракт с установленными приложениями.
// Набор ключей снят с repository.Order до того, как поля представления
// переехали в OrderView; переезд не должен ни добавить, ни потерять ключ.
func TestOrderViewJSONKeys(t *testing.T) {
	want := strings.Fields(`actions address assigned_at canceled_at comment completed_at counterparty created_at
		customer_id deadline_at executed_at executor_id executor_name executor_phone final_amount hold_amount id
		is_asap is_downgraded is_urgent photo_proof photo_required photo_url pickup_lat pickup_lon require_passport
		service_category service_variant service_variant_id status submit_fields`)
	wantMap := append(append([]string{}, want...), "can_accept", "category_name", "distance_km")
	sort.Strings(want)
	sort.Strings(wantMap)

	id := uuid.New()
	now := time.Now()
	s := "x"
	f := 1.0
	view := OrderView{
		Order: repository.Order{
			ID: id, CustomerID: id, ExecutorID: &id, ServiceVariantID: id,
			PhotoURL: &s, Address: &s, Comment: &s, PickupLat: &f, PickupLon: &f,
			CreatedAt: now, AssignedAt: &now, DeadlineAt: &now, CompletedAt: &now, CanceledAt: &now,
			ExecutedAt: &now, ExecutedAtDevice: &now, PhotoRequired: true, WatermarkSymbolID: &id, ProofKey: []byte("k"),
		},
		ExecutorPhone: "p", ExecutorName: "n",
		ServiceVariant: &repository.ServiceNode{}, ServiceCategory: &repository.ServiceNode{},
		PhotoProof: &OrderPhotoProof{}, SubmitFields: []string{"a"}, RequirePassport: true, ScriptExecuted: true,
		Counterparty: &OrderParty{}, Actions: &OrderActions{},
	}

	if got := jsonKeys(t, view); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("OrderView keys changed:\n got %v\nwant %v", got, want)
	}
	if got := jsonKeys(t, MapOrderView{OrderView: view, CategoryName: "c"}); strings.Join(got, " ") != strings.Join(wantMap, " ") {
		t.Errorf("MapOrderView keys changed:\n got %v\nwant %v", got, wantMap)
	}

	// Пустая карточка не отдаёт ключей представления: у поля без значения нет
	// ключа, как и раньше у голой строки заказа.
	bare := jsonKeys(t, OrderView{Order: repository.Order{ID: id}})
	for _, k := range []string{"actions", "counterparty", "photo_proof", "service_variant", "submit_fields", "executor_name"} {
		if sort.SearchStrings(bare, k) < len(bare) && bare[sort.SearchStrings(bare, k)] == k {
			t.Errorf("bare view leaks key %q", k)
		}
	}
}

func jsonKeys(t *testing.T, v interface{}) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
