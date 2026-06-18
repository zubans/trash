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

func (m *mockAdminRepo) CreateTopUpRequest(userID uuid.UUID, amount float64) (*repository.TopUpRequest, error) {
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

func (m *mockAdminRepo) ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	req, ok := m.requests[requestID]
	if !ok {
		return errors.New("not found")
	}
	if req.Status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}
	req.Status = "APPROVED"
	now := time.Now()
	req.UpdatedAt = &now
	req.AdminID = &adminID

	m.transactions = append(m.transactions, &repository.Transaction{
		ID:        uuid.New(),
		UserID:    req.UserID,
		Type:      "TOP_UP",
		Amount:    req.Amount,
		AdminID:   &adminID,
		CreatedAt: time.Now(),
	})
	return nil
}

func (m *mockAdminRepo) RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	req, ok := m.requests[requestID]
	if !ok {
		return errors.New("not found")
	}
	if req.Status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}
	req.Status = "REJECTED"
	now := time.Now()
	req.UpdatedAt = &now
	req.AdminID = &adminID
	return nil
}

func (m *mockAdminRepo) GetTransactions() ([]*repository.Transaction, error) {
	return m.transactions, nil
}

// mockSettingsRepo mocks repository.SettingsRepository.
type mockSettingsRepo struct {
	settings map[string]float64
}

func (m *mockSettingsRepo) GetSettings() (map[string]float64, error) {
	return m.settings, nil
}

func (m *mockSettingsRepo) UpdateSettings(settings map[string]float64) error {
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
	settingsRepo := &mockSettingsRepo{settings: make(map[string]float64)}
	tokenRepo := &mockTokenRepo{blacklisted: make(map[string]time.Time)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, "secret")

	user := &repository.User{
		ID:     uuid.New(),
		Phone:  "79991112233",
		Status: "ACTIVE",
	}
	userRepo.users[user.Phone] = user

	// Test ban
	err := svc.UpdateUserStatus(user.ID, "BANNED")
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
	err = svc.UpdateUserStatus(user.ID, "INVALID")
	if err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestAdminService_TopUpRequests(t *testing.T) {
	userRepo := newMockRepo()
	adminRepo := &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)}
	settingsRepo := &mockSettingsRepo{settings: make(map[string]float64)}
	tokenRepo := &mockTokenRepo{blacklisted: make(map[string]time.Time)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, "secret")

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
	settingsRepo := &mockSettingsRepo{settings: make(map[string]float64)}
	tokenRepo := &mockTokenRepo{blacklisted: make(map[string]time.Time)}

	svc := NewAdminService(userRepo, adminRepo, settingsRepo, tokenRepo, "secret")

	newSettings := map[string]float64{
		"standard_tariff_coeff": 1.5,
		"fine_amount":            200.0,
	}

	err := svc.UpdateSettings(newSettings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	current, err := svc.GetSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if current["standard_tariff_coeff"] != 1.5 || current["fine_amount"] != 200.0 {
		t.Errorf("settings mismatch: got %+v", current)
	}
}
