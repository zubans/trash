package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// withAdmin кладёт администратора в контекст запроса, как это делает RequireAuth.
func withAdmin(req *http.Request) *http.Request {
	admin := &repository.User{ID: uuid.New(), Role: repository.RoleAdmin, Status: "ACTIVE"}
	return req.WithContext(context.WithValue(req.Context(), middleware.UserKey, admin))
}

// withURLParam подставляет параметр маршрута chi.
func withURLParam(req *http.Request, name, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(name, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// Списки проводок и заказов несут total и фасеты только на первой странице
// или по явной просьбе (total=1, facets=1): COUNT(*) и DISTINCT по всей
// таблице на каждой странице — то, ради чего это и сделано.
func TestListHandlersSendTotalAndFacetsOnFirstPageOrOnRequest(t *testing.T) {
	h, _, ar, _ := setupTestHandler()
	ar.facetTypes = []string{"TOP_UP"}
	ar.facetServices = []string{"Вывоз"}

	cases := []struct {
		name      string
		query     string
		wantTotal bool
		wantFacet bool
	}{
		{"first page", "?limit=50&offset=0", true, true},
		{"second page", "?limit=50&offset=50", false, false},
		{"second page, total asked", "?limit=50&offset=50&total=1", true, false},
		{"second page, facets asked", "?limit=50&offset=50&facets=1", false, true},
	}
	for _, tc := range cases {
		t.Run("transactions/"+tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.GetTransactionsHandler(w, httptest.NewRequest(http.MethodGet, "/admin/transactions"+tc.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]json.RawMessage
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if _, ok := resp["transactions"]; !ok {
				t.Error("response lacks transactions")
			}
			if _, ok := resp["total"]; ok != tc.wantTotal {
				t.Errorf("total present = %v, want %v", ok, tc.wantTotal)
			}
			if _, ok := resp["types"]; ok != tc.wantFacet {
				t.Errorf("types present = %v, want %v", ok, tc.wantFacet)
			}
			if _, ok := resp["periods"]; ok != tc.wantFacet {
				t.Errorf("periods present = %v, want %v", ok, tc.wantFacet)
			}
			if got := ar.lastTxFilter.Page.WithTotal; got != tc.wantTotal {
				t.Errorf("repository asked to count = %v, want %v", got, tc.wantTotal)
			}
		})
		t.Run("orders/"+tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.GetOrdersHandler(w, httptest.NewRequest(http.MethodGet, "/admin/orders"+tc.query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]json.RawMessage
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if _, ok := resp["total"]; ok != tc.wantTotal {
				t.Errorf("total present = %v, want %v", ok, tc.wantTotal)
			}
			if _, ok := resp["services"]; ok != tc.wantFacet {
				t.Errorf("services present = %v, want %v", ok, tc.wantFacet)
			}
			if got := ar.lastOrdersFilter.Page.WithTotal; got != tc.wantTotal {
				t.Errorf("repository asked to count = %v, want %v", got, tc.wantTotal)
			}
		})
	}
}

// Фасеты берутся из кэша: второй запрос первой страницы не ходит в репозиторий.
func TestListHandlersCacheFacets(t *testing.T) {
	h, _, ar, _ := setupTestHandler()
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.GetTransactionsHandler(w, httptest.NewRequest(http.MethodGet, "/admin/transactions", nil))
		w = httptest.NewRecorder()
		h.GetOrdersHandler(w, httptest.NewRequest(http.MethodGet, "/admin/orders?status=active", nil))
	}
	if ar.txFacetCalls != 1 {
		t.Errorf("transaction facets loaded %d times, want 1", ar.txFacetCalls)
	}
	if ar.orderFacetCalls != 1 {
		t.Errorf("order facets loaded %d times, want 1", ar.orderFacetCalls)
	}
	// Другая группа статусов — другой ключ кэша.
	w := httptest.NewRecorder()
	h.GetOrdersHandler(w, httptest.NewRequest(http.MethodGet, "/admin/orders?status=completed", nil))
	if ar.orderFacetCalls != 2 {
		t.Errorf("order facets for another group loaded %d times total, want 2", ar.orderFacetCalls)
	}
}

// Коды ответов админских маршрутов идут по классу ошибки сервиса, а не одним
// 400 на всё: чужой id — 404, повторное решение — 409, негодный ввод — 422,
// правило — 409.
func TestAdminHandlerStatusCodesFollowErrorClass(t *testing.T) {
	h, ur, ar, _ := setupTestHandler()
	customer := &repository.User{ID: uuid.New(), Phone: "12345", Role: "CUSTOMER", Status: "ACTIVE"}
	ur.users[customer.ID] = customer

	post := func(handler http.HandlerFunc, path, id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req = withAdmin(withURLParam(req, "id", id))
		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}

	// Заявка, которой нет, — 404.
	if w := post(h.ApproveTopUpRequestsHandler, "/admin/finances/topups/x/approve", uuid.New().String(), ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown top-up request: status %d, want 404: %s", w.Code, w.Body.String())
	}
	// Повторное решение по заявке — 409.
	reqObj, _ := ar.CreateTopUpRequest(context.Background(), nil, customer.ID, money.FromRubles(100))
	if w := post(h.ApproveTopUpRequestsHandler, "/x", reqObj.ID.String(), ""); w.Code != http.StatusOK {
		t.Fatalf("first approval: status %d: %s", w.Code, w.Body.String())
	}
	if w := post(h.ApproveTopUpRequestsHandler, "/x", reqObj.ID.String(), ""); w.Code != http.StatusConflict {
		t.Errorf("second approval: status %d, want 409: %s", w.Code, w.Body.String())
	}
	// Пользователь, которого нет, — 404.
	if w := post(h.UpdateUserNameHandler, "/x", uuid.New().String(), `{"last_name":"А","first_name":"Б","patronymic":"В"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown user: status %d, want 404: %s", w.Code, w.Body.String())
	}
	// Негодный ввод — 422.
	if w := post(h.UpdateUserStatusHandler, "/x", customer.ID.String(), `{"status":"WEIRD"}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid status: status %d, want 422: %s", w.Code, w.Body.String())
	}
	if w := post(h.TopUpUserBalanceHandler, "/x", customer.ID.String(), `{"amount":0}`); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("zero top-up: status %d, want 422: %s", w.Code, w.Body.String())
	}
	// Правило площадки — 409: нельзя пополнить самому себе.
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"amount":100}`))
	admin := &repository.User{ID: uuid.New(), Role: repository.RoleAdmin, Status: "ACTIVE"}
	ur.users[admin.ID] = admin
	req = withURLParam(req, "id", admin.ID.String())
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, admin))
	w := httptest.NewRecorder()
	h.TopUpUserBalanceHandler(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("self top-up: status %d, want 409: %s", w.Code, w.Body.String())
	}
	// Фильтр списка с неизвестной ролью — 422, а не 500.
	w = httptest.NewRecorder()
	h.GetUsersHandler(w, httptest.NewRequest(http.MethodGet, "/admin/users?role=NOPE", nil))
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown role filter: status %d, want 422: %s", w.Code, w.Body.String())
	}
}

// fakeGeoRepo отдаёт пустой список аномалий; остальное обработчику не нужно.
type fakeGeoRepo struct {
	repository.ExecutorGeoRepository
}

func (fakeGeoRepo) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return []repository.GeoAlert{}, nil
}

// GET /admin/geo-alerts охраняется правом shifts.view на маршруте; сам
// обработчик роли не проверяет. Модератор с правом получает 200, а не 403.
func TestGetGeoAlertsDoesNotRequireAdminRole(t *testing.T) {
	h := NewExecutorGeoHandler(service.NewExecutorGeoService(fakeGeoRepo{}, &mockSettingsRepository{settings: map[string]string{}}))

	moderator := &repository.User{ID: uuid.New(), Role: repository.RoleModerator, Roles: []string{repository.RoleModerator}, Status: "ACTIVE"}
	req := httptest.NewRequest(http.MethodGet, "/admin/geo-alerts", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, moderator))
	w := httptest.NewRecorder()
	h.GetGeoAlerts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("moderator with shifts.view: status %d, want 200: %s", w.Code, w.Body.String())
	}

	// Без пользователя в контексте — 401: сюда без RequireAuth не попасть.
	w = httptest.NewRecorder()
	h.GetGeoAlerts(w, httptest.NewRequest(http.MethodGet, "/admin/geo-alerts", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: status %d, want 401", w.Code)
	}
}

// mockAddressRepo — адреса в памяти для тестов профиля.
type mockAddressRepo struct {
	byUser map[uuid.UUID][]repository.Address
}

func (m *mockAddressRepo) List(ctx context.Context, userID uuid.UUID) ([]repository.Address, error) {
	return append([]repository.Address(nil), m.byUser[userID]...), nil
}

func (m *mockAddressRepo) Add(ctx context.Context, q repository.Querier, userID uuid.UUID, address repository.Address) ([]repository.Address, error) {
	address.ID = uuid.New()
	m.byUser[userID] = append(m.byUser[userID], address)
	return m.List(ctx, userID)
}

func (m *mockAddressRepo) Delete(ctx context.Context, userID, addressID uuid.UUID) ([]repository.Address, error) {
	kept := m.byUser[userID][:0]
	found := false
	for _, a := range m.byUser[userID] {
		if a.ID == addressID {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return nil, repository.ErrAddressNotFound
	}
	m.byUser[userID] = kept
	return m.List(ctx, userID)
}

func (m *mockAddressRepo) SetDefault(ctx context.Context, userID, addressID uuid.UUID) ([]repository.Address, error) {
	return m.List(ctx, userID)
}

func (m *mockAddressRepo) SetDefaultByValue(ctx context.Context, userID uuid.UUID, address string) ([]repository.Address, error) {
	return m.List(ctx, userID)
}

// DELETE /user/address/{id} принимает и id, и номер строки: позицию разрешает
// сервис, а не обработчик. Чужой номер — 404, мусор — 422.
func TestDeleteAddressHandlerResolvesPositionalIndex(t *testing.T) {
	ur := &mockUserRepository{users: make(map[uuid.UUID]*repository.User)}
	user := &repository.User{ID: uuid.New(), Phone: "12345", Role: "CUSTOMER", Status: "ACTIVE"}
	ur.users[user.ID] = user
	addresses := &mockAddressRepo{byUser: map[uuid.UUID][]repository.Address{}}
	first, _ := addresses.Add(context.Background(), nil, user.ID, repository.Address{Address: "Москва, Арбат, 10"})
	addresses.Add(context.Background(), nil, user.ID, repository.Address{Address: "Москва, Тверская, 1"})

	h := NewProfileHandler(service.NewProfileService(ur, addresses))
	del := func(ref string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/user/address/"+ref, nil)
		req = withURLParam(req, "id", ref)
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
		w := httptest.NewRecorder()
		h.DeleteAddressHandler(w, req)
		return w
	}

	if w := del("5"); w.Code != http.StatusNotFound {
		t.Errorf("index out of range: status %d, want 404: %s", w.Code, w.Body.String())
	}
	if w := del("abc"); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("garbage ref: status %d, want 422: %s", w.Code, w.Body.String())
	}
	w := del("1")
	if w.Code != http.StatusOK {
		t.Fatalf("delete by index: status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Addresses []repository.Address `json:"addresses"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Addresses) != 1 || resp.Addresses[0].ID != first[0].ID {
		t.Fatalf("index 1 should delete the second address, left %+v", resp.Addresses)
	}
	if w := del(first[0].ID.String()); w.Code != http.StatusOK {
		t.Errorf("delete by id: status %d: %s", w.Code, w.Body.String())
	}
	if left, _ := addresses.List(context.Background(), user.ID); len(left) != 0 {
		t.Errorf("addresses left after deleting both: %+v", left)
	}
}
