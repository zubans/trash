package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Настройки магазина.
const (
	// SettingShopEnabled — выключатель: 0 прячет витрину и отклоняет покупки,
	// админка остаётся доступной.
	SettingShopEnabled = "shop_enabled"
	// SettingShopOfferVersion — текущая редакция оферты. Покупка, принятая по
	// другой, отклоняется с offer_changed.
	SettingShopOfferVersion = "shop_offer_version"
)

// defaultMaxQueuedPerks — сколько привилегий можно купить вперёд, когда товар
// не задал своего лимита (implementation_plan_shop.md §3.4).
const defaultMaxQueuedPerks = 3

// commissionLookback — окно, по которому витрина считает, сколько комиссии
// исполнитель платит: «за прошлый месяц вы заплатили…».
const commissionLookback = 30 * 24 * time.Hour

// ShopError — отказ магазина с кодом, который клиент переводит в текст
// (implementation_plan_shop.md §7). Сообщение — запасной текст для клиента,
// который код не знает.
type ShopError struct {
	Status  int                    `json:"-"`
	Code    string                 `json:"error"`
	Message string                 `json:"message"`
	Fields  map[string]string      `json:"fields,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (e *ShopError) Error() string { return e.Code + ": " + e.Message }

func shopErr(status int, code, message string) *ShopError {
	return &ShopError{Status: status, Code: code, Message: message}
}

// Коды отказов. Покупатель видит их переведёнными, поэтому менять код —
// значит менять ключ перевода на клиенте.
const (
	ShopErrShopDisabled         = "shop_disabled"
	ShopErrNotFound             = "not_found"
	ShopErrProductUnavailable   = "product_unavailable"
	ShopErrVerificationRequired = "verification_required"
	ShopErrAccountBlocked       = "account_blocked"
	ShopErrOfferChanged         = "offer_changed"
	ShopErrPriceChanged         = "price_changed"
	ShopErrOutOfStock           = "out_of_stock"
	ShopErrLimitReached         = "limit_reached"
	ShopErrPerkUseless          = "perk_useless"
	ShopErrInsufficientFunds    = "insufficient_funds"
	ShopErrInvalidRequest       = "invalid_request"
	ShopErrValidation           = "validation"
	ShopErrInvalidTransition    = "invalid_transition"
	ShopErrAlreadyCanceled      = "already_canceled"
	ShopErrCertificateRevealed  = "certificate_revealed"
	ShopErrInsufficientRevenue  = "insufficient_revenue"
)

func shopNotFound() *ShopError {
	return shopErr(http.StatusNotFound, ShopErrNotFound, "Не найдено")
}

func shopValidation(fields map[string]string) *ShopError {
	return &ShopError{Status: http.StatusUnprocessableEntity, Code: ShopErrValidation,
		Message: "Проверьте поля формы", Fields: fields}
}

// ShopService — магазин: витрина, покупка и её обработка.
//
// Всё, что двигает деньги, идёт через Ledger, а выдача — через те же подарки,
// что и у ачивок: склад один (implementation_plan_shop.md §2.1).
type ShopService struct {
	shop     repository.ShopRepository
	orders   repository.ShopOrderRepository
	perks    repository.PerkRepository
	gifts    repository.GiftRepository
	ledger   *Ledger
	levels   *Levels
	settings repository.SettingsRepository
	events   repository.EventRepository
	mail     repository.MailRepository
	roles    repository.RoleRepository
	now      func() time.Time
}

// NewShopService собирает магазин.
func NewShopService(shop repository.ShopRepository, orders repository.ShopOrderRepository,
	perks repository.PerkRepository, gifts repository.GiftRepository, ledger *Ledger,
	levels *Levels, settings repository.SettingsRepository) *ShopService {
	return &ShopService{shop: shop, orders: orders, perks: perks, gifts: gifts, ledger: ledger,
		levels: levels, settings: settings, now: time.Now}
}

// WithEvents подключает outbox: покупка публикует shop.purchased.
func (s *ShopService) WithEvents(events repository.EventRepository) *ShopService {
	s.events = events
	return s
}

// WithMail подключает внутреннюю почту: письма об оплате и смене статуса.
func (s *ShopService) WithMail(mail repository.MailRepository) *ShopService {
	s.mail = mail
	return s
}

// WithRoles подключает справочник ролей: товар нельзя открыть роли, которой
// нет.
func (s *ShopService) WithRoles(roles repository.RoleRepository) *ShopService {
	s.roles = roles
	return s
}

// Enabled сообщает, открыт ли магазин.
func (s *ShopService) Enabled(ctx context.Context) bool {
	return settingFloat(ctx, s.settings, SettingShopEnabled, 0) == 1
}

// OfferVersion — текущая редакция оферты.
func (s *ShopService) OfferVersion(ctx context.Context) int {
	return int(settingFloat(ctx, s.settings, SettingShopOfferVersion, 1))
}

func productVisibleTo(p *repository.ShopProduct, user *repository.User) bool {
	if len(p.Roles) == 0 {
		return true
	}
	for _, have := range userRoles(user) {
		for _, want := range p.Roles {
			if have == want {
				return true
			}
		}
	}
	return false
}

// forBuyer убирает то, что покупателю знать не нужно: точный остаток склада.
func forBuyer(p *repository.ShopProduct) *repository.ShopProduct {
	copy := *p
	copy.StockCount = nil
	return &copy
}

// Storefront — витрина.
type Storefront struct {
	Enabled      bool                      `json:"enabled"`
	OfferVersion int                       `json:"offer_version"`
	Products     []*repository.ShopProduct `json:"products"`
}

// Storefront возвращает активные товары, открытые ролям пользователя. При
// выключенном магазине витрина пуста, а флаг говорит клиенту спрятать пункт
// меню.
func (s *ShopService) Storefront(ctx context.Context, user *repository.User, category string) (*Storefront, error) {
	out := &Storefront{Enabled: s.Enabled(ctx), OfferVersion: s.OfferVersion(ctx),
		Products: []*repository.ShopProduct{}}
	if !out.Enabled {
		return out, nil
	}
	filter := repository.ShopProductFilter{ActiveOnly: true, Roles: userRoles(user)}
	if category != "" {
		filter.Category = &category
	}
	products, err := s.shop.ListProducts(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, p := range products {
		out.Products = append(out.Products, forBuyer(p))
	}
	return out, nil
}

// PerkQuote — «сейчас / с привилегией / окупаемость» для карточки
// привилегии (implementation_plan_shop.md §3.6). Считает сервер по той же
// формуле, что и подтверждение заказа, а не текст на карточке.
type PerkQuote struct {
	BasePercent float64 `json:"base_percent"`
	// LevelPercent — ставка по уровню, без привилегий: от неё считается
	// обещание «вдвое меньше».
	LevelPercent float64 `json:"level_percent"`
	// CurrentPercent — ставка прямо сейчас, с уже действующей привилегией.
	CurrentPercent  float64 `json:"current_percent"`
	PercentWithPerk float64 `json:"percent_with_perk"`
	// CommissionPaid — сколько комиссии заплачено за последние 30 дней.
	CommissionPaid money.Amount `json:"commission_paid"`
	// Savings — сколько сэкономила бы привилегия: за те же 30 дней для
	// множителя и пунктов, за свой срок от средней дневной комиссии — для
	// беспроцентного периода.
	Savings money.Amount `json:"savings"`
	// BreakevenTurnover — оборот за срок, при котором экономия покрывает цену.
	// nil, когда привилегия ставку не снижает.
	BreakevenTurnover *money.Amount `json:"breakeven_turnover,omitempty"`
	StartsAt          time.Time     `json:"starts_at"`
	ExpiresAt         time.Time     `json:"expires_at"`
	// Queued — привилегия начнёт действовать не сразу, а после очереди.
	Queued      bool `json:"queued"`
	QueueLength int  `json:"queue_length"`
	MaxQueued   int  `json:"max_queued"`
	// Useless — ставка по уровню уже 0: продавать половину от нуля нельзя.
	Useless bool `json:"useless"`
}

// ProductCard — карточка товара для покупателя.
type ProductCard struct {
	Product      *repository.ShopProduct `json:"product"`
	PerkQuote    *PerkQuote              `json:"perk_quote,omitempty"`
	OfferVersion int                     `json:"offer_version"`
	Purchased    int                     `json:"purchased"`
}

// Product возвращает карточку товара. Товар чужой роли — 404, а не 403: его
// существование не раскрывается.
func (s *ShopService) Product(ctx context.Context, user *repository.User, id uuid.UUID) (*ProductCard, error) {
	if !s.Enabled(ctx) {
		return nil, shopErr(http.StatusForbidden, ShopErrShopDisabled, "Магазин сейчас закрыт")
	}
	p, err := s.shop.GetProduct(ctx, id)
	if errors.Is(err, repository.ErrShopProductNotFound) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	if !p.IsActive || !productVisibleTo(p, user) {
		return nil, shopNotFound()
	}
	card := &ProductCard{Product: forBuyer(p), OfferVersion: s.OfferVersion(ctx)}
	if card.Purchased, err = s.orders.CountForUserProduct(ctx, nil, user.ID, p.ID); err != nil {
		return nil, err
	}
	if p.Kind == repository.ShopKindPerk {
		quote, err := s.perkQuote(ctx, user.ID, p)
		if err != nil {
			return nil, err
		}
		card.PerkQuote = quote
	}
	return card, nil
}

func maxQueued(p *repository.ShopProduct) int {
	if p.MaxActivePerUser != nil && *p.MaxActivePerUser > 0 {
		return *p.MaxActivePerUser
	}
	return defaultMaxQueuedPerks
}

func perkDays(p *repository.ShopProduct) int {
	if p.PerkDays == nil {
		return 0
	}
	return *p.PerkDays
}

func (s *ShopService) perkQuote(ctx context.Context, userID uuid.UUID, p *repository.ShopProduct) (*PerkQuote, error) {
	now := s.now()
	level := s.levels.For(ctx, nil, userID)
	kind := ""
	if p.PerkKind != nil {
		kind = *p.PerkKind
	}
	withPerk, ok := ApplyPerk(level.LevelPercent, level.BasePercent, kind, p.PerkValue)
	if !ok {
		withPerk = level.LevelPercent
	}
	days := perkDays(p)

	quote := &PerkQuote{
		BasePercent: level.BasePercent, LevelPercent: level.LevelPercent,
		CurrentPercent: level.Percent, PercentWithPerk: withPerk,
		MaxQueued: maxQueued(p), Useless: level.LevelPercent <= 0,
	}
	paid, err := s.orders.CommissionPaidSince(ctx, userID, now.Add(-commissionLookback))
	if err != nil {
		return nil, err
	}
	quote.CommissionPaid = paid

	reduction := level.LevelPercent - withPerk
	if level.LevelPercent > 0 && reduction > 0 {
		if kind == PerkKindCommissionFree {
			// Беспроцентный период — прямой отказ от выручки за срок, поэтому
			// экономия считается от средней дневной комиссии, а не от месяца.
			quote.Savings = paid.Scale(float64(days) / 30)
		} else {
			quote.Savings = paid.Scale(reduction / level.LevelPercent)
		}
		// Оборот, при котором экономия за срок покрывает цену:
		// цена / (снижение ставки в долях).
		breakeven := p.Price.Scale(100 / reduction)
		quote.BreakevenTurnover = &breakeven
	}

	start, err := s.perks.NextStart(ctx, nil, userID, CommissionPerkKinds, now)
	if err != nil {
		return nil, err
	}
	quote.StartsAt = start
	quote.ExpiresAt = start.AddDate(0, 0, days)
	quote.Queued = start.After(now)
	if quote.QueueLength, err = s.perks.CountQueued(ctx, nil, userID, CommissionPerkKinds, now); err != nil {
		return nil, err
	}
	return quote, nil
}

// PickupPoints — пункты выдачи, которые можно выбрать при оформлении. У
// закрытого магазина их нет, как нет и витрины.
func (s *ShopService) PickupPoints(ctx context.Context) ([]*repository.ShopPickupPoint, error) {
	if !s.Enabled(ctx) {
		return []*repository.ShopPickupPoint{}, nil
	}
	return s.shop.ListPickupPoints(ctx, true)
}

// PurchaseRequest — запрос покупки (implementation_plan_shop.md §7).
type PurchaseRequest struct {
	ProductID uuid.UUID `json:"product_id"`
	// RequestID создаётся клиентом при открытии окна оформления и переживает
	// повторное нажатие и обрыв сети: по нему повтор находит ту же покупку.
	RequestID     uuid.UUID    `json:"request_id"`
	ExpectedPrice money.Amount `json:"expected_price"`
	OfferVersion  int          `json:"offer_version"`
	Quantity      int          `json:"quantity"`
	Variant       string       `json:"variant"`
	Fulfillment   struct {
		Method        string    `json:"method"`
		PickupPointID uuid.UUID `json:"pickup_point_id"`
		Address       string    `json:"address"`
		Recipient     string    `json:"recipient"`
		Phone         string    `json:"phone"`
	} `json:"fulfillment"`
}

// Purchase покупает товар одной транзакцией (implementation_plan_shop.md §4.2):
// блокировка товара, проверки, запись покупки, списание, выдача, событие и
// письмо. Любой отказ после списания откатывает всё: взять деньги и не выдать
// купленное нельзя.
func (s *ShopService) Purchase(ctx context.Context, user *repository.User, req PurchaseRequest) (*repository.ShopOrder, error) {
	if !s.Enabled(ctx) {
		return nil, shopErr(http.StatusForbidden, ShopErrShopDisabled, "Магазин сейчас закрыт")
	}
	// Мягкий бан закрыт маршрутом (middleware/soft_ban.go), но проверка
	// здесь не даёт открыть покупку, если маршрут когда-нибудь добавят в
	// разрешённые по ошибке: трата — ровно то, чего заблокированному нельзя.
	if user.Status == "BANNED" || user.Status == "SOFT_BANNED" {
		return nil, shopErr(http.StatusForbidden, ShopErrAccountBlocked, "Аккаунт заблокирован")
	}
	if req.RequestID == uuid.Nil || req.ProductID == uuid.Nil {
		return nil, shopErr(http.StatusBadRequest, ShopErrInvalidRequest, "Не указан товар или идентификатор запроса")
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	var result *repository.ShopOrder
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		// Повтор того же запроса — вернуть уже созданную покупку. Проверка до
		// блокировки товара: повтор не должен ждать чужих покупок.
		if existing, err := s.orders.GetByRequest(ctx, tx, user.ID, req.RequestID); err == nil {
			result = existing
			return nil
		} else if !errors.Is(err, repository.ErrShopOrderNotFound) {
			return err
		}

		product, err := s.shop.LockProduct(ctx, tx, req.ProductID)
		if errors.Is(err, repository.ErrShopProductNotFound) {
			return shopNotFound()
		}
		if err != nil {
			return err
		}
		if !productVisibleTo(product, user) {
			return shopNotFound()
		}
		if !product.IsActive {
			return shopErr(http.StatusConflict, ShopErrProductUnavailable, "Товар снят с продажи")
		}
		if product.RequiresVerified && !user.IsVerified() {
			return shopErr(http.StatusForbidden, ShopErrVerificationRequired, "Товар доступен только верифицированным пользователям")
		}
		if version := s.OfferVersion(ctx); req.OfferVersion != version {
			e := shopErr(http.StatusConflict, ShopErrOfferChanged, "Условия оферты изменились")
			e.Details = map[string]interface{}{"offer_version": version}
			return e
		}
		if req.ExpectedPrice != product.Price {
			e := shopErr(http.StatusConflict, ShopErrPriceChanged, "Цена изменилась")
			e.Details = map[string]interface{}{"price": product.Price}
			return e
		}
		if product.Kind != repository.ShopKindPhysical && req.Quantity != 1 {
			return shopErr(http.StatusBadRequest, ShopErrInvalidRequest, "Этот товар покупается по одному")
		}
		if req.Quantity > product.MaxQtyPerOrder {
			return shopErr(http.StatusBadRequest, ShopErrInvalidRequest,
				fmt.Sprintf("Не больше %d шт. в одной покупке", product.MaxQtyPerOrder))
		}
		if product.PerUserLimit != nil {
			bought, err := s.orders.CountForUserProduct(ctx, tx, user.ID, product.ID)
			if err != nil {
				return err
			}
			if bought >= *product.PerUserLimit {
				return shopErr(http.StatusConflict, ShopErrLimitReached, "Лимит покупок этого товара исчерпан")
			}
		}

		order := &repository.ShopOrder{
			UserID: user.ID, RequestID: req.RequestID, ProductID: product.ID,
			ProductSnapshot: productSnapshot(product), Quantity: req.Quantity,
			UnitPrice: product.Price, Total: money.Amount(int64(product.Price) * int64(req.Quantity)),
			Status: repository.ShopOrderCompleted, OfferVersion: req.OfferVersion,
		}

		var perk *repository.UserPerk
		switch product.Kind {
		case repository.ShopKindPerk:
			if perk, err = s.preparePerk(ctx, tx, user.ID, product); err != nil {
				return err
			}
		case repository.ShopKindPhysical:
			fulfillment, variant, err := s.physicalFulfillment(ctx, product, req)
			if err != nil {
				return err
			}
			order.Fulfillment, order.Variant = fulfillment, variant
			order.Status = repository.ShopOrderPaid
		}

		created, err := s.orders.Create(ctx, tx, order)
		if err != nil {
			return err
		}
		if !created {
			// Такой же запрос пришёл параллельно и уже закоммитился: его покупка
			// и есть ответ, второго списания не будет.
			result, err = s.orders.GetByRequest(ctx, tx, user.ID, req.RequestID)
			return err
		}

		if err := s.ledger.ShopCharge(ctx, tx, user.ID, order.Total, order.ID); err != nil {
			if errors.Is(err, repository.ErrInsufficientFunds) {
				return shopErr(http.StatusUnprocessableEntity, ShopErrInsufficientFunds, "Недостаточно средств на балансе")
			}
			return err
		}

		if err := s.deliver(ctx, tx, user.ID, product, order, perk); err != nil {
			return err
		}
		if err := s.publishPurchase(ctx, tx, user.ID, product, order); err != nil {
			return err
		}
		// Письмо — в той же транзакции: откаченная покупка не должна оставить
		// письма «оплачено».
		s.notify(ctx, tx, user.ID, order, purchaseMailBody(order, perk))
		result = order
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[shop] user %s bought %s: order №%d, %s", user.ID, result.ProductID, result.Number, result.Total)
	return s.withDetails(ctx, result, false)
}

// preparePerk проверяет привилегию и ставит её в очередь
// (implementation_plan_shop.md §3.4, §3.6). Строка пользователя блокируется
// до конца транзакции, чтобы две покупки подряд не начались в один момент.
func (s *ShopService) preparePerk(ctx context.Context, tx *sql.Tx, userID uuid.UUID, product *repository.ShopProduct) (*repository.UserPerk, error) {
	if err := s.perks.LockQueue(ctx, tx, userID); err != nil {
		return nil, err
	}
	kind := ""
	if product.PerkKind != nil {
		kind = *product.PerkKind
	}
	if err := ValidatePerk(kind, product.PerkValue, perkDays(product)); err != nil {
		return nil, shopErr(http.StatusConflict, ShopErrProductUnavailable, "Товар настроен неверно и не продаётся")
	}
	// Проверяется ставка по уровню, без привилегий: во время беспроцентной
	// недели ставка тоже 0, но купить следующую в очередь можно.
	if level := s.levels.For(ctx, tx, userID); level.LevelPercent <= 0 {
		return nil, shopErr(http.StatusConflict, ShopErrPerkUseless,
			"Ваша комиссия уже 0 %: привилегия ничего не изменит")
	}
	now := s.now()
	queued, err := s.perks.CountQueued(ctx, tx, userID, CommissionPerkKinds, now)
	if err != nil {
		return nil, err
	}
	if queued >= maxQueued(product) {
		return nil, shopErr(http.StatusConflict, ShopErrLimitReached,
			"Куплено вперёд максимальное число привилегий")
	}
	start, err := s.perks.NextStart(ctx, tx, userID, CommissionPerkKinds, now)
	if err != nil {
		return nil, err
	}
	return &repository.UserPerk{
		UserID: userID, Kind: kind, Value: product.PerkValue,
		StartsAt: start, ExpiresAt: start.AddDate(0, 0, perkDays(product)),
	}, nil
}

// physicalFulfillment проверяет выбор покупателя для вещи: вариант, способ
// получения и его данные.
func (s *ShopService) physicalFulfillment(ctx context.Context, product *repository.ShopProduct, req PurchaseRequest) (map[string]interface{}, *string, error) {
	fields := map[string]string{}
	var variant *string
	if len(product.Variants) > 0 {
		found := false
		for _, v := range product.Variants {
			if v.Code == req.Variant {
				found = true
				break
			}
		}
		if !found {
			fields["variant"] = "Выберите вариант"
		} else {
			value := req.Variant
			variant = &value
		}
	}
	method := req.Fulfillment.Method
	allowed := false
	for _, m := range product.FulfillmentMethods {
		if m == method {
			allowed = true
		}
	}
	out := map[string]interface{}{"method": method}
	if variant != nil {
		out["variant"] = *variant
	}
	switch {
	case !allowed:
		fields["method"] = "Выберите способ получения"
	case method == repository.ShopFulfillmentPickup:
		points, err := s.shop.ListPickupPoints(ctx, true)
		if err != nil {
			return nil, nil, err
		}
		var point *repository.ShopPickupPoint
		for _, p := range points {
			if p.ID == req.Fulfillment.PickupPointID {
				point = p
			}
		}
		if point == nil {
			fields["pickup_point_id"] = "Выберите пункт выдачи"
		} else {
			out["pickup_point_id"] = point.ID.String()
			out["pickup_point"] = map[string]interface{}{"title": point.Title, "address": point.Address, "hours": point.Hours}
		}
	case method == repository.ShopFulfillmentDelivery:
		address := strings.TrimSpace(req.Fulfillment.Address)
		recipient := strings.TrimSpace(req.Fulfillment.Recipient)
		phone := strings.TrimSpace(req.Fulfillment.Phone)
		if address == "" {
			fields["address"] = "Укажите адрес доставки"
		}
		if recipient == "" {
			fields["recipient"] = "Укажите получателя"
		}
		if phone == "" {
			fields["phone"] = "Укажите телефон получателя"
		}
		out["address"], out["recipient"], out["phone"] = address, recipient, phone
	}
	if len(fields) > 0 {
		return nil, nil, shopValidation(fields)
	}
	return out, variant, nil
}

// deliver выдаёт купленное: привилегию — строкой в очереди, вещь и
// сертификат — купоном на каждую единицу.
func (s *ShopService) deliver(ctx context.Context, tx *sql.Tx, userID uuid.UUID, product *repository.ShopProduct,
	order *repository.ShopOrder, perk *repository.UserPerk) error {
	if perk != nil {
		perk.ShopOrderID = &order.ID
		if err := s.perks.Create(ctx, tx, perk); err != nil {
			return err
		}
		order.Perks = []*repository.UserPerk{perk}
		return nil
	}
	if product.GiftCode == nil {
		return shopErr(http.StatusConflict, ShopErrProductUnavailable, "Товар настроен неверно и не продаётся")
	}
	gift, err := s.gifts.Get(ctx, *product.GiftCode)
	if err != nil {
		return err
	}
	for i := 0; i < order.Quantity; i++ {
		coupon, err := s.gifts.IssueForShop(ctx, tx, gift, userID, order.ID, order.Fulfillment)
		if errors.Is(err, repository.ErrGiftUnavailable) {
			// В отличие от ачивки, пустой склад здесь — отказ всей покупки.
			return shopErr(http.StatusConflict, ShopErrOutOfStock, "Товар закончился")
		}
		if err != nil {
			return err
		}
		order.Coupons = append(order.Coupons, coupon)
	}
	return nil
}

func productSnapshot(p *repository.ShopProduct) map[string]interface{} {
	snapshot := map[string]interface{}{
		"title": p.Title, "kind": p.Kind, "category": p.Category,
		"price": p.Price,
	}
	if len(p.Images) > 0 {
		snapshot["image"] = p.Images[0]
	}
	if p.GiftCode != nil {
		snapshot["gift_code"] = *p.GiftCode
	}
	if p.PerkKind != nil {
		snapshot["perk_kind"] = *p.PerkKind
	}
	if p.PerkValue != nil {
		snapshot["perk_value"] = *p.PerkValue
	}
	if p.PerkDays != nil {
		snapshot["perk_days"] = *p.PerkDays
	}
	return snapshot
}

func (s *ShopService) publishPurchase(ctx context.Context, tx *sql.Tx, userID uuid.UUID, product *repository.ShopProduct, order *repository.ShopOrder) error {
	if s.events == nil {
		return nil
	}
	actor := userID
	return s.events.Publish(ctx, tx, &repository.DomainEvent{
		Type: repository.EventShopPurchased, SubjectType: repository.EventSubjectUser, SubjectID: userID,
		ActorID: &actor,
		Payload: map[string]interface{}{
			"shop_order_id": order.ID.String(), "number": order.Number,
			"product_id": product.ID.String(), "kind": product.Kind,
			"quantity": order.Quantity, "total": order.Total.Rubles(),
		},
	})
}

// productTitle — русское название из снимка покупки.
func productTitle(order *repository.ShopOrder) string {
	if title, ok := order.ProductSnapshot["title"].(map[string]interface{}); ok {
		if ru, ok := title["ru"].(string); ok && ru != "" {
			return ru
		}
	}
	return "товар"
}

func purchaseMailBody(order *repository.ShopOrder, perk *repository.UserPerk) string {
	body := fmt.Sprintf("Заказ №%d оплачен: %s, %s.", order.Number, productTitle(order), order.Total)
	if perk != nil {
		body += fmt.Sprintf(" Привилегия действует с %s по %s.",
			perk.StartsAt.Format("02.01.2006"), perk.ExpiresAt.Format("02.01.2006"))
	}
	if order.Status == repository.ShopOrderPaid {
		body += " Мы сообщим, когда заказ будет готов к выдаче или отправлен."
	}
	return body
}

// notify кладёт письмо о покупке во внутреннюю почту. Сбой письма не отменяет
// покупку: письмо — уведомление, а не часть сделки.
func (s *ShopService) notify(ctx context.Context, q repository.Querier, userID uuid.UUID, order *repository.ShopOrder, body string) {
	if s.mail == nil {
		return
	}
	if err := s.mail.Send(ctx, q, &repository.Mail{
		UserID: userID, Kind: repository.MailKindShop,
		Subject: fmt.Sprintf("Заказ №%d", order.Number), Body: body,
		RefType: "shop_order", RefID: order.ID.String(),
	}); err != nil {
		log.Printf("[shop] cannot mail user %s about order №%d: %v", userID, order.Number, err)
	}
}

// withDetails подцепляет к покупке купоны и привилегии, а для админки — ещё
// проводки, чат поддержки и обращение о возврате.
func (s *ShopService) withDetails(ctx context.Context, order *repository.ShopOrder, admin bool) (*repository.ShopOrder, error) {
	coupons, err := s.gifts.ListByShopOrder(ctx, nil, order.ID)
	if err != nil {
		return nil, err
	}
	order.Coupons = coupons
	perks, err := s.perks.ListByShopOrder(ctx, nil, order.ID)
	if err != nil {
		return nil, err
	}
	order.Perks = perks
	if !admin {
		return order, nil
	}
	if order.UserPhone, order.UserName, err = s.orders.Buyer(ctx, order.UserID); err != nil {
		return nil, err
	}
	if order.Transactions, err = s.orders.Transactions(ctx, order.ID); err != nil {
		return nil, err
	}
	if order.SupportChatID, err = s.orders.SupportChatID(ctx, order.UserID); err != nil {
		return nil, err
	}
	if order.RefundRequestAt, err = s.orders.RefundRequestAt(ctx, order.ID); err != nil {
		return nil, err
	}
	return order, nil
}

// MyOrders — покупки пользователя, свежие первыми.
func (s *ShopService) MyOrders(ctx context.Context, user *repository.User) ([]*repository.ShopOrder, error) {
	orders, err := s.orders.ListForUser(ctx, user.ID, 100)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		if _, err := s.withDetails(ctx, o, false); err != nil {
			return nil, err
		}
	}
	return orders, nil
}

// MyOrder — одна покупка. Чужая не находится вовсе.
func (s *ShopService) MyOrder(ctx context.Context, user *repository.User, id uuid.UUID) (*repository.ShopOrder, error) {
	order, err := s.orders.Get(ctx, nil, id)
	if errors.Is(err, repository.ErrShopOrderNotFound) || (err == nil && order.UserID != user.ID) {
		return nil, shopNotFound()
	}
	if err != nil {
		return nil, err
	}
	return s.withDetails(ctx, order, false)
}

// MyPerks — ставка с привилегией и очередь за ней.
type MyPerks struct {
	Level Level                  `json:"level"`
	Queue []*repository.UserPerk `json:"queue"`
}

// MyPerks возвращает действующую привилегию и очередь.
func (s *ShopService) MyPerks(ctx context.Context, user *repository.User) (*MyPerks, error) {
	queue, err := s.perks.ListQueue(ctx, user.ID, CommissionPerkKinds, s.now())
	if err != nil {
		return nil, err
	}
	return &MyPerks{Level: s.levels.For(ctx, nil, user.ID), Queue: queue}, nil
}

// parseShopNumber разбирает номер покупки из «№1042».
func parseShopNumber(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err == nil && n > 0
}

// proportionalRefund считает возврат за идущую привилегию — пропорционально
// неиспользованным дням (оферта, п. 8.6). Начатый день считается
// использованным: в первый день возвращается (дни − 1)/дни, в последний —
// ничего. Не начавшаяся возвращается полностью, истёкшая — нет.
func proportionalRefund(price money.Amount, perk *repository.UserPerk, now time.Time) money.Amount {
	if perk.RevokedAt != nil {
		return 0
	}
	if !now.After(perk.StartsAt) {
		return price
	}
	if !now.Before(perk.ExpiresAt) {
		return 0
	}
	total := perk.ExpiresAt.Sub(perk.StartsAt).Hours() / 24
	days := int(math.Round(total))
	if days <= 0 {
		return 0
	}
	used := int(math.Ceil(now.Sub(perk.StartsAt).Hours() / 24))
	if used >= days {
		return 0
	}
	return money.Amount(int64(price) * int64(days-used) / int64(days))
}
