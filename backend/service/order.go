package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// OrderService handles order lifecycle: creation, assignment, confirmation, cancellation.
type OrderService struct {
	orderRepo    repository.OrderRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	userRepo     repository.UserRepository
	shiftRepo    repository.ShiftRepository
	chatRepo     repository.ChatRepository
	catalogRepo  repository.ServiceCatalogRepository
	geocoder     *Geocoder
}

// NewOrderService creates an OrderService.
func NewOrderService(orderRepo repository.OrderRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, catalogRepo repository.ServiceCatalogRepository, geocoder *Geocoder) *OrderService {
	return &OrderService{orderRepo: orderRepo, ledger: ledger, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, catalogRepo: catalogRepo, geocoder: geocoder}
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
func (s *OrderService) CalculatePrice(serviceVariantID uuid.UUID, isUrgent, isAsap, isDowngraded bool) (money.Amount, error) {
	variant, err := s.catalogRepo.GetNodeByID(serviceVariantID)
	if err != nil {
		return money.Zero, err
	}
	if variant == nil || !variant.IsVariant() {
		return money.Zero, errors.New("invalid service variant")
	}
	if variant.BasePrice == nil {
		return money.Zero, errors.New("variant has no base price")
	}

	price := *variant.BasePrice

	if variant.IsAuction {
		return money.Zero, nil
	}

	if isDowngraded {
		return price, nil
	}

	// Scale rounds once, here, instead of letting a float coefficient smear the
	// result across the rest of the flow.
	settings := s.loadSettings()
	switch {
	case isAsap:
		price = price.Scale(settings["asap_tariff_coeff"])
	case isUrgent:
		price = price.Scale(settings["urgent_tariff_coeff"])
	}

	return price, nil
}

// CreateOrder creates a standard order and holds customer balance.
func (s *OrderService) CreateOrder(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, lat, lon *float64) (*repository.Order, error) {
	return s.CreateOrderWithComment(customerID, serviceVariantID, isUrgent, isAsap, address, "", lat, lon)
}

// CreateOrderWithComment creates a standard order with optional comment and
// holds the customer balance. Order creation, the balance hold and the ledger
// entry all happen in one transaction: the debit is guarded by the balance so
// concurrent requests cannot spend the same money twice, and a failure at any
// step leaves neither an order nor a hold behind.
func (s *OrderService) CreateOrderWithComment(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, comment string, lat, lon *float64) (*repository.Order, error) {
	if isUrgent && isAsap {
		return nil, errors.New("cannot set both urgent and asap flags")
	}

	variant, err := s.catalogRepo.GetNodeByID(serviceVariantID)
	if err != nil {
		return nil, err
	}
	if variant == nil || !variant.IsVariant() {
		return nil, errors.New("invalid service variant")
	}
	if !variant.IsActive {
		return nil, errors.New("service variant is not available")
	}
	if variant.IsAuction {
		return nil, errors.New("auction variants are ordered through the construction order endpoint")
	}

	holdAmount, err := s.CalculatePrice(serviceVariantID, isUrgent, isAsap, false)
	if err != nil {
		return nil, err
	}
	if !holdAmount.IsPositive() {
		return nil, errors.New("invalid order price")
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

	if err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		// The order row goes in first: the ledger entry references it, and
		// transactions.order_id is a foreign key checked immediately. Ordering
		// costs nothing here — both statements share one transaction, so a
		// failed hold rolls the order back with it.
		if err := s.orderRepo.Create(tx, order); err != nil {
			return err
		}
		// Reserve is a single conditional debit paired with a credit to escrow:
		// the money is not destroyed, it moves to the account that holds it for
		// the duration of the order.
		return s.ledger.Reserve(tx, customerID, repository.AccountEscrow, holdAmount, repository.TransactionTypeHold, &order.ID)
	}); err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, errors.New("insufficient balance")
		}
		return nil, err
	}

	// Everything below is best-effort: the order and its hold are already committed.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}
	if s.userRepo != nil && order.PickupLat != nil && order.PickupLon != nil {
		if err := s.userRepo.UpdateLastGeo(customerID, formatGeo(*order.PickupLat, *order.PickupLon)); err != nil {
			log.Printf("[OrderService] failed to update last_geo for %s: %v", customerID, err)
		}
	}

	metrics.OrderEvent("created")
	s.hydrateServiceVariant(order)
	return order, nil
}

// Create creates a new order for a customer (alias compatible with handler).
func (s *OrderService) Create(customerID uuid.UUID, req CreateOrderRequest) (*repository.Order, error) {
	return s.CreateOrderWithComment(customerID, req.ServiceVariantID, req.IsUrgent, false, req.Address, req.Comment, req.Lat, req.Lon)
}

// Accept allows an executor to take an order from the queue. Every restriction
// that the order list applies when showing an order is re-checked here, because
// the list is only a convenience — this method is the actual authorisation
// point.
func (s *OrderService) Accept(orderID, executorID uuid.UUID) error {
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil || shift == nil {
		return errors.New("executor has no active shift")
	}
	if shift.Status == repository.ShiftStatusPenalized {
		return errors.New("executor is penalized")
	}

	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID == executorID {
		return errors.New("нельзя брать собственный заказ")
	}
	if err := s.checkExecutorEligibility(executorID, order.ServiceVariantID); err != nil {
		return err
	}

	balance, err := s.ledger.GetBalance(executorID)
	if err != nil {
		return err
	}
	// The limit is configured as a magnitude and applied as a negative floor,
	// e.g. min_balance_limit=500 means "no new orders below -500".
	minBalanceLimit := money.FromRubles(-math.Abs(s.settingsFloat("min_balance_limit", defaultMinBalanceLimit)))
	if balance < minBalanceLimit {
		return fmt.Errorf("нельзя брать новые заказы: баланс %s ниже допустимого лимита (%s)", balance, minBalanceLimit)
	}

	maxActive := settingInt(s.settingsRepo, "max_active_orders", defaultMaxActiveOrders)
	activeCount, err := s.orderRepo.CountActiveOrdersByExecutor(executorID)
	if err != nil {
		return err
	}
	if activeCount >= maxActive {
		return fmt.Errorf("превышен лимит активных заказов (не более %d)", maxActive)
	}

	maxExecuted := settingInt(s.settingsRepo, "max_executed_unconfirmed_orders", defaultMaxExecutedUnconfirmed)
	executedCount, err := s.orderRepo.CountExecutedUnconfirmedOrdersByExecutor(executorID)
	if err != nil {
		return err
	}
	if executedCount >= maxExecuted {
		return fmt.Errorf("превышен лимит непотвержденных заказчиком исполненных заказов (не более %d)", maxExecuted)
	}

	if err := s.orderRepo.Assign(nil, orderID, executorID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("заказ уже взят другим исполнителем")
		}
		return err
	}
	metrics.OrderEvent("accepted")
	return nil
}

// checkExecutorEligibility loads the executor and the service variant and
// applies the shared age/verification rules.
func (s *OrderService) checkExecutorEligibility(executorID, variantID uuid.UUID) error {
	if s.userRepo == nil {
		return nil
	}
	executor, err := s.userRepo.FindByID(executorID)
	if err != nil {
		return errors.New("executor not found")
	}
	variant, err := s.catalogRepo.GetNodeByID(variantID)
	if err != nil {
		return err
	}
	return canExecutorTakeOrder(executor, variant)
}

// settingsFloat reads a numeric system setting with a fallback default.
func (s *OrderService) settingsFloat(key string, defaultValue float64) float64 {
	return settingFloat(s.settingsRepo, key, defaultValue)
}

// RejectAssignedOrder allows an executor to drop an assigned order. The
// executor is fined a share of the order value (see reject_penalty_share) and
// the order returns to the search pool. Fine and unassignment share one
// transaction, so the executor is never charged for an order that stayed
// assigned to them.
func (s *OrderService) RejectAssignedOrder(orderID, executorID uuid.UUID) error {
	share := s.settingsFloat("reject_penalty_share", defaultRejectPenaltyShare)
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}

	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
			return errors.New("order is not assigned to this executor")
		}

		// The penalty is collected, not destroyed: it lands on the fines account.
		penalty := order.HoldAmount.Scale(share)
		if err := s.ledger.Charge(tx, executorID, repository.AccountFines, penalty, repository.TransactionTypeFine, &order.ID); err != nil {
			return err
		}
		return s.orderRepo.Unassign(tx, orderID)
	})
	if err == nil {
		metrics.OrderEvent("rejected")
	}
	return err
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

	if err := s.orderRepo.Execute(nil, orderID); err != nil {
		return err
	}
	metrics.OrderEvent("executed")

	// Send system notification message in chat
	if s.chatRepo != nil {
		chat, err := s.chatRepo.GetChatByOrderID(orderID)
		if err == nil && chat != nil {
			_, _ = s.chatRepo.SaveMessage(chat.ID, executorID, "📦 Исполнитель отметил(а) выполнение заказа! Пожалуйста, подтвердите приемку работы.")
		}
	}

	return nil
}

// ConfirmOrder completes an order and processes payments. The order row is
// locked and re-read inside the transaction, so two concurrent confirmations
// cannot both pay out the executor, and the payout is derived from the hold
// that is actually still held (see the SLA downgrade path).
func (s *OrderService) ConfirmOrder(orderID uuid.UUID) error {
	// Counted after the transaction returns, never inside it: a confirmation
	// that rolled back paid nobody and must not show up as revenue.
	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(tx, orderID)
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
		isDowngraded := order.IsDowngraded
		if order.IsAsap && order.DeadlineAt != nil && time.Now().After(*order.DeadlineAt) {
			downgraded, err := s.CalculatePrice(order.ServiceVariantID, false, false, true)
			if err != nil {
				return err
			}
			if downgraded < finalAmount {
				isDowngraded = true
				finalAmount = downgraded
			}
		}

		// Escrow holds exactly order.HoldAmount for this order, and it drains
		// completely here: the unspent part back to the customer, the rest to
		// the executor.
		refund := order.HoldAmount.Sub(finalAmount)
		if err := s.ledger.Release(tx, repository.AccountEscrow, order.CustomerID, refund, repository.TransactionTypeRefund, &order.ID, nil); err != nil {
			return err
		}

		// The customer's money left the balance at hold time; this entry records
		// the hold being spent rather than a second debit.
		if err := s.ledger.Note(tx, order.CustomerID, repository.AccountEscrow, finalAmount, repository.TransactionTypePayment, &order.ID); err != nil {
			return err
		}

		if err := s.ledger.Release(tx, repository.AccountEscrow, *order.ExecutorID, finalAmount, repository.TransactionTypeReward, &order.ID, nil); err != nil {
			return err
		}

		if err := s.orderRepo.SetHoldAmount(tx, order.ID, money.Zero); err != nil {
			return err
		}
		return s.orderRepo.Confirm(tx, orderID, finalAmount, isDowngraded)
	})
	if err == nil {
		metrics.OrderEvent("confirmed")
	}
	return err
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

// CancelOrder cancels an active order and refunds the hold exactly once. The
// refund and the status change share one transaction and one row lock, and the
// hold is zeroed, so a repeated or concurrent cancel cannot pay out again.
func (s *OrderService) CancelOrder(orderID uuid.UUID) error {
	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.Status != repository.OrderStatusSearching && order.Status != repository.OrderStatusAssigned {
			return errors.New("order cannot be canceled")
		}

		if order.HoldAmount.IsPositive() {
			if err := s.ledger.Release(tx, repository.AccountEscrow, order.CustomerID, order.HoldAmount, repository.TransactionTypeRefund, &order.ID, nil); err != nil {
				return err
			}
			if err := s.orderRepo.SetHoldAmount(tx, order.ID, money.Zero); err != nil {
				return err
			}
		}
		return s.orderRepo.Cancel(tx, orderID)
	})
	if err == nil {
		metrics.OrderEvent("cancelled")
	}
	return err
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
	photoURL = strings.TrimSpace(photoURL)
	if photoURL == "" {
		return nil, errors.New("photo URL is required")
	}
	// Only a path produced by our own upload endpoint is accepted. The value
	// used to be stored verbatim and rendered in the admin panel, so an
	// arbitrary URL there is somebody else's content on our page.
	if !strings.HasPrefix(photoURL, "/uploads/") || strings.Contains(photoURL, "..") {
		return nil, errors.New("photo must be uploaded through the app")
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
		HoldAmount:       money.Zero,
		FinalAmount:      money.Zero,
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

	if err := s.orderRepo.Create(nil, order); err != nil {
		return nil, err
	}

	if s.userRepo != nil && order.PickupLat != nil && order.PickupLon != nil {
		if err := s.userRepo.UpdateLastGeo(customerID, formatGeo(*order.PickupLat, *order.PickupLon)); err != nil {
			log.Printf("[OrderService] failed to update last_geo for %s: %v", customerID, err)
		}
	}

	// Create the chat room for the new order. Non-fatal if it fails.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}

	metrics.OrderEvent("created_auction")
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
