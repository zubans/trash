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

// OrderService ведёт заказ от создания до закрытия: создание, взятие,
// отметку «Исполнил», подтверждение, отмену, чаевые — и собирает списки и
// карточки заказов под того, кто смотрит. Споры живут в DisputeService, а
// переходы, которыми пользуются соседи, описаны в OrderLifecycle.
type OrderService struct {
	orderRepo    repository.OrderRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	// acceptRadiusFallbackKM — запасной радиус взятия из окружения (см.
	// acceptRadiusKM); ноль — умолчание.
	acceptRadiusFallbackKM float64
	userRepo               repository.UserRepository
	shiftRepo              repository.ShiftRepository
	chatRepo               repository.ChatRepository
	catalogRepo            repository.ServiceCatalogRepository
	resolver               AddressResolver
	// Необязательно. Когда подключено, список ближайших привязывается к
	// собственной сохранённой рабочей позиции исполнителя, а не к присланным
	// клиентом координатам — к той же точке, что используют карта и проверка
	// радиуса принятия, — поэтому список не может разойтись с тем, что можно взять.
	executorGeoRepo repository.ExecutorGeoRepository
	// behaviors, claimRepo и events — обвязка скриптовых услуг. Все три
	// необязательны и ходят вместе: без них узлу услуги, называющему поведение,
	// отказывают, а не считают его молча обычным (см. проверки в eligibility.go), и
	// доменные события не публикуются.
	behaviors *Behaviors
	claimRepo repository.ServiceClaimRepository
	events    repository.EventRepository
	// levels выводит ставку комиссии из уровня исполнителя, а stats копит
	// агрегаты, по которым ачивки решают. Оба необязательны: без них комиссия
	// одна на всех, ровно как до появления геймификации.
	levels *Levels
	stats  repository.ExecutorStatsRepository
	// disputes нужны здесь ради двух вещей: закрыть заказ нельзя, пока его спор
	// открыт, а подтверждение заказчиком закрывает спор. Сами споры ведёт DisputeService.
	disputes repository.DisputeRepository
	// penalties — тихая блокировка ролей и период фото-подтверждения.
	penalties *PenaltyService
	// disputeNotifier сообщает сторонам о споре, который закрыло подтверждение.
	disputeNotifier *DisputeNotifier
	// photoProof — модуль фото-подтверждения. Без него заказы фото не требуют.
	photoProof PhotoProofGate
}

// NewOrderService создаёт OrderService.
func NewOrderService(orderRepo repository.OrderRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, catalogRepo repository.ServiceCatalogRepository, resolver AddressResolver) *OrderService {
	return &OrderService{orderRepo: orderRepo, ledger: ledger, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, catalogRepo: catalogRepo, resolver: resolver}
}

// WithAcceptRadiusFallback задаёт запасной радиус взятия, который действует,
// пока в админке радиус не настроен. main.go читает его из ACCEPT_RADIUS_KM.
func (s *OrderService) WithAcceptRadiusFallback(km float64) *OrderService {
	s.acceptRadiusFallbackKM = km
	return s
}

// WithAchievements подключает уровни и агрегаты. Пока их нет, ставка комиссии
// берётся общая, а счётчики исполнителя не ведутся, — то есть ровно то
// поведение, какое сервис имел до геймификации.
func (s *OrderService) WithAchievements(levels *Levels, stats repository.ExecutorStatsRepository) *OrderService {
	s.levels = levels
	s.stats = stats
	return s
}

// WithBehaviors подключает скриптовые услуги к жизненному циклу заказа: хуки
// ценообразования и допуска, claim «один раз на пользователя» и доменные
// события, на которые реагирует диспетчер поведений.
func (s *OrderService) WithBehaviors(behaviors *Behaviors, claimRepo repository.ServiceClaimRepository, events repository.EventRepository) *OrderService {
	s.behaviors = behaviors
	s.claimRepo = claimRepo
	s.events = events
	return s
}

// WithExecutorGeo подключает хранилище местоположений исполнителей, чтобы
// список ближайших разрешался по сохранённой на сервере позиции, а не по
// координатам из запроса, которым нельзя доверять.
func (s *OrderService) WithExecutorGeo(geoRepo repository.ExecutorGeoRepository) *OrderService {
	s.executorGeoRepo = geoRepo
	return s
}

// WithPenalties подключает тихую блокировку и период фото-подтверждения.
func (s *OrderService) WithPenalties(penalties *PenaltyService) *OrderService {
	s.penalties = penalties
	return s
}

// WithDisputes подключает хранилище споров: без него заказ закрывается, не
// глядя на спор, которого и быть не может.
func (s *OrderService) WithDisputes(disputes repository.DisputeRepository) *OrderService {
	s.disputes = disputes
	return s
}

// publishOrderEvent добавляет доменное событие о заказе внутри транзакции
// вызывающего. Ошибка записи возвращается, потому что незаписанное событие —
// это реакция, которой никогда не будет.
//
// Заказы услуг без поведения не публикуют ничего. Событие доставляется
// поведению своего заказа и никому больше, поэтому событие обычной услуги не
// смогло бы ничего сделать: писать по одному на каждый шаг жизненного цикла
// каждого заказа значило бы забить таблицу строками, чьё единственное будущее —
// пометка «обработано». Когда вариант разрешить не удалось, событие пишется
// всё равно: лишнее событие — это no-op, а пропущенное — неполученная награда.
func (s *OrderService) publishOrderEvent(ctx context.Context, tx *sql.Tx, eventType string, order *repository.Order, actorID *uuid.UUID) error {
	if s.events == nil || order == nil {
		return nil
	}
	if !eventsForEveryOrder[eventType] {
		if variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID); err == nil && !s.behaviors.governs(variant) {
			return nil
		}
	}
	return s.events.Publish(ctx, tx, &repository.DomainEvent{
		Type:        eventType,
		SubjectType: repository.EventSubjectOrder,
		SubjectID:   order.ID,
		ActorID:     actorID,
	})
}

// eventsForEveryOrder — события, которые публикуются по любому заказу, а не
// только по заказу скриптовой услуги.
//
// Правило выше — «событие обычной услуги ничего бы не сделало» — было верно,
// пока у outbox был один читатель. У ачивок другой субъект: они про человека, а
// не про услугу, и «этот исполнитель закрыл заказ за двадцать минут» одинаково
// важно на любой услуге. Остальные события по-прежнему публикуются только там,
// где их кто-то ждёт: по одному на каждый шаг каждого заказа — это таблица,
// чьё единственное будущее пометка «обработано».
var eventsForEveryOrder = map[string]bool{
	repository.EventOrderConfirmed:  true,
	repository.EventOrderCanceled:   true,
	repository.EventDisputeConceded: true,
}

// Ключи system_settings, управляющие автооткрытием смены при взятии заказа.
// SettingAutoShiftOnAcceptEnabled принимает «1»/«0» (по умолчанию включено),
// SettingAutoShiftDurationHours — одну из ShiftDurationsHours.
const (
	SettingAutoShiftOnAcceptEnabled = "auto_shift_on_accept_enabled"
	SettingAutoShiftDurationHours   = "auto_shift_duration_hours"

	// Самая короткая из разрешённых длительностей: смену открыли за
	// исполнителя, и чем она короче, тем меньше он рискует штрафом за
	// досрочный выход.
	defaultAutoShiftDurationHours = 1
)

// SettingOrderCommissionPercent — ключ system_settings, хранящий долю платформы
// с завершённого заказа в процентах от суммы, которую заказчик реально
// заплатил. Админы правят его на экране настроек.
const SettingOrderCommissionPercent = "order_commission_percent"

// commissionOn возвращает долю платформы с завершённого заказа. Доля ужимается
// в 0..100 процентов и здесь, и в валидаторе настроек: значение вне этого
// диапазона либо выплатило бы исполнителю больше, чем заплатил заказчик, либо
// взяло бы деньги, которых эскроу не держит, и ни то ни другое не стоит доверия
// к строке настроек. Округление происходит один раз, в Scale, а остаток
// достаётся исполнителю.
func commissionOn(amount money.Amount, settings map[string]float64) money.Amount {
	return commissionAt(amount, commissionPercent(settings))
}

// commissionPercent достаёт базовую ставку из настроек и ужимает её в 0..100.
func commissionPercent(settings map[string]float64) float64 {
	percent := settings[SettingOrderCommissionPercent]
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// commissionAt считает долю по уже выбранной ставке. Отделена от чтения
// настройки, потому что ставка теперь бывает персональной: уровень исполнителя
// снижает её, и та же арифметика обязана применяться к обеим.
func commissionAt(amount money.Amount, percent float64) money.Amount {
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

// CreateOrderRequest содержит данные, нужные для создания заказа.
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

// loadSettings читает числовые настройки с умолчаниями тарифных
// коэффициентов — карта, из которой считаются цена и базовая комиссия.
func (s *OrderService) loadSettings(ctx context.Context) map[string]float64 {
	settings := map[string]float64{
		"standard_tariff_coeff": 1.0,
		"urgent_tariff_coeff":   3.0,
		"asap_tariff_coeff":     8.0,
	}
	stored := loadSettingsMap(ctx, s.settingsRepo)
	for k, v := range stored {
		if k == "currency" {
			continue
		}
		// Нечисловые настройки в карту не попадают: у них нет умолчания, за
		// которое можно было бы спрятать негодное значение.
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			settings[k] = f
		}
	}
	return settings
}

// CalculatePrice возвращает цену для заданного варианта услуги и флагов срочности.
func (s *OrderService) CalculatePrice(ctx context.Context, serviceVariantID uuid.UUID, isUrgent, isAsap, isDowngraded bool) (money.Amount, error) {
	variant, err := s.catalogRepo.GetNodeByID(ctx, serviceVariantID)
	if err != nil {
		return money.Zero, err
	}
	return s.priceOf(ctx, variant, isUrgent, isAsap, isDowngraded)
}

// priceOf — CalculatePrice над уже загруженным вариантом.
func (s *OrderService) priceOf(ctx context.Context, variant *repository.ServiceNode, isUrgent, isAsap, isDowngraded bool) (money.Amount, error) {
	if variant == nil || !variant.IsVariant() {
		return money.Zero, ErrInvalidServiceVariant
	}
	// Поведение, назначающее цену своей услуге, перекрывает каталог целиком,
	// включая тарифные коэффициенты: «бесплатно» обязано оставаться бесплатным и
	// на срочном заказе, а понижение не может сделать дешевле, чем ничего.
	if scripted, ok, err := s.behaviors.Price(ctx, variant); err != nil {
		return money.Zero, err
	} else if ok {
		return scripted, nil
	}

	if variant.BasePrice == nil {
		return money.Zero, validationError("variant has no base price")
	}

	price := *variant.BasePrice

	if variant.IsAuction {
		return money.Zero, nil
	}

	if isDowngraded {
		return price, nil
	}

	// Scale округляет один раз, здесь, а не позволяет float-коэффициенту размазать
	// результат по остальному потоку.
	settings := s.loadSettings(ctx)
	switch {
	case isAsap:
		price = price.Scale(settings["asap_tariff_coeff"])
	case isUrgent:
		price = price.Scale(settings["urgent_tariff_coeff"])
	}

	return price, nil
}

// --- Создание ---------------------------------------------------------------

// preparedOrder — заказ, прошедший все проверки и готовый к записи. Разделение
// на «подготовить» и «записать» существует ради вызывающих со своей
// транзакцией: заявка на верификацию дозаполняет профиль и размещает заказ
// одним коммитом.
type preparedOrder struct {
	order   *repository.Order
	variant *repository.ServiceNode
	// auction — аукционная заявка: без удержания и без события создания.
	auction bool
}

// newOrder собирает строку заказа. Комментарий обрезается, пустой не хранится.
func newOrder(customerID uuid.UUID, variant *repository.ServiceNode, isUrgent, isAsap bool, address, comment string, hold money.Amount) *repository.Order {
	now := time.Now()
	var commentPtr *string
	if c := strings.TrimSpace(comment); c != "" {
		commentPtr = &c
	}
	var deadline *time.Time
	if isUrgent {
		d := now.Add(1 * time.Hour)
		deadline = &d
	} else if isAsap {
		d := now.Add(15 * time.Minute)
		deadline = &d
	}
	return &repository.Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: variant.ID,
		IsUrgent:         isUrgent,
		IsAsap:           isAsap,
		Comment:          commentPtr,
		Status:           repository.OrderStatusSearching,
		HoldAmount:       hold,
		FinalAmount:      hold,
		Address:          &address,
		CreatedAt:        now,
		DeadlineAt:       deadline,
	}
}

// resolvePickup записывает координаты подачи: предпочитает переданные lat/lon,
// иначе геокодирует адрес.
func (s *OrderService) resolvePickup(ctx context.Context, order *repository.Order, address string, lat, lon *float64) {
	if lat != nil && lon != nil {
		order.PickupLat = lat
		order.PickupLon = lon
		return
	}
	// От клиента координат нет (старая сборка или набранная строка):
	// разрешаем их один раз здесь, чтобы заказ можно было подобрать. Выбранная
	// подсказка несёт свои и в эту ветку не попадает.
	if s.resolver != nil && address != "" {
		if geo, err := s.resolver.Resolve(ctx, address); err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}
}

// createChatBestEffort заводит чат заказа. Неудача не фатальна: заказ и его
// удержание уже закоммичены, чат появится при первом сообщении.
func (s *OrderService) createChatBestEffort(ctx context.Context, orderID uuid.UUID) {
	if s.chatRepo == nil {
		return
	}
	if _, err := s.chatRepo.CreateChat(ctx, orderID); err != nil {
		log.Printf("[OrderService] failed to create chat for order %s: %v", orderID, err)
	}
}

// prepareOrder проверяет запрос обычного заказа и собирает заказ, ничего не записывая.
func (s *OrderService) prepareOrder(ctx context.Context, customerID uuid.UUID, req CreateOrderRequest) (*preparedOrder, error) {
	if req.IsUrgent && req.IsAsap {
		return nil, validationError("cannot set both urgent and asap flags")
	}

	variant, err := s.catalogRepo.GetNodeByID(ctx, req.ServiceVariantID)
	if err != nil {
		return nil, err
	}
	if variant == nil || !variant.IsVariant() {
		return nil, ErrInvalidServiceVariant
	}
	// Списанная услуга продолжает разрешаться для уже размещённых по ней
	// заказов, но новый заказ по ней создать нельзя.
	if !variant.IsOrderable() {
		return nil, ErrServiceVariantUnavailable
	}
	if variant.IsAuction {
		return nil, validationError("auction variants are ordered through the construction order endpoint")
	}
	if err := s.checkCustomerEligibility(ctx, customerID, variant); err != nil {
		return nil, err
	}

	holdAmount, err := s.priceOf(ctx, variant, req.IsUrgent, req.IsAsap, false)
	if err != nil {
		return nil, err
	}
	if holdAmount.IsNegative() {
		return nil, validationError("invalid order price")
	}

	order := newOrder(customerID, variant, req.IsUrgent, req.IsAsap, req.Address, req.Comment, holdAmount)
	s.resolvePickup(ctx, order, req.Address, req.Lat, req.Lon)
	return &preparedOrder{order: order, variant: variant}, nil
}

// checkCustomerEligibility — вариант с пометкой requires_verification может
// заказать только вручную верифицированный заказчик. Проверяется здесь, а не
// только прячется в каталоге, чтобы это нельзя было обойти отправкой известного
// id варианта.
func (s *OrderService) checkCustomerEligibility(ctx context.Context, customerID uuid.UUID, variant *repository.ServiceNode) error {
	if s.userRepo == nil {
		return nil
	}
	customer, err := s.userRepo.FindByID(ctx, customerID)
	if err != nil {
		return userNotFound(err)
	}
	return canCustomerOrderVariant(ctx, s.behaviors, s.penalties, customer, variant)
}

// placeOrderTx записывает подготовленный заказ в транзакции вызывающего.
// Создание заказа, удержание баланса и проводка происходят в одной
// транзакции: списание охраняется балансом, поэтому параллельные запросы не
// потратят одни деньги дважды, а сбой на любом шаге не оставит после себя ни
// заказа, ни удержания.
func (s *OrderService) placeOrderTx(ctx context.Context, tx *sql.Tx, p *preparedOrder) error {
	order := p.order
	// Строка заказа идёт первой: проводка на неё ссылается, а
	// transactions.order_id — внешний ключ, проверяемый немедленно. Порядок
	// здесь ничего не стоит — оба оператора делят одну транзакцию, поэтому
	// неудавшееся удержание откатывает заказ вместе с собой.
	if err := s.orderRepo.Create(ctx, tx, order); err != nil {
		return err
	}
	if p.auction {
		return nil
	}
	// Услуга, которую можно заказать один раз на пользователя, занимает свою
	// строку здесь, в той же транзакции, что и заказ. Два одновременных запроса
	// оба проходят хук can_order; строку получает только один.
	if s.behaviors.OncePerUser(p.variant) {
		if s.claimRepo == nil {
			return ErrServiceVariantUnavailable
		}
		if err := s.claimRepo.Claim(ctx, tx, order.CustomerID, p.variant.ID, order.ID); err != nil {
			if errors.Is(err, repository.ErrServiceAlreadyClaimed) {
				return ErrServiceAlreadyOrdered
			}
			return err
		}
	}
	// Reserve — это одно условное списание в паре с зачислением в эскроу:
	// деньги не уничтожаются, они переходят на счёт, который держит их на всё
	// время заказа.
	if err := s.ledger.Reserve(ctx, tx, order.CustomerID, repository.AccountEscrow, order.HoldAmount, repository.TransactionTypeHold, &order.ID); err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return ErrInsufficientBalance
		}
		return err
	}
	return s.publishOrderEvent(ctx, tx, repository.EventOrderCreated, order, &order.CustomerID)
}

// orderPlaced — всё, что происходит после коммита заказа: чат, метрики и
// карточка для ответа. По мере возможности: заказ и его удержание уже записаны.
func (s *OrderService) orderPlaced(ctx context.Context, p *preparedOrder) *OrderView {
	s.createChatBestEffort(ctx, p.order.ID)
	if p.auction {
		metrics.OrderEvent("created_auction")
	} else {
		metrics.OrderEvent("created")
		if !p.order.HoldAmount.IsPositive() {
			// Бесплатная услуга — поддерживаемый случай, поэтому это факт для
			// публикации, а не сбой для отчёта, — но опубликовать его надо, иначе заказ
			// не оставит следа вообще ни в одной денежной метрике.
			metrics.OrderCreatedFree()
		}
	}
	return s.viewOf(ctx, p.order)
}

// Create создаёт обычный заказ и удерживает баланс заказчика.
func (s *OrderService) Create(ctx context.Context, customerID uuid.UUID, req CreateOrderRequest) (*OrderView, error) {
	p, err := s.prepareOrder(ctx, customerID, req)
	if err != nil {
		return nil, err
	}
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.placeOrderTx(ctx, tx, p)
	}); err != nil {
		return nil, err
	}
	return s.orderPlaced(ctx, p), nil
}

// CreateConstructionOrder создаёт аукционный заказ на вывоз строительного мусора.
func (s *OrderService) CreateConstructionOrder(ctx context.Context, customerID uuid.UUID, photoURL, address, comment string, lat, lon *float64) (*OrderView, error) {
	photoURL = strings.TrimSpace(photoURL)
	if photoURL == "" {
		return nil, validationError("photo URL is required")
	}
	// Принимается только путь, порождённый нашим собственным эндпоинтом загрузки.
	// Значение раньше сохранялось дословно и рисовалось в админ-панели, поэтому
	// произвольный URL там — это чужой контент на нашей странице.
	if !strings.HasPrefix(photoURL, "/uploads/") || strings.Contains(photoURL, "..") {
		return nil, validationError("photo must be uploaded through the app")
	}

	// GetNodeByCode видит только живые узлы, поэтому списанный строительный
	// вариант читается как отсутствующий, а не как ошибка базы.
	variant, err := s.catalogRepo.GetNodeByCode(ctx, "trash_construction")
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if variant == nil || variant.IsDeleted() {
		return nil, fmt.Errorf("%w: construction variant is missing from the catalog", ErrNotConfigured)
	}
	if !variant.IsActive {
		return nil, ErrServiceVariantUnavailable
	}
	// Та же проверка верификации заказчика, что и на пути обычного заказа, — на
	// случай, если строительный вариант помечен requires_verification.
	if err := s.checkCustomerEligibility(ctx, customerID, variant); err != nil {
		return nil, err
	}

	order := newOrder(customerID, variant, false, false, address, comment, money.Zero)
	order.PhotoURL = &photoURL
	s.resolvePickup(ctx, order, address, lat, lon)
	p := &preparedOrder{order: order, variant: variant, auction: true}

	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.placeOrderTx(ctx, tx, p)
	}); err != nil {
		return nil, err
	}
	return s.orderPlaced(ctx, p), nil
}

// --- Взятие и исполнение ----------------------------------------------------

// Accept позволяет исполнителю взять заказ из очереди. Каждое ограничение,
// которое список заказов применяет при показе, перепроверяется здесь, потому
// что список — лишь удобство, а настоящей точкой авторизации является этот
// метод.
func (s *OrderService) Accept(ctx context.Context, orderID, executorID uuid.UUID) error {
	settings := loadSettingsMap(ctx, s.settingsRepo)

	shift, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return err
	}
	if shift != nil && shift.Status == repository.ShiftStatusPenalized {
		return ErrExecutorPenalized
	}
	// Смена без смены больше не тупик: исполнитель, нажавший «взять заказ»,
	// уже сказал, что готов работать, поэтому смену открывают за него. Здесь
	// только решается, что она понадобится; сама смена создаётся ниже, когда
	// заказ уже прошёл все проверки, — иначе отказ по балансу или лимиту
	// оставлял бы за исполнителем открытую смену, за досрочный выход из которой
	// берут штраф.
	autoOpenShift := shift == nil
	if autoOpenShift && !settings.bool(SettingAutoShiftOnAcceptEnabled, true) {
		return ErrNoActiveShift
	}

	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return orderNotFound(err)
	}
	if order.CustomerID == executorID {
		return ErrOwnOrder
	}
	if err := s.checkExecutorEligibility(ctx, executorID, order); err != nil {
		return err
	}
	if err := s.checkAcceptRadius(ctx, settings, executorID, order); err != nil {
		return err
	}

	balance, err := s.ledger.GetBalance(ctx, executorID)
	if err != nil {
		return err
	}
	// Предел настраивается как модуль и применяется как отрицательный пол,
	// например min_balance_limit=500 означает «никаких новых заказов ниже -500».
	minBalanceLimit := money.FromRubles(-math.Abs(settings.float("min_balance_limit", defaultMinBalanceLimit)))
	if balance < minBalanceLimit {
		return ruleError(fmt.Sprintf("нельзя брать новые заказы: баланс %s ниже допустимого лимита (%s)", balance, minBalanceLimit))
	}
	maxActive := settings.int("max_active_orders", defaultMaxActiveOrders)
	maxExecuted := settings.int("max_executed_unconfirmed_orders", defaultMaxExecutedUnconfirmed)

	// Смена открывается до назначения, потому что назначенный заказ обязан
	// принадлежать исполнителю на смене: по смене его находит автоподбор и по
	// ней же считается штраф за досрочный уход.
	var openedShift *repository.Shift
	if autoOpenShift {
		openedShift, err = s.shiftRepo.StartShift(ctx, executorID, autoShiftDurationHours(settings))
		if err != nil {
			log.Printf("[OrderService] failed to auto-open shift for executor %s: %v", executorID, err)
			return ErrNoActiveShift
		}
		metrics.ShiftEvent("auto_started")
	}

	// Лимиты, назначение и порождаемое им событие делят одну транзакцию под
	// блокировкой исполнителя: два параллельных взятия не пройдут оба под
	// одним счётчиком, а поведение, реагирующее на принятый заказ, не увидит
	// заказ, который так и не назначили.
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.orderRepo.LockExecutor(ctx, tx, executorID); err != nil {
			return err
		}
		activeCount, err := s.orderRepo.CountActiveOrdersByExecutor(ctx, tx, executorID)
		if err != nil {
			return err
		}
		if activeCount >= maxActive {
			return ruleError(fmt.Sprintf("превышен лимит активных заказов (не более %d)", maxActive))
		}
		executedCount, err := s.orderRepo.CountExecutedUnconfirmedOrdersByExecutor(ctx, tx, executorID)
		if err != nil {
			return err
		}
		if executedCount >= maxExecuted {
			return ruleError(fmt.Sprintf("превышен лимит непотвержденных заказчиком исполненных заказов (не более %d)", maxExecuted))
		}
		if err := s.orderRepo.Assign(ctx, tx, orderID, executorID); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return ErrOrderTaken
			}
			return err
		}
		if err := s.requirePhotoProofTx(ctx, tx, order, executorID); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventOrderAccepted, order, &executorID)
	}); err != nil {
		// Смену открыли только ради этого заказа, а заказа не будет — например,
		// его успел взять другой исполнитель. Закрываем её тем же путём, что и
		// отработавшую до конца смену (без штрафа): иначе исполнитель остался бы
		// со сменой, которую не открывал и за досрочный выход из которой платит.
		if openedShift != nil {
			if endErr := s.shiftRepo.End(ctx, nil, openedShift.ID); endErr != nil {
				log.Printf("[OrderService] failed to roll back auto-opened shift %s: %v", openedShift.ID, endErr)
			} else {
				metrics.ShiftEvent("auto_rolled_back")
			}
		}
		return err
	}
	metrics.OrderEvent("accepted")
	return nil
}

// autoShiftDurationHours возвращает длительность автоматически открываемой
// смены. Значение вне списка разрешённых игнорируется, а не создаёт смену,
// которую исполнитель не смог бы открыть сам.
func autoShiftDurationHours(settings settingsMap) int {
	hours := settings.int(SettingAutoShiftDurationHours, defaultAutoShiftDurationHours)
	if !IsValidShiftDuration(hours) {
		return defaultAutoShiftDurationHours
	}
	return hours
}

// checkExecutorEligibility применяет общий предикат видимости/принятия — тот
// же, что используют списки заказов, поэтому исполнитель может принять только
// то, что видит.
func (s *OrderService) checkExecutorEligibility(ctx context.Context, executorID uuid.UUID, order *repository.Order) error {
	if s.userRepo == nil {
		return nil
	}
	_, err := eligibilityFor(ctx, s.userRepo, s.catalogRepo, s.behaviors, s.penalties, executorID, order)
	return err
}

// checkAcceptRadius не даёт взять заказ дальше радиуса взятия.
//
// Радиус существовал только на клиенте: карта считала can_accept и прятала
// кнопку, а сервер расстояние не смотрел вовсе. Кнопка — не ограничение:
// список «Заказы поблизости» на дашборде показывал её безусловно на радиусе в
// 5 км, и заказ за пределами круга брался обычным нажатием, не говоря уже о
// прямом вызове эндпоинта.
//
// Радиус тот же, что рисует карта (acceptRadiusKM), и позиция та же,
// авторитетная серверная, — иначе проверка расходилась бы с тем, что видит
// исполнитель.
//
// Автоподбор эта проверка не трогает: воркер назначает заказы через
// orderRepo.Assign, минуя Accept, и живёт по своему auto_match_radius_km.
func (s *OrderService) checkAcceptRadius(ctx context.Context, settings settingsMap, executorID uuid.UUID, order *repository.Order) error {
	// Без хранилища позиций проверять нечего: так собран сервис в тестах, где
	// география не участвует. В main.go оно подключено всегда (WithExecutorGeo),
	// и именно поэтому проверка там работает.
	if s.executorGeoRepo == nil {
		return nil
	}
	// Заказ без координат к точке на карте не привязан, и мерить до него нечего.
	// Такие заказы не попадают ни в один гео-список, но взять по прямой ссылке
	// их можно, и отказывать здесь было бы отказом по причине, которой нет.
	if order.PickupLat == nil || order.PickupLon == nil {
		return nil
	}

	lat, lon, _, err := s.executorGeoRepo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		return err
	}
	if lat == nil || lon == nil {
		return ErrWorkPositionUnknown
	}

	radiusKM := acceptRadiusKM(settings, s.acceptRadiusFallbackKM)
	distanceKM := HaversineDistanceKM(*lat, *lon, *order.PickupLat, *order.PickupLon)
	if distanceKM > radiusKM {
		return ruleError(fmt.Sprintf("заказ вне зоны взятия: до него %.1f км, разрешено %.1f км", distanceKM, radiusKM))
	}
	return nil
}

// RejectAssignedOrder позволяет исполнителю бросить назначенный заказ.
// Исполнителя штрафуют на долю стоимости заказа (см. reject_penalty_share), а
// заказ возвращается в пул поиска. Штраф и снятие назначения делят одну
// транзакцию, поэтому с исполнителя никогда не спишут за заказ, оставшийся за
// ним.
func (s *OrderService) RejectAssignedOrder(ctx context.Context, orderID, executorID uuid.UUID) error {
	share := settingFloat(ctx, s.settingsRepo, "reject_penalty_share", defaultRejectPenaltyShare)
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if !executorCanReject(order, executorID) {
			return ErrOrderNotAssignedToExecutor
		}

		// Штраф собирают, а не уничтожают: он попадает на счёт штрафов.
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

// ReturnToWork возвращает заказ на проверке (исполнитель отметил «Исполнил»,
// заказчик ещё не подтвердил) исполнителю в работу. Так администратор или
// модератор снимает отметку, поставленную без выполненной работы. Деньги не
// двигаются: удержание заказчика остаётся на эскроу до подтверждения.
func (s *OrderService) ReturnToWork(ctx context.Context, orderID, actorID uuid.UUID) error {
	var order *repository.Order
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		locked, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if locked.Status != repository.OrderStatusExecuted {
			return ErrOrderNotOnReview
		}
		if err := s.orderRepo.ReturnToWork(ctx, tx, orderID); err != nil {
			return err
		}
		order = locked
		return s.publishOrderEvent(ctx, tx, repository.EventOrderReturned, order, &actorID)
	}); err != nil {
		return err
	}
	metrics.OrderEvent("returned")
	log.Printf("[AUDIT] user %s returned order %s to work", actorID, orderID)

	if order.ExecutorID != nil {
		systemChatMessage(ctx, s.chatRepo, orderID, *order.ExecutorID, "🔄 Администрация вернула заказ в работу: отметка «Исполнил» снята.")
	}
	return nil
}

// ExecuteOrderAt — отметка «Исполнил» с временем устройства. Оно приходит,
// когда отметка пролежала в офлайн-очереди, и хранится рядом со временем
// сервера: арбитраж показывает оба.
//
// Заказ, требующий фото-подтверждения, без загруженного снимка места заказа
// исполненным не становится — ErrPhotoProofRequired.
func (s *OrderService) ExecuteOrderAt(ctx context.Context, orderID, executorID uuid.UUID, deviceAt *time.Time) error {
	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return orderNotFound(err)
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
		return ErrOrderNotAssignedToExecutor
	}
	// Заказ, который закрывает скрипт услуги (верификация — по совпадению данных),
	// отметкой исполнителя не закрывается: иначе заказчик подтвердил бы работу,
	// которой не было. Кнопку приложение не рисует, здесь — для запросов в обход.
	if s.behaviors != nil && s.catalogRepo != nil {
		variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID)
		if err != nil {
			return err
		}
		if !s.behaviors.ManualExecute(variant) {
			return ErrManualExecuteDisabled
		}
	}

	// Отметка о выполненной работе — это то, что верифицирует заказчика в услуге
	// верификации, поэтому событие обязано быть таким же надёжным, как смена статуса.
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if order.PhotoRequired && s.photoProof != nil {
			has, err := s.photoProof.HasRequiredPhotoTx(ctx, tx, orderID)
			if err != nil {
				return err
			}
			if !has {
				return ErrPhotoProofRequired
			}
		}
		if err := s.orderRepo.Execute(ctx, tx, orderID); err != nil {
			return err
		}
		if deviceAt != nil && !deviceAt.IsZero() {
			if err := s.orderRepo.SetExecutedAtDevice(ctx, tx, orderID, *deviceAt); err != nil {
				return err
			}
		}
		return s.publishOrderEvent(ctx, tx, repository.EventOrderExecuted, order, &executorID)
	}); err != nil {
		return err
	}
	metrics.OrderEvent("executed")

	systemChatMessage(ctx, s.chatRepo, orderID, executorID, "📦 Исполнитель отметил(а) выполнение заказа! Пожалуйста, подтвердите приемку работы.")
	return nil
}

// --- Подтверждение и отмена -------------------------------------------------

// ConfirmOrder завершает заказ и проводит платежи. Строка заказа блокируется и
// перечитывается внутри транзакции, поэтому два параллельных подтверждения не
// могут оба выплатить исполнителю, а выплата выводится из удержания, которое
// реально ещё удерживается (см. путь понижения SLA).
func (s *OrderService) ConfirmOrder(ctx context.Context, orderID uuid.UUID) error {
	// Считается после возврата транзакции и никогда внутри неё: откаченное
	// подтверждение никому не заплатило и не должно попадать в выручку.
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.confirmTx(ctx, tx, orderID)
	})
	if err == nil {
		metrics.OrderEvent("confirmed")
	}
	return err
}

// Confirm завершает заказ конкретного заказчика. Подтверждение оспоренного
// заказа закрывает его спор: заказчик и исполнитель договорились, и арбитру
// решать больше нечего.
func (s *OrderService) Confirm(ctx context.Context, customerID, orderID uuid.UUID) error {
	var closed *repository.Dispute
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if order.CustomerID != customerID {
			return ErrForbidden
		}
		if order.Status == repository.OrderStatusDisputed {
			closed, err = closeOpenDisputeTx(ctx, tx, s.disputes, orderID, repository.DisputeClosing{
				Closure:  repository.DisputeClosureCustomerConfirmed,
				ClosedBy: &customerID,
			})
			if err != nil {
				return err
			}
		}
		return s.confirmTx(ctx, tx, orderID)
	})
	if err != nil {
		return err
	}
	metrics.OrderEvent("confirmed")
	if closed != nil {
		s.disputeNotifier.DisputeClosed(ctx, closed)
	}
	return nil
}

// CancelOrder отменяет активный заказ и возвращает удержание ровно один раз.
// Возврат и смена статуса делят одну транзакцию и одну блокировку строки, а
// удержание обнуляется, поэтому повторная или параллельная отмена не выплатит снова.
func (s *OrderService) CancelOrder(ctx context.Context, orderID uuid.UUID) error {
	return s.cancel(ctx, orderID, customerCancelStatuses...)
}

// CancelUnclaimedAuction отменяет аукционную заявку, истёкшую без победителя.
// В отличие от CancelOrder он отказывает заказу, который уже дошёл до
// ASSIGNED.
//
// Различие важно из-за гонки, которую семидневная зачистка иначе проигрывала
// бы: воркер выбирает истёкшие заявки, а заказчик может принять ставку по одной
// из них раньше, чем воркер до неё доберётся. Именно принятие ставки переводит
// аукцион в ASSIGNED и двигает деньги в эскроу, поэтому отмена после этого
// отняла бы работу у только что выигравшего исполнителя, вернула деньги только
// что решившемуся заказчику, и всё это из-за скана, начавшегося мгновениями
// раньше. Отменять здесь можно только по причине «никто это не забрал».
func (s *OrderService) CancelUnclaimedAuction(ctx context.Context, orderID uuid.UUID) error {
	return s.cancel(ctx, orderID, repository.OrderStatusSearching)
}

func (s *OrderService) cancel(ctx context.Context, orderID uuid.UUID, allowed ...repository.OrderStatus) error {
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		return s.cancelTx(ctx, tx, orderID, allowed...)
	})
	if err == nil {
		metrics.OrderEvent("cancelled")
	}
	return err
}

// Cancel отменяет заказ конкретного заказчика. Владение проверяется под той же
// блокировкой строки, что и сама отмена, как в Confirm: заказ, сменивший
// владельца между чтением и отменой, невозможен, но и проверять его вне
// транзакции незачем.
func (s *OrderService) Cancel(ctx context.Context, customerID, orderID uuid.UUID) error {
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if order.CustomerID != customerID {
			return ErrForbidden
		}
		return s.cancelLockedTx(ctx, tx, order, customerCancelStatuses...)
	})
	if err == nil {
		metrics.OrderEvent("cancelled")
	}
	return err
}

// --- Чаевые -----------------------------------------------------------------

// maxTipAmount — потолок от промаха пальцем на одни чаевые. Настоящее
// ограничение — проверка баланса; это лишь не даёт списать очевидно ошибочную
// сумму до того, как заказчик заметит.
var maxTipAmount = money.FromRubles(100_000)

// TipOrder позволяет заказчику дать чаевые исполнителю завершённого заказа.
// Чаевые переходят с баланса заказчика на баланс исполнителя, не более одного
// раза на заказ: однократная охрана и списание делят одну транзакцию и одну
// блокировку строки, поэтому дублирующий запрос не спишет дважды. Возвращает
// ошибку нехватки баланса, когда заказчик не может покрыть чаевые.
func (s *OrderService) TipOrder(ctx context.Context, customerID, orderID uuid.UUID, amount money.Amount) error {
	if !amount.IsPositive() {
		return validationError("tip amount must be positive")
	}
	if amount > maxTipAmount {
		return validationError("tip amount is too large")
	}

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if order.CustomerID != customerID {
			return ErrForbidden
		}
		if order.Status != repository.OrderStatusCompleted {
			return ErrTipNotAllowed
		}
		if order.ExecutorID == nil {
			return ErrOrderHasNoExecutor
		}

		tipped, err := s.ledger.HasTip(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if tipped {
			return ErrTipAlreadySent
		}

		return s.ledger.Tip(ctx, tx, customerID, *order.ExecutorID, amount, &order.ID)
	})
	// ErrInsufficientFunds пробрасывается, чтобы обработчик отрисовал её тем же
	// «недостаточно средств» / 422, что и удержание по заказу.
	if err == nil {
		metrics.OrderEvent("tipped")
	}
	return err
}

// --- Списки -----------------------------------------------------------------

// GetAvailableConstructionOrdersForExecutor возвращает открытые строительные
// заказы, которые исполнитель может увидеть и по которым может сделать ставку.
//
// Фильтр — тот же canViewOrTakeOrder, что и у карты, списка ближайших и самой
// ставки: раньше список проверял верификацию и возраст сам и показывал заказы,
// по которым ставку затем отклоняли (бан, тихая блокировка, только для
// модераторов, скрипт услуги, собственный заказ). Правило «аукцион только по
// заказу верифицированного заказчика» живёт в самом предикате, поэтому список
// и ставка отказывают по одним и тем же заказам.
func (s *OrderService) GetAvailableConstructionOrdersForExecutor(ctx context.Context, executorID uuid.UUID) ([]*OrderView, error) {
	orders, err := s.orderRepo.GetAvailableAuctionOrders(ctx)
	if err != nil {
		return nil, err
	}

	var viewer *repository.User
	if s.userRepo != nil {
		viewer, _ = s.userRepo.FindByID(ctx, executorID)
	}
	blocked := silentlyBlocked(ctx, s.penalties, viewer, repository.RoleExecutor)

	// Варианты, исполнители и заказчики, которых осматривает фильтр ниже, — всё за
	// фиксированное число запросов, а не по набору на заказ.
	views, customers := s.viewsOf(ctx, orders)
	presentFor(executorViewer(executorID), views, customers, time.Now())

	filtered := []*OrderView{}
	for _, v := range views {
		if v.CustomerID == executorID {
			continue
		}
		if canViewOrTakeOrder(ctx, s.behaviors, blocked, viewer, customers[v.CustomerID], v.ServiceVariant) != nil {
			continue
		}
		filtered = append(filtered, v)
	}
	return filtered, nil
}

// ListAssigned возвращает заказы, назначенные исполнителю.
func (s *OrderService) ListAssigned(ctx context.Context, executorID uuid.UUID) ([]*OrderView, error) {
	orders, err := s.orderRepo.FindAssignedByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	return s.presentOrders(ctx, executorViewer(executorID), orders), nil
}

// ListByCustomer возвращает заказы, созданные заказчиком, — страницу размера
// по умолчанию, см. repository.DefaultHistoryPageSize.
func (s *OrderService) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]*OrderView, error) {
	orders, err := s.orderRepo.FindByCustomer(ctx, customerID, 0)
	if err != nil {
		return nil, err
	}
	return s.presentOrders(ctx, customerViewer(customerID), orders), nil
}

// ExecutorHistory отдаёт недавние заказы исполнителя для экрана истории —
// собранными так же, как остальные его ленты.
func (s *OrderService) ExecutorHistory(ctx context.Context, executorID uuid.UUID) ([]*OrderView, error) {
	rows, err := s.orderRepo.FindAllByExecutor(ctx, executorID, 0)
	if err != nil {
		return nil, err
	}
	orders := make([]*repository.Order, len(rows))
	for i := range rows {
		orders[i] = &rows[i]
	}
	return s.presentOrders(ctx, executorViewer(executorID), orders), nil
}

// systemChatMessage пишет служебное сообщение в чат заказа. Сбой не отменяет
// действия, которое уже закоммичено: сообщение — уведомление, а не его часть.
func systemChatMessage(ctx context.Context, chatRepo repository.ChatRepository, orderID, senderID uuid.UUID, text string) {
	if chatRepo == nil {
		return
	}
	chat, err := chatRepo.GetChatByOrderID(ctx, orderID)
	if err == nil && chat != nil {
		_, _ = chatRepo.SaveMessage(ctx, chat.ID, senderID, text)
	}
}
