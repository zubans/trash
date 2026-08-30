package service

import (
	"context"
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
	resolver     AddressResolver
	// Optional. When wired, the nearby list is anchored to the executor's own
	// stored working position instead of client supplied coordinates — the same
	// point the map and the accept-radius check use — so the list can never
	// diverge from what the executor can actually take.
	executorGeoRepo repository.ExecutorGeoRepository
}

// NewOrderService creates an OrderService.
func NewOrderService(orderRepo repository.OrderRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, catalogRepo repository.ServiceCatalogRepository, resolver AddressResolver) *OrderService {
	return &OrderService{orderRepo: orderRepo, ledger: ledger, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, catalogRepo: catalogRepo, resolver: resolver}
}

// WithExecutorGeo wires the executor location store so the nearby list is
// resolved from the server-side stored position rather than trusting request
// coordinates.
func (s *OrderService) WithExecutorGeo(geoRepo repository.ExecutorGeoRepository) *OrderService {
	s.executorGeoRepo = geoRepo
	return s
}

// SettingOrderCommissionPercent is the system_settings key holding the
// platform's share of a completed order, as a percentage of the amount the
// customer actually paid. Admins edit it in the settings screen.
const SettingOrderCommissionPercent = "order_commission_percent"

// commissionOn returns the platform's share of a completed order. The share is
// clamped to 0..100 percent here as well as in the settings validator: a value
// outside that range would either pay the executor more than the customer paid
// or take money escrow is not holding, and neither is worth trusting a settings
// row about. Rounding happens once, in Scale, and the remainder goes to the
// executor.
func commissionOn(amount money.Amount, settings map[string]float64) money.Amount {
	percent := settings[SettingOrderCommissionPercent]
	if percent <= 0 {
		return money.Zero
	}
	if percent > 100 {
		percent = 100
	}
	commission := amount.Scale(percent / 100)
	if commission > amount {
		commission = amount
	}
	return commission
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

func (s *OrderService) hydrateServiceVariant(ctx context.Context, order *repository.Order) {
	if order == nil {
		return
	}
	if variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID); err == nil && variant != nil {
		order.ServiceVariant = variant
	}
	if order.ExecutorID != nil && s.userRepo != nil {
		if execUser, err := s.userRepo.FindByID(ctx, *order.ExecutorID); err == nil && execUser != nil {
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

func (s *OrderService) loadSettings(ctx context.Context) map[string]float64 {
	settings := map[string]float64{
		"standard_tariff_coeff": 1.0,
		"urgent_tariff_coeff":   3.0,
		"asap_tariff_coeff":     8.0,
	}
	if s.settingsRepo != nil {
		repoSettings, err := s.settingsRepo.GetSettings(ctx)
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
func (s *OrderService) CalculatePrice(ctx context.Context, serviceVariantID uuid.UUID, isUrgent, isAsap, isDowngraded bool) (money.Amount, error) {
	variant, err := s.catalogRepo.GetNodeByID(ctx, serviceVariantID)
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
	settings := s.loadSettings(ctx)
	switch {
	case isAsap:
		price = price.Scale(settings["asap_tariff_coeff"])
	case isUrgent:
		price = price.Scale(settings["urgent_tariff_coeff"])
	}

	return price, nil
}

// CreateOrder creates a standard order and holds customer balance.
func (s *OrderService) CreateOrder(ctx context.Context, customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, lat, lon *float64) (*repository.Order, error) {
	return s.CreateOrderWithComment(ctx, customerID, serviceVariantID, isUrgent, isAsap, address, "", lat, lon)
}

// CreateOrderWithComment creates a standard order with optional comment and
// holds the customer balance. Order creation, the balance hold and the ledger
// entry all happen in one transaction: the debit is guarded by the balance so
// concurrent requests cannot spend the same money twice, and a failure at any
// step leaves neither an order nor a hold behind.
func (s *OrderService) CreateOrderWithComment(ctx context.Context, customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, comment string, lat, lon *float64) (*repository.Order, error) {
	if isUrgent && isAsap {
		return nil, errors.New("cannot set both urgent and asap flags")
	}

	variant, err := s.catalogRepo.GetNodeByID(ctx, serviceVariantID)
	if err != nil {
		return nil, err
	}
	if variant == nil || !variant.IsVariant() {
		return nil, errors.New("invalid service variant")
	}
	// A retired service keeps resolving for the orders already placed on it,
	// but no new order may be created for it.
	if !variant.IsOrderable() {
		return nil, errors.New("service variant is not available")
	}
	if variant.IsAuction {
		return nil, errors.New("auction variants are ordered through the construction order endpoint")
	}

	// A variant marked requires_verification may only be ordered by a manually
	// verified customer. Enforced here, not just hidden in the catalog, so it
	// cannot be bypassed by posting a known variant id.
	if s.userRepo != nil {
		customer, err := s.userRepo.FindByID(ctx, customerID)
		if err != nil {
			return nil, err
		}
		if err := canCustomerOrderVariant(customer, variant); err != nil {
			return nil, err
		}
	}

	holdAmount, err := s.CalculatePrice(ctx, serviceVariantID, isUrgent, isAsap, false)
	if err != nil {
		return nil, err
	}
	if holdAmount.IsNegative() {
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
	} else if s.resolver != nil && address != "" {
		// No coordinates from the client (an older build, or a typed line):
		// resolve them once here so the order is matchable. A picked suggestion
		// carries its own and never reaches this branch.
		if geo, err := s.resolver.Resolve(ctx, address); err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		// The order row goes in first: the ledger entry references it, and
		// transactions.order_id is a foreign key checked immediately. Ordering
		// costs nothing here — both statements share one transaction, so a
		// failed hold rolls the order back with it.
		if err := s.orderRepo.Create(ctx, tx, order); err != nil {
			return err
		}
		// Reserve is a single conditional debit paired with a credit to escrow:
		// the money is not destroyed, it moves to the account that holds it for
		// the duration of the order.
		return s.ledger.Reserve(ctx, tx, customerID, repository.AccountEscrow, holdAmount, repository.TransactionTypeHold, &order.ID)
	}); err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, errors.New("insufficient balance")
		}
		return nil, err
	}

	// Everything below is best-effort: the order and its hold are already committed.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(ctx, order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}

	metrics.OrderEvent("created")
	s.hydrateServiceVariant(ctx, order)
	return order, nil
}

// Create creates a new order for a customer (alias compatible with handler).
func (s *OrderService) Create(ctx context.Context, customerID uuid.UUID, req CreateOrderRequest) (*repository.Order, error) {
	return s.CreateOrderWithComment(ctx, customerID, req.ServiceVariantID, req.IsUrgent, false, req.Address, req.Comment, req.Lat, req.Lon)
}

// Accept allows an executor to take an order from the queue. Every restriction
// that the order list applies when showing an order is re-checked here, because
// the list is only a convenience — this method is the actual authorisation
// point.
func (s *OrderService) Accept(ctx context.Context, orderID, executorID uuid.UUID) error {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	if err != nil || shift == nil {
		return errors.New("executor has no active shift")
	}
	if shift.Status == repository.ShiftStatusPenalized {
		return errors.New("executor is penalized")
	}

	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID == executorID {
		return errors.New("нельзя брать собственный заказ")
	}
	if err := s.checkExecutorEligibility(ctx, executorID, order); err != nil {
		return err
	}

	balance, err := s.ledger.GetBalance(ctx, executorID)
	if err != nil {
		return err
	}
	// The limit is configured as a magnitude and applied as a negative floor,
	// e.g. min_balance_limit=500 means "no new orders below -500".
	minBalanceLimit := money.FromRubles(-math.Abs(s.settingsFloat(ctx, "min_balance_limit", defaultMinBalanceLimit)))
	if balance < minBalanceLimit {
		return fmt.Errorf("нельзя брать новые заказы: баланс %s ниже допустимого лимита (%s)", balance, minBalanceLimit)
	}

	maxActive := settingInt(ctx, s.settingsRepo, "max_active_orders", defaultMaxActiveOrders)
	activeCount, err := s.orderRepo.CountActiveOrdersByExecutor(ctx, executorID)
	if err != nil {
		return err
	}
	if activeCount >= maxActive {
		return fmt.Errorf("превышен лимит активных заказов (не более %d)", maxActive)
	}

	maxExecuted := settingInt(ctx, s.settingsRepo, "max_executed_unconfirmed_orders", defaultMaxExecutedUnconfirmed)
	executedCount, err := s.orderRepo.CountExecutedUnconfirmedOrdersByExecutor(ctx, executorID)
	if err != nil {
		return err
	}
	if executedCount >= maxExecuted {
		return fmt.Errorf("превышен лимит непотвержденных заказчиком исполненных заказов (не более %d)", maxExecuted)
	}

	if err := s.orderRepo.Assign(ctx, nil, orderID, executorID); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("заказ уже взят другим исполнителем")
		}
		return err
	}
	metrics.OrderEvent("accepted")
	return nil
}

// checkExecutorEligibility loads the executor, the service variant and the
// customer, and applies the shared visibility/accept predicate — the same one
// the order lists use, so an executor can only accept what they can see.
func (s *OrderService) checkExecutorEligibility(ctx context.Context, executorID uuid.UUID, order *repository.Order) error {
	if s.userRepo == nil {
		return nil
	}
	viewer, err := s.userRepo.FindByID(ctx, executorID)
	if err != nil {
		return errors.New("executor not found")
	}
	variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID)
	if err != nil {
		return err
	}
	customer, _ := s.userRepo.FindByID(ctx, order.CustomerID)
	return canViewOrTakeOrder(viewer, customer, variant)
}

// settingsFloat reads a numeric system setting with a fallback default.
func (s *OrderService) settingsFloat(ctx context.Context, key string, defaultValue float64) float64 {
	return settingFloat(ctx, s.settingsRepo, key, defaultValue)
}

// RejectAssignedOrder allows an executor to drop an assigned order. The
// executor is fined a share of the order value (see reject_penalty_share) and
// the order returns to the search pool. Fine and unassignment share one
// transaction, so the executor is never charged for an order that stayed
// assigned to them.
func (s *OrderService) RejectAssignedOrder(ctx context.Context, orderID, executorID uuid.UUID) error {
	share := s.settingsFloat(ctx, "reject_penalty_share", defaultRejectPenaltyShare)
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
			return errors.New("order is not assigned to this executor")
		}

		// The penalty is collected, not destroyed: it lands on the fines account.
		penalty := order.HoldAmount.Scale(share)
		if err := s.ledger.Charge(ctx, tx, executorID, repository.AccountFines, penalty, repository.TransactionTypeFine, &order.ID); err != nil {
			return err
		}
		return s.orderRepo.Unassign(ctx, tx, orderID)
	})
	if err == nil {
		metrics.OrderEvent("rejected")
	}
	return err
}

// ExecuteOrder marks an order as EXECUTED by the executor and sends a system chat message.
func (s *OrderService) ExecuteOrder(ctx context.Context, orderID, executorID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
		return errors.New("order is not assigned to this executor")
	}

	if err := s.orderRepo.Execute(ctx, nil, orderID); err != nil {
		return err
	}
	metrics.OrderEvent("executed")

	// Send system notification message in chat
	if s.chatRepo != nil {
		chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
		if err == nil && chat != nil {
			_, _ = s.chatRepo.SaveMessage(ctx, chat.ID, executorID, "📦 Исполнитель отметил(а) выполнение заказа! Пожалуйста, подтвердите приемку работы.")
		}
	}

	return nil
}

// ConfirmOrder completes an order and processes payments. The order row is
// locked and re-read inside the transaction, so two concurrent confirmations
// cannot both pay out the executor, and the payout is derived from the hold
// that is actually still held (see the SLA downgrade path).
func (s *OrderService) ConfirmOrder(ctx context.Context, orderID uuid.UUID) error {
	// Counted after the transaction returns, never inside it: a confirmation
	// that rolled back paid nobody and must not show up as revenue.
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		// The customer may approve either after the executor marked the order as
		// EXECUTED, or earlier while it is still ASSIGNED — approving early simply
		// closes the order and pays the executor the held amount, same as the
		// EXECUTED path below.
		if order.Status != repository.OrderStatusExecuted && order.Status != repository.OrderStatusAssigned {
			return errors.New("order must be assigned or marked as executed before confirmation")
		}
		if order.ExecutorID == nil {
			return errors.New("order has no executor")
		}

		finalAmount := order.HoldAmount
		isDowngraded := order.IsDowngraded
		if order.IsAsap && order.DeadlineAt != nil && time.Now().After(*order.DeadlineAt) {
			downgraded, err := s.CalculatePrice(ctx, order.ServiceVariantID, false, false, true)
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
		if err := s.ledger.Release(ctx, tx, repository.AccountEscrow, order.CustomerID, refund, repository.TransactionTypeRefund, &order.ID, nil); err != nil {
			return err
		}

		// The customer's money left the balance at hold time; this entry records
		// the hold being spent rather than a second debit.
		if err := s.ledger.Note(ctx, tx, order.CustomerID, repository.AccountEscrow, finalAmount, repository.TransactionTypePayment, &order.ID); err != nil {
			return err
		}

		// The platform keeps its share of what the customer paid and the executor
		// is rewarded the rest. Escrow still drains to exactly zero for this
		// order: refund + commission + reward = the hold.
		commission := commissionOn(finalAmount, s.loadSettings(ctx))
		if err := s.ledger.Commission(ctx, tx, *order.ExecutorID, commission, &order.ID); err != nil {
			return err
		}

		if err := s.ledger.Release(ctx, tx, repository.AccountEscrow, *order.ExecutorID, finalAmount.Sub(commission), repository.TransactionTypeReward, &order.ID, nil); err != nil {
			return err
		}

		if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
			return err
		}
		return s.orderRepo.Confirm(ctx, tx, orderID, finalAmount, isDowngraded)
	})
	if err == nil {
		metrics.OrderEvent("confirmed")
	}
	return err
}

// maxTipAmount is a fat-finger ceiling on a single tip. The balance check is
// the real limit; this only stops an obviously mistaken amount from being
// charged before the customer notices.
var maxTipAmount = money.FromRubles(100_000)

// TipOrder lets a customer tip the executor of a completed order. The tip moves
// from the customer's balance to the executor's, at most once per order: the
// once-only guard and the charge share one transaction and one row lock, so a
// duplicate request cannot charge twice. Returns an insufficient-balance error
// when the customer cannot cover the tip.
func (s *OrderService) TipOrder(ctx context.Context, customerID, orderID uuid.UUID, amount money.Amount) error {
	if !amount.IsPositive() {
		return errors.New("tip amount must be positive")
	}
	if amount > maxTipAmount {
		return errors.New("tip amount is too large")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.CustomerID != customerID {
			return errors.New("forbidden")
		}
		if order.Status != repository.OrderStatusCompleted {
			return errors.New("tips can only be sent for completed orders")
		}
		if order.ExecutorID == nil {
			return errors.New("order has no executor")
		}

		tipped, err := s.ledger.HasTip(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if tipped {
			return errors.New("this order has already been tipped")
		}

		return s.ledger.Tip(ctx, tx, customerID, *order.ExecutorID, amount, &order.ID)
	})
	// ErrInsufficientFunds is passed through so the handler renders it as the
	// same "недостаточно средств" / 422 as an order hold does.
	if err == nil {
		metrics.OrderEvent("tipped")
	}
	return err
}

// Confirm completes an order for a specific customer (alias compatible with handler).
func (s *OrderService) Confirm(ctx context.Context, customerID, orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID != customerID {
		return errors.New("forbidden")
	}
	return s.ConfirmOrder(ctx, orderID)
}

// CancelOrder cancels an active order and refunds the hold exactly once. The
// refund and the status change share one transaction and one row lock, and the
// hold is zeroed, so a repeated or concurrent cancel cannot pay out again.
func (s *OrderService) CancelOrder(ctx context.Context, orderID uuid.UUID) error {
	return s.cancel(ctx, orderID, repository.OrderStatusSearching, repository.OrderStatusAssigned)
}

// CancelUnclaimedAuction cancels an auction request that expired without anyone
// winning it. Unlike CancelOrder it refuses an order that has already reached
// ASSIGNED.
//
// The distinction matters because of a race the seven-day sweep would otherwise
// lose: the worker selects the expired requests, and a customer can accept a
// bid on one of them before the worker gets to it. Accepting a bid is what puts
// an auction into ASSIGNED and moves the money into escrow, so cancelling it
// then would take a job away from an executor who had just won it, refund a
// customer who had just committed, and do both because of a scan that started
// moments earlier. Only "nobody claimed this" is a reason to cancel here.
func (s *OrderService) CancelUnclaimedAuction(ctx context.Context, orderID uuid.UUID) error {
	return s.cancel(ctx, orderID, repository.OrderStatusSearching)
}

func (s *OrderService) cancel(ctx context.Context, orderID uuid.UUID, allowed ...repository.OrderStatus) error {
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		permitted := false
		for _, status := range allowed {
			if order.Status == status {
				permitted = true
				break
			}
		}
		if !permitted {
			return errors.New("order cannot be canceled")
		}

		if order.HoldAmount.IsPositive() {
			if err := s.ledger.Release(ctx, tx, repository.AccountEscrow, order.CustomerID, order.HoldAmount, repository.TransactionTypeRefund, &order.ID, nil); err != nil {
				return err
			}
			if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
				return err
			}
		}
		return s.orderRepo.Cancel(ctx, tx, orderID)
	})
	if err == nil {
		metrics.OrderEvent("cancelled")
	}
	return err
}

// Cancel cancels an order for a specific customer (alias compatible with handler).
func (s *OrderService) Cancel(ctx context.Context, customerID, orderID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.CustomerID != customerID {
		return errors.New("forbidden")
	}
	return s.CancelOrder(ctx, orderID)
}

// CreateConstructionOrder creates a construction waste auction order.
func (s *OrderService) CreateConstructionOrder(ctx context.Context, customerID uuid.UUID, photoURL, address, comment string, lat, lon *float64) (*repository.Order, error) {
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

	// GetNodeByCode only sees live nodes, so a retired construction variant
	// reads as a missing one rather than as a database error.
	variant, err := s.catalogRepo.GetNodeByCode(ctx, "trash_construction")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if variant == nil || variant.IsDeleted() {
		return nil, errors.New("construction variant not found")
	}
	if !variant.IsActive {
		return nil, errors.New("service variant is not available")
	}

	// Same customer-verification gate as the standard order path, in case the
	// construction variant is flagged requires_verification.
	if s.userRepo != nil {
		customer, err := s.userRepo.FindByID(ctx, customerID)
		if err != nil {
			return nil, err
		}
		if err := canCustomerOrderVariant(customer, variant); err != nil {
			return nil, err
		}
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
	} else if s.resolver != nil && address != "" {
		// No coordinates from the client (an older build, or a typed line):
		// resolve them once here so the order is matchable. A picked suggestion
		// carries its own and never reaches this branch.
		if geo, err := s.resolver.Resolve(ctx, address); err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	if err := s.orderRepo.Create(ctx, nil, order); err != nil {
		return nil, err
	}

	// Create the chat room for the new order. Non-fatal if it fails.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(ctx, order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}

	metrics.OrderEvent("created_auction")
	s.hydrateServiceVariant(ctx, order)
	return order, nil
}

// GetAvailableConstructionOrders returns open construction waste orders.
func (s *OrderService) GetAvailableConstructionOrders(ctx context.Context) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetAvailableAuctionOrders(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)
	}
	return orders, nil
}

// GetAvailableConstructionOrdersForExecutor returns open construction waste orders filtered for an executor.
func (s *OrderService) GetAvailableConstructionOrdersForExecutor(ctx context.Context, executorID uuid.UUID) ([]*repository.Order, error) {
	executor, _ := s.userRepo.FindByID(ctx, executorID)
	executorAge := 0
	executorVerified := false
	if executor != nil {
		executorAge = executor.GetAge()
		executorVerified = executor.IsVerified()
	}

	orders, err := s.orderRepo.GetAvailableAuctionOrders(ctx)
	if err != nil {
		return nil, err
	}

	filtered := []*repository.Order{}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)

		// 1. Filter: Customer MUST be verified ("показ заказов только от верифицированных пользователей")
		customer, err := s.userRepo.FindByID(ctx, o.CustomerID)
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
func (s *OrderService) FindNearbyOrders(ctx context.Context, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	orders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)
	}
	return orders, nil
}

// FindNearbyOrdersForExecutor returns searching standard/large orders near the given coordinates filtered for an executor.
func (s *OrderService) FindNearbyOrdersForExecutor(ctx context.Context, executorID uuid.UUID, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	// Anchor the search to the executor's authoritative stored position, the
	// same point the map and the accept-radius check use. Client coordinates
	// (device GPS, which may be absent or default to a base location) are only a
	// fallback when the store is not wired, keeping the list from diverging from
	// what the executor can actually accept.
	if s.executorGeoRepo != nil {
		storedLat, storedLon, _, err := s.executorGeoRepo.GetExecutorLocation(ctx, executorID)
		if err != nil {
			return nil, err
		}
		if storedLat == nil || storedLon == nil {
			// No working position set yet: nothing is acceptable, so nothing is listed.
			return []*repository.Order{}, nil
		}
		lat, lon = *storedLat, *storedLon
	}

	// The viewer's role set and verification decide what they may see; roles are
	// loaded with the user, so a moderator sees moderator-only orders too.
	viewer, _ := s.userRepo.FindByID(ctx, executorID)

	orders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}

	filtered := []*repository.Order{}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)

		// One predicate for both the map and this list, and the same one the
		// accept path enforces: moderator-only orders go to moderators; normal
		// orders follow the customer-verification segmentation and the standard
		// executor gates (requires_verification, min_age, ban).
		customer, _ := s.userRepo.FindByID(ctx, o.CustomerID)
		if canViewOrTakeOrder(viewer, customer, o.ServiceVariant) != nil {
			continue
		}

		filtered = append(filtered, o)
	}

	return filtered, nil
}

// ListAssigned returns orders assigned to an executor.
func (s *OrderService) ListAssigned(ctx context.Context, executorID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetExecutorAssignedOrders(ctx, executorID)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)
	}
	return orders, nil
}

// ListByCustomer returns orders created by a customer.
func (s *OrderService) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetCustomerOrders(ctx, customerID)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		s.hydrateServiceVariant(ctx, o)
	}
	return orders, nil
}
