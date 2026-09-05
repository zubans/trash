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

// --- Моки ---

type mockOrderRepo struct {
	orders []*repository.Order
	// assignErr, когда задан, отдаётся вместо назначения: так тест
	// воспроизводит проигранную гонку за заказ.
	assignErr error
}

func (m *mockOrderRepo) CreateOrderWithHold(ctx context.Context, customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, holdAmount money.Amount, lastGeo string) (*repository.Order, error) {
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

func (m *mockOrderRepo) ConfirmOrderExecution(ctx context.Context, orderID uuid.UUID) error {
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

func (m *mockOrderRepo) CancelOrder(ctx context.Context, orderID uuid.UUID) error {
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

func (m *mockOrderRepo) Unassign(ctx context.Context, q repository.Querier, orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.Status = "SEARCHING"
			o.ExecutorID = nil
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) GetPendingOrders(ctx context.Context) ([]*repository.Order, error) {
	var pending []*repository.Order
	for _, o := range m.orders {
		if o.Status == "SEARCHING" {
			pending = append(pending, o)
		}
	}
	return pending, nil
}

func (m *mockOrderRepo) GetOrdersMissingCoordinates(ctx context.Context, limit int) ([]*repository.Order, error) {
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

func (m *mockOrderRepo) SetPickupCoordinates(ctx context.Context, orderID uuid.UUID, lat, lon float64) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.PickupLat = &lat
			o.PickupLon = &lon
			return nil
		}
	}
	return nil
}

func (m *mockOrderRepo) AssignOrder(ctx context.Context, orderID uuid.UUID, executorID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.ExecutorID = &executorID
			o.Status = "ASSIGNED"
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockOrderRepo) CountActiveOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error) {
	var count int
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && (o.Status == repository.OrderStatusAssigned || o.Status == "ASSIGNED") {
			count++
		}
	}
	return count, nil
}

func (m *mockOrderRepo) CountActiveOrdersByExecutors(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(executorIDs))
	for _, id := range executorIDs {
		n, err := m.CountActiveOrdersByExecutor(ctx, id)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			counts[id] = n
		}
	}
	return counts, nil
}

func (m *mockOrderRepo) CountExecutedUnconfirmedOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error) {
	var count int
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && (o.Status == repository.OrderStatusExecuted || o.Status == "EXECUTED") {
			count++
		}
	}
	return count, nil
}

func (m *mockOrderRepo) GetExecutorAssignedOrders(ctx context.Context, executorID uuid.UUID) ([]*repository.Order, error) {
	var assigned []*repository.Order
	for _, o := range m.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID && o.Status == repository.OrderStatusAssigned {
			assigned = append(assigned, o)
		}
	}
	return assigned, nil
}

func (m *mockOrderRepo) GetCustomerOrders(ctx context.Context, customerID uuid.UUID) ([]*repository.Order, error) {
	var cust []*repository.Order
	for _, o := range m.orders {
		if o.CustomerID == customerID {
			cust = append(cust, o)
		}
	}
	return cust, nil
}

// FindOpenByCustomer возвращает незавершённые заказы заказчика — те, которые
// доменное событие о нём ещё может изменить.
func (m *mockOrderRepo) FindOpenByCustomer(ctx context.Context, customerID uuid.UUID) ([]*repository.Order, error) {
	var open []*repository.Order
	for _, o := range m.orders {
		if o.CustomerID != customerID {
			continue
		}
		switch o.Status {
		case repository.OrderStatusSearching, repository.OrderStatusAssigned, repository.OrderStatusExecuted:
			open = append(open, o)
		}
	}
	return open, nil
}

func (m *mockOrderRepo) GetOrderByID(ctx context.Context, orderID uuid.UUID) (*repository.Order, error) {
	for _, o := range m.orders {
		if o.ID == orderID {
			return o, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockOrderRepo) CreateConstructionOrder(ctx context.Context, customerID uuid.UUID, serviceVariantID uuid.UUID, photoURL, lastGeo string) (*repository.Order, error) {
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

func (m *mockOrderRepo) GetAvailableAuctionOrders(ctx context.Context) ([]*repository.Order, error) {
	var list []*repository.Order
	for _, o := range m.orders {
		if o.ServiceVariantID == constructionVariantID && o.Status == "SEARCHING" {
			list = append(list, o)
		}
	}
	return list, nil
}

// Методы, требуемые интерфейсом OrderRepository.
func (m *mockOrderRepo) Create(ctx context.Context, q repository.Querier, order *repository.Order) error {
	m.orders = append(m.orders, order)
	return nil
}

func (m *mockOrderRepo) FindByID(ctx context.Context, id uuid.UUID) (*repository.Order, error) {
	return m.GetOrderByID(context.Background(), id)
}

func (m *mockOrderRepo) FindAssignedByExecutor(ctx context.Context, executorID uuid.UUID) ([]repository.Order, error) {
	orders, err := m.GetExecutorAssignedOrders(context.Background(), executorID)
	if err != nil {
		return nil, err
	}
	result := make([]repository.Order, len(orders))
	for i, o := range orders {
		result[i] = *o
	}
	return result, nil
}

func (m *mockOrderRepo) FindByCustomer(ctx context.Context, customerID uuid.UUID) ([]repository.Order, error) {
	orders, err := m.GetCustomerOrders(context.Background(), customerID)
	if err != nil {
		return nil, err
	}
	result := make([]repository.Order, len(orders))
	for i, o := range orders {
		result[i] = *o
	}
	return result, nil
}

func (m *mockOrderRepo) Assign(ctx context.Context, q repository.Querier, orderID, executorID uuid.UUID) error {
	// Назначение — единственное место, где настоящий репозиторий проигрывает
	// гонку за заказ; assignErr позволяет тесту воспроизвести этот проигрыш.
	if m.assignErr != nil {
		return m.assignErr
	}
	return m.AssignOrder(context.Background(), orderID, executorID)
}

// AssignWithHold повторяет охраняемое назначение, используемое при принятии ставки.
func (m *mockOrderRepo) AssignWithHold(ctx context.Context, q repository.Querier, orderID, executorID uuid.UUID, holdAmount money.Amount) error {
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

// LockForUpdate повторяет чтение с блокировкой строки из настоящего репозитория.
func (m *mockOrderRepo) LockForUpdate(ctx context.Context, q repository.Querier, orderID uuid.UUID) (*repository.Order, error) {
	return m.GetOrderByID(context.Background(), orderID)
}

func (m *mockOrderRepo) SetHoldAmount(ctx context.Context, q repository.Querier, orderID uuid.UUID, holdAmount money.Amount) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			o.HoldAmount = holdAmount
			return nil
		}
	}
	return repository.ErrConflict
}

func (m *mockOrderRepo) Confirm(ctx context.Context, q repository.Querier, orderID uuid.UUID, finalAmount money.Amount, isDowngraded bool) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			if o.Status != repository.OrderStatusExecuted && o.Status != repository.OrderStatusAssigned {
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

func (m *mockOrderRepo) Cancel(ctx context.Context, q repository.Querier, orderID uuid.UUID) error {
	for _, o := range m.orders {
		if o.ID == orderID {
			// Повторяет охраняемый UPDATE: отменить можно только живой заказ,
			// а вторая отмена сообщает о конфликте вместо успеха.
			if o.Status != repository.OrderStatusSearching && o.Status != repository.OrderStatusAssigned {
				return repository.ErrConflict
			}
			o.Status = repository.OrderStatusCanceled
			return nil
		}
	}
	return repository.ErrConflict
}

// FindNearbyOrders повторяет настоящий репозиторий: заказы в поиске, несущие
// координаты и попадающие в радиус. Раньше это была заглушка, ничего не
// возвращавшая, — безвредная, пока карта фильтровала в Go и читала каждый
// ожидающий заказ; теперь, когда карта просит репозиторий ограничить поиск,
// заглушка здесь молча делала бы карту пустой.
func (m *mockOrderRepo) FindNearbyOrders(ctx context.Context, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	var nearby []*repository.Order
	for _, o := range m.orders {
		if o.Status != repository.OrderStatusSearching {
			continue
		}
		if o.PickupLat == nil || o.PickupLon == nil {
			continue
		}
		if HaversineDistanceKM(lat, lon, *o.PickupLat, *o.PickupLon)*1000 <= float64(radiusMeters) {
			nearby = append(nearby, o)
		}
	}
	return nearby, nil
}

func (m *mockOrderRepo) Execute(ctx context.Context, q repository.Querier, orderID uuid.UUID) error {
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

func (m *mockCatalogRepo) CreateNode(ctx context.Context, node *repository.ServiceNode) error {
	return nil
}
func (m *mockCatalogRepo) UpdateNode(ctx context.Context, node *repository.ServiceNode) error {
	return nil
}
func (m *mockCatalogRepo) DeleteNode(ctx context.Context, id uuid.UUID) error  { return nil }
func (m *mockCatalogRepo) RestoreNode(ctx context.Context, id uuid.UUID) error { return nil }
func (m *mockCatalogRepo) GetNodeByID(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, error) {
	n, ok := m.nodes[id]
	if !ok {
		return nil, nil
	}
	return n, nil
}
func (m *mockCatalogRepo) GetNodesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*repository.ServiceNode, error) {
	found := make(map[uuid.UUID]*repository.ServiceNode, len(ids))
	for _, id := range ids {
		if n, ok := m.nodes[id]; ok {
			found[id] = n
		}
	}
	return found, nil
}
func (m *mockCatalogRepo) GetNodeByCode(ctx context.Context, code string) (*repository.ServiceNode, error) {
	for _, n := range m.nodes {
		if n.Code == code {
			return n, nil
		}
	}
	return nil, nil
}
func (m *mockCatalogRepo) GetRootCategories(ctx context.Context, filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetChildren(ctx context.Context, parentID uuid.UUID, filter repository.ServiceNodeFilter) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetDescendants(ctx context.Context, ancestorID uuid.UUID, maxDepth *int) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetAncestors(ctx context.Context, descendantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetVariantPath(ctx context.Context, variantID uuid.UUID) ([]*repository.ServiceNode, error) {
	return nil, nil
}
func (m *mockCatalogRepo) GetActiveVariants(ctx context.Context) ([]*repository.ServiceNode, error) {
	return nil, nil
}

// ListNodesWithScript: ни одна из этих фикстур не несёт собственного скрипта.
func (m *mockCatalogRepo) ListNodesWithScript(ctx context.Context) ([]*repository.ServiceNode, error) {
	var withScript []*repository.ServiceNode
	for _, node := range m.nodes {
		if node.HasOwnScript() {
			withScript = append(withScript, node)
		}
	}
	return withScript, nil
}

func (m *mockCatalogRepo) GetVariantWithCategory(ctx context.Context, id uuid.UUID) (*repository.ServiceNode, []*repository.ServiceNode, error) {
	return nil, nil, nil
}
func (m *mockCatalogRepo) HasChildren(ctx context.Context, id uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockCatalogRepo) HasOrders(ctx context.Context, id uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockCatalogRepo) IsDescendantOf(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return false, nil
}

type orderMockSettingsRepo struct {
	settings map[string]string
}

func (m *orderMockSettingsRepo) GetSettings(ctx context.Context) (map[string]string, error) {
	return m.settings, nil
}

func (m *orderMockSettingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	return nil
}

type orderMockShiftRepo struct{}

func (m *orderMockShiftRepo) GetActiveShift(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return &repository.Shift{Status: repository.ShiftStatusActive}, nil
}

func (m *orderMockShiftRepo) StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) EndShift(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetShiftByID(ctx context.Context, id uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetShiftsByExecutor(ctx context.Context, executorID uuid.UUID) ([]*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) GetActiveShifts(ctx context.Context) ([]*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) UploadLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	return nil
}

func (m *orderMockShiftRepo) CheckShiftGeofence(ctx context.Context, shift *repository.Shift, lat, lon float64) (bool, error) {
	return true, nil
}

func (m *orderMockShiftRepo) ApplyEarlyEndPenalty(ctx context.Context, shiftID uuid.UUID, amount money.Amount) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) FindActiveByExecutor(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return m.GetActiveShift(context.Background(), executorID)
}

func (m *orderMockShiftRepo) Create(ctx context.Context, shift *repository.Shift) error { return nil }

func (m *orderMockShiftRepo) End(ctx context.Context, shiftID uuid.UUID) error { return nil }

func (m *orderMockShiftRepo) Penalize(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
	return nil
}

func (m *orderMockShiftRepo) EarlyEnd(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
	return nil
}

func (m *orderMockShiftRepo) GetLastShiftByExecutor(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return nil, nil
}

func (m *orderMockShiftRepo) UpdateShiftStatus(ctx context.Context, shiftID uuid.UUID, status string) error {
	return nil
}

type mockUserRepo struct {
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{}
}

func (m *mockUserRepo) FindByPhone(ctx context.Context, phone string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) Create(ctx context.Context, user *repository.User) error { return nil }
func (m *mockUserRepo) FindByID(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	// Верифицированный совершеннолетний пользователь: правила допуска проверяются отдельно.
	birth := time.Now().AddDate(-30, 0, 0)
	return &repository.User{ID: id, Role: "EXECUTOR", Status: "ACTIVE", Verified: true, BirthDate: &birth}, nil
}
func (m *mockUserRepo) FindByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*repository.User, error) {
	// Повторяет FindByID выше, чтобы вызывающий, пакетирующий чтения, видел тех же
	// пользователей, что и запрашивающий их по одному.
	found := make(map[uuid.UUID]*repository.User, len(ids))
	for _, id := range ids {
		u, err := m.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		found[id] = u
	}
	return found, nil
}
func (m *mockUserRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}
func (m *mockUserRepo) UpdateRole(ctx context.Context, id uuid.UUID, role string) error { return nil }

// UpdateVerifiedTx выполняет ту же запись; у подделки нет транзакций, поэтому
// querier игнорируется.
func (m *mockUserRepo) UpdateVerifiedTx(ctx context.Context, q repository.Querier, id uuid.UUID, verified bool) error {
	return m.UpdateVerified(ctx, id, verified)
}

func (m *mockUserRepo) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	return nil
}
func (m *mockUserRepo) UpdateBalance(ctx context.Context, id uuid.UUID, balance money.Amount) error {
	return nil
}
func (m *mockUserRepo) CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error {
	return nil
}
func (m *mockUserRepo) GetCustomerProfile(ctx context.Context, userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}
func (m *mockUserRepo) FindByEmail(ctx context.Context, email string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) FindByEmailVerificationToken(ctx context.Context, token string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) VerifyEmailToken(ctx context.Context, token string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	return nil
}
func (m *mockUserRepo) ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	return nil
}
func (m *mockUserRepo) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	return nil
}

func (m *mockOrderRepo) FindAllByExecutor(ctx context.Context, executorID uuid.UUID, limit int) ([]repository.Order, error) {
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

func (m *mockTransactionRepo) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	return m.balance(userID), nil
}

func (m *mockTransactionRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	m.balances[userID] = m.balance(userID).Add(delta)
	return nil
}

// Debit отказывается уходить ниже нуля, как охраняемый UPDATE в настоящем репозитории.
func (m *mockTransactionRepo) Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	if m.balance(userID) < amount {
		return repository.ErrInsufficientFunds
	}
	m.balances[userID] = m.balance(userID).Sub(amount)
	return nil
}

func (m *mockTransactionRepo) CreateTransaction(ctx context.Context, tx *sql.Tx, t *repository.Transaction) error {
	m.txs = append(m.txs, t)
	return nil
}

func (m *mockTransactionRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockTransactionRepo) HasTip(ctx context.Context, q repository.Querier, orderID uuid.UUID) (bool, error) {
	for _, t := range m.txs {
		if t.OrderID != nil && *t.OrderID == orderID && t.Type == string(repository.TransactionTypeTip) {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockTransactionRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return fn(nil)
}

// --- Тесты ---

func TestOrderService_CalculatePrice(t *testing.T) {
	setRepo := &orderMockSettingsRepo{
		settings: map[string]string{
			"urgent_tariff_coeff": "3.0",
			"asap_tariff_coeff":   "8.0",
		},
	}
	catalog := newMockCatalogRepo()
	srv := NewOrderService(&mockOrderRepo{}, testLedger(), setRepo, newMockUserRepo(), &orderMockShiftRepo{}, nil, catalog, nil)

	p, err := srv.CalculatePrice(context.Background(), standardVariantID, false, false, false)
	if err != nil || p != money.FromRubles(100) {
		t.Errorf("expected 100.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(context.Background(), largeVariantID, true, false, false)
	if err != nil || p != money.FromRubles(600) {
		t.Errorf("expected 600.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(context.Background(), standardVariantID, false, true, false)
	if err != nil || p != money.FromRubles(800) {
		t.Errorf("expected 800.0, got %s, err: %v", p, err)
	}

	p, err = srv.CalculatePrice(context.Background(), constructionVariantID, false, false, false)
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
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}
	if order.PickupLat == nil || *order.PickupLat != lat || order.PickupLon == nil || *order.PickupLon != lon {
		t.Errorf("expected pickup coordinates to be saved on order, got lat=%v, lon=%v", order.PickupLat, order.PickupLon)
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
	_ = orderRepo.AssignOrder(context.Background(), order.ID, executorID)
	_ = orderRepo.Execute(context.Background(), nil, order.ID)

	err := srv.ConfirmOrder(context.Background(), order.ID)
	if err != nil {
		t.Errorf("expected success confirming order, got err: %v", err)
	}

	err = srv.ConfirmOrder(context.Background(), order.ID)
	if err == nil {
		t.Errorf("expected error double confirming")
	}

	order2, _ := srv.CreateOrder(context.Background(), customerID, largeVariantID, false, false, "", nil, nil)
	err = srv.CancelOrder(context.Background(), order2.ID)
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

	// Принимаются только пути, порождённые нашим собственным эндпоинтом загрузки;
	// произвольный URL в итоге отрисовался бы в админ-панели.
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
	_ = orderRepo.AssignOrder(context.Background(), order.ID, executorID)

	// Имитируем дедлайн в прошлом, чтобы включить понижение.
	past := time.Now().Add(-time.Hour)
	for _, o := range orderRepo.orders {
		if o.ID == order.ID {
			o.DeadlineAt = &past
			o.Status = repository.OrderStatusExecuted
		}
	}

	err := srv.ConfirmOrder(context.Background(), order.ID)
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

	// Проверяем лимит в 3 активных заказа
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

	err := srv.Accept(context.Background(), newOrderID, executorID)
	if err == nil || err.Error() != "превышен лимит активных заказов (не более 3)" {
		t.Errorf("expected active orders limit error, got: %v", err)
	}

	// Сбрасываем назначенные заказы и проверяем лимит в 6 выполненных неподтверждённых
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

	err2 := srv.Accept(context.Background(), newOrderID2, executorID)
	if err2 == nil || err2.Error() != "превышен лимит непотвержденных заказчиком исполненных заказов (не более 6)" {
		t.Errorf("expected executed unconfirmed orders limit error, got: %v", err2)
	}
}

// mockAccounts — SystemAccountRepository в памяти. Он повторяет настоящий
// достаточно близко, чтобы тест проверял, куда ушли деньги, а не только что ушли.
type mockAccounts struct {
	balances map[string]money.Amount
}

func newMockAccounts() *mockAccounts {
	return &mockAccounts{balances: map[string]money.Amount{
		repository.AccountEscrow:     0,
		repository.AccountFines:      0,
		repository.AccountDeposits:   0,
		repository.AccountPayouts:    0,
		repository.AccountCommission: 0,
		repository.AccountBonuses:    0,
	}}
}

func (m *mockAccounts) Credit(ctx context.Context, q repository.Querier, code string, amount money.Amount) error {
	if _, ok := m.balances[code]; !ok {
		return repository.ErrUnknownSystemAccount
	}
	m.balances[code] = m.balances[code].Add(amount)
	return nil
}

func (m *mockAccounts) Debit(ctx context.Context, q repository.Querier, code string, amount money.Amount) error {
	if _, ok := m.balances[code]; !ok {
		return repository.ErrUnknownSystemAccount
	}
	m.balances[code] = m.balances[code].Sub(amount)
	return nil
}

// DebitAvailable повторяет охраняемое списание из настоящего репозитория: счёт
// нельзя увести ниже нуля выплатой.
func (m *mockAccounts) DebitAvailable(ctx context.Context, q repository.Querier, code string, amount money.Amount) error {
	balance, ok := m.balances[code]
	if !ok {
		return repository.ErrUnknownSystemAccount
	}
	if balance < amount {
		return repository.ErrInsufficientFunds
	}
	m.balances[code] = balance.Sub(amount)
	return nil
}

func (m *mockAccounts) Get(ctx context.Context, code string) (*repository.SystemAccount, error) {
	balance, ok := m.balances[code]
	if !ok {
		return nil, repository.ErrUnknownSystemAccount
	}
	return &repository.SystemAccount{Code: code, Balance: balance}, nil
}

func (m *mockAccounts) List(ctx context.Context) ([]repository.SystemAccount, error) {
	var list []repository.SystemAccount
	for code, balance := range m.balances {
		list = append(list, repository.SystemAccount{Code: code, Balance: balance})
	}
	return list, nil
}

// newTestLedger собирает реестр поверх балансов и счетов в памяти.
func newTestLedger(txRepo *mockTransactionRepo) (*Ledger, *mockAccounts) {
	accounts := newMockAccounts()
	return NewLedger(txRepo, accounts), accounts
}

// testLedger — обычный случай: свежий реестр, чьи стороны никто не разглядывает.
func testLedger() *Ledger {
	l, _ := newTestLedger(&mockTransactionRepo{})
	return l
}

func (m *mockUserRepo) UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error {
	return nil
}

// TestOrderService_TipOrder покрывает поток чаевых: деньги идут от заказчика к
// исполнителю, ровно один раз и только по завершённому заказу.
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
	if err := srv.TipOrder(context.Background(), customerID, orderID, tip); err != nil {
		t.Fatalf("tip failed: %v", err)
	}
	if got := txRepo.balance(customerID); got != money.FromRubles(850) {
		t.Errorf("customer balance: expected 850, got %s", got)
	}
	if got := txRepo.balance(executorID); got != money.FromRubles(150) {
		t.Errorf("executor balance: expected 150, got %s", got)
	}

	// Вторые чаевые по тому же заказу отклоняются, и ничего не двигается.
	if err := srv.TipOrder(context.Background(), customerID, orderID, tip); err == nil {
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
		if err := srv.TipOrder(context.Background(), customerID, o.ID, money.Zero); err == nil {
			t.Fatal("expected a zero tip to be rejected")
		}
	})

	t.Run("not the customer", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(1000))
		if err := srv.TipOrder(context.Background(), uuid.New(), o.ID, money.FromRubles(50)); err == nil {
			t.Fatal("expected a tip from a stranger to be rejected")
		}
	})

	t.Run("order not completed", func(t *testing.T) {
		o := completed()
		o.Status = repository.OrderStatusAssigned
		srv := makeService(o, money.FromRubles(1000))
		if err := srv.TipOrder(context.Background(), customerID, o.ID, money.FromRubles(50)); err == nil {
			t.Fatal("expected a tip on an unfinished order to be rejected")
		}
	})

	t.Run("insufficient balance", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(10))
		err := srv.TipOrder(context.Background(), customerID, o.ID, money.FromRubles(50))
		if !errors.Is(err, repository.ErrInsufficientFunds) {
			t.Fatalf("expected insufficient funds, got %v", err)
		}
	})

	t.Run("above the ceiling", func(t *testing.T) {
		o := completed()
		srv := makeService(o, money.FromRubles(1_000_000))
		if err := srv.TipOrder(context.Background(), customerID, o.ID, money.FromRubles(200_000)); err == nil {
			t.Fatal("expected an oversized tip to be rejected")
		}
	})
}

func (m *mockUserRepo) ListUserRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	return nil, nil
}

func (m *mockUserRepo) SetUserRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	return nil
}

// Автооткрытие смены при взятии заказа.
//
// Смена, открытая за исполнителя, стоит ему денег при досрочном выходе, поэтому
// проверяется не только то, что она открывается, но и то, что она не остаётся
// открытой, когда заказ так и не достался исполнителю.
func TestOrderService_AcceptAutoOpensShift(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	settings := &mockSettingsRepo{settings: map[string]string{}}
	srv := NewOrderService(orderRepo, testLedger(), settings, newMockUserRepo(), shiftRepo, nil, newMockCatalogRepo(), nil)

	executorID := uuid.New()
	orderID := uuid.New()
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:     orderID,
		Status: repository.OrderStatusSearching,
	})

	if err := srv.Accept(context.Background(), orderID, executorID); err != nil {
		t.Fatalf("accept without a shift should open one, got: %v", err)
	}

	shift, _ := shiftRepo.GetActiveShift(context.Background(), executorID)
	if shift == nil {
		t.Fatal("expected an active shift after accepting an order without one")
	}
	if shift.DurationHours != defaultAutoShiftDurationHours {
		t.Errorf("expected auto shift of %d h, got %d", defaultAutoShiftDurationHours, shift.DurationHours)
	}
}

func TestOrderService_AcceptAutoShiftHonoursSettings(t *testing.T) {
	newOrder := func(repo *mockOrderRepo) uuid.UUID {
		id := uuid.New()
		repo.orders = append(repo.orders, &repository.Order{ID: id, Status: repository.OrderStatusSearching})
		return id
	}

	t.Run("disabled keeps the old refusal", func(t *testing.T) {
		orderRepo := &mockOrderRepo{}
		shiftRepo := &mockShiftRepo{}
		settings := &mockSettingsRepo{settings: map[string]string{SettingAutoShiftOnAcceptEnabled: "0"}}
		srv := NewOrderService(orderRepo, testLedger(), settings, newMockUserRepo(), shiftRepo, nil, newMockCatalogRepo(), nil)

		err := srv.Accept(context.Background(), newOrder(orderRepo), uuid.New())
		if err == nil || err.Error() != "executor has no active shift" {
			t.Errorf("expected the no-shift refusal, got: %v", err)
		}
		if len(shiftRepo.shifts) != 0 {
			t.Errorf("expected no shift to be opened, got %d", len(shiftRepo.shifts))
		}
	})

	t.Run("configured duration is used", func(t *testing.T) {
		orderRepo := &mockOrderRepo{}
		shiftRepo := &mockShiftRepo{}
		settings := &mockSettingsRepo{settings: map[string]string{SettingAutoShiftDurationHours: "3"}}
		srv := NewOrderService(orderRepo, testLedger(), settings, newMockUserRepo(), shiftRepo, nil, newMockCatalogRepo(), nil)

		executorID := uuid.New()
		if err := srv.Accept(context.Background(), newOrder(orderRepo), executorID); err != nil {
			t.Fatalf("unexpected accept error: %v", err)
		}
		shift, _ := shiftRepo.GetActiveShift(context.Background(), executorID)
		if shift == nil || shift.DurationHours != 3 {
			t.Errorf("expected a 3 h auto shift, got %+v", shift)
		}
	})

	// Длительность вне разрешённого списка — это настройка, которую исполнитель
	// не смог бы выбрать сам, поэтому она игнорируется, а не открывает смену.
	t.Run("invalid duration falls back to the default", func(t *testing.T) {
		orderRepo := &mockOrderRepo{}
		shiftRepo := &mockShiftRepo{}
		settings := &mockSettingsRepo{settings: map[string]string{SettingAutoShiftDurationHours: "7"}}
		srv := NewOrderService(orderRepo, testLedger(), settings, newMockUserRepo(), shiftRepo, nil, newMockCatalogRepo(), nil)

		executorID := uuid.New()
		if err := srv.Accept(context.Background(), newOrder(orderRepo), executorID); err != nil {
			t.Fatalf("unexpected accept error: %v", err)
		}
		shift, _ := shiftRepo.GetActiveShift(context.Background(), executorID)
		if shift == nil || shift.DurationHours != defaultAutoShiftDurationHours {
			t.Errorf("expected the default auto shift duration, got %+v", shift)
		}
	})
}

// Заказ, который успел уйти другому исполнителю, не должен оставлять за собой
// смену: за досрочный выход из неё берут штраф, а открывал её не исполнитель.
func TestOrderService_AcceptRollsBackAutoShiftWhenOrderIsGone(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	shiftRepo := &mockShiftRepo{}
	settings := &mockSettingsRepo{settings: map[string]string{}}
	srv := NewOrderService(orderRepo, testLedger(), settings, newMockUserRepo(), shiftRepo, nil, newMockCatalogRepo(), nil)

	orderID := uuid.New()
	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:     orderID,
		Status: repository.OrderStatusSearching,
	})
	// Назначение проигрывает гонку: заказ уже забрал другой исполнитель.
	orderRepo.assignErr = repository.ErrConflict

	executorID := uuid.New()
	err := srv.Accept(context.Background(), orderID, executorID)
	if err == nil {
		t.Fatal("expected accepting an already assigned order to fail")
	}

	shift, _ := shiftRepo.GetActiveShift(context.Background(), executorID)
	if shift != nil {
		t.Errorf("expected the auto-opened shift to be closed again, got %+v", shift)
	}
}
