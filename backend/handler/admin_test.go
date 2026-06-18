package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// mockUserRepository implements repository.UserRepository for testing.
type mockUserRepository struct {
	users map[uuid.UUID]*repository.User
}

func (m *mockUserRepository) FindByPhone(phone string) (*repository.User, error) {
	for _, u := range m.users {
		if u.Phone == phone {
			return u, nil
		}
	}
	return nil, nil
}

func (m *mockUserRepository) Create(user *repository.User) error {
	id := uuid.New()
	user.ID = id
	m.users[id] = user
	return nil
}

func (m *mockUserRepository) FindByID(id uuid.UUID) (*repository.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (m *mockUserRepository) UpdateStatus(id uuid.UUID, status string) error {
	if u, ok := m.users[id]; ok {
		u.Status = status
	}
	return nil
}

func (m *mockUserRepository) UpdateBalance(id uuid.UUID, balance float64) error {
	if u, ok := m.users[id]; ok {
		u.Balance = balance
	}
	return nil
}

// mockAdminRepository implements repository.AdminRepository.
type mockAdminRepository struct {
	users    []*repository.User
	requests map[uuid.UUID]*repository.TopUpRequest
}

func (m *mockAdminRepository) GetUsers(page, limit int, role, status, search string) ([]*repository.User, int, error) {
	return m.users, len(m.users), nil
}

func (m *mockAdminRepository) GetTopUpRequests() ([]*repository.TopUpRequest, error) {
	var list []*repository.TopUpRequest
	for _, r := range m.requests {
		list = append(list, r)
	}
	return list, nil
}

func (m *mockAdminRepository) GetTopUpRequestByID(id uuid.UUID) (*repository.TopUpRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockAdminRepository) CreateTopUpRequest(userID uuid.UUID, amount float64) (*repository.TopUpRequest, error) {
	r := &repository.TopUpRequest{
		ID:        uuid.New(),
		UserID:    userID,
		Amount:    amount,
		Status:    "PENDING",
		CreatedAt: time.Now(),
	}
	m.requests[r.ID] = r
	return r, nil
}

func (m *mockAdminRepository) ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	r := m.requests[requestID]
	r.Status = "APPROVED"
	r.AdminID = &adminID
	now := time.Now()
	r.UpdatedAt = &now
	return nil
}

func (m *mockAdminRepository) RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	r := m.requests[requestID]
	r.Status = "REJECTED"
	r.AdminID = &adminID
	now := time.Now()
	r.UpdatedAt = &now
	return nil
}

func (m *mockAdminRepository) GetTransactions() ([]*repository.Transaction, error) {
	return nil, nil
}

// mockSettingsRepository implements repository.SettingsRepository.
type mockSettingsRepository struct {
	settings map[string]float64
}

func (m *mockSettingsRepository) GetSettings() (map[string]float64, error) {
	return m.settings, nil
}

func (m *mockSettingsRepository) UpdateSettings(settings map[string]float64) error {
	for k, v := range settings {
		m.settings[k] = v
	}
	return nil
}

// mockTokenRepository implements repository.TokenRepository.
type mockTokenRepository struct{}

func (m *mockTokenRepository) IsTokenRevoked(tokenHash string) (bool, error) {
	return false, nil
}

func (m *mockTokenRepository) RevokeToken(tokenHash string, expiresAt time.Time) error {
	return nil
}

func setupTestHandler() (*AdminHandler, *mockUserRepository, *mockAdminRepository, *mockSettingsRepository) {
	ur := &mockUserRepository{users: make(map[uuid.UUID]*repository.User)}
	ar := &mockAdminRepository{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	sr := &mockSettingsRepository{settings: make(map[string]float64)}
	tr := &mockTokenRepository{}

	svc := service.NewAdminService(ur, ar, sr, tr, "secret")
	h := NewAdminHandler(svc)
	return h, ur, ar, sr
}

func TestGetUsersHandler(t *testing.T) {
	h, ur, ar, _ := setupTestHandler()

	u1 := &repository.User{ID: uuid.New(), Phone: "12345", Role: "CUSTOMER", Status: "ACTIVE"}
	u2 := &repository.User{ID: uuid.New(), Phone: "67890", Role: "ADMIN", Status: "ACTIVE"}
	ur.users[u1.ID] = u1
	ur.users[u2.ID] = u2
	ar.users = []*repository.User{u1, u2}

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()

	h.GetUsersHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp struct {
		Users []*repository.User `json:"users"`
		Total int                `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Users) != 2 || resp.Total != 2 {
		t.Errorf("unexpected response size: %+v", resp)
	}
}

func TestUpdateUserStatusHandler(t *testing.T) {
	h, ur, _, _ := setupTestHandler()

	u := &repository.User{ID: uuid.New(), Phone: "12345", Role: "CUSTOMER", Status: "ACTIVE"}
	ur.users[u.ID] = u

	body, _ := json.Marshal(map[string]string{"status": "BANNED"})
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+u.ID.String()+"/status", bytes.NewBuffer(body))

	// Inject URL param using Chi context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", u.ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.UpdateUserStatusHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	if ur.users[u.ID].Status != "BANNED" {
		t.Errorf("expected user status to be BANNED, got %s", ur.users[u.ID].Status)
	}
}

func TestApproveTopUpRequestsHandler(t *testing.T) {
	h, ur, ar, _ := setupTestHandler()

	customer := &repository.User{ID: uuid.New(), Phone: "12345", Role: "CUSTOMER", Status: "ACTIVE"}
	admin := &repository.User{ID: uuid.New(), Phone: "99999", Role: "ADMIN", Status: "ACTIVE"}
	ur.users[customer.ID] = customer
	ur.users[admin.ID] = admin

	reqObj, _ := ar.CreateTopUpRequest(customer.ID, 150.00)

	req := httptest.NewRequest(http.MethodPost, "/admin/finances/topups/"+reqObj.ID.String()+"/approve", nil)

	// Inject Chi URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", reqObj.ID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// Inject logged-in admin user into context
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserKey, admin))

	w := httptest.NewRecorder()

	h.ApproveTopUpRequestsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	savedReq := ar.requests[reqObj.ID]
	if savedReq.Status != "APPROVED" {
		t.Errorf("expected request status APPROVED, got %s", savedReq.Status)
	}
}
