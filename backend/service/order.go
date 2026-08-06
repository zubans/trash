package service

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// OrderService handles order lifecycle: creation, assignment, confirmation, cancellation.
type OrderService struct {
	orderRepo       repository.OrderRepository
	transactionRepo repository.TransactionRepository
	settingsRepo    repository.SettingsRepository
	userRepo        repository.UserRepository
	shiftRepo       repository.ShiftRepository
	chatRepo        repository.ChatRepository
	geocoder        *Geocoder
}

// NewOrderService creates an OrderService.
func NewOrderService(orderRepo repository.OrderRepository, transactionRepo repository.TransactionRepository, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, geocoder *Geocoder) *OrderService {
	return &OrderService{orderRepo: orderRepo, transactionRepo: transactionRepo, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, geocoder: geocoder}
}

// CreateOrderRequest contains the data needed to create an order.
type CreateOrderRequest struct {
	VolumeType  string   `json:"volume_type"`
	SpeedTariff string   `json:"speed_tariff"`
	PhotoURL    *string  `json:"photo_url,omitempty"`
	Address     string   `json:"address"`
	Lat         *float64 `json:"lat,omitempty"`
	Lon         *float64 `json:"lon,omitempty"`
}

func basePriceByVolume(volumeType string) float64 {
	switch volumeType {
	case "STANDARD":
		return 100.0
	case "LARGE":
		return 200.0
	case "CONSTRUCTION":
		return 500.0
	default:
		return 100.0
	}
}

func coefficientByTariff(settings map[string]float64, speedTariff string) float64 {
	switch speedTariff {
	case "URGENT":
		return settings["urgent_tariff_coeff"]
	case "ASAP":
		return settings["asap_tariff_coeff"]
	case "REGULAR", "CUSTOM":
		return settings["standard_tariff_coeff"]
	default:
		return settings["standard_tariff_coeff"]
	}
}

func (s *OrderService) loadSettings() map[string]float64 {
	settings := map[string]float64{
		"standard_tariff_coeff": 1.0,
		"urgent_tariff_coeff":   3.0,
		"asap_tariff_coeff":     8.0,
	}
	if s.settingsRepo != nil {
		repoSettings, err := s.settingsRepo.GetSettings()
		if err == nil {
			for k, v := range repoSettings {
				if k == "currency" {
					continue
				}
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					settings[k] = f
				}
			}
		}
	}
	return settings
}

// CalculatePrice returns the price for a given volume and speed tariff.
func (s *OrderService) CalculatePrice(volumeType, speedTariff string) (float64, error) {
	settings := s.loadSettings()
	base := basePriceByVolume(volumeType)
	coeff := coefficientByTariff(settings, speedTariff)
	return base * coeff, nil
}

// CreateOrder creates a standard order and holds customer balance.
func (s *OrderService) CreateOrder(customerID uuid.UUID, volumeType, speedTariff, address string, lat, lon *float64) (*repository.Order, error) {
	holdAmount, err := s.CalculatePrice(volumeType, speedTariff)
	if err != nil {
		return nil, err
	}

	balance, err := s.transactionRepo.GetBalance(customerID)
	if err != nil {
		return nil, err
	}
	if balance < holdAmount {
		return nil, errors.New("insufficient balance")
	}

	var deadline *time.Time
	now := time.Now()
	if speedTariff == "URGENT" {
		d := now.Add(1 * time.Hour)
		deadline = &d
	} else if speedTariff == "ASAP" {
		d := now.Add(15 * time.Minute)
		deadline = &d
	}

	order := &repository.Order{
		ID:          uuid.New(),
		CustomerID:  customerID,
		VolumeType:  volumeType,
		SpeedTariff: speedTariff,
		Status:      repository.OrderStatusSearching,
		HoldAmount:  holdAmount,
		FinalAmount: holdAmount,
		Address:     &address,
		CreatedAt:   now,
		DeadlineAt:  deadline,
	}

	// Resolve coordinates: prefer provided lat/lon, otherwise geocode the address.
	if lat != nil && lon != nil {
		order.PickupLat = lat
		order.PickupLon = lon
	} else if s.geocoder != nil && address != "" {
		geo, err := s.geocoder.Geocode(address)
		if err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	// Persist the order first; financial operations are wrapped in a transaction.
	if err := s.orderRepo.Create(order); err != nil {
		return nil, err
	}

	// Create the chat room for the new order. Non-fatal if it fails.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(order.ID); err != nil {
			// Log and continue; order is already created.
			_ = err
		}
	}

	if err := s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
		if err := s.transactionRepo.UpdateBalance(tx, customerID, -holdAmount); err != nil {
			return err
		}
		return s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
			UserID:  customerID,
			OrderID: &order.ID,
			Type:    string(repository.TransactionTypeHold),
			Amount:  holdAmount,
		})
	}); err != nil {
		return nil, err
	}

	if s.userRepo != nil && address != "" {
		if err := s.userRepo.UpdateLastGeo(customerID, address); err != nil {
			// Non-fatal: order is already created, log and continue.
			_ = err
		}
	}

	return order, nil
}

// Create creates a new order for a customer (alias compatible with handler).
func (s *OrderService) Create(customerID uuid.UUID, req CreateOrderRequest) (*repository.Order, error) {
	return s.CreateOrder(customerID, req.VolumeType, req.SpeedTariff, req.Address, req.Lat, req.Lon)
}

// Accept allows an executor to take an order from the queue.
func (s *OrderService) Accept(orderID, executorID uuid.UUID) error {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil || shift == nil {
		return errors.New("executor has no active shift")
	}
	if shift.Status == repository.ShiftStatusPenalized {
		return errors.New("executor is penalized")
	}
	return s.orderRepo.Assign(orderID, executorID)
}

// ConfirmOrder completes an order and processes payments.
func (s *OrderService) ConfirmOrder(orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusAssigned {
		return errors.New("order is not assigned")
	}
	if order.ExecutorID == nil {
		return errors.New("order has no executor")
	}

	finalAmount := order.HoldAmount
	if order.SpeedTariff == "ASAP" && order.DeadlineAt != nil && time.Now().After(*order.DeadlineAt) {
		order.IsDowngraded = true
		finalAmount, _ = s.CalculatePrice(order.VolumeType, "REGULAR")
	}

	return s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
		// Refund overpayment to customer.
		refund := order.HoldAmount - finalAmount
		if refund > 0 {
			if err := s.transactionRepo.UpdateBalance(tx, order.CustomerID, refund); err != nil {
				return err
			}
			if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
				UserID:  order.CustomerID,
				OrderID: &order.ID,
				Type:    string(repository.TransactionTypeRefund),
				Amount:  refund,
			}); err != nil {
				return err
			}
		}

		// Charge customer final amount.
		if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
			UserID:  order.CustomerID,
			OrderID: &order.ID,
			Type:    string(repository.TransactionTypePayment),
			Amount:  finalAmount,
		}); err != nil {
			return err
		}

		// Reward executor.
		if err := s.transactionRepo.UpdateBalance(tx, *order.ExecutorID, finalAmount); err != nil {
			return err
		}
		if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
			UserID:  *order.ExecutorID,
			OrderID: &order.ID,
			Type:    string(repository.TransactionTypeReward),
			Amount:  finalAmount,
		}); err != nil {
			return err
		}

		return s.orderRepo.Confirm(orderID, finalAmount)
	})
}

// Confirm completes an order for a specific customer (alias compatible with handler).
func (s *OrderService) Confirm(customerID, orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID != customerID {
		return errors.New("forbidden")
	}
	return s.ConfirmOrder(orderID)
}

// CancelOrder cancels an active order and refunds the hold.
func (s *OrderService) CancelOrder(orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusSearching && order.Status != repository.OrderStatusAssigned {
		return errors.New("order cannot be canceled")
	}

	if err := s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
		if err := s.transactionRepo.UpdateBalance(tx, order.CustomerID, order.HoldAmount); err != nil {
			return err
		}
		if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
			UserID:  order.CustomerID,
			OrderID: &order.ID,
			Type:    string(repository.TransactionTypeRefund),
			Amount:  order.HoldAmount,
		}); err != nil {
			return err
		}
		return s.orderRepo.Cancel(orderID)
	}); err != nil {
		return err
	}
	return nil
}

// Cancel cancels an order for a specific customer (alias compatible with handler).
func (s *OrderService) Cancel(customerID, orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID != customerID {
		return errors.New("forbidden")
	}
	return s.CancelOrder(orderID)
}

// CreateConstructionOrder creates a construction waste auction order.
func (s *OrderService) CreateConstructionOrder(customerID uuid.UUID, photoURL, address string, lat, lon *float64) (*repository.Order, error) {
	if photoURL == "" {
		return nil, errors.New("photo URL is required")
	}
	order := &repository.Order{
		ID:          uuid.New(),
		CustomerID:  customerID,
		VolumeType:  "CONSTRUCTION",
		SpeedTariff: "CUSTOM",
		Status:      repository.OrderStatusSearching,
		HoldAmount:  0,
		FinalAmount: 0,
		PhotoURL:    &photoURL,
		Address:     &address,
		CreatedAt:   time.Now(),
	}

	if lat != nil && lon != nil {
		order.PickupLat = lat
		order.PickupLon = lon
	} else if s.geocoder != nil && address != "" {
		geo, err := s.geocoder.Geocode(address)
		if err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	if s.userRepo != nil && address != "" {
		if err := s.userRepo.UpdateLastGeo(customerID, address); err != nil {
			// Non-fatal: order is already created, log and continue.
			_ = err
		}
	}
	if err := s.orderRepo.Create(order); err != nil {
		return nil, err
	}

	// Create the chat room for the new order. Non-fatal if it fails.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(order.ID); err != nil {
			// Log and continue; order is already created.
			_ = err
		}
	}

	return order, nil
}

// GetAvailableConstructionOrders returns open construction waste orders.
func (s *OrderService) GetAvailableConstructionOrders() ([]*repository.Order, error) {
	return s.orderRepo.GetAvailableConstructionOrders()
}

// FindNearbyOrders returns searching standard/large orders near the given coordinates within radiusMeters.
func (s *OrderService) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	return s.orderRepo.FindNearbyOrders(lat, lon, radiusMeters)
}

// ListAssigned returns orders assigned to an executor.
func (s *OrderService) ListAssigned(executorID uuid.UUID) ([]repository.Order, error) {
	return s.orderRepo.FindAssignedByExecutor(executorID)
}

// ListByCustomer returns orders created by a customer.
func (s *OrderService) ListByCustomer(customerID uuid.UUID) ([]repository.Order, error) {
	return s.orderRepo.FindByCustomer(customerID)
}
