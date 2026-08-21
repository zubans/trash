package service

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// --- Mocks ---

type mockOrderRepo struct {
	orders []*repository.Order
}

func (m *mockOrderRepo) CreateOrderWithHold(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, holdAmount float64, lastGeo string) (*repository.Order, error) {
	o := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: serviceVariantID,
		IsUrgent:         isUrgent,
		IsAsap:           isAsap,
		Status:           "SEARCHING",
		HoldAmount:       holdAmount,
		FinalAmount:      holdAmount,
	}
	m.orders = append(m.orders, o)
	return o, nil
}

func (m *mockOrderRepo) ConfirmOrderExecution(orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status == "COMPLETED" {
				return errors.New("already completed")
			}
			o.Status = "COMPLETED"
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) CancelOrder(orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status == "CANCELED" {
				return errors.New("already canceled")
			}
			o.Status = "CANCELED"
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) Unassign(orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.Status = "SEARCHING"
			o.ExecutorID = nil
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) GetPendingOrders() ([]*repository.Order, error) {
	var pending []*repository.Order
	for _, o := range m.orders {
		if o.Status == "SEARCHING" {
			pending = append(pending, o)
		}
	}
	return pending, nil
}

func (m *mockOrderRepo) AssignOrder(orderID uuid.UUID, executorID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.ExecutorID = &executorID
			o.Status = "ASSIGNED"
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) GetExecutorAssignedOrders(executorID uuid.UUID) ([]*repository.Order, error) {
	var assigned []*repository.Order
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && o.Status == repository.OrderStatusAssigned {
			assigned = append(assigned, o)
		}
	}
	return assigned, nil
}

func (m *mockOrderRepo) GetCustomerOrders(customerID uuid.UUID) ([]*repository.Order, error) {
	var cust []*repository.Order
	for _, o := range m.orders {
		if o.CustomerID == customerID {
			cust = append(cust, o)
		}
	}
	return cust, nil
}

func (m *mockOrderRepo) GetOrderByID(orderID uuid.UUID) (*repository.Order, error) {
	for _, o := range m.orders {
		if o.ID == orderID {
			return o, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockOrderRepo) CreateConstructionOrder(customerID uuid.UUID, serviceVariantID uuid.UUID, photoURL, lastGeo string) (*repository.Order, error) {
	o := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: serviceVariantID,
		IsUrgent:         false,
		IsAsap:           false,
		Status:           "SEARCHING",
		HoldAmount:       0,
		FinalAmount:      0,
		PhotoURL:         &photoURL,
	}
	m.orders = append(m.orders, o)
	return o, nil
}

func (m *mockOrderRepo) GetAvailableAuctionOrders() ([]*repository.Order, error) {
	var list []*repository.Order
	for _, o := range m.orders {
		if o.ServiceVariantID == constructionVariantID && o.Status == "SEARCHING" {
			list = append(list, o)
		}
	}
	return list, nil
}

// Methods required by the OrderRepository interface.
func (m *mockOrderRepo) Create(order *repository.Order) error {
	m.orders = append(m.orders, order)
	return nil
}

func (m *mockOrderRepo) FindByID(id uuid.UUID) (*repository.Order, error) {
	return m.GetOrderByID(id)
}

func (m *mockOrderRepo) FindAssignedByExecutor(executorID uuid.UUID) ([]repository.Order, error) {
	orders, err := m.GetExecutorAssignedOrders(executorID)
	if err != nil {
		return nil, err
	}
	result := make([]repository.Order, len(orders))
	for i, o := range orders {
		result[i] = *o
	}
	return result, nil
}

func (m *mockOrderRepo) FindByCustomer(customerID uuid.UUID) ([]repository.Order, error) {
	orders, err := m.GetCustomerOrders(customerID)
	if err != nil {
		return nil, err
	}
	result := make([]repository.Order, len(orders))
	for i, o := range orders {
		result[i] = *o
	}
	return result, nil
}

func (m *mockOrderRepo) Assign(orderID, executorID uuid.UUID) error {
	return m.AssignOrder(orderID, executorID)
}

func (m *mockOrderRepo) Confirm(orderID uuid.UUID, finalAmount float64, isDowngraded bool) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status == "COMPLETED" {
				return errors.New("already completed")
			}
			o.Status = "COMPLETED"
			o.FinalAmount = finalAmount
			o.IsDowngraded = isDowngraded
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) Cancel(orderID uuid.UUID) error {
	return m.CancelOrder(orderID)
}

func (m *mockOrderRepo) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	return nil, nil
}

func (m *mockOrderRepo) Execute(orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.Status = repository.OrderStatusExecuted
			return nil
		}
	}
	return errors.New("not found")
}

var (
	standardVariantID    = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	largeVariantID       = uuid.MustParse("66666666-6666-6666-6666-666666666666")
	constructionVariantID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
)

type mockCatalogRepo struct {
	nodes map[uuid.UUID]*repository.ServiceNode
}

func newMockCatalogRepo() *mockCatalogRepo {
	bp100 := 100.0
	bp200 := 200.0
	bp0 := 0.0
	return &mockCatalogRepo{
		nodes: map[uuid.UUID]*repository.ServiceNode{
			standardVariantID: {
				ID:        standardVariantID,
				Code:      "trash_standard_single",
				NodeType:  repository.ServiceNodeTypeVariant,
				BasePrice: &bp100,
				IsAuction: false,
				IsActive:  true,
			},
			largeVariantID: {
				ID:        largeVariantID,
				Code:      "trash_large_regular",
				NodeType:  repository.ServiceNodeTypeVariant,
				BasePrice: &bp200,
				IsAuction: false,
				IsActive:  true,
			},
			constructionVariantID: {
				ID:        constructionVariantID,
				Code:      "trash_construction",
				NodeType:  repository.ServiceNodeTypeVariant,
				BasePrice: &bp0,
				IsAuction: true,
				IsActive:  true,
			},
		},
	}
}

func (m *mockCatalogRepo) CreateNode(node *repository.ServiceNode) error { return nil }
func (m *mockCatalogRepo) UpdateNode(node *repository.ServiceNode) error { return nil }
func (m *mockCatalogRepo) DeleteNode(id uuid.UUID) error                { return nil }
func (m *mockCatalogRepo) GetNodeByID(id uuid.UUID) (*repository.ServiceNode, error) {
	n, ok := m.nodes[id]
	if !ok {
		return nil, nil
	}
	return n, nil
}
func (m *mockCatalogRepo) GetNodeByCode(code string) (*repository.ServiceNode, error) {
	for _, n := range m.nodes {
		if n.Code == code {
			return n, nil
		}
	}
	return nil, nil
}
func (m *mockCatalogRepo) GetRootCategories(activeOnly bool) ([]*repository.ServiceNode, error) { return nil, nil }
func (m *mockCatalogRepo) GetChildren(parentID uuid.UUID, activeOnly bool) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetDescendants(ancestorID uuid.UUID, maxDepth *int) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetAncestors(descendantID uuid.UUID) ([]*repository.ServiceNode, error) { return nil, nil }
func (m *mockCatalogRepo) GetVariantPath(variantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetActiveVariants() ([]*repository.ServiceNode, error) { return nil, nil }
func (m *mockCatalogRepo) GetVariantWithCategory(id uuid.UUID) (*repository.ServiceNode, []*repository.ServiceNode, error) {
	return nil, nil, nil
}
func (m *mockCatalogRepo) HasChildren(id uuid.UUID) (bool, error)         { return false, nil }
func (m *mockCatalogRepo) HasOrders(id uuid.UUID) (bool, error)           { return false, nil }
func (m *mockCatalogRepo) IsDescendantOf(a, b uuid.UUID) (bool, error) { return false, nil }

type orderMockSettingsRepo struct {
	settings map[string]string
}

func (m *orderMockSettingsRepo) GetSettings() (map[string]string, error) {
	return m.settings, nil
}

func (m *orderMockSettingsRepo) UpdateSettings(settings map[string]string) error {
	return nil
}

type orderMockShiftRepo struct{}

func (m *orderMockShiftRepo) GetActiveShift(executorID uuid.UUID) (*repository.Shift, error) {
	return &repository.Shift{Status: repository.ShiftStatusActive}, nil
}

func (m *orderMockShiftRepo) StartShift(executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) EndShift(executorID uuid.UUID) (*repository.Shift, error) { return nil, nil }

func (m *orderMockShiftRepo) GetShiftByID(id uuid.UUID) (*repository.Shift, error) { return nil, nil }

func (m *orderMockShiftRepo) GetShiftsByExecutor(executorID uuid.UUID) ([]*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetActiveShifts() ([]*repository.Shift, error) { return nil, nil }

func (m *orderMockShiftRepo) UploadLocation(executorID uuid.UUID, lat, lon float64) error { return nil }

func (m *orderMockShiftRepo) CheckShiftGeofence(shift *repository.Shift, lat, lon float64) (bool, error) {
	return true, nil
}

func (m *orderMockShiftRepo) ApplyEarlyEndPenalty(shiftID uuid.UUID, amount float64) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) FindActiveByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	return m.GetActiveShift(executorID)
}

func (m *orderMockShiftRepo) Create(shift *repository.Shift) error { return nil }

func (m *orderMockShiftRepo) End(shiftID uuid.UUID) error { return nil }

func (m *orderMockShiftRepo) Penalize(shiftID uuid.UUID, fine float64) error { return nil }

func (m *orderMockShiftRepo) SaveGPSLog(log *repository.GPSLog) error { return nil }

func (m *orderMockShiftRepo) EarlyEnd(shiftID uuid.UUID, fine float64) error { return nil }

func (m *orderMockShiftRepo) GetLastShiftByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) UpdateShiftStatus(shiftID uuid.UUID, status string) error { return nil }

func (m *orderMockShiftRepo) AddGPSLog(shiftID uuid.UUID, lat, lon float64, isInside bool) error { return nil }

func (m *orderMockShiftRepo) GetLastGPSLogs(shiftID uuid.UUID, count int) ([]bool, error) { return nil, nil }

func (m *orderMockShiftRepo) GetGeozoneByID(id int) (*repository.Geozone, error) { return nil, nil }

type mockUserRepo struct {
	lastGeo map[uuid.UUID]string
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{lastGeo: make(map[uuid.UUID]string)}
}

func (m *mockUserRepo) FindByPhone(phone string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) Create(user *repository.User) error                 { return nil }
func (m *mockUserRepo) FindByID(id uuid.UUID) (*repository.User, error)    { return nil, nil }
func (m *mockUserRepo) UpdateStatus(id uuid.UUID, status string) error     { return nil }
func (m *mockUserRepo) UpdateRole(id uuid.UUID, role string) error         { return nil }
func (m *mockUserRepo) UpdateBalance(id uuid.UUID, balance float64) error  { return nil }
func (m *mockUserRepo) UpdateLastGeo(id uuid.UUID, lastGeo string) error {
	m.lastGeo[id] = lastGeo
	return nil
}
func (m *mockUserRepo) CreateCustomerProfile(userID uuid.UUID, address, lastGeo string) error {
	return nil
}
func (m *mockUserRepo) GetCustomerProfile(userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}
func (m *mockUserRepo) FindByEmail(email string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) FindByEmailVerificationToken(token string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) VerifyEmailToken(token string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error { return nil }
func (m *mockUserRepo) ResetPasswordWithCode(email, code, newHashedPassword string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) UpdateCustomerAddress(userID uuid.UUID, address string) error { return nil }
func (m *mockUserRepo) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error { return nil }

func (m *mockOrderRepo) FindAllByExecutor(executorID uuid.UUID) ([]repository.Order, error) {
	return nil, nil
}

type mockTransactionRepo struct {
	txs []*repository.Transaction
}

func (m *mockTransactionRepo) GetBalance(userID uuid.UUID) (float64, error) {
	return 10000.0, nil
}

func (m *mockTransactionRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error {
	return nil
}

func (m *mockTransactionRepo) CreateTransaction(tx *sql.Tx, t *repository.Transaction) error {
	m.txs = append(m.txs, t)
	return nil
}

func (m *mockTransactionRepo) GetTransactionsByUserID(userID uuid.UUID) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockTransactionRepo) RunInTx(fn func(*sql.Tx) error) error {
	return fn(nil)
}

// --- Tests ---

func TestOrderService_CalculatePrice(t *testing.T) {
	setRepo := &orderMockSettingsRepo{
		settings: map[string]string{
			"urgent_tariff_coeff": "3.0",
			"asap_tariff_coeff":   "8.0",
		},
	}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(&mockOrderRepo{}, &mockTransactionRepo{}, setRepo, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	p, err := srv.CalculatePrice(standardVariantID, false, false, false)
	if err != nil || p != 100.0 {
		t.Errorf("expected 100.0, got %f, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(largeVariantID, true, false, false)
	if err != nil || p != 600.0 {
		t.Errorf("expected 600.0, got %f, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(standardVariantID, false, true, false)
	if err != nil || p != 800.0 {
		t.Errorf("expected 800.0, got %f, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(constructionVariantID, false, false, false)
	if err != nil || p != 0.0 {
		t.Errorf("expected 0.0 for auction, got %f, err: %v", p, err)
	}
}

func TestOrderService_CreateOrder(t *testing.T) {
	setRepo := &orderMockSettingsRepo{settings: map[string]string{}}
	orderRepo := &mockOrderRepo{}
	userRepo := newMockUserRepo()
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, setRepo, userRepo, &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	order, err := srv.CreateOrder(customerID, standardVariantID, false, false, "55.7558,37.6173", nil, nil)
	if userRepo.lastGeo[customerID] != "55.7558,37.6173" {
		t.Errorf("expected last_geo to be saved")
	}
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}
	if order.HoldAmount != 100.0 {
		t.Errorf("expected hold amount 100.0, got %f", order.HoldAmount)
	}
	if order.Status != "SEARCHING" {
		t.Errorf("expected status SEARCHING, got %s", order.Status)
	}
	if order.ServiceVariantID != standardVariantID {
		t.Errorf("expected service variant %s, got %s", standardVariantID, order.ServiceVariantID)
	}
}

func TestOrderService_CreateOrder_BothUrgencyFlagsRejected(t *testing.T) {
	catalog := newMockCatalogRepo()
	srv := NewOrderService(&mockOrderRepo{}, &mockTransactionRepo{}, &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)
	_, err := srv.CreateOrder(uuid.New(), standardVariantID, true, true, "addr", nil, nil)
	if err == nil {
		t.Error("expected error when both urgency flags are set")
	}
}

func TestOrderService_ConfirmAndCancel(t *testing.T) {
	setRepo := &orderMockSettingsRepo{settings: map[string]string{}}
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, setRepo, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	order, _ := srv.CreateOrder(customerID, standardVariantID, false, false, "", nil, nil)
	executorID := uuid.New()
	_ = orderRepo.AssignOrder(order.ID, executorID)
	_ = orderRepo.Execute(order.ID)

	err := srv.ConfirmOrder(order.ID)
	if err != nil {
		t.Errorf("expected success confirming order, got err: %v", err)
	}

	err = srv.ConfirmOrder(order.ID)
	if err == nil {
		t.Errorf("expected error double confirming")
	}

	order2, _ := srv.CreateOrder(customerID, largeVariantID, false, false, "", nil, nil)
	err = srv.CancelOrder(order2.ID)
	if err != nil {
		t.Errorf("expected success canceling order, got err: %v", err)
	}
}

func TestOrderService_CreateConstructionOrder(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	_, err := srv.CreateConstructionOrder(customerID, "", "", nil, nil)
	if err == nil {
		t.Error("expected error creating construction order without photo URL")
	}

	order, err := srv.CreateConstructionOrder(customerID, "http://somephoto.jpg", "55.75,37.61", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error creating construction order: %v", err)
	}
	if order.ServiceVariantID != constructionVariantID {
		t.Errorf("expected construction variant %s, got %s", constructionVariantID, order.ServiceVariantID)
	}
	if order.HoldAmount != 0 {
		t.Errorf("expected hold amount 0 for auction, got %f", order.HoldAmount)
	}
}

func TestOrderService_AsapDowngradeOnConfirm(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	order, _ := srv.CreateOrder(customerID, standardVariantID, false, true, "", nil, nil)
	executorID := uuid.New()
	_ = orderRepo.AssignOrder(order.ID, executorID)

	// Simulate deadline in the past to trigger downgrade.
	past := time.Now().Add(-time.Hour)
	for _, o := range orderRepo.orders {
		if o.ID == order.ID {
			o.DeadlineAt = &past
			o.Status = repository.OrderStatusExecuted
		}
	}

	err := srv.ConfirmOrder(order.ID)
	if err != nil {
		t.Fatalf("unexpected error confirming order: %v", err)
	}

	for _, o := range orderRepo.orders {
		if o.ID == order.ID {
			if !o.IsDowngraded {
				t.Error("expected order to be downgraded")
			}
			if o.FinalAmount != 100.0 {
				t.Errorf("expected final amount 100.0 after downgrade, got %f", o.FinalAmount)
			}
		}
	}
}
