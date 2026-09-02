package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// mockAdminRepo подменяет repository.AdminRepository.
type mockAdminRepo struct {
	users        []*repository.User
	requests     map[uuid.UUID]*repository.TopUpRequest
	withdrawals  map[uuid.UUID]*repository.WithdrawalRequest
	transactions []*repository.Transaction
}

func (m *mockAdminRepo) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*repository.User, int, error) {
	return m.users, len(m.users), nil
}

func (m *mockAdminRepo) GetTopUpRequests(ctx context.Context, limit, offset int) ([]*repository.TopUpRequest, error) {
	var reqs []*repository.TopUpRequest
	for _, r := range m.requests {
		reqs = append(reqs, r)
	}
	return reqs, nil
}

func (m *mockAdminRepo) GetTopUpRequestByID(ctx context.Context, id uuid.UUID) (*repository.TopUpRequest, error) {
	r, ok := m.requests[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return r, nil
}

func (m *mockAdminRepo) CreateTopUpRequest(ctx context.Context, q repository.Querier, userID uuid.UUID, amount money.Amount) (*repository.TopUpRequest, error) {
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

func (m *mockAdminRepo) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*repository.WithdrawalRequest, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetWithdrawalRequestByID(ctx context.Context, id uuid.UUID) (*repository.WithdrawalRequest, error) {
	return nil, errors.New("not found")
}

func (m *mockAdminRepo) CreateWithdrawalRequest(ctx context.Context, q repository.Querier, userID uuid.UUID, amount money.Amount) (*repository.WithdrawalRequest, error) {
	req := &repository.WithdrawalRequest{ID: uuid.New(), UserID: userID, Amount: amount, Status: "PENDING", CreatedAt: time.Now()}
	if m.withdrawals == nil {
		m.withdrawals = make(map[uuid.UUID]*repository.WithdrawalRequest)
	}
	m.withdrawals[req.ID] = req
	return req, nil
}

func (m *mockAdminRepo) GetTransactions(ctx context.Context, limit, offset int) ([]*repository.Transaction, error) {
	return m.transactions, nil
}

func (m *mockAdminRepo) TopUpUserBalance(ctx context.Context, userID, adminID uuid.UUID, amount money.Amount) error {
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

func (m *mockAdminRepo) GetActiveShifts(ctx context.Context) ([]*repository.AdminShift, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetActiveOrders(ctx context.Context, limit, offset int) ([]*repository.AdminOrder, error) {
	return nil, nil
}

func (m *mockAdminRepo) GetCompletedOrders(ctx context.Context, f repository.CompletedOrdersFilter) ([]*repository.AdminOrder, int, error) {
	return nil, 0, nil
}

func (m *mockAdminRepo) CompletedOrderFacets(ctx context.Context) (repository.CompletedOrderFacets, error) {
	return repository.CompletedOrderFacets{}, nil
}

// mockSettingsRepo подменяет repository.SettingsRepository.
type mockSettingsRepo struct {
	settings map[string]string
}

func (m *mockSettingsRepo) GetSettings(ctx context.Context) (map[string]string, error) {
	return m.settings, nil
}

func (m *mockSettingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		m.settings[k] = v
	}
	return nil
}

// mockTokenRepo подменяет repository.TokenRepository.
type mockTokenRepo struct {
	blacklisted map[string]time.Time
}

func (m *mockTokenRepo) IsTokenRevoked(ctx context.Context, tokenHash string) (bool, error) {
	exp, ok := m.blacklisted[tokenHash]
	if !ok {
		return false, nil
	}
	if time.Now().After(exp) {
		return false, nil
	}
	return true, nil
}

func (m *mockTokenRepo) RevokeToken(ctx context.Context, tokenHash string, expiresAt time.Time) error {
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

	// Проверяем бан
	err := svc.UpdateUserStatus(context.Background(), user.ID, adminID, "BANNED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := userRepo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("failed to find user: %v", err)
	}
	if updated.Status != "BANNED" {
		t.Errorf("expected BANNED, got %s", updated.Status)
	}

	// Проверяем недопустимый статус
	err = svc.UpdateUserStatus(context.Background(), user.ID, adminID, "INVALID")
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

	// 1. Создаём заявку на пополнение
	req, err := svc.CreateTopUpRequest(context.Background(), user.ID, money.FromRubles(500.0))
	if err != nil {
		t.Fatalf("unexpected error creating top-up: %v", err)
	}
	if req.Amount != money.FromRubles(500) || req.Status != "PENDING" {
		t.Errorf("unexpected request data: %+v", req)
	}

	// 2. Одобряем заявку
	adminID := uuid.New()
	err = svc.ApproveTopUpRequest(context.Background(), req.ID, adminID)
	if err != nil {
		t.Fatalf("unexpected error approving: %v", err)
	}

	approvedReq, err := adminRepo.GetTopUpRequestByID(context.Background(), req.ID)
	if err != nil {
		t.Fatalf("failed to get request: %v", err)
	}
	if approvedReq.Status != "APPROVED" || *approvedReq.AdminID != adminID {
		t.Errorf("request was not approved correctly: %+v", approvedReq)
	}

	// 3. Пробуем одобрить повторно (должно упасть)
	err = svc.ApproveTopUpRequest(context.Background(), req.ID, adminID)
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

	err := svc.UpdateSettings(context.Background(), newSettings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	current, err := svc.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if current["standard_tariff_coeff"] != "1.5" || current["geofence_fine_amount"] != "200.0" || current["currency"] != "RUB" {
		t.Errorf("settings mismatch: got %+v", current)
	}
}

// CountAdmins сообщает, сколько существует администраторов (нужно, чтобы защитить последнего).
func (m *mockAdminRepo) CountAdmins(ctx context.Context) (int, error) {
	return 2, nil
}

// HasPendingWithdrawal сообщает о существующей открытой заявке на вывод.
func (m *mockAdminRepo) HasPendingWithdrawal(ctx context.Context, userID uuid.UUID) (bool, error) {
	return false, nil
}

// LockWithdrawalRequest и SetWithdrawalStatus обслуживают процесс вывода теперь,
// когда он живёт в AdminService.
func (m *mockAdminRepo) LockWithdrawalRequest(ctx context.Context, q repository.Querier, requestID uuid.UUID) (*repository.WithdrawalRequest, error) {
	if req, ok := m.withdrawals[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepo) SetWithdrawalStatus(ctx context.Context, q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.withdrawals[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}

func (m *mockAdminRepo) LockTopUpRequest(ctx context.Context, q repository.Querier, requestID uuid.UUID) (*repository.TopUpRequest, error) {
	if req, ok := m.requests[requestID]; ok {
		return req, nil
	}
	return nil, repository.ErrConflict
}

func (m *mockAdminRepo) SetTopUpStatus(ctx context.Context, q repository.Querier, requestID, adminID uuid.UUID, status string) error {
	req, ok := m.requests[requestID]
	if !ok || req.Status != "PENDING" {
		return repository.ErrConflict
	}
	req.Status = status
	req.AdminID = &adminID
	return nil
}
