package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// mockUserRepo is an in-memory implementation of repository.UserRepository for tests.
type mockUserRepo struct {
	users map[string]*repository.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*repository.User)}
}

func (m *mockUserRepo) FindByPhone(phone string) (*repository.User, error) {
	if u, ok := m.users[phone]; ok {
		return u, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockUserRepo) Create(user *repository.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	m.users[user.Phone] = user
	return nil
}

func (m *mockUserRepo) FindByID(id uuid.UUID) (*repository.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockUserRepo) UpdateStatus(id uuid.UUID, status string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Status = status
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockUserRepo) UpdateRole(id uuid.UUID, role string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Role = role
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockUserRepo) UpdateBalance(id uuid.UUID, balance float64) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Balance = balance
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockUserRepo) UpdateLastGeo(id uuid.UUID, lastGeo string) error {
	return nil
}

func (m *mockUserRepo) CreateCustomerProfile(userID uuid.UUID, address string) error {
	return nil
}

func (m *mockUserRepo) GetCustomerProfile(userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}

func (m *mockUserRepo) UpdateCustomerAddress(userID uuid.UUID, address string) error {
	return nil
}

func newTestPublicHandler() *PublicHandler {
	repo := newMockUserRepo()
	return NewPublicHandler(service.NewAuthService(repo))
}

func TestHealthHandler(t *testing.T) {
	h := newTestPublicHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.HealthHandler(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("HealthHandler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("HealthHandler returned wrong Content-Type: got %v want %v", ct, "application/json")
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("HealthHandler returned invalid JSON: %v", err)
	}
	if status, ok := resp["status"]; !ok || status != "ok" {
		t.Fatalf("HealthHandler returned wrong body: %v", resp)
	}
}

func TestRegisterHandler(t *testing.T) {
	h := newTestPublicHandler()
	body, _ := json.Marshal(RegisterRequest{Phone: "+79001234567", Password: "secret123", Address: "Moscow"})
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.RegisterHandler(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("RegisterHandler returned wrong status: got %v want %v, body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp RegisterResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("RegisterHandler returned invalid JSON: %v", err)
	}
	if resp.Phone != "+79001234567" || resp.Role != "CUSTOMER" || resp.ID == "" {
		t.Fatalf("RegisterHandler returned unexpected body: %+v", resp)
	}
}

func TestRegisterHandlerDuplicate(t *testing.T) {
	h := newTestPublicHandler()
	body, _ := json.Marshal(RegisterRequest{Phone: "+79001234567", Password: "secret123", Address: "Moscow"})

	req1 := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	h.RegisterHandler(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	rr2 := httptest.NewRecorder()
	h.RegisterHandler(rr2, req2)

	if rr2.Code != http.StatusConflict {
		t.Fatalf("RegisterHandler duplicate returned wrong status: got %v want %v", rr2.Code, http.StatusConflict)
	}
}

func TestLoginHandler(t *testing.T) {
	h := newTestPublicHandler()
	phone := "+79001234567"
	password := "secret123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	h.authService.Register(phone, password, "Moscow")

	body, _ := json.Marshal(AuthRequest{Phone: phone, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.LoginHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("LoginHandler returned wrong status: got %v want %v, body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp AuthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("LoginHandler returned invalid JSON: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("LoginHandler returned empty token")
	}

	_ = hash
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	h := newTestPublicHandler()
	h.authService.Register("+79001234567", "secret123", "Moscow")

	body, _ := json.Marshal(AuthRequest{Phone: "+79001234567", Password: "wrongpassword"})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.LoginHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("LoginHandler returned wrong status: got %v want %v", rr.Code, http.StatusUnauthorized)
	}
}
