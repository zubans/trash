package service

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockOrderRepo struct {
	orders []*repository.Order
}

func (m *mockOrderRepo) CreateOrderWithHold(customerID uuid.UUID, volume string, tariff string, holdAmount float64, lastGeo string) (*repository.Order, error) {
	o := &repository.Order{
		ID:          uuid.New(),
		CustomerID:  customerID,
		VolumeType:  volume,
		SpeedTariff: tariff,
		Status:      "SEARCHING",
		HoldAmount:  holdAmount,
		FinalAmount: holdAmount,
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

func (m *mockOrderRepo) CreateConstructionOrder(customerID uuid.UUID, photoURL, lastGeo string) (*repository.Order, error) {
	o := &repository.Order{
		ID:          uuid.New(),
		CustomerID:  customerID,
		VolumeType:  "CONSTRUCTION",
		SpeedTariff: "CUSTOM",
		Status:      "SEARCHING",
		HoldAmount:  0,
		FinalAmount: 0,
		PhotoURL:    &photoURL,
	}
	m.orders = append(m.orders, o)
	return o, nil
}

func (m *mockOrderRepo) GetAvailableConstructionOrders() ([]*repository.Order, error) {
	var list []*repository.Order
	for _, o := range m.orders {
		if o.VolumeType == "CONSTRUCTION" && o.Status == "SEARCHING" {
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

func (m *mockOrderRepo) Confirm(orderID uuid.UUID, finalAmount float64) error {
	return m.ConfirmOrderExecution(orderID)
}

func (m *mockOrderRepo) Cancel(orderID uuid.UUID) error {
	return m.CancelOrder(orderID)
}

func (m *mockOrderRepo) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	return nil, nil
}

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
func (m *mockUserRepo) UpdateCustomerAddress(userID uuid.UUID, address string) error { return nil }

type mockTransactionRepo struct{}

func (m *mockTransactionRepo) GetBalance(userID uuid.UUID) (float64, error) {
	return 10000.0, nil
}

func (m *mockTransactionRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error {
	return nil
}

func (m *mockTransactionRepo) CreateTransaction(tx *sql.Tx, t *repository.Transaction) error {
	return nil
}

func (m *mockTransactionRepo) RunInTx(fn func(*sql.Tx) error) error {
	return fn(nil)
}

func TestOrderService_CalculatePrice(t *testing.T) {
	setRepo := &mockSettingsRepo{
		settings: map[string]string{
			"standard_tariff_coeff": "1.0",
			"urgent_tariff_coeff":   "3.0",
			"asap_tariff_coeff":     "8.0",
		},
	}
	srv := NewOrderService(&mockOrderRepo{}, &mockTransactionRepo{}, setRepo, newMockUserRepo(), &mockShiftRepo{}, nil, nil)

	// Case 1: Standard Regular
	p, err := srv.CalculatePrice("STANDARD", "REGULAR")
	if err != nil || p != 100.0 {
		t.Errorf("expected 100.0, got %f, err: %v", p, err)
	}

	// Case 2: Large Urgent
	p, err = srv.CalculatePrice("LARGE", "URGENT")
	if err != nil || p != 600.0 {
		t.Errorf("expected 600.0, got %f, err: %v", p, err)
	}

	// Case 3: Construction ASAP
	p, err = srv.CalculatePrice("CONSTRUCTION", "ASAP")
	if err != nil || p != 4000.0 {
		t.Errorf("expected 4000.0, got %f, err: %v", p, err)
	}
}

func TestOrderService_CreateOrder(t *testing.T) {
	setRepo := &mockSettingsRepo{
		settings: map[string]string{
			"standard_tariff_coeff": "1.0",
		},
	}
	orderRepo := &mockOrderRepo{}
	userRepo := newMockUserRepo()
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, setRepo, userRepo, &mockShiftRepo{}, nil, nil)

	customerID := uuid.New()
	order, err := srv.CreateOrder(customerID, "STANDARD", "REGULAR", "55.7558,37.6173", nil, nil)
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
}

func TestOrderService_ConfirmAndCancel(t *testing.T) {
	setRepo := &mockSettingsRepo{
		settings: map[string]string{
			"standard_tariff_coeff": "1.0",
		},
	}
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, setRepo, newMockUserRepo(), &mockShiftRepo{}, nil, nil)

	customerID := uuid.New()
	order, _ := srv.CreateOrder(customerID, "STANDARD", "REGULAR", "", nil, nil)
	executorID := uuid.New()
	_ = orderRepo.AssignOrder(order.ID, executorID)

	err := srv.ConfirmOrder(order.ID)
	if err != nil {
		t.Errorf("expected success confirming order, got err: %v", err)
	}

	// Double confirm should fail (mock repository logic)
	err = srv.ConfirmOrder(order.ID)
	if err == nil {
		t.Errorf("expected error double confirming")
	}

	// Cancel a different order
	order2, _ := srv.CreateOrder(customerID, "LARGE", "REGULAR", "", nil, nil)
	err = srv.CancelOrder(order2.ID)
	if err != nil {
		t.Errorf("expected success canceling order, got err: %v", err)
	}
}

func TestOrderService_CreateConstructionOrder(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	srv := NewOrderService(orderRepo, &mockTransactionRepo{}, nil, newMockUserRepo(), &mockShiftRepo{}, nil, nil)

	customerID := uuid.New()
	_, err := srv.CreateConstructionOrder(customerID, "", "", nil, nil)
	if err == nil {
		t.Error("expected error creating construction order without photo URL")
	}

	order, err := srv.CreateConstructionOrder(customerID, "http://somephoto.jpg", "55.75,37.61", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error creating construction order: %v", err)
	}

	if order.VolumeType != "CONSTRUCTION" || order.SpeedTariff != "CUSTOM" {
		t.Errorf("unexpected volume %s or tariff %s", order.VolumeType, order.SpeedTariff)
	}
}
