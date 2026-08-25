package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// mockAdminRepo mocks repository.AdminRepository.
type mockAdminRepo struct {
	users        []*repository.User
	requests     map[uuid.UUID]*repository.TopUpRequest
	withdrawals  map[uuid.UUID]*repository.WithdrawalRequest
	transactions []*repository.Transaction
}

func (m *mockAdminRepo) GetUsers(page, limit int, role, status, search string) ([]*repository.User, int, error) {
	return m.users, len(m.users), nil
}

func (m *mockAdminRepo) GetTopUpRequests() ([]*repository.TopUpRequest, error) {
	var reqs []*repository.TopUpRequest
	for _, r := range m.requests {
		reqs = append(reqs, r)
	}
	return reqs, nil
}

func (m *mockAdminRepo) GetTopUpRequestByID(id uuid.UUID) (*repository.TopUpRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return r, nil
}

func (m *mockAdminRepo) CreateTopUpRequest(q repository.Querier, userID uuid.UUID, amount float64) (*repository.TopUpRequest, error) {
	req := &repository.TopUpRequest{
		ID:        uuid.New(),
		UserID:    userID,
		Amount:    amount,
		Status:    "PENDING",
		CreatedAt: time.Now(),
	}
	m.requests[req.ID] = req
	return req, nil
}

func (m *mockAdminRepo) GetWithdrawalRequests() ([]*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetWithdrawalRequestByID(id uuid.UUID) (*repository.WithdrawalRequest, error) {
	return nil, errors.New("not found")
}

func (m *mockAdminRepo) CreateWithdrawalRequest(q repository.Querier, userID uuid.UUID, amount float64) (*repository.WithdrawalRequest, error) {
	req := &repository.WithdrawalRequest{ID: uuid.New(), UserID: userID, Amount: amount, Status: "PENDING", CreatedAt: time.Now()}
	if m.withdrawals == nil {
		m.withdrawals = make(map[uuid.UUID]*repository.WithdrawalRequest)
	}
	m.withdrawals[req.ID] = req
	return req, nil
}

func (m *mockAdminRepo) GetTransactions() ([]*repository.Transaction, error) {
	return m.transactions, nil
}

func (m *mockAdminRepo) TopUpUserBalance(userID, adminID uuid.UUID, amount float64) error {
	m.transactions = append(m.transactions, &repository.Transaction{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      "TOP_UP",
		Amount:    amount,
		AdminID:   &adminID,
		CreatedAt: time.Now(),
	})
	return nil
}

func (m *mockAdminRepo) GetActiveShifts() ([]*repository.AdminShift, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetActiveOrders() ([]*repository.AdminOrder, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetCompletedOrders() ([]*repository.AdminOrder, error) {
	return nil, nil
}

// mockSettingsRepo mocks repository.SettingsRepository.
type mockSettingsRepo struct {
	settings map[string]string
}

func (m *mockSettingsRepo) GetSettings() (map[string]string, error) {
	return m.settings, nil
}

func (m *mockSettingsRepo) UpdateSettings(settings map[string]string) error {
	for k, v := range settings {
		m.settings[k] = v
	}
	return nil
}

// mockTokenRepo mocks repository.TokenRepository.
type mockTokenRepo struct {
	blacklisted map[string]time.Time
}

func (m *mockTokenRepo) IsTokenRevoked(tokenHash string) (bool, error) {
	exp, ok := m.blacklisted[tokenHash]
	if !ok {
		return false, nil
	}
	if time.Now().After(exp) {
		return false, nil
	}
	return true, nil
}

func (m *mockTokenRepo) RevokeToken(tokenHash string, expiresAt time.Time) error {
	m.blacklisted[tokenHash] = expiresAt
	return nil
}

func TestAdminService_UpdateUserStatus(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(&mockTransactionRepo{}, newMockAccounts()))

	user := &repository.User{
		ID:     uuid.New(),
		Phone:  "79991112233",
		Status: "ACTIVE",
	}
	userRepo.users[user.Phone] = user

	adminID := uuid.New()

	// Test ban
	err := svc.UpdateUserStatus(user.ID, adminID, "BANNED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := userRepo.FindByID(user.ID)
	if err != nil {
		t.Fatalf("failed to find user: %v", err)
	}
	if updated.Status != "BANNED" {
		t.Errorf("expected BANNED, got %s", updated.Status)
	}

	// Test invalid status
	err = svc.UpdateUserStatus(user.ID, adminID, "INVALID")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestAdminService_TopUpRequests(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(&mockTransactionRepo{}, newMockAccounts()))

	user := &repository.User{
		ID:     uuid.New(),
		Phone:  "79991112233",
		Status: "ACTIVE",
	}
	userRepo.users[user.Phone] = user

	// 1. Create top up request
	req, err := svc.CreateTopUpRequest(user.ID, 500.0)
	if err != nil {
		t.Fatalf("unexpected error creating top-up: %v", err)
	}
	if req.Amount != 500.0 || req.Status != "PENDING" {
		t.Errorf("unexpected request data: %+v", req)
	}

	// 2. Approve request
	adminID := uuid.New()
	err = svc.ApproveTopUpRequest(req.ID, adminID)
	if err != nil {
		t.Fatalf("unexpected error approving: %v", err)
	}

	approvedReq, err := adminRepo.GetTopUpRequestByID(req.ID)
	if err != nil {
		t.Fatalf("failed to get request: %v", err)
	}
	if approvedReq.Status != "APPROVED" || *approvedReq.AdminID != adminID {
		t.Errorf("request was not approved correctly: %+v", approvedReq)
	}

	// 3. Try approving again (should fail)
	err = svc.ApproveTopUpRequest(req.ID, adminID)
	if err == nil {
		t.Error("expected error trying to approve an already approved request")
	}
}

func TestAdminService_Settings(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]string)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, "secret", nil).
		WithLedger(NewLedger(&mockTransactionRepo{}, newMockAccounts()))

	newSettings := map[string]string{
		"standard_tariff_coeff": "1.5",
		"geofence_fine_amount":  "200.0",
		"currency":              "RUB",
	}

	err := svc.UpdateSettings(newSettings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	current, err := svc.GetSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if current["standard_tariff_coeff"] != "1.5" || current["geofence_fine_amount"] != "200.0" || current["currency"] != "RUB" {
		t.Errorf("settings mismatch: got %+v", current)
	}
}

// CountAdmins reports how many administrators exist (used to protect the last one).
func (m *mockAdminRepo) CountAdmins() (int, error) {
	return 2, nil
}

// HasPendingWithdrawal reports an existing open withdrawal request.
func (m *mockAdminRepo) HasPendingWithdrawal(userID uuid.UUID) (bool, error) {
	return false, nil
}

// LockWithdrawalRequest and SetWithdrawalStatus back the withdrawal workflow now
// that it lives in AdminService.
func (m *mockAdminRepo) LockWithdrawalRequest(q repository.Querier, requestID uuid.UUID) (*repository.WithdrawalRequest, error) {
	if req, ok := m.withdrawals[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepo) SetWithdrawalStatus(q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.withdrawals[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}

func (m *mockAdminRepo) LockTopUpRequest(q repository.Querier, requestID uuid.UUID) (*repository.TopUpRequest, error) {
	if req, ok := m.requests[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepo) SetTopUpStatus(q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.requests[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}
