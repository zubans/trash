package service

import (
	"context"
	"database/sql"
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
	ErrVerificationUnavailable = ruleError("услуга верификации сейчас недоступна")
	// ErrAlreadyVerified — аккаунт уже подтверждён, заказывать нечего.
	ErrAlreadyVerified = stateError("ваш аккаунт уже подтверждён")
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
	Order *OrderView `json:"order,omitempty"`
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
	orders    OrderLifecycle
	tx        TxRunner
}

// NewExecutorVerificationService собирает сервис заявок на верификацию. tx —
// транзакция, в которой дозаполнение профиля и заказ коммитятся вместе.
func NewExecutorVerificationService(users repository.UserRepository, addresses repository.AddressRepository, catalog repository.ServiceCatalogRepository, orderRepo repository.OrderRepository, behaviors *Behaviors, orders OrderLifecycle, tx TxRunner) *ExecutorVerificationService {
	return &ExecutorVerificationService{users: users, addresses: addresses, catalog: catalog, orderRepo: orderRepo, behaviors: behaviors, orders: orders, tx: tx}
}

// Status сообщает, подтверждён ли аккаунт, чего не хватает для заявки и есть ли
// уже открытый заказ.
func (s *ExecutorVerificationService) Status(ctx context.Context, userID uuid.UUID) (*ExecutorVerificationStatus, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, userNotFound(err)
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
	}
	if order != nil {
		status.Order = &OrderView{Order: *order}
	}
	if address != nil {
		status.Address = address.Address
	}
	return status, nil
}

// Request дозаполняет недостающие данные и размещает заказ на верификацию.
// Сначала проверяется всё присланное, и только потом что-либо пишется: отказ по
// дате рождения не должен оставлять после себя сохранённое ФИО. Записи —
// имя, дата рождения, адрес и сам заказ — идут одной транзакцией: заявка либо
// размещена целиком, либо не оставила следа.
func (s *ExecutorVerificationService) Request(ctx context.Context, userID uuid.UUID, req ExecutorVerificationRequest) (*OrderView, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, userNotFound(err)
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

	var placed *preparedOrder
	if err := s.tx.RunInTx(ctx, func(tx *sql.Tx) error {
		if nameChanged {
			if err := s.users.UpdateUserName(ctx, tx, userID, lastName, firstName, patronymic); err != nil {
				return err
			}
		}
		if birthDate != nil {
			if err := s.users.UpdateUserBirthDate(ctx, tx, userID, *birthDate); err != nil {
				return err
			}
		}
		if address == nil {
			record := req.Address.ToRecord()
			record.IsDefault = true
			saved, err := s.addresses.Add(ctx, tx, userID, record)
			if err != nil {
				return err
			}
			if address = defaultOf(saved); address == nil {
				return errors.New("не удалось сохранить адрес")
			}
		}
		p, err := s.orders.prepareOrder(ctx, userID, CreateOrderRequest{
			ServiceVariantID: variant.ID, Address: address.Address, Lat: address.Lat, Lon: address.Lon,
		})
		if err != nil {
			return err
		}
		placed = p
		return s.orders.placeOrderTx(ctx, tx, p)
	}); err != nil {
		return nil, err
	}
	return s.orders.orderPlaced(ctx, placed), nil
}

// Cancel отменяет открытую заявку — так же, как заказчик отменяет свой заказ.
func (s *ExecutorVerificationService) Cancel(ctx context.Context, userID uuid.UUID) error {
	order, err := s.openOrder(ctx, userID)
	if err != nil {
		return err
	}
	if order == nil {
		return ErrVerificationRequestNotFound
	}
	return s.orders.Cancel(ctx, userID, order.ID)
}

// variant находит включённый вариант услуги верификации: первый заказываемый
// среди verificationVariants.
func (s *ExecutorVerificationService) variant(ctx context.Context) (*repository.ServiceNode, error) {
	variants, err := s.verificationVariants(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range variants {
		if v.IsOrderable() {
			return v, nil
		}
	}
	return nil, ErrVerificationUnavailable
}

// verificationVariants — варианты услуги верификации, найденные по системному
// коду: сам узел с кодом, если это вариант, или живые варианты под ним, если
// это категория. Когда кода в каталоге нет, остаётся библиотечное поведение:
// узлы, исполняющие его скрипт, знает движок — по коду, не обходом каталога.
func (s *ExecutorVerificationService) verificationVariants(ctx context.Context) ([]*repository.ServiceNode, error) {
	node, err := s.catalog.GetNodeByCode(ctx, VerificationServiceCode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	switch {
	case node == nil:
		return s.behaviorVariants(ctx)
	case node.IsVariant():
		return []*repository.ServiceNode{node}, nil
	default:
		return s.catalog.GetChildren(ctx, node.ID, repository.FilterLive())
	}
}

// behaviorVariants — варианты, исполняющие библиотечное поведение верификации,
// когда системный код не задан. Это запасной путь: он читает все живые
// варианты, и держать его основным значило бы платить за обход каталога на
// каждый экран верификации.
func (s *ExecutorVerificationService) behaviorVariants(ctx context.Context) ([]*repository.ServiceNode, error) {
	all, err := s.catalog.GetActiveVariants(ctx)
	if err != nil {
		return nil, err
	}
	var matched []*repository.ServiceNode
	for _, v := range all {
		if s.behaviors.Code(v) == VerificationBehaviorCode {
			matched = append(matched, v)
		}
	}
	return matched, nil
}

// openOrder возвращает незавершённый заказ пользователя на верификацию.
// Незакрытых заказов у человека единицы, а история — сотни, поэтому читаются
// только открытые.
func (s *ExecutorVerificationService) openOrder(ctx context.Context, userID uuid.UUID) (*repository.Order, error) {
	open, err := s.orderRepo.FindOpenByCustomer(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(open) == 0 {
		return nil, nil
	}
	variants, err := s.verificationVariants(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[uuid.UUID]bool, len(variants))
	for _, v := range variants {
		ids[v.ID] = true
	}
	for _, o := range open {
		if ids[o.ServiceVariantID] {
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
	return defaultOf(addresses), nil
}

// defaultOf выбирает адрес по умолчанию, иначе первый.
func defaultOf(addresses []repository.Address) *repository.Address {
	for i := range addresses {
		if addresses[i].IsDefault {
			return &addresses[i]
		}
	}
	if len(addresses) > 0 {
		return &addresses[0]
	}
	return nil
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
