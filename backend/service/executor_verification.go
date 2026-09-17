package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Заявка исполнителя на верификацию.
//
// Отдельной механики у неё нет: это обычный заказ на услугу верификации, тот же,
// что может сделать заказчик из каталога, — его берёт модератор, приезжает по
// адресу и сверяет паспорт с данными аккаунта (см. behaviors/verification).
// Исполнитель не ходит в каталог заказчика, поэтому здесь собрано то, что тот
// делает руками: найти вариант услуги, убедиться, что сверять есть с чем, и
// разместить заказ на свой адрес.

// VerificationBehaviorCode — библиотечное поведение услуги верификации.
const VerificationBehaviorCode = "verification"

// VerificationServiceCode — системный код услуги верификации в каталоге (у
// варианта или у его категории). По поведению узел находится, только пока он
// выполняет библиотечный скрипт: сохранение в админке записывает в узел
// собственную копию, и движок зовёт её уже по id узла. Системный код админ
// задаёт сам, и от такой правки он не меняется.
const VerificationServiceCode = "verified"

// Поля, без которых модератору не с чем сверять паспорт или некуда ехать.
const (
	VerificationFieldLastName   = "last_name"
	VerificationFieldFirstName  = "first_name"
	VerificationFieldPatronymic = "patronymic"
	VerificationFieldBirthDate  = "birth_date"
	VerificationFieldAddress    = "address"
)

var (
	// ErrVerificationUnavailable — услуга верификации выключена или не заведена.
	ErrVerificationUnavailable = errors.New("услуга верификации сейчас недоступна")
	// ErrAlreadyVerified — аккаунт уже подтверждён, заказывать нечего.
	ErrAlreadyVerified = errors.New("ваш аккаунт уже подтверждён")
)

// VerificationDataMissingError перечисляет незаполненные поля: клиент по нему
// рисует форму ровно под то, чего не хватает.
type VerificationDataMissingError struct {
	Fields []string
}

func (e *VerificationDataMissingError) Error() string {
	return "заполните данные для верификации: " + strings.Join(e.Fields, ", ")
}

// ExecutorVerificationStatus — то, что экран исполнителя знает о верификации.
type ExecutorVerificationStatus struct {
	IsVerified bool `json:"is_verified"`
	// Missing — поля, которые нужно заполнить до заявки.
	Missing []string `json:"missing"`
	Address string   `json:"address,omitempty"`
	// Order — незавершённый заказ на верификацию, если он уже размещён.
	Order *repository.Order `json:"order,omitempty"`
}

// ExecutorVerificationRequest дозаполняет недостающие данные. Уже заполненные
// поля аккаунта не перезаписываются: модератор сверяет паспорт именно с ними.
type ExecutorVerificationRequest struct {
	LastName   string
	FirstName  string
	Patronymic string
	BirthDate  string
	Address    *Address
}

// ExecutorVerificationService размещает заказ на верификацию от имени исполнителя.
type ExecutorVerificationService struct {
	users     repository.UserRepository
	addresses repository.AddressRepository
	catalog   repository.ServiceCatalogRepository
	orderRepo repository.OrderRepository
	behaviors *Behaviors
	orders    *OrderService
}

// NewExecutorVerificationService собирает сервис заявок на верификацию.
func NewExecutorVerificationService(users repository.UserRepository, addresses repository.AddressRepository, catalog repository.ServiceCatalogRepository, orderRepo repository.OrderRepository, behaviors *Behaviors, orders *OrderService) *ExecutorVerificationService {
	return &ExecutorVerificationService{users: users, addresses: addresses, catalog: catalog, orderRepo: orderRepo, behaviors: behaviors, orders: orders}
}

// Status сообщает, подтверждён ли аккаунт, чего не хватает для заявки и есть ли
// уже открытый заказ.
func (s *ExecutorVerificationService) Status(ctx context.Context, userID uuid.UUID) (*ExecutorVerificationStatus, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	address, err := s.defaultAddress(ctx, userID)
	if err != nil {
		return nil, err
	}
	order, err := s.openOrder(ctx, userID)
	if err != nil {
		return nil, err
	}
	status := &ExecutorVerificationStatus{
		IsVerified: user.IsVerified(),
		Missing:    missingVerificationFields(user, address != nil),
		Order:      order,
	}
	if address != nil {
		status.Address = address.Address
	}
	return status, nil
}

// Request дозаполняет недостающие данные и размещает заказ на верификацию.
// Сначала проверяется всё присланное, и только потом что-либо пишется: отказ по
// дате рождения не должен оставлять после себя сохранённое ФИО.
func (s *ExecutorVerificationService) Request(ctx context.Context, userID uuid.UUID, req ExecutorVerificationRequest) (*repository.Order, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.IsVerified() {
		return nil, ErrAlreadyVerified
	}
	variant, err := s.variant(ctx)
	if err != nil {
		return nil, err
	}
	address, err := s.defaultAddress(ctx, userID)
	if err != nil {
		return nil, err
	}

	lastName := fillEmpty(user.LastName, req.LastName)
	firstName := fillEmpty(user.FirstName, req.FirstName)
	patronymic := fillEmpty(user.Patronymic, req.Patronymic)
	nameChanged := lastName != user.LastName || firstName != user.FirstName || patronymic != user.Patronymic

	var missing []string
	if lastName == "" {
		missing = append(missing, VerificationFieldLastName)
	}
	if firstName == "" {
		missing = append(missing, VerificationFieldFirstName)
	}
	if patronymic == "" {
		missing = append(missing, VerificationFieldPatronymic)
	}
	if user.BirthDate == nil && strings.TrimSpace(req.BirthDate) == "" {
		missing = append(missing, VerificationFieldBirthDate)
	}
	if address == nil && req.Address == nil {
		missing = append(missing, VerificationFieldAddress)
	}
	if len(missing) > 0 {
		return nil, &VerificationDataMissingError{Fields: missing}
	}

	var birthDate *time.Time
	if user.BirthDate == nil {
		parsed, err := parseBirthDate(req.BirthDate)
		if err != nil {
			return nil, err
		}
		birthDate = &parsed
	}
	if address == nil {
		if err := req.Address.Validate(); err != nil {
			return nil, err
		}
	}

	if nameChanged {
		if err := s.users.UpdateUserName(ctx, userID, lastName, firstName, patronymic); err != nil {
			return nil, err
		}
	}
	if birthDate != nil {
		if err := s.users.UpdateUserBirthDate(ctx, userID, *birthDate); err != nil {
			return nil, err
		}
	}
	if address == nil {
		record := req.Address.ToRecord()
		record.IsDefault = true
		if _, err := s.addresses.Add(ctx, userID, record); err != nil {
			return nil, err
		}
		if address, err = s.defaultAddress(ctx, userID); err != nil {
			return nil, err
		}
		if address == nil {
			return nil, errors.New("не удалось сохранить адрес")
		}
	}

	return s.orders.CreateOrderWithComment(ctx, userID, variant.ID, false, false, address.Address, "", address.Lat, address.Lon)
}

// Cancel отменяет открытую заявку — так же, как заказчик отменяет свой заказ.
func (s *ExecutorVerificationService) Cancel(ctx context.Context, userID uuid.UUID) error {
	order, err := s.openOrder(ctx, userID)
	if err != nil {
		return err
	}
	if order == nil {
		return errors.New("заявка на верификацию не найдена")
	}
	return s.orders.Cancel(ctx, userID, order.ID)
}

// variant находит включённый вариант услуги верификации.
func (s *ExecutorVerificationService) variant(ctx context.Context) (*repository.ServiceNode, error) {
	variants, err := s.catalog.GetActiveVariants(ctx)
	if err != nil {
		return nil, err
	}
	orderable := make([]*repository.ServiceNode, 0, len(variants))
	for _, v := range variants {
		if v.IsOrderable() {
			orderable = append(orderable, v)
		}
	}
	matched, err := s.verificationNodes(ctx, orderable)
	if err != nil {
		return nil, err
	}
	for _, v := range orderable {
		if matched[v.ID] {
			return v, nil
		}
	}
	return nil, ErrVerificationUnavailable
}

// verificationNodes отмечает варианты, относящиеся к услуге верификации: по
// системному коду варианта или его категории либо по библиотечному поведению.
func (s *ExecutorVerificationService) verificationNodes(ctx context.Context, variants []*repository.ServiceNode) (map[uuid.UUID]bool, error) {
	parentIDs := []uuid.UUID{}
	for _, v := range variants {
		if v.ParentID != nil {
			parentIDs = append(parentIDs, *v.ParentID)
		}
	}
	parents := map[uuid.UUID]*repository.ServiceNode{}
	if len(parentIDs) > 0 {
		found, err := s.catalog.GetNodesByIDs(ctx, parentIDs)
		if err != nil {
			return nil, err
		}
		parents = found
	}
	matched := map[uuid.UUID]bool{}
	for _, v := range variants {
		switch {
		case v.Code == VerificationServiceCode, s.behaviors.Code(v) == VerificationBehaviorCode:
			matched[v.ID] = true
		case v.ParentID != nil && parents[*v.ParentID] != nil && parents[*v.ParentID].Code == VerificationServiceCode:
			matched[v.ID] = true
		}
	}
	return matched, nil
}

// openOrder возвращает незавершённый заказ пользователя на верификацию.
func (s *ExecutorVerificationService) openOrder(ctx context.Context, userID uuid.UUID) (*repository.Order, error) {
	orders, err := s.orderRepo.GetCustomerOrders(ctx, userID)
	if err != nil {
		return nil, err
	}
	var open []*repository.Order
	variantIDs := []uuid.UUID{}
	for _, o := range orders {
		if o.Status == repository.OrderStatusCompleted || o.Status == repository.OrderStatusCanceled {
			continue
		}
		open = append(open, o)
		variantIDs = append(variantIDs, o.ServiceVariantID)
	}
	if len(open) == 0 {
		return nil, nil
	}
	nodes, err := s.catalog.GetNodesByIDs(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	variants := make([]*repository.ServiceNode, 0, len(nodes))
	for _, n := range nodes {
		variants = append(variants, n)
	}
	matched, err := s.verificationNodes(ctx, variants)
	if err != nil {
		return nil, err
	}
	for _, o := range open {
		if matched[o.ServiceVariantID] {
			return o, nil
		}
	}
	return nil, nil
}

// defaultAddress — адрес, с которого начинаются заказы пользователя.
func (s *ExecutorVerificationService) defaultAddress(ctx context.Context, userID uuid.UUID) (*repository.Address, error) {
	if s.addresses == nil {
		return nil, nil
	}
	addresses, err := s.addresses.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range addresses {
		if addresses[i].IsDefault {
			return &addresses[i], nil
		}
	}
	if len(addresses) > 0 {
		return &addresses[0], nil
	}
	return nil, nil
}

func missingVerificationFields(user *repository.User, hasAddress bool) []string {
	missing := []string{}
	if strings.TrimSpace(user.LastName) == "" {
		missing = append(missing, VerificationFieldLastName)
	}
	if strings.TrimSpace(user.FirstName) == "" {
		missing = append(missing, VerificationFieldFirstName)
	}
	if strings.TrimSpace(user.Patronymic) == "" {
		missing = append(missing, VerificationFieldPatronymic)
	}
	if user.BirthDate == nil {
		missing = append(missing, VerificationFieldBirthDate)
	}
	if !hasAddress {
		missing = append(missing, VerificationFieldAddress)
	}
	return missing
}

// fillEmpty берёт присланное значение, только если в аккаунте поле пустое.
func fillEmpty(current, proposed string) string {
	if current = strings.TrimSpace(current); current != "" {
		return current
	}
	return strings.TrimSpace(proposed)
}
