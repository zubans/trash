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

// OrderService ведёт жизненный цикл заказа: создание, назначение, подтверждение, отмену.
type OrderService struct {
	orderRepo    repository.OrderRepository
	ledger       *Ledger
	settingsRepo repository.SettingsRepository
	userRepo     repository.UserRepository
	shiftRepo    repository.ShiftRepository
	chatRepo     repository.ChatRepository
	catalogRepo  repository.ServiceCatalogRepository
	resolver     AddressResolver
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
	repository.EventOrderConfirmed: true,
	repository.EventOrderCanceled:  true,
}

// NewOrderService создаёт OrderService.
func NewOrderService(orderRepo repository.OrderRepository, ledger *Ledger, settingsRepo repository.SettingsRepository, userRepo repository.UserRepository, shiftRepo repository.ShiftRepository, chatRepo repository.ChatRepository, catalogRepo repository.ServiceCatalogRepository, resolver AddressResolver) *OrderService {
	return &OrderService{orderRepo: orderRepo, ledger: ledger, settingsRepo: settingsRepo, userRepo: userRepo, shiftRepo: shiftRepo, chatRepo: chatRepo, catalogRepo: catalogRepo, resolver: resolver}
}

// WithExecutorGeo подключает хранилище местоположений исполнителей, чтобы
// список ближайших разрешался по сохранённой на сервере позиции, а не по
// координатам из запроса, которым нельзя доверять.
func (s *OrderService) WithExecutorGeo(geoRepo repository.ExecutorGeoRepository) *OrderService {
	s.executorGeoRepo = geoRepo
	return s
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

// hydrateServiceVariant заполняет у заказа вариант услуги и личность
// исполнителя. Это однозаказная форма hydrateServiceVariants, которую
// используют списковые эндпоинты; обе делят одну реализацию, чтобы
// отрисованный заказ выглядел одинаково, каким бы путём его ни получили.
func (s *OrderService) hydrateServiceVariant(ctx context.Context, order *repository.Order) {
	if order == nil {
		return
	}
	s.hydrateServiceVariants(ctx, []*repository.Order{order})
}

// hydrateServiceVariants заполняет целую страницу заказов двумя запросами.
//
// Делать это по одному заказу стоило двух запросов на строку — вариант и, для
// назначенных заказов, исполнитель — на каждом списковом эндпоинте, который
// опрашивают приложения. Здесь чтения пакетные; разбор полей заказа не изменился.
func (s *OrderService) hydrateServiceVariants(ctx context.Context, orders []*repository.Order) {
	if len(orders) == 0 {
		return
	}

	variantIDs := make([]uuid.UUID, 0, len(orders))
	executorIDs := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		if o == nil {
			continue
		}
		variantIDs = append(variantIDs, o.ServiceVariantID)
		if o.ExecutorID != nil {
			executorIDs = append(executorIDs, *o.ExecutorID)
		}
	}

	variants := map[uuid.UUID]*repository.ServiceNode{}
	if s.catalogRepo != nil {
		if loaded, err := s.catalogRepo.GetNodesByIDs(ctx, variantIDs); err == nil {
			variants = loaded
		}
	}
	categories := loadOrderCategories(ctx, s.catalogRepo, variants)
	executors := map[uuid.UUID]*repository.User{}
	if s.userRepo != nil && len(executorIDs) > 0 {
		if loaded, err := s.userRepo.FindByIDs(ctx, executorIDs); err == nil {
			executors = loaded
		}
	}

	for _, o := range orders {
		if o == nil {
			continue
		}
		if variant := variants[o.ServiceVariantID]; variant != nil {
			o.ServiceVariant = variant
			o.ServiceCategory = categoryOf(variant, categories)
			// Что исполнитель обязан отправить, прежде чем этот заказ можно завершить.
			// Только имена полей: их значения — то, с чем идёт сверка, и исполнителю
			// их показывать нельзя.
			if manifest, ok := s.behaviors.Manifest(variant); ok {
				o.SubmitFields = manifest.CheckFields
			}
		}
		if o.ExecutorID == nil {
			continue
		}
		if execUser := executors[*o.ExecutorID]; execUser != nil {
			o.ExecutorPhone = execUser.Phone
			o.ExecutorName = shortDisplayName(execUser)
		}
	}
}

// shortDisplayName отдаёт «Имя Отчество Ф.» — форму, в которой приложения
// показывают исполнителя заказа.
func shortDisplayName(u *repository.User) string {
	var nameParts []string
	if u.FirstName != "" {
		nameParts = append(nameParts, u.FirstName)
	}
	if u.Patronymic != "" {
		nameParts = append(nameParts, u.Patronymic)
	}
	if u.LastName != "" {
		runes := []rune(strings.TrimSpace(u.LastName))
		if len(runes) > 0 {
			nameParts = append(nameParts, string(runes[0])+".")
		}
	}
	return strings.Join(nameParts, " ")
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

// CalculatePrice возвращает цену для заданного варианта услуги и флагов срочности.
func (s *OrderService) CalculatePrice(ctx context.Context, serviceVariantID uuid.UUID, isUrgent, isAsap, isDowngraded bool) (money.Amount, error) {
	variant, err := s.catalogRepo.GetNodeByID(ctx, serviceVariantID)
	if err != nil {
		return money.Zero, err
	}
	if variant == nil || !variant.IsVariant() {
		return money.Zero, errors.New("invalid service variant")
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
		return money.Zero, errors.New("variant has no base price")
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

// CreateOrder создаёт обычный заказ и удерживает баланс заказчика.
func (s *OrderService) CreateOrder(ctx context.Context, customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, address string, lat, lon *float64) (*repository.Order, error) {
	return s.CreateOrderWithComment(ctx, customerID, serviceVariantID, isUrgent, isAsap, address, "", lat, lon)
}

// CreateOrderWithComment создаёт обычный заказ с необязательным комментарием и
// удерживает баланс заказчика. Создание заказа, удержание баланса и проводка
// происходят в одной транзакции: списание охраняется балансом, поэтому
// параллельные запросы не потратят одни деньги дважды, а сбой на любом шаге не
// оставит после себя ни заказа, ни удержания.
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
	// Списанная услуга продолжает разрешаться для уже размещённых по ней
	// заказов, но новый заказ по ней создать нельзя.
	if !variant.IsOrderable() {
		return nil, errors.New("service variant is not available")
	}
	if variant.IsAuction {
		return nil, errors.New("auction variants are ordered through the construction order endpoint")
	}

	// Вариант с пометкой requires_verification может заказать только вручную
	// верифицированный заказчик. Проверяется здесь, а не только прячется в
	// каталоге, чтобы это нельзя было обойти отправкой известного id варианта.
	if s.userRepo != nil {
		customer, err := s.userRepo.FindByID(ctx, customerID)
		if err != nil {
			return nil, err
		}
		if err := canCustomerOrderVariant(ctx, s.behaviors, customer, variant); err != nil {
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

	// Разрешаем координаты: предпочитаем переданные lat/lon, иначе геокодируем адрес.
	if lat != nil && lon != nil {
		order.PickupLat = lat
		order.PickupLon = lon
	} else if s.resolver != nil && address != "" {
		// От клиента координат нет (старая сборка или набранная строка):
		// разрешаем их один раз здесь, чтобы заказ можно было подобрать. Выбранная
		// подсказка несёт свои и в эту ветку не попадает.
		if geo, err := s.resolver.Resolve(ctx, address); err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		// Строка заказа идёт первой: проводка на неё ссылается, а
		// transactions.order_id — внешний ключ, проверяемый немедленно. Порядок
		// здесь ничего не стоит — оба оператора делят одну транзакцию, поэтому
		// неудавшееся удержание откатывает заказ вместе с собой.
		if err := s.orderRepo.Create(ctx, tx, order); err != nil {
			return err
		}
		// Услуга, которую можно заказать один раз на пользователя, занимает свою
		// строку здесь, в той же транзакции, что и заказ. Два одновременных запроса
		// оба проходят хук can_order; строку получает только один.
		if s.behaviors.OncePerUser(variant) {
			if s.claimRepo == nil {
				return errors.New("service variant is not available")
			}
			if err := s.claimRepo.Claim(ctx, tx, customerID, variant.ID, order.ID); err != nil {
				return err
			}
		}
		// Reserve — это одно условное списание в паре с зачислением в эскроу:
		// деньги не уничтожаются, они переходят на счёт, который держит их на всё
		// время заказа.
		if err := s.ledger.Reserve(ctx, tx, customerID, repository.AccountEscrow, holdAmount, repository.TransactionTypeHold, &order.ID); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventOrderCreated, order, &customerID)
	}); err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return nil, errors.New("insufficient balance")
		}
		if errors.Is(err, repository.ErrServiceAlreadyClaimed) {
			return nil, errors.New("услуга уже была заказана")
		}
		return nil, err
	}

	// Всё ниже — по мере возможности: заказ и его удержание уже закоммичены.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(ctx, order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}

	metrics.OrderEvent("created")
	if !order.HoldAmount.IsPositive() {
		// Бесплатная услуга — поддерживаемый случай, поэтому это факт для
		// публикации, а не сбой для отчёта, — но опубликовать его надо, иначе заказ
		// не оставит следа вообще ни в одной денежной метрике.
		metrics.OrderCreatedFree()
	}
	s.hydrateServiceVariant(ctx, order)
	return order, nil
}

// Create создаёт новый заказ для заказчика (псевдоним, совместимый с обработчиком).
func (s *OrderService) Create(ctx context.Context, customerID uuid.UUID, req CreateOrderRequest) (*repository.Order, error) {
	return s.CreateOrderWithComment(ctx, customerID, req.ServiceVariantID, req.IsUrgent, false, req.Address, req.Comment, req.Lat, req.Lon)
}

// Accept позволяет исполнителю взять заказ из очереди. Каждое ограничение,
// которое список заказов применяет при показе, перепроверяется здесь, потому
// что список — лишь удобство, а настоящей точкой авторизации является этот
// метод.
func (s *OrderService) Accept(ctx context.Context, orderID, executorID uuid.UUID) error {
	shift, err := s.shiftRepo.GetActiveShift(ctx, executorID)
	hasShift := err == nil && shift != nil
	if hasShift && shift.Status == repository.ShiftStatusPenalized {
		return errors.New("executor is penalized")
	}
	// Смена без смены больше не тупик: исполнитель, нажавший «взять заказ»,
	// уже сказал, что готов работать, поэтому смену открывают за него. Здесь
	// только решается, что она понадобится; сама смена создаётся ниже, когда
	// заказ уже прошёл все проверки, — иначе отказ по балансу или лимиту
	// оставлял бы за исполнителем открытую смену, за досрочный выход из которой
	// берут штраф.
	autoOpenShift := !hasShift
	if autoOpenShift && !s.autoShiftOnAcceptEnabled(ctx) {
		return errors.New("executor has no active shift")
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
	// Предел настраивается как модуль и применяется как отрицательный пол,
	// например min_balance_limit=500 означает «никаких новых заказов ниже -500».
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

	// Смена открывается до назначения, потому что назначенный заказ обязан
	// принадлежать исполнителю на смене: по смене его находит автоподбор и по
	// ней же считается штраф за досрочный уход.
	var openedShift *repository.Shift
	if autoOpenShift {
		openedShift, err = s.shiftRepo.StartShift(ctx, executorID, s.autoShiftDurationHours(ctx))
		if err != nil {
			log.Printf("[OrderService] failed to auto-open shift for executor %s: %v", executorID, err)
			return errors.New("executor has no active shift")
		}
		metrics.ShiftEvent("auto_started")
	}

	// Назначение и порождаемое им событие делят одну транзакцию: поведение,
	// реагирующее на принятый заказ, не должно ни увидеть заказ, который так и не
	// назначили, ни пропустить тот, который назначили.
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.orderRepo.Assign(ctx, tx, orderID, executorID); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventOrderAccepted, order, &executorID)
	}); err != nil {
		// Смену открыли только ради этого заказа, а заказа не будет — например,
		// его успел взять другой исполнитель. Закрываем её тем же путём, что и
		// отработавшую до конца смену (без штрафа): иначе исполнитель остался бы
		// со сменой, которую не открывал и за досрочный выход из которой платит.
		if openedShift != nil {
			if endErr := s.shiftRepo.End(ctx, openedShift.ID); endErr != nil {
				log.Printf("[OrderService] failed to roll back auto-opened shift %s: %v", openedShift.ID, endErr)
			} else {
				metrics.ShiftEvent("auto_rolled_back")
			}
		}
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("заказ уже взят другим исполнителем")
		}
		return err
	}
	metrics.OrderEvent("accepted")
	return nil
}

// autoShiftOnAcceptEnabled сообщает, открывать ли смену за исполнителя, который
// берёт заказ без неё. Включено по умолчанию; выключение возвращает прежнее
// поведение — отказ с «executor has no active shift».
func (s *OrderService) autoShiftOnAcceptEnabled(ctx context.Context) bool {
	if s.settingsRepo == nil {
		return true
	}
	settings, err := s.settingsRepo.GetSettings(ctx)
	if err != nil {
		return true
	}
	v, ok := settings[SettingAutoShiftOnAcceptEnabled]
	if !ok {
		return true
	}
	return v != "0"
}

// autoShiftDurationHours возвращает длительность автоматически открываемой
// смены. Значение вне списка разрешённых игнорируется, а не создаёт смену,
// которую исполнитель не смог бы открыть сам.
func (s *OrderService) autoShiftDurationHours(ctx context.Context) int {
	hours := settingInt(ctx, s.settingsRepo, SettingAutoShiftDurationHours, defaultAutoShiftDurationHours)
	if !IsValidShiftDuration(hours) {
		return defaultAutoShiftDurationHours
	}
	return hours
}

// checkExecutorEligibility загружает исполнителя, вариант услуги и заказчика и
// применяет общий предикат видимости/принятия — тот же, что используют списки
// заказов, поэтому исполнитель может принять только то, что видит.
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
	return canViewOrTakeOrder(ctx, s.behaviors, viewer, customer, variant)
}

// settingsFloat читает числовую системную настройку со значением по умолчанию.
func (s *OrderService) settingsFloat(ctx context.Context, key string, defaultValue float64) float64 {
	return settingFloat(ctx, s.settingsRepo, key, defaultValue)
}

// RejectAssignedOrder позволяет исполнителю бросить назначенный заказ.
// Исполнителя штрафуют на долю стоимости заказа (см. reject_penalty_share), а
// заказ возвращается в пул поиска. Штраф и снятие назначения делят одну
// транзакцию, поэтому с исполнителя никогда не спишут за заказ, оставшийся за
// ним.
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

// ExecuteOrder помечает заказ как EXECUTED исполнителем и шлёт системное сообщение в чат.
func (s *OrderService) ExecuteOrder(ctx context.Context, orderID, executorID uuid.UUID) error {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusAssigned || order.ExecutorID == nil || *order.ExecutorID != executorID {
		return errors.New("order is not assigned to this executor")
	}

	// Отметка о выполненной работе — это то, что верифицирует заказчика в услуге
	// верификации, поэтому событие обязано быть таким же надёжным, как смена статуса.
	if err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := s.orderRepo.Execute(ctx, tx, orderID); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventOrderExecuted, order, &executorID)
	}); err != nil {
		return err
	}
	metrics.OrderEvent("executed")

	// Отправляем системное уведомление в чат
	if s.chatRepo != nil {
		chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
		if err == nil && chat != nil {
			_, _ = s.chatRepo.SaveMessage(ctx, chat.ID, executorID, "📦 Исполнитель отметил(а) выполнение заказа! Пожалуйста, подтвердите приемку работы.")
		}
	}

	return nil
}

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

// confirmTx — само подтверждение, внутри транзакции вызывающего. У него два
// вызывающих: заказчик, который подтверждает, и применитель поведений,
// закрывающий заказ, который скрипт объявил завершённым (скажем, состоявшуюся
// верификацию). Оба обязаны выплачивать ровно теми же шагами, поэтому копия у
// них одна.
func (s *OrderService) confirmTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	// Заказчик может одобрить и после того, как исполнитель пометил заказ
	// EXECUTED, и раньше, пока он ещё ASSIGNED, — раннее одобрение просто
	// закрывает заказ и платит исполнителю удержанную сумму, так же как путь
	// EXECUTED ниже.
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

	// Ставка платформы теперь персональная: уровень исполнителя снимает с неё
	// по проценту за уровень, до нуля. Уровень читается внутри этой же
	// транзакции, поэтому баллы, начисленные параллельно, не могут применить
	// себя к заказу задним числом.
	level := s.commissionLevel(ctx, tx, *order.ExecutorID)
	commission := commissionAt(finalAmount, level.Percent)

	// Эскроу держит по этому заказу ровно order.HoldAmount, и здесь он
	// опустошается полностью: возврат заказчику + комиссия + вознаграждение.
	// Распределение целиком делает реестр — он единственный, кому видно все три
	// части сразу и кто поэтому может проверить, что исполнителю не досталось
	// больше уплаченного заказчиком.
	if err := s.ledger.SettleOrder(ctx, tx, OrderSettlement{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		ExecutorID: *order.ExecutorID,
		Hold:       order.HoldAmount,
		Paid:       finalAmount,
		Commission: commission,
	}); err != nil {
		return err
	}

	if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
		return err
	}
	if err := s.orderRepo.Confirm(ctx, tx, orderID, finalAmount, isDowngraded); err != nil {
		return err
	}
	// Ставка и уровень сохраняются в заказе: без них через месяц никто не
	// объяснит, почему по двум одинаковым заказам разная комиссия.
	if err := s.orderRepo.SetCommission(ctx, tx, order.ID, level.Percent, level.Level); err != nil {
		return err
	}
	if err := s.recordCompletion(ctx, tx, order, finalAmount); err != nil {
		return err
	}
	return s.publishOrderEvent(ctx, tx, repository.EventOrderConfirmed, order, nil)
}

// commissionLevel читает уровень исполнителя внутри транзакции подтверждения.
// Без подключённых уровней это нулевой уровень, то есть базовая ставка.
func (s *OrderService) commissionLevel(ctx context.Context, tx *sql.Tx, executorID uuid.UUID) Level {
	if s.levels == nil {
		base := commissionPercent(s.loadSettings(ctx))
		return Level{BasePercent: base, Percent: base}
	}
	return s.levels.For(ctx, tx, executorID)
}

// recordCompletion пополняет агрегаты исполнителя в той же транзакции, что и
// подтверждение. Отдельным проходом их считать нельзя: агрегат, посчитанный
// позже, расходится с заказами ровно в тот момент, когда проход упал, — а по
// нему решают, выдать ли ачивку.
func (s *OrderService) recordCompletion(ctx context.Context, tx *sql.Tx, order *repository.Order, finalAmount money.Amount) error {
	if s.stats == nil || order.ExecutorID == nil {
		return nil
	}
	minutes := 0
	if !order.CreatedAt.IsZero() {
		minutes = int(time.Since(order.CreatedAt).Minutes())
	}
	return s.stats.RecordCompletion(ctx, tx, repository.CompletedOrder{
		ExecutorID: *order.ExecutorID,
		CustomerID: order.CustomerID,
		Minutes:    minutes,
		Earned:     finalAmount,
	})
}

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
	// ErrInsufficientFunds пробрасывается, чтобы обработчик отрисовал её тем же
	// «недостаточно средств» / 422, что и удержание по заказу.
	if err == nil {
		metrics.OrderEvent("tipped")
	}
	return err
}

// Confirm завершает заказ конкретного заказчика (псевдоним, совместимый с обработчиком).
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

// CancelOrder отменяет активный заказ и возвращает удержание ровно один раз.
// Возврат и смена статуса делят одну транзакцию и одну блокировку строки, а
// удержание обнуляется, поэтому повторная или параллельная отмена не выплатит снова.
func (s *OrderService) CancelOrder(ctx context.Context, orderID uuid.UUID) error {
	return s.cancel(ctx, orderID, repository.OrderStatusSearching, repository.OrderStatusAssigned)
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

// cancelTx — сама отмена, внутри транзакции вызывающего: тот же возврат,
// освобождение claim'а и событие, отменил ли заказчик, вымел ли воркер
// невостребованный аукцион или попросил скрипт поведения.
func (s *OrderService) cancelTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, allowed ...repository.OrderStatus) error {
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
	if err := s.orderRepo.Cancel(ctx, tx, orderID); err != nil {
		return err
	}
	// Отменённый заказ возвращает пользователю его единственную попытку. Без
	// этого заказчик, отменивший заказ верификации, никогда не смог бы заказать
	// другой, а значит, и никогда не верифицировался бы.
	if s.claimRepo != nil {
		variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID)
		if err == nil && s.behaviors.ReleasesClaimOnCancel(variant) {
			if err := s.claimRepo.ReleaseByOrder(ctx, tx, orderID); err != nil {
				return err
			}
		}
	}
	// Отмена засчитывается исполнителю, если он у заказа был: ачивки смотрят на
	// неё так же, как на выполнение, и агрегат должен меняться там же, где
	// меняется сам заказ.
	if s.stats != nil && order.ExecutorID != nil {
		if err := s.stats.RecordCancel(ctx, tx, *order.ExecutorID); err != nil {
			return err
		}
	}
	return s.publishOrderEvent(ctx, tx, repository.EventOrderCanceled, order, nil)
}

// Cancel отменяет заказ конкретного заказчика (псевдоним, совместимый с обработчиком).
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

// CreateConstructionOrder создаёт аукционный заказ на вывоз строительного мусора.
func (s *OrderService) CreateConstructionOrder(ctx context.Context, customerID uuid.UUID, photoURL, address, comment string, lat, lon *float64) (*repository.Order, error) {
	photoURL = strings.TrimSpace(photoURL)
	if photoURL == "" {
		return nil, errors.New("photo URL is required")
	}
	// Принимается только путь, порождённый нашим собственным эндпоинтом загрузки.
	// Значение раньше сохранялось дословно и рисовалось в админ-панели, поэтому
	// произвольный URL там — это чужой контент на нашей странице.
	if !strings.HasPrefix(photoURL, "/uploads/") || strings.Contains(photoURL, "..") {
		return nil, errors.New("photo must be uploaded through the app")
	}

	// GetNodeByCode видит только живые узлы, поэтому списанный строительный
	// вариант читается как отсутствующий, а не как ошибка базы.
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

	// Та же проверка верификации заказчика, что и на пути обычного заказа, — на
	// случай, если строительный вариант помечен requires_verification.
	if s.userRepo != nil {
		customer, err := s.userRepo.FindByID(ctx, customerID)
		if err != nil {
			return nil, err
		}
		if err := canCustomerOrderVariant(ctx, s.behaviors, customer, variant); err != nil {
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
		// От клиента координат нет (старая сборка или набранная строка):
		// разрешаем их один раз здесь, чтобы заказ можно было подобрать. Выбранная
		// подсказка несёт свои и в эту ветку не попадает.
		if geo, err := s.resolver.Resolve(ctx, address); err == nil {
			order.PickupLat = &geo.Lat
			order.PickupLon = &geo.Lon
		}
	}

	if err := s.orderRepo.Create(ctx, nil, order); err != nil {
		return nil, err
	}

	// Создаём чат-комнату для нового заказа. Неудача не фатальна.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(ctx, order.ID); err != nil {
			log.Printf("[OrderService] failed to create chat for order %s: %v", order.ID, err)
		}
	}

	metrics.OrderEvent("created_auction")
	s.hydrateServiceVariant(ctx, order)
	return order, nil
}

// GetAvailableConstructionOrders возвращает открытые заказы на вывоз строительного мусора.
func (s *OrderService) GetAvailableConstructionOrders(ctx context.Context) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetAvailableAuctionOrders(ctx)
	if err != nil {
		return nil, err
	}
	s.hydrateServiceVariants(ctx, orders)
	return orders, nil
}

// GetAvailableConstructionOrdersForExecutor возвращает открытые строительные заказы, отфильтрованные для исполнителя.
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

	// Варианты, исполнители и заказчики, которых осматривает фильтр ниже, — всё за
	// фиксированное число запросов, а не по набору на заказ.
	s.hydrateServiceVariants(ctx, orders)
	customers := s.customersOf(ctx, orders)

	filtered := []*repository.Order{}
	for _, o := range orders {
		// 1. Фильтр: заказчик ОБЯЗАН быть верифицирован («показ заказов только от верифицированных пользователей»)
		if customer := customers[o.CustomerID]; customer != nil {
			if !customer.IsVerified() {
				continue
			}
		}

		// 2. Фильтр: если вариант услуги требует верификации, исполнитель должен быть верифицирован
		if o.ServiceVariant != nil {
			if o.ServiceVariant.RequiresVerification && !executorVerified {
				continue
			}
			// 3. Фильтр: если у варианта услуги есть возрастное ограничение (min_age > 0), возраст исполнителя должен быть >= min_age
			if o.ServiceVariant.MinAge > 0 && executorAge < o.ServiceVariant.MinAge {
				continue
			}
		}

		filtered = append(filtered, o)
	}

	return filtered, nil
}

// FindNearbyOrders возвращает обычные/крупные заказы в поиске рядом с заданными координатами в пределах radiusMeters.
func (s *OrderService) FindNearbyOrders(ctx context.Context, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	orders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}
	s.hydrateServiceVariants(ctx, orders)
	return orders, nil
}

// FindNearbyOrdersForExecutor возвращает обычные/крупные заказы в поиске рядом с координатами, отфильтрованные для исполнителя.
func (s *OrderService) FindNearbyOrdersForExecutor(ctx context.Context, executorID uuid.UUID, lat, lon float64, radiusMeters int) ([]*repository.Order, error) {
	// Привязываем поиск к авторитетной сохранённой позиции исполнителя — той же
	// точке, что используют карта и проверка радиуса принятия. Координаты клиента
	// (GPS устройства, который может отсутствовать или падать в базовую точку) —
	// лишь запасной вариант, когда хранилище не подключено, и это не даёт списку
	// разойтись с тем, что исполнитель реально может принять.
	if s.executorGeoRepo != nil {
		storedLat, storedLon, _, err := s.executorGeoRepo.GetExecutorLocation(ctx, executorID)
		if err != nil {
			return nil, err
		}
		if storedLat == nil || storedLon == nil {
			// Рабочая позиция ещё не задана: принять нечего, поэтому и в списке ничего нет.
			return []*repository.Order{}, nil
		}
		lat, lon = *storedLat, *storedLon
	}

	// Что смотрящему можно видеть, решают его набор ролей и верификация; роли
	// грузятся вместе с пользователем, поэтому модератор видит и заказы для модераторов.
	viewer, _ := s.userRepo.FindByID(ctx, executorID)

	orders, err := s.orderRepo.FindNearbyOrders(ctx, lat, lon, radiusMeters)
	if err != nil {
		return nil, err
	}

	s.hydrateServiceVariants(ctx, orders)
	customers := s.customersOf(ctx, orders)

	filtered := []*repository.Order{}
	for _, o := range orders {
		// Один предикат и для карты, и для этого списка, и тот же, что применяет
		// путь принятия: заказы только для модераторов идут модераторам; обычные
		// заказы следуют сегментации по верификации заказчика и стандартным
		// проверкам исполнителя (requires_verification, min_age, бан).
		if canViewOrTakeOrder(ctx, s.behaviors, viewer, customers[o.CustomerID], o.ServiceVariant) != nil {
			continue
		}

		filtered = append(filtered, o)
	}

	return filtered, nil
}

// customersOf пакетно загружает заказчиков, разместивших данные заказы, — для
// фильтров списка, которые смотрят на состояние верификации заказчика.
//
// Неудачная загрузка даёт пустую карту, которую вызывающие читают как «нет
// сведений о заказчике» — то же, что раньше давало неудачное чтение по одному
// заказу, и то прочтение, которое правила допуска уже умеют обрабатывать.
func (s *OrderService) customersOf(ctx context.Context, orders []*repository.Order) map[uuid.UUID]*repository.User {
	if s.userRepo == nil || len(orders) == 0 {
		return map[uuid.UUID]*repository.User{}
	}
	ids := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		if o != nil {
			ids = append(ids, o.CustomerID)
		}
	}
	loaded, err := s.userRepo.FindByIDs(ctx, ids)
	if err != nil {
		return map[uuid.UUID]*repository.User{}
	}
	return loaded
}

// ListAssigned возвращает заказы, назначенные исполнителю.
func (s *OrderService) ListAssigned(ctx context.Context, executorID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetExecutorAssignedOrders(ctx, executorID)
	if err != nil {
		return nil, err
	}
	s.hydrateServiceVariants(ctx, orders)
	return orders, nil
}

// ListByCustomer возвращает заказы, созданные заказчиком.
func (s *OrderService) ListByCustomer(ctx context.Context, customerID uuid.UUID) ([]*repository.Order, error) {
	orders, err := s.orderRepo.GetCustomerOrders(ctx, customerID)
	if err != nil {
		return nil, err
	}
	s.hydrateServiceVariants(ctx, orders)
	return orders, nil
}

// loadOrderCategories возвращает родительские категории вариантов одним
// запросом на список, а не одним на заказ. Подпись «категория / услуга» нужна
// на каждом экране заказов, поэтому загрузка живёт в одном месте.
func loadOrderCategories(
	ctx context.Context,
	catalogRepo repository.ServiceCatalogRepository,
	variants map[uuid.UUID]*repository.ServiceNode,
) map[uuid.UUID]*repository.ServiceNode {
	if catalogRepo == nil || len(variants) == 0 {
		return nil
	}
	parentIDs := make([]uuid.UUID, 0, len(variants))
	for _, v := range variants {
		if v != nil && v.ParentID != nil {
			parentIDs = append(parentIDs, *v.ParentID)
		}
	}
	if len(parentIDs) == 0 {
		return nil
	}
	loaded, err := catalogRepo.GetNodesByIDs(ctx, parentIDs)
	if err != nil {
		return nil
	}
	return loaded
}

// categoryOf находит категорию варианта в уже загруженной пачке.
func categoryOf(variant *repository.ServiceNode, categories map[uuid.UUID]*repository.ServiceNode) *repository.ServiceNode {
	if variant == nil || variant.ParentID == nil || categories == nil {
		return nil
	}
	return categories[*variant.ParentID]
}
