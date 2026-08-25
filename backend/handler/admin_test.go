package handler

import (
	"bytes"
	"context"
	"database/sql"
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
	users     map[uuid.UUID]*repository.User
	addresses map[uuid.UUID]string
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

func (m *mockUserRepository) UpdateRole(id uuid.UUID, role string) error {
	if u, ok := m.users[id]; ok {
		u.Role = role
	}
	return nil
}

func (m *mockUserRepository) UpdateBalance(id uuid.UUID, balance float64) error {
	if u, ok := m.users[id]; ok {
		u.Balance = balance
	}
	return nil
}

func (m *mockUserRepository) UpdateLastGeo(id uuid.UUID, lastGeo string) error {
	return nil
}

func (m *mockUserRepository) CreateCustomerProfile(userID uuid.UUID, address, lastGeo string) error {
	return nil
}

func (m *mockUserRepository) GetCustomerProfile(userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}

func (m *mockUserRepository) FindByEmail(email string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) FindByEmailVerificationToken(token string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) VerifyEmailToken(token string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error {
	return nil
}

func (m *mockUserRepository) ResetPasswordWithCode(email, code, newHashedPassword string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) UpdateCustomerAddress(userID uuid.UUID, address string) error {
	if m.addresses == nil {
		m.addresses = make(map[uuid.UUID]string)
	}
	m.addresses[userID] = address
	return nil
}

func (m *mockUserRepository) UpdateUserBirthDate(userID uuid.UUID, birthDate time.Time) error {
	if u, ok := m.users[userID]; ok {
		bd := birthDate
		u.BirthDate = &bd
	}
	return nil
}

func (m *mockUserRepository) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error {
	if u, ok := m.users[userID]; ok {
		u.LastName = lastName
		u.FirstName = firstName
		u.Patronymic = patronymic
	}
	return nil
}

// mockAdminRepository implements repository.AdminRepository.
type mockAdminRepository struct {
	users       []*repository.User
	requests    map[uuid.UUID]*repository.TopUpRequest
	withdrawals map[uuid.UUID]*repository.WithdrawalRequest
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

func (m *mockAdminRepository) CreateTopUpRequest(q repository.Querier, userID uuid.UUID, amount float64) (*repository.TopUpRequest, error) {
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

func (m *mockAdminRepository) GetWithdrawalRequests() ([]*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetWithdrawalRequestByID(id uuid.UUID) (*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepository) CreateWithdrawalRequest(q repository.Querier, userID uuid.UUID, amount float64) (*repository.WithdrawalRequest, error) {
	req := &repository.WithdrawalRequest{ID: uuid.New(), UserID: userID, Amount: amount, Status: "PENDING", CreatedAt: time.Now()}
	if m.withdrawals == nil {
		m.withdrawals = make(map[uuid.UUID]*repository.WithdrawalRequest)
	}
	m.withdrawals[req.ID] = req
	return req, nil
}

func (m *mockAdminRepository) GetTransactions() ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockAdminRepository) TopUpUserBalance(userID, adminID uuid.UUID, amount float64) error {
	return nil
}

func (m *mockAdminRepository) GetActiveShifts() ([]*repository.AdminShift, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetActiveOrders() ([]*repository.AdminOrder, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetCompletedOrders() ([]*repository.AdminOrder, error) {
	return nil, nil
}

// mockSettingsRepository implements repository.SettingsRepository.
type mockSettingsRepository struct {
	settings map[string]string
}

func (m *mockSettingsRepository) GetSettings() (map[string]string, error) {
	return m.settings, nil
}

func (m *mockSettingsRepository) UpdateSettings(settings map[string]string) error {
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
	sr := &mockSettingsRepository{settings: make(map[string]string)}

	svc := service.NewAdminService(ur, ar, sr, "secret", nil).
		WithLedger(service.NewLedger(&mockLedgerTxRepo{balances: map[uuid.UUID]float64{}}, &mockLedgerAccounts{}))
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
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	// The acting administrator is taken from the request context so the change
	// can be attributed and self-demotion refused.
	admin := &repository.User{ID: uuid.New(), Role: "ADMIN", Status: "ACTIVE"}
	ctx = context.WithValue(ctx, middleware.UserKey, admin)
	req = req.WithContext(ctx)

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

	reqObj, _ := ar.CreateTopUpRequest(nil, customer.ID, 150.00)

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

// CountAdmins reports how many administrators exist (used to protect the last one).
func (m *mockAdminRepository) CountAdmins() (int, error) {
	return 2, nil
}

// HasPendingWithdrawal reports an existing open withdrawal request.
func (m *mockAdminRepository) HasPendingWithdrawal(userID uuid.UUID) (bool, error) {
	return false, nil
}

// LockWithdrawalRequest and SetWithdrawalStatus back the withdrawal workflow now
// that it lives in AdminService.
func (m *mockAdminRepository) LockWithdrawalRequest(q repository.Querier, requestID uuid.UUID) (*repository.WithdrawalRequest, error) {
	if req, ok := m.withdrawals[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepository) SetWithdrawalStatus(q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.withdrawals[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}

func (m *mockAdminRepository) LockTopUpRequest(q repository.Querier, requestID uuid.UUID) (*repository.TopUpRequest, error) {
	if req, ok := m.requests[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepository) SetTopUpStatus(q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.requests[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}

// --- ledger doubles for the handler tests ---

type mockLedgerTxRepo struct {
	balances map[uuid.UUID]float64
}

func (m *mockLedgerTxRepo) GetBalance(userID uuid.UUID) (float64, error) {
	return m.balances[userID], nil
}

func (m *mockLedgerTxRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error {
	m.balances[userID] += delta
	return nil
}

func (m *mockLedgerTxRepo) Debit(tx *sql.Tx, userID uuid.UUID, amount float64) error {
	if m.balances[userID] < amount {
		return repository.ErrInsufficientFunds
	}
	m.balances[userID] -= amount
	return nil
}

func (m *mockLedgerTxRepo) CreateTransaction(tx *sql.Tx, t *repository.Transaction) error { return nil }

func (m *mockLedgerTxRepo) GetTransactionsByUserID(userID uuid.UUID) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockLedgerTxRepo) RunInTx(fn func(*sql.Tx) error) error { return fn(nil) }

type mockLedgerAccounts struct{}

func (m *mockLedgerAccounts) Credit(q repository.Querier, code string, amount float64) error {
	return nil
}
func (m *mockLedgerAccounts) Debit(q repository.Querier, code string, amount float64) error {
	return nil
}
func (m *mockLedgerAccounts) Get(code string) (*repository.SystemAccount, error) {
	return &repository.SystemAccount{Code: code}, nil
}
func (m *mockLedgerAccounts) List() ([]repository.SystemAccount, error) { return nil, nil }
