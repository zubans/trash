package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"healthlogin/backend/money"
)

// Роды товара магазина. Различие между ними — то, чем товар выдаётся: у
// привилегии есть срок и строка user_perks, у вещи — склад подарка и купон, у
// сертификата — пул кодов.
const (
	// ShopKindPerk — привилегия на срок: сниженная или нулевая комиссия.
	ShopKindPerk = "PERK"
	// ShopKindPhysical — вещь; выдаётся купоном, который гасит администратор.
	ShopKindPhysical = "PHYSICAL"
	// ShopKindCertificate — код партнёра из пула, показывается по запросу владельца.
	ShopKindCertificate = "CERTIFICATE"
)

// ErrShopProductNotFound возвращается для товара, которого нет.
var ErrShopProductNotFound = errors.New("shop product not found")

// ErrShopPickupPointNotFound возвращается для пункта выдачи, которого нет.
var ErrShopPickupPointNotFound = errors.New("shop pickup point not found")

// ShopProductVariant — вариант товара, например размер. Своего склада у
// варианта в первой версии нет: остаток общий на товар, а выбранный вариант
// едет пожеланием в fulfillment покупки.
type ShopProductVariant struct {
	Code  string                 `json:"code"`
	Title map[string]interface{} `json:"title"`
}

// ShopProduct — карточка товара витрины.
type ShopProduct struct {
	ID          uuid.UUID              `json:"id"`
	Kind        string                 `json:"kind"`
	Category    string                 `json:"category"`
	Title       map[string]interface{} `json:"title"`
	Description map[string]interface{} `json:"description"`
	// Images — до пяти путей вида /uploads/shop/…, в порядке показа.
	Images         []string      `json:"images"`
	Price          money.Amount  `json:"price"`
	CompareAtPrice *money.Amount `json:"compare_at_price,omitempty"`
	// Roles — кому виден и доступен товар. Пустой набор — всем.
	Roles            []string `json:"roles"`
	RequiresVerified bool     `json:"requires_verified"`
	// PerUserLimit — сколько всего раз можно купить одному человеку. nil — без лимита.
	PerUserLimit   *int `json:"per_user_limit,omitempty"`
	MaxQtyPerOrder int  `json:"max_qty_per_order"`
	// GiftCode — подарок, которым выдаётся товар родов PHYSICAL и CERTIFICATE.
	// Склад один на ачивки и магазин: футболка за «Марафонца» и футболка за
	// 1500 ₽ лежат на одной полке.
	GiftCode           *string              `json:"gift_code,omitempty"`
	Variants           []ShopProductVariant `json:"variants"`
	FulfillmentMethods []string             `json:"fulfillment_methods"`
	// Параметры привилегии. У COMMISSION_FREE значения нет вовсе — смысл
	// PerkValue задаёт вид: множитель (0;1] или пункты процента (>0).
	PerkKind         *string   `json:"perk_kind,omitempty"`
	PerkValue        *float64  `json:"perk_value,omitempty"`
	PerkDays         *int      `json:"perk_days,omitempty"`
	MaxActivePerUser *int      `json:"max_active_per_user,omitempty"`
	SortOrder        int       `json:"sort_order"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// InStock — можно ли выдать товар прямо сейчас: не ограничен у PERK,
	// склад подарка у PHYSICAL, свободные коды у CERTIFICATE. Считается
	// запросом списка, а не хранится.
	InStock bool `json:"in_stock"`
	// StockCount — точный остаток для админских экранов. Покупателю отдаётся
	// только InStock: точное число на витрине — лишняя информация.
	StockCount *int `json:"stock_count,omitempty"`
}

// ShopProductFilter — отбор списка товаров. Нулевые поля не фильтруют.
type ShopProductFilter struct {
	Kind     *string
	Category *string
	// ActiveOnly ограничивает список витриной; админка видит всё.
	ActiveOnly bool
	// AllRoles отключает фильтр по ролям — это право админки. Витрина фильтрует
	// всегда: товар чужой роли из списка не попадает, и «передали nil по
	// ошибке» не может превратиться в «показали всё всем».
	AllRoles bool
	// Roles — роли смотрящего. При AllRoles не читается.
	Roles []string
}

// ShopPickupPoint — пункт выдачи из справочника.
type ShopPickupPoint struct {
	ID       uuid.UUID              `json:"id"`
	Title    map[string]interface{} `json:"title"`
	Address  string                 `json:"address"`
	Hours    *string                `json:"hours,omitempty"`
	IsActive bool                   `json:"is_active"`
}

// ShopRepository хранит каталог магазина и пункты выдачи.
type ShopRepository interface {
	ListProducts(ctx context.Context, filter ShopProductFilter) ([]*ShopProduct, error)
	GetProduct(ctx context.Context, id uuid.UUID) (*ShopProduct, error)
	// LockProduct читает товар под блокировку FOR UPDATE в транзакции
	// вызывающего — так покупка видит цену и лимиты, которые не изменятся до
	// коммита.
	LockProduct(ctx context.Context, q Querier, id uuid.UUID) (*ShopProduct, error)
	UpsertProduct(ctx context.Context, p *ShopProduct) error
	// CountProductOrders считает покупки товара. Род и подарок товара с
	// продажами менять нельзя: на них уже ссылаются снимки покупок, и тихая
	// смена превратила бы прошлые продажи в то, чего не было.
	CountProductOrders(ctx context.Context, productID uuid.UUID) (int, error)

	ListPickupPoints(ctx context.Context, activeOnly bool) ([]*ShopPickupPoint, error)
	CreatePickupPoint(ctx context.Context, p *ShopPickupPoint) error
	// Удаления пункта нет: он только выключается через is_active, DELETE в API
	// не предусмотрен — выключенный пункт перестаёт предлагаться при
	// оформлении и остаётся в справочнике.
	UpdatePickupPoint(ctx context.Context, p *ShopPickupPoint) error
}

type shopRepo struct {
	db *sql.DB
}

// NewShopRepository создаёт ShopRepository.
func NewShopRepository(db *sql.DB) ShopRepository {
	return &shopRepo{db: db}
}

func (r *shopRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const shopProductColumns = `p.id, p.kind, p.category, p.title, p.description, p.images,
	p.price, p.compare_at_price, p.roles, p.requires_verified, p.per_user_limit, p.max_qty_per_order,
	p.gift_code, p.variants, p.fulfillment_methods,
	p.perk_kind, p.perk_value, p.perk_days, p.max_active_per_user,
	p.sort_order, p.is_active, p.created_at, p.updated_at`

// shopStockSelect подцепляет остаток одним запросом: склад подарка для вещи и
// число свободных кодов для сертификата. Считать это отдельным запросом на
// товар означало бы N+1 на каждой странице витрины. Активность подарка нужна
// тут же: выключенный подарок — это «нет в наличии», и витрина обязана сказать
// об этом раньше, чем покупка упадёт на out_of_stock.
const shopStockSelect = `,
	CASE p.kind
		WHEN 'PERK' THEN NULL
		WHEN 'PHYSICAL' THEN g.stock
		ELSE COALESCE(fc.free_codes, 0)
	END AS stock_count,
	g.is_active AS gift_active`

const shopStockJoin = `
	LEFT JOIN gifts g ON g.code = p.gift_code
	LEFT JOIN (SELECT gift_code, COUNT(*)::int AS free_codes
	           FROM gift_codes WHERE issued_to IS NULL GROUP BY gift_code) fc
	      ON fc.gift_code = p.gift_code`

func (r *shopRepo) ListProducts(ctx context.Context, filter ShopProductFilter) ([]*ShopProduct, error) {
	query := `SELECT ` + shopProductColumns + shopStockSelect + `
		FROM shop_products p` + shopStockJoin
	var (
		where []string
		args  []interface{}
	)
	if filter.Kind != nil {
		args = append(args, *filter.Kind)
		where = append(where, "p.kind = $"+strconv.Itoa(len(args)))
	}
	if filter.Category != nil {
		args = append(args, *filter.Category)
		where = append(where, "p.category = $"+strconv.Itoa(len(args)))
	}
	if filter.ActiveOnly {
		where = append(where, "p.is_active")
	}
	if !filter.AllRoles {
		args = append(args, pq.Array(filter.Roles))
		// Пустой набор ролей на товаре — «всем».
		where = append(where, "(p.roles = '{}' OR p.roles && $"+strconv.Itoa(len(args))+")")
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY p.sort_order, p.created_at"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*ShopProduct, 0)
	for rows.Next() {
		p, err := scanShopProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *shopRepo) GetProduct(ctx context.Context, id uuid.UUID) (*ShopProduct, error) {
	return r.getProduct(ctx, r.db, id, "")
}

// LockProduct читает товар под блокировку до конца транзакции вызывающего.
// Блокируется только строка товара: «FOR UPDATE» без OF упирался бы в
// агрегированный подзапрос свободных кодов, который блокировать нечего.
func (r *shopRepo) LockProduct(ctx context.Context, q Querier, id uuid.UUID) (*ShopProduct, error) {
	return r.getProduct(ctx, r.exec(q), id, " FOR UPDATE OF p")
}

func (r *shopRepo) getProduct(ctx context.Context, q Querier, id uuid.UUID, lock string) (*ShopProduct, error) {
	p, err := scanShopProduct(q.QueryRowContext(ctx,
		`SELECT `+shopProductColumns+shopStockSelect+`
		 FROM shop_products p`+shopStockJoin+` WHERE p.id = $1`+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShopProductNotFound
	}
	return p, err
}

func scanShopProduct(row rowScanner) (*ShopProduct, error) {
	var p ShopProduct
	var title, description, images, variants []byte
	var roles, fulfillment []string
	// Цена читается как BIGINT в int64, а не в money.Amount: его Scan ждёт
	// NUMERIC в рублях и домножил бы копейки на сто.
	var price, compareAt sql.NullInt64
	var perUserLimit, maxActivePerUser, perkDays sql.NullInt64
	var giftCode, perkKind sql.NullString
	var perkValue sql.NullFloat64
	var stock sql.NullInt64
	var giftActive sql.NullBool

	if err := row.Scan(&p.ID, &p.Kind, &p.Category, &title, &description, &images,
		&price, &compareAt, pq.Array(&roles), &p.RequiresVerified, &perUserLimit, &p.MaxQtyPerOrder,
		&giftCode, &variants, pq.Array(&fulfillment),
		&perkKind, &perkValue, &perkDays, &maxActivePerUser,
		&p.SortOrder, &p.IsActive, &p.CreatedAt, &p.UpdatedAt, &stock, &giftActive); err != nil {
		return nil, err
	}
	if len(title) > 0 {
		_ = json.Unmarshal(title, &p.Title)
	}
	if len(description) > 0 {
		_ = json.Unmarshal(description, &p.Description)
	}
	p.Images = make([]string, 0)
	if len(images) > 0 {
		_ = json.Unmarshal(images, &p.Images)
	}
	p.Variants = make([]ShopProductVariant, 0)
	if len(variants) > 0 {
		_ = json.Unmarshal(variants, &p.Variants)
	}
	p.Roles = roles
	p.FulfillmentMethods = fulfillment
	p.Price = money.Amount(price.Int64)
	if compareAt.Valid {
		value := money.Amount(compareAt.Int64)
		p.CompareAtPrice = &value
	}
	if perUserLimit.Valid {
		value := int(perUserLimit.Int64)
		p.PerUserLimit = &value
	}
	if giftCode.Valid {
		p.GiftCode = &giftCode.String
	}
	if perkKind.Valid {
		p.PerkKind = &perkKind.String
	}
	if perkValue.Valid {
		p.PerkValue = &perkValue.Float64
	}
	if perkDays.Valid {
		value := int(perkDays.Int64)
		p.PerkDays = &value
	}
	if maxActivePerUser.Valid {
		value := int(maxActivePerUser.Int64)
		p.MaxActivePerUser = &value
	}
	p.InStock = shopInStock(p.Kind, stock, giftActive)
	if stock.Valid {
		value := int(stock.Int64)
		p.StockCount = &value
	}
	return &p, nil
}

// shopInStock сводит остаток к булеву «можно выдать»: склад NULL у вещи — «не
// ограничен», как и у подарков ачивок, но выключенный подарок не выдаётся,
// что бы ни показывал его склад.
func shopInStock(kind string, stock sql.NullInt64, giftActive sql.NullBool) bool {
	switch kind {
	case ShopKindPerk:
		return true
	case ShopKindPhysical:
		if giftActive.Valid && !giftActive.Bool {
			return false
		}
		return !stock.Valid || stock.Int64 > 0
	default: // CERTIFICATE: свободных кодов 0 — выдавать нечего.
		if giftActive.Valid && !giftActive.Bool {
			return false
		}
		return stock.Valid && stock.Int64 > 0
	}
}

func (r *shopRepo) UpsertProduct(ctx context.Context, p *ShopProduct) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	// Пустой набор и NULL — разные значения для базы, но одно для вызывающего:
	// «ролей нет» — это «всем», а не отказ NOT NULL.
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if p.FulfillmentMethods == nil {
		p.FulfillmentMethods = []string{}
	}
	if p.Images == nil {
		p.Images = []string{}
	}
	if p.Variants == nil {
		p.Variants = []ShopProductVariant{}
	}
	// Незаполненное количество — это «одна единица в заказе», как и DEFAULT в
	// миграции: переданный ноль заглушил бы DEFAULT и записал 0, который CHECK
	// отклоняет, а покупка — считала бы разрешённым нулевое количество.
	if p.MaxQtyPerOrder <= 0 {
		p.MaxQtyPerOrder = 1
	}
	title, err := json.Marshal(p.Title)
	if err != nil {
		return err
	}
	description, err := json.Marshal(p.Description)
	if err != nil {
		return err
	}
	images, err := json.Marshal(p.Images)
	if err != nil {
		return err
	}
	variants, err := json.Marshal(p.Variants)
	if err != nil {
		return err
	}
	// Цены уходят в BIGINT копеек числом. *money.Amount здесь нельзя: его
	// Valuer отдаёт десятичную строку в рублях, и вставка с «старой ценой»
	// падала бы на «invalid input syntax for type bigint».
	var compareAt *int64
	if p.CompareAtPrice != nil {
		v := int64(*p.CompareAtPrice)
		compareAt = &v
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO shop_products (id, kind, category, title, description, images,
			price, compare_at_price, roles, requires_verified, per_user_limit, max_qty_per_order,
			gift_code, variants, fulfillment_methods,
			perk_kind, perk_value, perk_days, max_active_per_user, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (id) DO UPDATE SET
			kind = EXCLUDED.kind, category = EXCLUDED.category, title = EXCLUDED.title,
			description = EXCLUDED.description, images = EXCLUDED.images,
			price = EXCLUDED.price, compare_at_price = EXCLUDED.compare_at_price,
			roles = EXCLUDED.roles, requires_verified = EXCLUDED.requires_verified,
			per_user_limit = EXCLUDED.per_user_limit, max_qty_per_order = EXCLUDED.max_qty_per_order,
			gift_code = EXCLUDED.gift_code, variants = EXCLUDED.variants,
			fulfillment_methods = EXCLUDED.fulfillment_methods,
			perk_kind = EXCLUDED.perk_kind, perk_value = EXCLUDED.perk_value,
			perk_days = EXCLUDED.perk_days, max_active_per_user = EXCLUDED.max_active_per_user,
			sort_order = EXCLUDED.sort_order, is_active = EXCLUDED.is_active,
			updated_at = now()
	`, p.ID, p.Kind, p.Category, title, description, images,
		int64(p.Price), compareAt, pq.Array(p.Roles), p.RequiresVerified, p.PerUserLimit, p.MaxQtyPerOrder,
		p.GiftCode, variants, pq.Array(p.FulfillmentMethods),
		p.PerkKind, p.PerkValue, p.PerkDays, p.MaxActivePerUser, p.SortOrder, p.IsActive)
	return err
}

func (r *shopRepo) CountProductOrders(ctx context.Context, productID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM shop_orders WHERE product_id = $1`, productID).Scan(&count)
	return count, err
}

func (r *shopRepo) ListPickupPoints(ctx context.Context, activeOnly bool) ([]*ShopPickupPoint, error) {
	query := `SELECT id, title, address, hours, is_active FROM shop_pickup_points`
	if activeOnly {
		query += ` WHERE is_active`
	}
	query += ` ORDER BY title->>'ru', address`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*ShopPickupPoint, 0)
	for rows.Next() {
		p, err := scanPickupPoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPickupPoint(row rowScanner) (*ShopPickupPoint, error) {
	var p ShopPickupPoint
	var title []byte
	if err := row.Scan(&p.ID, &title, &p.Address, &p.Hours, &p.IsActive); err != nil {
		return nil, err
	}
	if len(title) > 0 {
		_ = json.Unmarshal(title, &p.Title)
	}
	return &p, nil
}

func (r *shopRepo) CreatePickupPoint(ctx context.Context, p *ShopPickupPoint) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	title, err := json.Marshal(p.Title)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO shop_pickup_points (id, title, address, hours, is_active) VALUES ($1, $2, $3, $4, $5)`,
		p.ID, title, p.Address, p.Hours, p.IsActive)
	return err
}

func (r *shopRepo) UpdatePickupPoint(ctx context.Context, p *ShopPickupPoint) error {
	title, err := json.Marshal(p.Title)
	if err != nil {
		return err
	}
	err = execExpectingOne(ctx, r.db,
		`UPDATE shop_pickup_points SET title = $2, address = $3, hours = $4, is_active = $5 WHERE id = $1`,
		p.ID, title, p.Address, p.Hours, p.IsActive)
	if errors.Is(err, ErrConflict) {
		return ErrShopPickupPointNotFound
	}
	return err
}
