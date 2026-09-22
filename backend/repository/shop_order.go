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

// Статусы покупки (implementation_plan_shop.md §4.3). Привилегия и
// сертификат выдаются сразу и сразу COMPLETED; вещь проходит PAID →
// PROCESSING → SHIPPED → COMPLETED. CANCELED — только отмена администратором с
// возвратом, из любого статуса.
const (
	ShopOrderPaid       = "PAID"
	ShopOrderProcessing = "PROCESSING"
	ShopOrderShipped    = "SHIPPED"
	ShopOrderCompleted  = "COMPLETED"
	ShopOrderCanceled   = "CANCELED"
)

// Способы получения вещи.
const (
	ShopFulfillmentPickup   = "PICKUP"
	ShopFulfillmentDelivery = "DELIVERY"
)

// ErrShopOrderNotFound возвращается для покупки, которой нет или которая чужая.
var ErrShopOrderNotFound = errors.New("shop order not found")

// ShopOrder — покупка в магазине.
type ShopOrder struct {
	ID uuid.UUID `json:"id"`
	// Number — «Заказ №1042» для людей: его диктуют в поддержку и пишут в
	// обращение о возврате.
	Number    int64     `json:"number"`
	UserID    uuid.UUID `json:"user_id"`
	RequestID uuid.UUID `json:"request_id"`
	ProductID uuid.UUID `json:"product_id"`
	// ProductSnapshot — название, род и параметры товара на момент покупки:
	// товар потом правят, а спор разбирается по тому, что было куплено.
	ProductSnapshot map[string]interface{} `json:"product_snapshot"`
	Variant         *string                `json:"variant,omitempty"`
	Quantity        int                    `json:"quantity"`
	UnitPrice       money.Amount           `json:"unit_price"`
	Total           money.Amount           `json:"total"`
	Status          string                 `json:"status"`
	// Fulfillment — способ получения, пункт выдачи или адрес, получатель,
	// телефон и трек-номер.
	Fulfillment    map[string]interface{} `json:"fulfillment"`
	RefundedAmount money.Amount           `json:"refunded_amount"`
	OfferVersion   int                    `json:"offer_version"`
	CancelReason   *string                `json:"cancel_reason,omitempty"`
	CanceledBy     *uuid.UUID             `json:"canceled_by,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`

	// Поля ниже заполняются для экранов, а не хранятся в строке.
	UserPhone string `json:"user_phone,omitempty"`
	UserName  string `json:"user_name,omitempty"`
	// SupportChatID — чат поддержки покупателя: возврат оформляется там.
	SupportChatID *uuid.UUID `json:"support_chat_id,omitempty"`
	// RefundRequestAt — когда покупатель написал в поддержку с номером этой
	// покупки, если после этого поддержка ещё не отвечала.
	RefundRequestAt *time.Time     `json:"refund_request_at,omitempty"`
	Coupons         []*UserGift    `json:"coupons,omitempty"`
	Perks           []*UserPerk    `json:"perks,omitempty"`
	Transactions    []*Transaction `json:"transactions,omitempty"`
}

// ShopOrderFilter — отбор покупок для админки. Нулевые поля не фильтруют.
type ShopOrderFilter struct {
	Status    string
	ProductID *uuid.UUID
	From      *time.Time
	To        *time.Time
	// Search — номер покупки или телефон покупателя.
	Search string
	// RefundRequested оставляет покупки с неотвеченным обращением о возврате.
	RefundRequested bool
	Limit           int
	Offset          int
}

// ShopSalesRow — продажи одного товара за период.
type ShopSalesRow struct {
	ProductID uuid.UUID              `json:"product_id"`
	Title     map[string]interface{} `json:"title"`
	Kind      string                 `json:"kind"`
	Orders    int                    `json:"orders"`
	Quantity  int                    `json:"quantity"`
	Total     money.Amount           `json:"total"`
	Refunded  money.Amount           `json:"refunded"`
}

// ShopOrderRepository хранит покупки.
type ShopOrderRepository interface {
	// Create записывает покупку в транзакции вызывающего. Повтор с тем же
	// request_id от того же пользователя не создаёт второй строки: created
	// тогда false, и вызывающий обязан вернуть существующую покупку, не
	// списывая деньги второй раз.
	Create(ctx context.Context, q Querier, order *ShopOrder) (created bool, err error)
	GetByRequest(ctx context.Context, q Querier, userID, requestID uuid.UUID) (*ShopOrder, error)
	Get(ctx context.Context, q Querier, id uuid.UUID) (*ShopOrder, error)
	// Lock читает покупку под FOR UPDATE: смена статуса и отмена не должны
	// пройти одновременно.
	Lock(ctx context.Context, tx *sql.Tx, id uuid.UUID) (*ShopOrder, error)
	ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*ShopOrder, error)
	List(ctx context.Context, filter ShopOrderFilter) ([]*ShopOrder, int, error)
	// CountForUserProduct — сколько раз пользователь купил товар, не считая
	// отменённых покупок: отменённая не должна съедать лимит.
	CountForUserProduct(ctx context.Context, q Querier, userID, productID uuid.UUID) (int, error)
	// CountByStatus — для бейджа в меню админки.
	CountByStatus(ctx context.Context, status string) (int, error)
	SetStatus(ctx context.Context, q Querier, id uuid.UUID, status string, fulfillment map[string]interface{}) error
	Cancel(ctx context.Context, q Querier, id uuid.UUID, reason string, adminID uuid.UUID, refund money.Amount) error
	// Numbers переводит номера покупок пользователя в их id — для ссылок из
	// чата поддержки. Чужие номера не находятся.
	Numbers(ctx context.Context, userID uuid.UUID, numbers []int64) (map[int64]uuid.UUID, error)
	// RefundRequestAt — неотвеченное обращение о возврате по одной покупке,
	// так же, как его видит список.
	RefundRequestAt(ctx context.Context, id uuid.UUID) (*time.Time, error)
	// Buyer — телефон и имя покупателя для карточки покупки в админке.
	Buyer(ctx context.Context, userID uuid.UUID) (phone, name string, err error)
	// SupportChatID — чат поддержки пользователя, если он уже заведён.
	SupportChatID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	// Transactions — проводки покупки: оплата и возвраты.
	Transactions(ctx context.Context, id uuid.UUID) ([]*Transaction, error)
	// Sales — продажи и возвраты по товарам за период [from, to).
	Sales(ctx context.Context, from, to time.Time) ([]*ShopSalesRow, error)
	// CommissionPaidSince — сколько комиссии исполнитель заплатил начиная с
	// since, по проводкам COMMISSION. Нужна витрине для окупаемости привилегии.
	CommissionPaidSince(ctx context.Context, userID uuid.UUID, since time.Time) (money.Amount, error)
}

type shopOrderRepo struct {
	db *sql.DB
}

// NewShopOrderRepository создаёт ShopOrderRepository.
func NewShopOrderRepository(db *sql.DB) ShopOrderRepository {
	return &shopOrderRepo{db: db}
}

func (r *shopOrderRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const shopOrderColumns = `o.id, o.number, o.user_id, o.request_id, o.product_id, o.product_snapshot,
	o.variant, o.quantity, o.unit_price, o.total, o.status, o.fulfillment, o.refunded_amount,
	o.offer_version, o.cancel_reason, o.canceled_by, o.created_at, o.updated_at`

func scanShopOrder(row rowScanner, extra ...interface{}) (*ShopOrder, error) {
	var o ShopOrder
	var snapshot, fulfillment []byte
	// Деньги в shop_orders — BIGINT копеек, а Scan у money.Amount ждёт NUMERIC
	// в рублях: читаем числом, как и цены товара.
	var unitPrice, total, refunded int64
	dest := []interface{}{&o.ID, &o.Number, &o.UserID, &o.RequestID, &o.ProductID, &snapshot,
		&o.Variant, &o.Quantity, &unitPrice, &total, &o.Status, &fulfillment, &refunded,
		&o.OfferVersion, &o.CancelReason, &o.CanceledBy, &o.CreatedAt, &o.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	o.UnitPrice, o.Total, o.RefundedAmount = money.Amount(unitPrice), money.Amount(total), money.Amount(refunded)
	o.ProductSnapshot = map[string]interface{}{}
	if len(snapshot) > 0 {
		_ = json.Unmarshal(snapshot, &o.ProductSnapshot)
	}
	o.Fulfillment = map[string]interface{}{}
	if len(fulfillment) > 0 {
		_ = json.Unmarshal(fulfillment, &o.Fulfillment)
	}
	return &o, nil
}

func (r *shopOrderRepo) Create(ctx context.Context, q Querier, order *ShopOrder) (bool, error) {
	if order.ID == uuid.Nil {
		order.ID = uuid.New()
	}
	snapshot, err := json.Marshal(order.ProductSnapshot)
	if err != nil {
		return false, err
	}
	if order.Fulfillment == nil {
		order.Fulfillment = map[string]interface{}{}
	}
	fulfillment, err := json.Marshal(order.Fulfillment)
	if err != nil {
		return false, err
	}
	// ON CONFLICT DO NOTHING, а не проверка перед вставкой: две одинаковые
	// попытки, пришедшие одновременно, иначе обе увидели бы «покупки нет».
	// Вторая здесь дождётся коммита первой и ничего не вставит.
	err = r.exec(q).QueryRowContext(ctx, `
		INSERT INTO shop_orders (id, user_id, request_id, product_id, product_snapshot, variant,
			quantity, unit_price, total, status, fulfillment, offer_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, request_id) DO NOTHING
		RETURNING number, created_at, updated_at`,
		order.ID, order.UserID, order.RequestID, order.ProductID, snapshot, order.Variant,
		order.Quantity, int64(order.UnitPrice), int64(order.Total), order.Status, fulfillment, order.OfferVersion,
	).Scan(&order.Number, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *shopOrderRepo) one(ctx context.Context, q Querier, where string, args ...interface{}) (*ShopOrder, error) {
	o, err := scanShopOrder(r.exec(q).QueryRowContext(ctx,
		`SELECT `+shopOrderColumns+` FROM shop_orders o WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrShopOrderNotFound
	}
	return o, err
}

func (r *shopOrderRepo) GetByRequest(ctx context.Context, q Querier, userID, requestID uuid.UUID) (*ShopOrder, error) {
	return r.one(ctx, q, `o.user_id = $1 AND o.request_id = $2`, userID, requestID)
}

func (r *shopOrderRepo) Get(ctx context.Context, q Querier, id uuid.UUID) (*ShopOrder, error) {
	return r.one(ctx, q, `o.id = $1`, id)
}

func (r *shopOrderRepo) Lock(ctx context.Context, tx *sql.Tx, id uuid.UUID) (*ShopOrder, error) {
	return r.one(ctx, tx, `o.id = $1 FOR UPDATE`, id)
}

func (r *shopOrderRepo) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*ShopOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+shopOrderColumns+` FROM shop_orders o
		WHERE o.user_id = $1 ORDER BY o.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ShopOrder, 0)
	for rows.Next() {
		o, err := scanShopOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// refundRequestSelect находит обращение о возврате: последнее сообщение
// покупателя в его чате поддержки, где упомянут номер покупки, если после него
// поддержка ещё ничего не писала. Номер ищется так же, как его подсвечивает
// чат: «№» и цифры, после которых цифр больше нет.
const refundRequestSelect = `(
	SELECT req.created_at FROM support_chats sc
	JOIN LATERAL (
		SELECT sm.created_at FROM support_messages sm
		WHERE sm.chat_id = sc.id AND sm.sender_id = o.user_id AND NOT sm.is_deleted
		  AND sm.text ~ ('№\s*' || o.number::text || '([^0-9]|$)')
		ORDER BY sm.created_at DESC LIMIT 1
	) req ON true
	WHERE sc.user_id = o.user_id
	  AND NOT EXISTS (
		SELECT 1 FROM support_messages reply
		WHERE reply.chat_id = sc.id AND reply.sender_id <> o.user_id
		  AND reply.created_at > req.created_at)
)`

func (r *shopOrderRepo) List(ctx context.Context, filter ShopOrderFilter) ([]*ShopOrder, int, error) {
	var (
		where []string
		args  []interface{}
	)
	arg := func(v interface{}) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if filter.Status != "" {
		where = append(where, "o.status = "+arg(filter.Status))
	}
	if filter.ProductID != nil {
		where = append(where, "o.product_id = "+arg(*filter.ProductID))
	}
	if filter.From != nil {
		where = append(where, "o.created_at >= "+arg(*filter.From))
	}
	if filter.To != nil {
		where = append(where, "o.created_at < "+arg(*filter.To))
	}
	if search := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(filter.Search), "№")); search != "" {
		if n, err := strconv.ParseInt(search, 10, 64); err == nil {
			where = append(where, "(o.number = "+arg(n)+" OR u.phone LIKE "+arg("%"+search+"%")+")")
		} else {
			where = append(where, "u.phone LIKE "+arg("%"+search+"%"))
		}
	}
	if filter.RefundRequested {
		// Обращение по отменённой покупке уже разобрано: отмена и есть ответ.
		where = append(where, "o.status <> "+arg(ShopOrderCanceled), refundRequestSelect+" IS NOT NULL")
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop_orders o JOIN users u ON u.id = o.user_id`+cond,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + shopOrderColumns + `, u.phone,
		TRIM(CONCAT_WS(' ', u.last_name, u.first_name, u.patronymic)), ` + refundRequestSelect + `
		FROM shop_orders o JOIN users u ON u.id = o.user_id` + cond +
		` ORDER BY o.created_at DESC LIMIT ` + arg(limit) + ` OFFSET ` + arg(offset)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*ShopOrder, 0)
	for rows.Next() {
		var phone, name string
		var requestAt sql.NullTime
		o, err := scanShopOrder(rows, &phone, &name, &requestAt)
		if err != nil {
			return nil, 0, err
		}
		o.UserPhone, o.UserName = phone, name
		if requestAt.Valid {
			at := requestAt.Time
			o.RefundRequestAt = &at
		}
		out = append(out, o)
	}
	return out, total, rows.Err()
}

func (r *shopOrderRepo) RefundRequestAt(ctx context.Context, id uuid.UUID) (*time.Time, error) {
	var at sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT `+refundRequestSelect+` FROM shop_orders o WHERE o.id = $1`, id).Scan(&at)
	if err != nil || !at.Valid {
		return nil, err
	}
	return &at.Time, nil
}

func (r *shopOrderRepo) CountForUserProduct(ctx context.Context, q Querier, userID, productID uuid.UUID) (int, error) {
	var count int
	err := r.exec(q).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM shop_orders WHERE user_id = $1 AND product_id = $2 AND status <> $3`,
		userID, productID, ShopOrderCanceled).Scan(&count)
	return count, err
}

func (r *shopOrderRepo) CountByStatus(ctx context.Context, status string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop_orders WHERE status = $1`, status).Scan(&count)
	return count, err
}

func (r *shopOrderRepo) SetStatus(ctx context.Context, q Querier, id uuid.UUID, status string, fulfillment map[string]interface{}) error {
	if fulfillment == nil {
		fulfillment = map[string]interface{}{}
	}
	raw, err := json.Marshal(fulfillment)
	if err != nil {
		return err
	}
	err = execExpectingOne(ctx, r.exec(q),
		`UPDATE shop_orders SET status = $2, fulfillment = $3, updated_at = now() WHERE id = $1`,
		id, status, raw)
	if errors.Is(err, ErrConflict) {
		return ErrShopOrderNotFound
	}
	return err
}

func (r *shopOrderRepo) Cancel(ctx context.Context, q Querier, id uuid.UUID, reason string, adminID uuid.UUID, refund money.Amount) error {
	// Статус проверяется ещё раз в самом операторе: отмена, прошедшая мимо
	// блокировки, не должна вернуть деньги второй раз.
	err := execExpectingOne(ctx, r.exec(q), `
		UPDATE shop_orders
		   SET status = $2, cancel_reason = $3, canceled_by = $4,
		       refunded_amount = refunded_amount + $5, updated_at = now()
		 WHERE id = $1 AND status <> $2`,
		id, ShopOrderCanceled, reason, adminID, int64(refund))
	if errors.Is(err, ErrConflict) {
		return ErrShopOrderNotFound
	}
	return err
}

func (r *shopOrderRepo) Numbers(ctx context.Context, userID uuid.UUID, numbers []int64) (map[int64]uuid.UUID, error) {
	out := map[int64]uuid.UUID{}
	if len(numbers) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT number, id FROM shop_orders WHERE user_id = $1 AND number = ANY($2)`,
		userID, pq.Array(numbers))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		var id uuid.UUID
		if err := rows.Scan(&n, &id); err != nil {
			return nil, err
		}
		out[n] = id
	}
	return out, rows.Err()
}

func (r *shopOrderRepo) Buyer(ctx context.Context, userID uuid.UUID) (string, string, error) {
	var phone, name string
	err := r.db.QueryRowContext(ctx, `
		SELECT phone, TRIM(CONCAT_WS(' ', last_name, first_name, patronymic)) FROM users WHERE id = $1`,
		userID).Scan(&phone, &name)
	return phone, name, err
}

func (r *shopOrderRepo) SupportChatID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT id FROM support_chats WHERE user_id = $1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *shopOrderRepo) Transactions(ctx context.Context, id uuid.UUID) ([]*Transaction, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, type::text, amount, COALESCE(counterparty, ''), admin_id, created_at
		FROM transactions WHERE shop_order_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Transaction, 0)
	for rows.Next() {
		var t Transaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.Type, &t.Amount, &t.Counterparty, &t.AdminID, &t.CreatedAt); err != nil {
			return nil, err
		}
		shopOrderID := id
		t.ShopOrderID = &shopOrderID
		if sign, ok := LedgerSign(TransactionType(t.Type)); ok {
			t.Direction = sign
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *shopOrderRepo) Sales(ctx context.Context, from, to time.Time) ([]*ShopSalesRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.product_id, p.title, p.kind, COUNT(*), COALESCE(SUM(o.quantity), 0),
		       COALESCE(SUM(o.total), 0), COALESCE(SUM(o.refunded_amount), 0)
		FROM shop_orders o JOIN shop_products p ON p.id = o.product_id
		WHERE o.created_at >= $1 AND o.created_at < $2
		GROUP BY o.product_id, p.title, p.kind
		ORDER BY SUM(o.total) DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*ShopSalesRow, 0)
	for rows.Next() {
		var row ShopSalesRow
		var title []byte
		var total, refunded int64
		if err := rows.Scan(&row.ProductID, &title, &row.Kind, &row.Orders, &row.Quantity, &total, &refunded); err != nil {
			return nil, err
		}
		row.Total, row.Refunded = money.Amount(total), money.Amount(refunded)
		if len(title) > 0 {
			_ = json.Unmarshal(title, &row.Title)
		}
		out = append(out, &row)
	}
	return out, rows.Err()
}

func (r *shopOrderRepo) CommissionPaidSince(ctx context.Context, userID uuid.UUID, since time.Time) (money.Amount, error) {
	var paid money.Amount
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM transactions
		WHERE user_id = $1 AND type = $2 AND created_at >= $3`,
		userID, TransactionTypeCommission, since).Scan(&paid)
	return paid, err
}
