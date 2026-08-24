package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	catalogRepo     repository.ServiceCatalogRepository
	geocoder        *Geocoder
}

// NewOrderService creates an OrderService.
func NewOrderService(orderRepo repository.OrderRepository, transactionRepo repository.TransactionRepository, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, catalogRepo repository.ServiceCatalogRepository, geocoder *Geocoder) *OrderService {
	return &OrderService{orderRepo: orderRepo, transactionRepo: transactionRepo, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, catalogRepo: catalogRepo, geocoder: geocoder}
}

// CreateOrderRequest contains the data needed to create an order.
type CreateOrderRequest struct {
	ServiceVariantID uuid.UUID `json:"service_variant_id"`
	IsUrgent         bool      `json:"is_urgent"`
	IsAsap           bool      `json:"is_asap"`
	Comment          string    `json:"comment,omitempty"`
	PhotoURL         *string   `json:"photo_url,omitempty"`
	Address          string    `json:"address"`
	Lat              *float64  `json:"lat,omitempty"`
	Lon              *float64  `json:"lon,omitempty"`
}

func (s *OrderService) hydrateServiceVariant(order *repository.Order) {
	if order == nil {
		return
	}
	if variant, err := s.catalogRepo.GetNodeByID(order.ServiceVariantID); err == nil && variant != nil {
		order.ServiceVariant = variant
	}
	if order.ExecutorID != nil && s.userRepo != nil {
		if execUser, err := s.userRepo.FindByID(*order.ExecutorID); err == nil && execUser != nil {
			order.ExecutorPhone = execUser.Phone
			var nameParts []string
			if execUser.FirstName != "" {
				nameParts = append(nameParts, execUser.FirstName)
			}
			if execUser.Patronymic != "" {
				nameParts = append(nameParts, execUser.Patronymic)
			}
			if execUser.LastName != "" {
				runes := []rune(strings.TrimSpace(execUser.LastName))
				if len(runes) > 0 {
					nameParts = append(nameParts, string(runes[0])+".")
				}
			}
			order.ExecutorName = strings.Join(nameParts, " ")
		}
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

// CalculatePrice returns the price for a given service variant and urgency flags.
func (s *OrderService) CalculatePrice(serviceVariantID uuid.UUID, isUrgent, isAsap, isDowngraded bool) (float64, error) {
	variant, err := s.catalogRepo.GetNodeByID(serviceVariantID)
	if err != nil {
		return 0, err
	}
	if variant == nil || !variant.IsVariant() {
		return 0, errors.New("invalid service variant")
	}
	if variant.BasePrice == nil {
		return 0, errors.New("variant has no base price")
	}

	price := *variant.BasePrice

	if variant.IsAuction {
		return 0, nil
	}

	if isDowngraded {
		return price, nil
	}

	settings := s.loadSettings()
	switch {
	case isAsap:
		price *= settings["asap_tariff_coeff"]
	case isUrgent:
		price *= settings["urgent_tariff_coeff"]
	}

	return price, nil
}

// CreateOrder creates a standard order and holds customer balance.
func (s *OrderService) CreateOrder(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, lat, lon *float64) (*repository.Order, error) {
	return s.CreateOrderWithComment(customerID, serviceVariantID, isUrgent, isAsap, address, "", lat, lon)
}

// CreateOrderWithComment creates a standard order with optional comment and holds customer balance.
func (s *OrderService) CreateOrderWithComment(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, comment string, lat, lon *float64) (*repository.Order, error) {
	if isUrgent && isAsap {
		return nil, errors.New("cannot set both urgent and asap flags")
	}

	holdAmount, err := s.CalculatePrice(serviceVariantID, isUrgent, isAsap, false)
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
	if isUrgent {
		d := now.Add(1 * time.Hour)
		deadline = &d
	} else if isAsap {
		d := now.Add(15 * time.Minute)
		deadline = &d
	}

	var commentPtr *string
	if strings.TrimSpace(comment) != "" {
		c := strings.TrimSpace(comment)
		commentPtr = &c
	}

	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: serviceVariantID,
		IsUrgent:         isUrgent,
		IsAsap:           isAsap,
		Comment:          commentPtr,
		Status:           repository.OrderStatusSearching,
		HoldAmount:       holdAmount,
		FinalAmount:      holdAmount,
		Address:          &address,
		CreatedAt:        now,
		DeadlineAt:       deadline,
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

	s.hydrateServiceVariant(order)
	return order, nil
}

// Create creates a new order for a customer (alias compatible with handler).
func (s *OrderService) Create(customerID uuid.UUID, req CreateOrderRequest) (*repository.Order, error) {
	return s.CreateOrderWithComment(customerID, req.ServiceVariantID, req.IsUrgent, false, req.Address, req.Comment, req.Lat, req.Lon)
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

	balance, err := s.transactionRepo.GetBalance(executorID)
	if err != nil {
		return err
	}
	minBalanceLimit := 0.0
	if s.settingsRepo != nil {
		st, _ := s.settingsRepo.GetSettings()
		if valStr, ok := st["min_balance_limit"]; ok {
			if parsed, pErr := strconv.ParseFloat(valStr, 64); pErr == nil {
				minBalanceLimit = parsed
			}
		}
	}
	// Limit is specified as negative threshold (e.g. -500.0 or 0.0)
	if minBalanceLimit > 0 {
		minBalanceLimit = -minBalanceLimit
	}
	if balance < minBalanceLimit {
		return errors.New(fmt.Sprintf("нельзя брать новые заказы: баланс %.2f ниже допустимого лимита (%.2f)", balance, minBalanceLimit))
	}

	activeCount, err := s.orderRepo.CountActiveOrdersByExecutor(executorID)
	if err != nil {
		return err
	}
	if activeCount >= 3 {
		return errors.New("превышен лимит активных заказов (не более 3)")
	}

	executedCount, err := s.orderRepo.CountExecutedUnconfirmedOrdersByExecutor(executorID)
	if err != nil {
		return err
	}
	if executedCount >= 6 {
		return errors.New("превышен лимит непотвержденных заказчиком исполненных заказов (не более 6)")
	}

	return s.orderRepo.Assign(orderID, executorID)
}

// RejectAssignedOrder allows an executor to reject an assigned order with a 50% penalty fine.
func (s *OrderService) RejectAssignedOrder(orderID, executorID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
		return errors.New("order is not assigned to this executor")
	}

	penalty := order.HoldAmount * 0.5

	return s.transactionRepo.RunInTx(func(tx *sql.Tx) error {
		if penalty > 0 {
			if err := s.transactionRepo.UpdateBalance(tx, executorID, -penalty); err != nil {
				return err
			}
			if err := s.transactionRepo.CreateTransaction(tx, &repository.Transaction{
				UserID:  executorID,
				OrderID: &order.ID,
				Type:    "FINE",
				Amount:  penalty,
			}); err != nil {
				return err
			}
		}
		return s.orderRepo.Unassign(orderID)
	})
}

// ExecuteOrder marks an order as EXECUTED by the executor and sends a system chat message.
func (s *OrderService) ExecuteOrder(orderID, executorID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
		return errors.New("order is not assigned to this executor")
	}

	if err := s.orderRepo.Execute(orderID); err != nil {
		return err
	}

	// Send system notification message in chat
	if s.chatRepo != nil {
		chat, err := s.chatRepo.GetChatByOrderID(orderID)
		if err == nil && chat != nil {
			_, _ = s.chatRepo.SaveMessage(chat.ID, executorID, "📦 Исполнитель отметил(а) выполнение заказа! Пожалуйста, подтвердите приемку работы.")
		}
	}

	return nil
}

// ConfirmOrder completes an order and processes payments.
func (s *OrderService) ConfirmOrder(orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusExecuted {
		return errors.New("order must be marked as executed by the executor before confirmation")
	}
	if order.ExecutorID == nil {
		return errors.New("order has no executor")
	}

	finalAmount := order.HoldAmount
	if order.IsAsap && order.DeadlineAt != nil && time.Now().After(*order.DeadlineAt) {
		order.IsDowngraded = true
		finalAmount, _ = s.CalculatePrice(order.ServiceVariantID, false, false, true)
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

		return s.orderRepo.Confirm(orderID, finalAmount, order.IsDowngraded)
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
func (s *OrderService) CreateConstructionOrder(customerID uuid.UUID, photoURL, address, comment string, lat, lon *float64) (*repository.Order, error) {
	if photoURL == "" {
		return nil, errors.New("photo URL is required")
	}

	variant, err := s.catalogRepo.GetNodeByCode("trash_construction")
	if err != nil {
		return nil, err
	}
	if variant == nil {
		return nil, errors.New("construction variant not found")
	}

	var commentPtr *string
	if strings.TrimSpace(comment) != "" {
		c := strings.TrimSpace(comment)
		commentPtr = &c
	}

	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: variant.ID,
		IsUrgent:         false,
		IsAsap:           false,
		Comment:          commentPtr,
		Status:           repository.OrderStatusSearching,
		HoldAmount:       0,
		FinalAmount:      0,
		PhotoURL:         &photoURL,
		Address:          &address,
		CreatedAt:        time.Now(),
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

	s.hydrateServiceVariant(order)
	return order, nil
}

// GetAvailableConstructionOrders returns open construction waste orders.
func (s *OrderService) GetAvailableConstructionOrders() ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetAvailableAuctionOrders()
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(o)
	}
	return orders, nil
}

// GetAvailableConstructionOrdersForExecutor returns open construction waste orders filtered for an executor.
func (s *OrderService) GetAvailableConstructionOrdersForExecutor(executorID uuid.UUID) ([]*repository.Order, error) {
	executor, _ := s.userRepo.FindByID(executorID)
	executorAge := 0
	executorVerified := false
	if executor != nil {
		executorAge = executor.GetAge()
		executorVerified = executor.IsVerified()
	}

	orders, err := s.orderRepo.GetAvailableAuctionOrders()
	if err != nil {
		return nil, err
	}

	filtered := []*repository.Order{}
	for _, o := range orders {
		s.hydrateServiceVariant(o)

		// 1. Filter: Customer MUST be verified ("показ заказов только от верифицированных пользователей")
		customer, err := s.userRepo.FindByID(o.CustomerID)
		if err == nil && customer != nil {
			if !customer.IsVerified() {
				continue
			}
		}

		// 2. Filter: If service variant requires verification, executor must be verified
		if o.ServiceVariant != nil {
			if o.ServiceVariant.RequiresVerification && !executorVerified {
				continue
			}
			// 3. Filter: If service variant has age restriction (min_age > 0), executor age must be >= min_age
			if o.ServiceVariant.MinAge > 0 && executorAge < o.ServiceVariant.MinAge {
				continue
			}
		}

		filtered = append(filtered, o)
	}

	return filtered, nil
}

// FindNearbyOrders returns searching standard/large orders near the given coordinates within radiusMeters.
func (s *OrderService) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	orders, err := s.orderRepo.FindNearbyOrders(lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(o)
	}
	return orders, nil
}

// FindNearbyOrdersForExecutor returns searching standard/large orders near the given coordinates filtered for an executor.
func (s *OrderService) FindNearbyOrdersForExecutor(executorID uuid.UUID, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	executor, _ := s.userRepo.FindByID(executorID)
	executorAge := 0
	executorVerified := false
	if executor != nil {
		executorAge = executor.GetAge()
		executorVerified = executor.IsVerified()
	}

	orders, err := s.orderRepo.FindNearbyOrders(lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}

	filtered := []*repository.Order{}
	for _, o := range orders {
		s.hydrateServiceVariant(o)

		// 1. Filter: Customer MUST be verified ("показ заказов только от верифицированных пользователей")
		customer, err := s.userRepo.FindByID(o.CustomerID)
		if err == nil && customer != nil {
			if !customer.IsVerified() {
				continue
			}
		}

		// 2. Filter: If service variant requires verification, executor must be verified
		if o.ServiceVariant != nil {
			if o.ServiceVariant.RequiresVerification && !executorVerified {
				continue
			}
			// 3. Filter: If service variant has age restriction (min_age > 0), executor age must be >= min_age
			if o.ServiceVariant.MinAge > 0 && executorAge < o.ServiceVariant.MinAge {
				continue
			}
		}

		filtered = append(filtered, o)
	}

	return filtered, nil
}

// ListAssigned returns orders assigned to an executor.
func (s *OrderService) ListAssigned(executorID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetExecutorAssignedOrders(executorID)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(o)
	}
	return orders, nil
}

// ListByCustomer returns orders created by a customer.
func (s *OrderService) ListByCustomer(customerID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetCustomerOrders(customerID)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(o)
	}
	return orders, nil
}
