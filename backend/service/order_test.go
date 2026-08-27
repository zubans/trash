package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// --- Mocks ---

type mockOrderRepo struct {
	orders []*repository.Order
}

func (m *mockOrderRepo) CreateOrderWithHold(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, holdAmount money.Amount, lastGeo string) (*repository.Order, error) {
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

func (m *mockOrderRepo) Unassign(q repository.Querier, orderID uuid.UUID) error {
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

func (m *mockOrderRepo) GetOrdersMissingCoordinates(limit int) ([]*repository.Order, error) {
	var missing []*repository.Order
	for _, o := range m.orders {
		if o.Status == "SEARCHING" && (o.PickupLat == nil || o.PickupLon == nil) &&
			o.Address != nil && *o.Address != "" {
			missing = append(missing, o)
			if len(missing) >= limit {
				break
			}
		}
	}
	return missing, nil
}

func (m *mockOrderRepo) SetPickupCoordinates(orderID uuid.UUID, lat, lon float64) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.PickupLat = &lat
			o.PickupLon = &lon
			return nil
		}
	}
	return nil
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

func (m *mockOrderRepo) CountActiveOrdersByExecutor(executorID uuid.UUID) (int, error) {
	var count int
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && (o.Status == repository.OrderStatusAssigned || o.Status == "ASSIGNED") {
			count++
		}
	}
	return count, nil
}

func (m *mockOrderRepo) CountExecutedUnconfirmedOrdersByExecutor(executorID uuid.UUID) (int, error) {
	var count int
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && (o.Status == repository.OrderStatusExecuted || o.Status == "EXECUTED") {
			count++
		}
	}
	return count, nil
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
func (m *mockOrderRepo) Create(q repository.Querier, order *repository.Order) error {
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

func (m *mockOrderRepo) Assign(q repository.Querier, orderID, executorID uuid.UUID) error {
	return m.AssignOrder(orderID, executorID)
}

// AssignWithHold mirrors the guarded assignment used when a bid is accepted.
func (m *mockOrderRepo) AssignWithHold(q repository.Querier, orderID, executorID uuid.UUID, holdAmount money.Amount) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status != repository.OrderStatusSearching || o.ExecutorID != nil {
				return repository.ErrConflict
			}
			exec := executorID
			o.ExecutorID = &exec
			o.Status = repository.OrderStatusAssigned
			o.HoldAmount = holdAmount
			o.FinalAmount = holdAmount
			return nil
		}
	}
	return repository.ErrConflict
}

// LockForUpdate mirrors the real repository's row lock read.
func (m *mockOrderRepo) LockForUpdate(q repository.Querier, orderID uuid.UUID) (*repository.Order, error) {
	return m.GetOrderByID(orderID)
}

func (m *mockOrderRepo) SetHoldAmount(q repository.Querier, orderID uuid.UUID, holdAmount money.Amount) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.HoldAmount = holdAmount
			return nil
		}
	}
	return repository.ErrConflict
}

func (m *mockOrderRepo) Confirm(q repository.Querier, orderID uuid.UUID, finalAmount money.Amount, isDowngraded bool) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status != repository.OrderStatusExecuted {
				return repository.ErrConflict
			}
			o.Status = "COMPLETED"
			o.FinalAmount = finalAmount
			o.IsDowngraded = isDowngraded
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) Cancel(q repository.Querier, orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			// Mirrors the guarded UPDATE: only a live order can be canceled,
			// and a second cancel reports a conflict instead of succeeding.
			if o.Status != repository.OrderStatusSearching && o.Status != repository.OrderStatusAssigned {
				return repository.ErrConflict
			}
			o.Status = repository.OrderStatusCanceled
			return nil
		}
	}
	return repository.ErrConflict
}

func (m *mockOrderRepo) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	return nil, nil
}

func (m *mockOrderRepo) Execute(q repository.Querier, orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.Status = repository.OrderStatusExecuted
			return nil
		}
	}
	return errors.New("not found")
}

var (
	standardVariantID     = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	largeVariantID        = uuid.MustParse("66666666-6666-6666-6666-666666666666")
	constructionVariantID = uuid.MustParse("77777777-7777-7777-7777-777777777777")
)

type mockCatalogRepo struct {
	nodes map[uuid.UUID]*repository.ServiceNode
}

func newMockCatalogRepo() *mockCatalogRepo {
	bp100 := money.FromRubles(100)
	bp200 := money.FromRubles(200)
	bp0 := money.Zero
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
func (m *mockCatalogRepo) DeleteNode(id uuid.UUID) error                 { return nil }
func (m *mockCatalogRepo) RestoreNode(id uuid.UUID) error                { return nil }
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
func (m *mockCatalogRepo) GetRootCategories(filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetChildren(parentID uuid.UUID, filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetDescendants(ancestorID uuid.UUID, maxDepth *int) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetAncestors(descendantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetVariantPath(variantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetActiveVariants() ([]*repository.ServiceNode, error) { return nil, nil }
func (m *mockCatalogRepo) GetVariantWithCategory(id uuid.UUID) (*repository.ServiceNode, []*repository.ServiceNode, error) {
	return nil, nil, nil
}
func (m *mockCatalogRepo) HasChildren(id uuid.UUID) (bool, error)      { return false, nil }
func (m *mockCatalogRepo) HasOrders(id uuid.UUID) (bool, error)        { return false, nil }
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

func (m *orderMockShiftRepo) EndShift(executorID uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetShiftByID(id uuid.UUID) (*repository.Shift, error) { return nil, nil }

func (m *orderMockShiftRepo) GetShiftsByExecutor(executorID uuid.UUID) ([]*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetActiveShifts() ([]*repository.Shift, error) { return nil, nil }

func (m *orderMockShiftRepo) UploadLocation(executorID uuid.UUID, lat, lon float64) error { return nil }

func (m *orderMockShiftRepo) CheckShiftGeofence(shift *repository.Shift, lat, lon float64) (bool, error) {
	return true, nil
}

func (m *orderMockShiftRepo) ApplyEarlyEndPenalty(shiftID uuid.UUID, amount money.Amount) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) FindActiveByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	return m.GetActiveShift(executorID)
}

func (m *orderMockShiftRepo) Create(shift *repository.Shift) error { return nil }

func (m *orderMockShiftRepo) End(shiftID uuid.UUID) error { return nil }

func (m *orderMockShiftRepo) Penalize(shiftID uuid.UUID, fine money.Amount) error { return nil }

func (m *orderMockShiftRepo) SaveGPSLog(log *repository.GPSLog) error { return nil }

func (m *orderMockShiftRepo) EarlyEnd(shiftID uuid.UUID, fine money.Amount) error { return nil }

func (m *orderMockShiftRepo) GetLastShiftByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) UpdateShiftStatus(shiftID uuid.UUID, status string) error { return nil }

func (m *orderMockShiftRepo) AddGPSLog(shiftID uuid.UUID, lat, lon float64, isInside bool) error {
	return nil
}

func (m *orderMockShiftRepo) GetLastGPSLogs(shiftID uuid.UUID, count int) ([]bool, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetGeozoneByID(id int) (*repository.Geozone, error) { return nil, nil }

type mockUserRepo struct {
	lastGeo map[uuid.UUID]string
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{lastGeo: make(map[uuid.UUID]string)}
}

func (m *mockUserRepo) FindByPhone(phone string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) Create(user *repository.User) error                 { return nil }
func (m *mockUserRepo) FindByID(id uuid.UUID) (*repository.User, error) {
	// A verified adult user: eligibility rules are exercised separately.
	birth := time.Now().AddDate(-30, 0, 0)
	return &repository.User{ID: id, Role: "EXECUTOR", Status: "ACTIVE", Verified: true, BirthDate: &birth}, nil
}
func (m *mockUserRepo) UpdateStatus(id uuid.UUID, status string) error         { return nil }
func (m *mockUserRepo) UpdateRole(id uuid.UUID, role string) error             { return nil }
func (m *mockUserRepo) UpdateVerified(id uuid.UUID, verified bool) error       { return nil }
func (m *mockUserRepo) UpdateBalance(id uuid.UUID, balance money.Amount) error { return nil }
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
func (m *mockUserRepo) FindByEmailVerificationToken(token string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) VerifyEmailToken(token string) (*repository.User, error) { return nil, nil }
func (m *mockUserRepo) SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error {
	return nil
}
func (m *mockUserRepo) ResetPasswordWithCode(email, code, newHashedPassword string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdateCustomerAddress(userID uuid.UUID, address string) error { return nil }
func (m *mockUserRepo) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error {
	return nil
}
func (m *mockUserRepo) UpdateUserBirthDate(userID uuid.UUID, birthDate time.Time) error { return nil }

func (m *mockOrderRepo) FindAllByExecutor(executorID uuid.UUID) ([]repository.Order, error) {
	return nil, nil
}

type mockTransactionRepo struct {
	txs      []*repository.Transaction
	balances map[uuid.UUID]money.Amount
}

const mockDefaultBalance = money.Amount(10000 * 100)

func (m *mockTransactionRepo) balance(userID uuid.UUID) money.Amount {
	if m.balances == nil {
		m.balances = make(map[uuid.UUID]money.Amount)
	}
	if _, ok := m.balances[userID]; !ok {
		m.balances[userID] = mockDefaultBalance
	}
	return m.balances[userID]
}

func (m *mockTransactionRepo) GetBalance(userID uuid.UUID) (money.Amount, error) {
	return m.balance(userID), nil
}

func (m *mockTransactionRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	m.balances[userID] = m.balance(userID).Add(delta)
	return nil
}

// Debit refuses to go below zero, like the guarded UPDATE in the real repository.
func (m *mockTransactionRepo) Debit(tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	if m.balance(userID) < amount {
		return repository.ErrInsufficientFunds
	}
	m.balances[userID] = m.balance(userID).Sub(amount)
	return nil
}

func (m *mockTransactionRepo) CreateTransaction(tx *sql.Tx, t *repository.Transaction) error {
	m.txs = append(m.txs, t)
	return nil
}

func (m *mockTransactionRepo) GetTransactionsByUserID(userID uuid.UUID) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockTransactionRepo) HasTip(q repository.Querier, orderID uuid.UUID) (bool, error) {
	for _, t := range m.txs {
		if t.OrderID != nil && *t.OrderID == orderID && t.Type == string(repository.TransactionTypeTip) {
			return true, nil
		}
	}
	return false, nil
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
	srv := NewOrderService(&mockOrderRepo{}, testLedger(), setRepo, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	p, err := srv.CalculatePrice(standardVariantID, false, false, false)
	if err != nil || p != money.FromRubles(100) {
		t.Errorf("expected 100.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(largeVariantID, true, false, false)
	if err != nil || p != money.FromRubles(600) {
		t.Errorf("expected 600.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(standardVariantID, false, true, false)
	if err != nil || p != money.FromRubles(800) {
		t.Errorf("expected 800.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(constructionVariantID, false, false, false)
	if err != nil || p != money.Zero {
		t.Errorf("expected 0.0 for auction, got %s, err: %v", p, err)
	}
}

func TestOrderService_CreateOrder(t *testing.T) {
	setRepo := &orderMockSettingsRepo{settings: map[string]string{}}
	orderRepo := &mockOrderRepo{}
	userRepo := newMockUserRepo()
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, testLedger(), setRepo, userRepo, &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	lat, lon := 55.7558, 37.6173
	order, err := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "Россия, Москва, Тверская улица, д. 1", &lat, &lon)
	// last_geo is parsed as "lat,lon" by the matching worker, so only
	// coordinates may be written into it — never the address string.
	if userRepo.lastGeo[customerID] != formatGeo(lat, lon) {
		t.Errorf("expected last_geo to hold coordinates, got %q", userRepo.lastGeo[customerID])
	}
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}
	if order.HoldAmount != money.FromRubles(100) {
		t.Errorf("expected hold amount 100.0, got %s", order.HoldAmount)
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
	srv := NewOrderService(&mockOrderRepo{}, testLedger(), &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)
	_, err := srv.CreateOrder(context.Background(), uuid.New(), standardVariantID, true, true, "addr", nil, nil)
	if err == nil {
		t.Error("expected error when both urgency flags are set")
	}
}

func TestOrderService_ConfirmAndCancel(t *testing.T) {
	setRepo := &orderMockSettingsRepo{settings: map[string]string{}}
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, testLedger(), setRepo, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	order, _ := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, false, "", nil, nil)
	executorID := uuid.New()
	_ = orderRepo.AssignOrder(order.ID, executorID)
	_ = orderRepo.Execute(nil, order.ID)

	err := srv.ConfirmOrder(order.ID)
	if err != nil {
		t.Errorf("expected success confirming order, got err: %v", err)
	}

	err = srv.ConfirmOrder(order.ID)
	if err == nil {
		t.Errorf("expected error double confirming")
	}

	order2, _ := srv.CreateOrder(context.Background(), customerID, largeVariantID, false, false, "", nil, nil)
	err = srv.CancelOrder(order2.ID)
	if err != nil {
		t.Errorf("expected success canceling order, got err: %v", err)
	}
}

func TestOrderService_CreateConstructionOrder(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, testLedger(), nil, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	_, err := srv.CreateConstructionOrder(context.Background(), customerID, "", "", "", nil, nil)
	if err == nil {
		t.Error("expected error creating construction order without photo URL")
	}

	// Only paths produced by our own upload endpoint are accepted; an arbitrary
	// URL would end up rendered in the admin panel.
	if _, err := srv.CreateConstructionOrder(context.Background(), customerID, "http://somephoto.jpg", "55.75,37.61", "", nil, nil); err == nil {
		t.Error("expected an external photo URL to be refused")
	}
	if _, err := srv.CreateConstructionOrder(context.Background(), customerID, "/uploads/../../etc/passwd", "55.75,37.61", "", nil, nil); err == nil {
		t.Error("expected a traversing photo path to be refused")
	}

	order, err := srv.CreateConstructionOrder(context.Background(), customerID, "/uploads/chat/photo.jpg", "55.75,37.61", "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error creating construction order: %v", err)
	}
	if order.ServiceVariantID != constructionVariantID {
		t.Errorf("expected construction variant %s, got %s", constructionVariantID, order.ServiceVariantID)
	}
	if order.HoldAmount != 0 {
		t.Errorf("expected hold amount 0 for auction, got %s", order.HoldAmount)
	}
}

func TestOrderService_AsapDowngradeOnConfirm(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, testLedger(), &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	customerID := uuid.New()
	order, _ := srv.CreateOrder(context.Background(), customerID, standardVariantID, false, true, "", nil, nil)
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
			if o.FinalAmount != money.FromRubles(100) {
				t.Errorf("expected final amount 100.0 after downgrade, got %s", o.FinalAmount)
			}
		}
	}
}

func TestOrderService_AcceptLimits(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	shiftRepo := &orderMockShiftRepo{}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(orderRepo, testLedger(), nil, newMockUserRepo(), shiftRepo, nil, catalog, nil)

	executorID := uuid.New()

	// Test 3 active orders limit
	for i := 0; i < 3; i++ {
		oID := uuid.New()
		orderRepo.orders = append(orderRepo.orders, &repository.Order{
			ID:         oID,
			ExecutorID: &executorID,
			Status:     repository.OrderStatusAssigned,
		})
	}

	newOrderID := uuid.New()
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:     newOrderID,
		Status: repository.OrderStatusSearching,
	})

	err := srv.Accept(newOrderID, executorID)
	if err == nil || err.Error() != "превышен лимит активных заказов (не более 3)" {
		t.Errorf("expected active orders limit error, got: %v", err)
	}

	// Reset assigned orders and test 6 executed unconfirmed limit
	orderRepo.orders = nil
	for i := 0; i < 6; i++ {
		oID := uuid.New()
		orderRepo.orders = append(orderRepo.orders, &repository.Order{
			ID:         oID,
			ExecutorID: &executorID,
			Status:     repository.OrderStatusExecuted,
		})
	}

	newOrderID2 := uuid.New()
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:     newOrderID2,
		Status: repository.OrderStatusSearching,
	})

	err2 := srv.Accept(newOrderID2, executorID)
	if err2 == nil || err2.Error() != "превышен лимит непотвержденных заказчиком исполненных заказов (не более 6)" {
		t.Errorf("expected executed unconfirmed orders limit error, got: %v", err2)
	}
}

// mockAccounts is an in-memory SystemAccountRepository. It mirrors the real one
// closely enough that a test can assert where money went, not just that it left.
type mockAccounts struct {
	balances map[string]money.Amount
}

func newMockAccounts() *mockAccounts {
	return &mockAccounts{balances: map[string]money.Amount{
		repository.AccountEscrow:   0,
		repository.AccountFines:    0,
		repository.AccountDeposits: 0,
		repository.AccountPayouts:  0,
	}}
}

func (m *mockAccounts) Credit(q repository.Querier, code string, amount money.Amount) error {
	if _, ok := m.balances[code]; !ok {
		return repository.ErrUnknownSystemAccount
	}
	m.balances[code] = m.balances[code].Add(amount)
	return nil
}

func (m *mockAccounts) Debit(q repository.Querier, code string, amount money.Amount) error {
	if _, ok := m.balances[code]; !ok {
		return repository.ErrUnknownSystemAccount
	}
	m.balances[code] = m.balances[code].Sub(amount)
	return nil
}

func (m *mockAccounts) Get(code string) (*repository.SystemAccount, error) {
	balance, ok := m.balances[code]
	if !ok {
		return nil, repository.ErrUnknownSystemAccount
	}
	return &repository.SystemAccount{Code: code, Balance: balance}, nil
}

func (m *mockAccounts) List() ([]repository.SystemAccount, error) {
	var list []repository.SystemAccount
	for code, balance := range m.balances {
		list = append(list, repository.SystemAccount{Code: code, Balance: balance})
	}
	return list, nil
}

// newTestLedger wires a ledger over in-memory balances and accounts.
func newTestLedger(txRepo *mockTransactionRepo) (*Ledger, *mockAccounts) {
	accounts := newMockAccounts()
	return NewLedger(txRepo, accounts), accounts
}

// testLedger is the common case: a fresh ledger whose sides are not inspected.
func testLedger() *Ledger {
	l, _ := newTestLedger(&mockTransactionRepo{})
	return l
}

func (m *mockUserRepo) UpdatePassword(userID uuid.UUID, newHashedPassword string) error { return nil }

// TestOrderService_TipOrder covers the tip flow: money moves from the customer
// to the executor, exactly once, and only for a completed order.
func TestOrderService_TipOrder(t *testing.T) {
	customerID := uuid.New()
	executorID := uuid.New()
	orderID := uuid.New()

	txRepo := &mockTransactionRepo{balances: map[uuid.UUID]money.Amount{
		customerID: money.FromRubles(1000),
		executorID: money.FromRubles(0),
	}}
	ledger, _ := newTestLedger(txRepo)

	orderRepo := &mockOrderRepo{orders: []*repository.Order{{
		ID:         orderID,
		CustomerID: customerID,
		ExecutorID: &executorID,
		Status:     repository.OrderStatusCompleted,
	}}}

	srv := NewOrderService(orderRepo, ledger, &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	tip := money.FromRubles(150)
	if err := srv.TipOrder(customerID, orderID, tip); err != nil {
		t.Fatalf("tip failed: %v", err)
	}
	if got := txRepo.balance(customerID); got != money.FromRubles(850) {
		t.Errorf("customer balance: expected 850, got %s", got)
	}
	if got := txRepo.balance(executorID); got != money.FromRubles(150) {
		t.Errorf("executor balance: expected 150, got %s", got)
	}

	// A second tip on the same order is refused, and nothing moves.
	if err := srv.TipOrder(customerID, orderID, tip); err == nil {
		t.Fatal("expected a second tip to be rejected")
	}
	if got := txRepo.balance(customerID); got != money.FromRubles(850) {
		t.Errorf("customer balance changed after a rejected tip: %s", got)
	}
}

func TestOrderService_TipOrder_Rejections(t *testing.T) {
	customerID := uuid.New()
	executorID := uuid.New()

	makeService := func(order *repository.Order, balance money.Amount) *OrderService {
		txRepo := &mockTransactionRepo{balances: map[uuid.UUID]money.Amount{customerID: balance}}
		ledger, _ := newTestLedger(txRepo)
		orderRepo := &mockOrderRepo{orders: []*repository.Order{order}}
		return NewOrderService(orderRepo, ledger, &orderMockSettingsRepo{}, newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)
	}

	completed := func() *repository.Order {
		return &repository.Order{ID: uuid.New(), CustomerID: customerID, ExecutorID: &executorID, Status: repository.OrderStatusCompleted}
	}

	t.Run("non-positive amount", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(1000))
		if err := srv.TipOrder(customerID, o.ID, money.Zero); err == nil {
			t.Fatal("expected a zero tip to be rejected")
		}
	})

	t.Run("not the customer", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(1000))
		if err := srv.TipOrder(uuid.New(), o.ID, money.FromRubles(50)); err == nil {
			t.Fatal("expected a tip from a stranger to be rejected")
		}
	})

	t.Run("order not completed", func(t *testing.T) {
		o := completed()
		o.Status = repository.OrderStatusAssigned
		srv := makeService(o, money.FromRubles(1000))
		if err := srv.TipOrder(customerID, o.ID, money.FromRubles(50)); err == nil {
			t.Fatal("expected a tip on an unfinished order to be rejected")
		}
	})

	t.Run("insufficient balance", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(10))
		err := srv.TipOrder(customerID, o.ID, money.FromRubles(50))
		if !errors.Is(err, repository.ErrInsufficientFunds) {
			t.Fatalf("expected insufficient funds, got %v", err)
		}
	})

	t.Run("above the ceiling", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(1_000_000))
		if err := srv.TipOrder(customerID, o.ID, money.FromRubles(200_000)); err == nil {
			t.Fatal("expected an oversized tip to be rejected")
		}
	})
}
