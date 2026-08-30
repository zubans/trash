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
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// mockUserRepository implements repository.UserRepository for testing.
type mockUserRepository struct {
	users     map[uuid.UUID]*repository.User
	addresses map[uuid.UUID]string
}

func (m *mockUserRepository) FindByPhone(ctx context.Context, phone string) (*repository.User, error) {
	for _, u := range m.users {
		if u.Phone == phone {
			return u, nil
		}
	}
	return nil, nil
}

func (m *mockUserRepository) Create(ctx context.Context, user *repository.User) error {
	id := uuid.New()
	user.ID = id
	m.users[id] = user
	return nil
}

func (m *mockUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, nil
	}
	return u, nil
}

func (m *mockUserRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	if u, ok := m.users[id]; ok {
		u.Status = status
	}
	return nil
}

func (m *mockUserRepository) UpdateRole(ctx context.Context, id uuid.UUID, role string) error {
	if u, ok := m.users[id]; ok {
		u.Role = role
	}
	return nil
}

func (m *mockUserRepository) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	if u, ok := m.users[id]; ok {
		u.Verified = verified
	}
	return nil
}

func (m *mockUserRepository) UpdateBalance(ctx context.Context, id uuid.UUID, balance money.Amount) error {
	if u, ok := m.users[id]; ok {
		u.Balance = balance
	}
	return nil
}

func (m *mockUserRepository) CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error {
	return nil
}

func (m *mockUserRepository) GetCustomerProfile(ctx context.Context, userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}

func (m *mockUserRepository) FindByEmail(ctx context.Context, email string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) FindByEmailVerificationToken(ctx context.Context, token string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) VerifyEmailToken(ctx context.Context, token string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	return nil
}

func (m *mockUserRepository) ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
	return nil, nil
}

func (m *mockUserRepository) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	if u, ok := m.users[userID]; ok {
		bd := birthDate
		u.BirthDate = &bd
	}
	return nil
}

func (m *mockUserRepository) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
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

func (m *mockAdminRepository) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*repository.User, int, error) {
	return m.users, len(m.users), nil
}

func (m *mockAdminRepository) GetTopUpRequests(ctx context.Context, limit, offset int) ([]*repository.TopUpRequest, error) {
	var list []*repository.TopUpRequest
	for _, r := range m.requests {
		list = append(list, r)
	}
	return list, nil
}

func (m *mockAdminRepository) GetTopUpRequestByID(ctx context.Context, id uuid.UUID) (*repository.TopUpRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockAdminRepository) CreateTopUpRequest(ctx context.Context, q repository.Querier, userID uuid.UUID, amount money.Amount) (*repository.TopUpRequest, error) {
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

func (m *mockAdminRepository) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetWithdrawalRequestByID(ctx context.Context, id uuid.UUID) (*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepository) CreateWithdrawalRequest(ctx context.Context, q repository.Querier, userID uuid.UUID, amount money.Amount) (*repository.WithdrawalRequest, error) {
	req := &repository.WithdrawalRequest{ID: uuid.New(), UserID: userID, Amount: amount, Status: "PENDING", CreatedAt: time.Now()}
	if m.withdrawals == nil {
		m.withdrawals = make(map[uuid.UUID]*repository.WithdrawalRequest)
	}
	m.withdrawals[req.ID] = req
	return req, nil
}

func (m *mockAdminRepository) GetTransactions(ctx context.Context, limit, offset int) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockAdminRepository) TopUpUserBalance(ctx context.Context, userID, adminID uuid.UUID, amount money.Amount) error {
	return nil
}

func (m *mockAdminRepository) GetActiveShifts(ctx context.Context) ([]*repository.AdminShift, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetActiveOrders(ctx context.Context, limit, offset int) ([]*repository.AdminOrder, error) {
	return nil, nil
}

func (m *mockAdminRepository) GetCompletedOrders(ctx context.Context, limit, offset int) ([]*repository.AdminOrder, error) {
	return nil, nil
}

// mockSettingsRepository implements repository.SettingsRepository.
type mockSettingsRepository struct {
	settings map[string]string
}

func (m *mockSettingsRepository) GetSettings(ctx context.Context) (map[string]string, error) {
	return m.settings, nil
}

func (m *mockSettingsRepository) UpdateSettings(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		m.settings[k] = v
	}
	return nil
}

// mockTokenRepository implements repository.TokenRepository.
type mockTokenRepository struct{}

func (m *mockTokenRepository) IsTokenRevoked(ctx context.Context, tokenHash string) (bool, error) {
	return false, nil
}

func (m *mockTokenRepository) RevokeToken(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	return nil
}

func setupTestHandler() (*AdminHandler, *mockUserRepository, *mockAdminRepository, *mockSettingsRepository) {
	ur := &mockUserRepository{users: make(map[uuid.UUID]*repository.User)}
	ar := &mockAdminRepository{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	sr := &mockSettingsRepository{settings: make(map[string]string)}

	svc := service.NewAdminService(ur, ar, sr, "secret", nil).
		WithLedger(service.NewLedger(&mockLedgerTxRepo{balances: map[uuid.UUID]money.Amount{}}, &mockLedgerAccounts{}))
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

	reqObj, _ := ar.CreateTopUpRequest(context.Background(), nil, customer.ID, 150.00)

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
func (m *mockAdminRepository) CountAdmins(ctx context.Context) (int, error) {
	return 2, nil
}

// HasPendingWithdrawal reports an existing open withdrawal request.
func (m *mockAdminRepository) HasPendingWithdrawal(ctx context.Context, userID uuid.UUID) (bool, error) {
	return false, nil
}

// LockWithdrawalRequest and SetWithdrawalStatus back the withdrawal workflow now
// that it lives in AdminService.
func (m *mockAdminRepository) LockWithdrawalRequest(ctx context.Context, q repository.Querier, requestID uuid.UUID) (*repository.WithdrawalRequest, error) {
	if req, ok := m.withdrawals[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepository) SetWithdrawalStatus(ctx context.Context, q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.withdrawals[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}

func (m *mockAdminRepository) LockTopUpRequest(ctx context.Context, q repository.Querier, requestID uuid.UUID) (*repository.TopUpRequest, error) {
	if req, ok := m.requests[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepository) SetTopUpStatus(ctx context.Context, q repository.Querier, requestID, adminID uuid.UUID, status string) error {
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
	balances map[uuid.UUID]money.Amount
}

func (m *mockLedgerTxRepo) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	return m.balances[userID], nil
}

func (m *mockLedgerTxRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	m.balances[userID] = m.balances[userID].Add(delta)
	return nil
}

func (m *mockLedgerTxRepo) Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	if m.balances[userID] < amount {
		return repository.ErrInsufficientFunds
	}
	m.balances[userID] = m.balances[userID].Sub(amount)
	return nil
}

func (m *mockLedgerTxRepo) CreateTransaction(ctx context.Context, tx *sql.Tx, t *repository.Transaction) error {
	return nil
}

func (m *mockLedgerTxRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockLedgerTxRepo) HasTip(ctx context.Context, q repository.Querier, orderID uuid.UUID) (bool, error) {
	return false, nil
}

func (m *mockLedgerTxRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error { return fn(nil) }

type mockLedgerAccounts struct{}

func (m *mockLedgerAccounts) Credit(ctx context.Context, q repository.Querier, code string, amount money.Amount) error {
	return nil
}
func (m *mockLedgerAccounts) Debit(ctx context.Context, q repository.Querier, code string, amount money.Amount) error {
	return nil
}
func (m *mockLedgerAccounts) Get(ctx context.Context, code string) (*repository.SystemAccount, error) {
	return &repository.SystemAccount{Code: code}, nil
}
func (m *mockLedgerAccounts) List(ctx context.Context) ([]repository.SystemAccount, error) {
	return nil, nil
}

func (m *mockUserRepository) UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.Password = newHashedPassword
			return nil
		}
	}
	return nil
}
