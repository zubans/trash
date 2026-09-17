package repository

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// OrderStatus представляет статус заказа в его жизненном цикле.
type OrderStatus string

const (
	OrderStatusSearching OrderStatus = "SEARCHING"
	OrderStatusAssigned  OrderStatus = "ASSIGNED"
	OrderStatusExecuted  OrderStatus = "EXECUTED"
	// OrderStatusDisputed — исполнитель отметил выполнение, а заказчик его
	// оспорил. Заказ открыт, пока спор не закрыт.
	OrderStatusDisputed  OrderStatus = "DISPUTED"
	OrderStatusCompleted OrderStatus = "COMPLETED"
	OrderStatusCanceled  OrderStatus = "CANCELED"
)

// Order представляет заказ клиента.
type Order struct {
	ID               uuid.UUID    `json:"id"`
	CustomerID       uuid.UUID    `json:"customer_id"`
	ExecutorID       *uuid.UUID   `json:"executor_id,omitempty"`
	ExecutorPhone    string       `json:"executor_phone,omitempty"`
	ExecutorName     string       `json:"executor_name,omitempty"`
	ServiceVariantID uuid.UUID    `json:"service_variant_id"`
	ServiceVariant   *ServiceNode `json:"service_variant,omitempty"`
	// ServiceCategory — родительская категория варианта. Едет вместе с заказом,
	// потому что клиент подписывает заказ как «категория / услуга», а достать её
	// сам он не может: /service-categories отдаёт только корни, и у вложенного
	// каталога родитель варианта в этот список не попадает.
	ServiceCategory *ServiceNode `json:"service_category,omitempty"`
	IsUrgent        bool         `json:"is_urgent"`
	IsAsap          bool         `json:"is_asap"`
	Status          OrderStatus  `json:"status"`
	HoldAmount      money.Amount `json:"hold_amount"`
	FinalAmount     money.Amount `json:"final_amount"`
	IsDowngraded    bool         `json:"is_downgraded"`
	PhotoURL        *string      `json:"photo_url,omitempty"`
	Address         *string      `json:"address,omitempty"`
	Comment         *string      `json:"comment,omitempty"`
	PickupLat       *float64     `json:"pickup_lat,omitempty"`
	PickupLon       *float64     `json:"pickup_lon,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	AssignedAt      *time.Time   `json:"assigned_at,omitempty"`
	DeadlineAt      *time.Time   `json:"deadline_at,omitempty"`
	CompletedAt     *time.Time   `json:"completed_at,omitempty"`
	CanceledAt      *time.Time   `json:"canceled_at,omitempty"`
	// ExecutedAt — когда исполнитель отметил «Исполнил», по часам сервера;
	// ExecutedAtDevice — по часам телефона. Они расходятся, когда отметка
	// пролежала в офлайн-очереди.
	ExecutedAt       *time.Time `json:"executed_at,omitempty"`
	ExecutedAtDevice *time.Time `json:"-"`
	// PhotoRequired — заказ закрывается только с фото-подтверждением: при
	// взятии у исполнителя или заказчика шёл период доп. задания.
	PhotoRequired bool `json:"photo_required"`
	// WatermarkSymbolID и ProofKey — жест и служебные данные проверки снимка,
	// выданные заказу при взятии. Наружу не отдаются как есть: исполнитель
	// получает их в PhotoProof, заказчик не получает вовсе.
	WatermarkSymbolID *uuid.UUID `json:"-"`
	ProofKey          []byte     `json:"-"`
	// PhotoProof собирается для исполнителя заказа (service/order_view.go);
	// колонки за ним нет.
	PhotoProof *OrderPhotoProof `json:"photo_proof,omitempty"`
	// SubmitFields называет данные, которые исполнитель обязан отправить на
	// проверку до завершения этого заказа, — поля личности в заказе верификации.
	// Оно заполняется при отрисовке заказа, из поведения услуги; за ним не стоит
	// колонки, и оно никогда не несёт сами значения.
	SubmitFields []string `json:"submit_fields,omitempty"`
	// ScriptExecuted — заказ закрывает скрипт услуги, а не отметка исполнителя
	// (manual_execute = false в манифесте). Заполняется при отрисовке, наружу
	// выходит как actions.execute.
	ScriptExecuted bool `json:"-"`
	// Counterparty и Actions собираются под того, кто смотрит на заказ
	// (service/order_view.go); колонок за ними нет.
	Counterparty *OrderParty   `json:"counterparty,omitempty"`
	Actions      *OrderActions `json:"actions,omitempty"`
}

// OrderPhotoProof — что исполнитель должен знать о фото-подтверждении заказа,
// чтобы снять его без сети: какой жест показать и служебные данные проверки.
type OrderPhotoProof struct {
	Required bool          `json:"required"`
	Gesture  *OrderGesture `json:"gesture,omitempty"`
	Nonce    string        `json:"nonce,omitempty"`
}

// OrderGesture — жест заказа в том виде, в каком его показывает попап.
type OrderGesture struct {
	Code         string `json:"code"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	HintImageURL string `json:"hint_image_url,omitempty"`
	FitsInSelfie bool   `json:"fits_in_selfie"`
}

// OrderParty — вторая сторона заказа глазами смотрящего: заказчику —
// исполнитель, исполнителю — заказчик.
type OrderParty struct {
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	// Hidden — сторона известна, но этому смотрящему её не показывают.
	Hidden bool `json:"hidden,omitempty"`
}

// OrderActions — что смотрящий может сделать с заказом сейчас. Решает сервер
// по роли и статусу; приложение только рисует кнопки.
type OrderActions struct {
	Cancel bool `json:"cancel"`
	Reject bool `json:"reject"`
	Review bool `json:"review"`
	// Dispute — заказчик может заявить, что заказ не выполнен.
	Dispute bool `json:"dispute"`
	// Concede — исполнитель может признать, что оспоренный заказ не выполнен.
	Concede bool `json:"concede"`
	// Execute — исполнитель может отметить заказ исполненным («Исполнил»).
	Execute bool `json:"execute"`
}

// OrderRepository описывает операции хранения заказов.
type OrderRepository interface {
	Create(ctx context.Context, q Querier, order *Order) error
	FindByID(ctx context.Context, id uuid.UUID) (*Order, error)
	GetOrderByID(ctx context.Context, id uuid.UUID) (*Order, error)
	FindAssignedByExecutor(ctx context.Context, executorID uuid.UUID) ([]Order, error)
	// FindAllByExecutor возвращает заказы исполнителя, сначала недавно
	// завершённые, не более limit (см. DefaultHistoryPageSize).
	FindAllByExecutor(ctx context.Context, executorID uuid.UUID, limit int) ([]Order, error)
	FindByCustomer(ctx context.Context, customerID uuid.UUID) ([]Order, error)
	GetPendingOrders(ctx context.Context) ([]*Order, error)
	// GetOrdersMissingCoordinates возвращает заказы в поиске, у которых есть адрес,
	// но нет координат подачи, чтобы фоновая задача могла их геокодировать.
	GetOrdersMissingCoordinates(ctx context.Context, limit int) ([]*Order, error)
	// SetPickupCoordinates заполняет координаты подачи заказа после отложенного
	// геокодирования. Он трогает только две колонки и ничего больше.
	SetPickupCoordinates(ctx context.Context, orderID uuid.UUID, lat, lon float64) error
	FindNearbyOrders(ctx context.Context, lat, lon float64, radiusMeters int) ([]*Order, error)
	// Изменяющие операции принимают Querier, чтобы вызывающий мог выполнить их
	// внутри своей транзакции; передайте nil, чтобы работать на пуле соединений.
	// Они возвращают ErrConflict, когда сущность была не в ожидаемом состоянии.
	Assign(ctx context.Context, q Querier, orderID, executorID uuid.UUID) error
	Execute(ctx context.Context, q Querier, orderID uuid.UUID) error
	// SetExecutedAtDevice записывает, когда исполнитель нажал «Исполнил» по часам
	// телефона.
	SetExecutedAtDevice(ctx context.Context, q Querier, orderID uuid.UUID, at time.Time) error
	// MarkDisputed переводит исполненный заказ в DISPUTED.
	MarkDisputed(ctx context.Context, q Querier, orderID uuid.UUID) error
	Confirm(ctx context.Context, q Querier, orderID uuid.UUID, finalAmount money.Amount, isDowngraded bool) error
	// SetCommission сохраняет ставку, по которой заказ закрыли, и уровень
	// исполнителя на тот момент. Ставка стала персональной, и без этой записи
	// разницу между двумя одинаковыми заказами объяснить нечем.
	SetCommission(ctx context.Context, q Querier, orderID uuid.UUID, percent float64, level int) error
	Cancel(ctx context.Context, q Querier, orderID uuid.UUID) error
	Unassign(ctx context.Context, q Querier, orderID uuid.UUID) error
	LockForUpdate(ctx context.Context, q Querier, orderID uuid.UUID) (*Order, error)
	SetHoldAmount(ctx context.Context, q Querier, orderID uuid.UUID, holdAmount money.Amount) error
	AssignWithHold(ctx context.Context, q Querier, orderID, executorID uuid.UUID, holdAmount money.Amount) error
	CountActiveOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error)
	// CountActiveOrdersByExecutors отвечает на тот же вопрос для набора
	// исполнителей одним запросом. Воркер подбора задаёт его раз на кандидата за
	// цикл; исполнители без назначенного заказа в результате отсутствуют, что
	// читается как нулевое количество.
	CountActiveOrdersByExecutors(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]int, error)
	CountExecutedUnconfirmedOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error)

	GetExecutorAssignedOrders(ctx context.Context, executorID uuid.UUID) ([]*Order, error)
	GetCustomerOrders(ctx context.Context, customerID uuid.UUID) ([]*Order, error)
	// FindOpenByCustomer возвращает незавершённые заказы заказчика: те, которые
	// доменное событие о нём ещё может изменить. Ограничено статусом, а не
	// размером страницы, потому что «ещё выполняется» — небольшое множество, какой
	// бы длинной ни была история заказчика.
	FindOpenByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Order, error)
	GetAvailableAuctionOrders(ctx context.Context) ([]*Order, error)
}

// orderRepo реализует OrderRepository поверх *sql.DB.
type orderRepo struct {
	db *sql.DB
}

// NewOrderRepository создаёт новый OrderRepository.
func NewOrderRepository(db *sql.DB) OrderRepository {
	return &orderRepo{db: db}
}

func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const EarthRadius = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	lat1Rad := lat1 * math.Pi / 180.0
	lat2Rad := lat2 * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadius * c
}

const orderColumns = `
    o.id, o.customer_id, o.executor_id, o.service_variant_id, o.is_urgent, o.is_asap, o.status,
    o.hold_amount, o.final_amount, o.is_downgraded, o.photo_url, o.address, o.pickup_lat, o.pickup_lon,
    o.comment, o.created_at, o.assigned_at, o.deadline_at, o.completed_at, o.canceled_at,
    o.executed_at, o.executed_at_device, o.photo_required, o.watermark_symbol_id, o.proof_key
`

const orderInsertColumns = `
    id, customer_id, executor_id, service_variant_id, is_urgent, is_asap, status,
    hold_amount, final_amount, is_downgraded, photo_url, address, pickup_lat, pickup_lon,
    comment, created_at, assigned_at, deadline_at, completed_at, canceled_at
`

func scanOrderRow(row *sql.Row) (Order, error) {
	var o Order
	var symbolID uuid.NullUUID
	err := row.Scan(
		&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
		&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address,
		&o.PickupLat, &o.PickupLon, &o.Comment, &o.CreatedAt,
		&o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
		&o.ExecutedAt, &o.ExecutedAtDevice, &o.PhotoRequired, &symbolID, &o.ProofKey,
	)
	if symbolID.Valid {
		id := symbolID.UUID
		o.WatermarkSymbolID = &id
	}
	return o, err
}

func scanOrderRows(rows *sql.Rows) (Order, error) {
	var o Order
	var symbolID uuid.NullUUID
	err := rows.Scan(
		&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
		&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address,
		&o.PickupLat, &o.PickupLon, &o.Comment, &o.CreatedAt,
		&o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
		&o.ExecutedAt, &o.ExecutedAtDevice, &o.PhotoRequired, &symbolID, &o.ProofKey,
	)
	if symbolID.Valid {
		id := symbolID.UUID
		o.WatermarkSymbolID = &id
	}
	return o, err
}

func (r *orderRepo) Create(ctx context.Context, q Querier, order *Order) error {
	_, err := r.exec(ctx, q).ExecContext(ctx,
		`INSERT INTO orders (`+orderInsertColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)`,
		order.ID, order.CustomerID, order.ExecutorID, order.ServiceVariantID, order.IsUrgent, order.IsAsap,
		order.Status, order.HoldAmount, order.FinalAmount, order.IsDowngraded, order.PhotoURL,
		order.Address, order.PickupLat, order.PickupLon, order.Comment,
		order.CreatedAt, order.AssignedAt, order.DeadlineAt, order.CompletedAt, order.CanceledAt,
	)
	return err
}

func (r *orderRepo) FindByID(ctx context.Context, id uuid.UUID) (*Order, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.id = $1`, id,
	)
	o, err := scanOrderRow(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *orderRepo) GetOrderByID(ctx context.Context, id uuid.UUID) (*Order, error) {
	return r.FindByID(ctx, id)
}

func (r *orderRepo) FindAssignedByExecutor(ctx context.Context, executorID uuid.UUID) ([]Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.executor_id = $1 AND o.status IN ($2, $3, $4) ORDER BY o.created_at DESC`,
		executorID, OrderStatusAssigned, OrderStatusExecuted, OrderStatusDisputed,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *orderRepo) FindAllByExecutor(ctx context.Context, executorID uuid.UUID, limit int) ([]Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.executor_id = $1 ORDER BY COALESCE(o.completed_at, o.canceled_at, o.created_at) DESC LIMIT $2`,
		executorID, historyLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *orderRepo) FindByCustomer(ctx context.Context, customerID uuid.UUID) ([]Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.customer_id = $1 ORDER BY COALESCE(o.completed_at, o.canceled_at, o.created_at) DESC`,
		customerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (r *orderRepo) GetPendingOrders(ctx context.Context) ([]*Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o WHERE o.status = $1`,
		OrderStatusSearching,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []*Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, &o)
	}
	return orders, rows.Err()
}

// GetOrdersMissingCoordinates возвращает не более limit заказов в поиске, у
// которых непустой адрес, но нет сохранённых координат подачи. Карта
// исполнителя рисует только заказы с координатами, поэтому иначе эти остались
// бы невидимыми до повторного геокодирования.
func (r *orderRepo) GetOrdersMissingCoordinates(ctx context.Context, limit int) ([]*Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o
		 WHERE o.status = $1
		   AND (o.pickup_lat IS NULL OR o.pickup_lon IS NULL)
		   AND o.address IS NOT NULL AND o.address <> ''
		 ORDER BY o.created_at
		 LIMIT $2`,
		OrderStatusSearching, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []*Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, &o)
	}
	return orders, rows.Err()
}

// SetPickupCoordinates записывает заказу только координаты подачи.
func (r *orderRepo) SetPickupCoordinates(ctx context.Context, orderID uuid.UUID, lat, lon float64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE orders SET pickup_lat = $2, pickup_lon = $3 WHERE id = $1`,
		orderID, lat, lon,
	)
	return err
}

// FindNearbyOrders возвращает заказы в поиске с координатами подачи в пределах radiusMeters от (lat, lon).
// Кубический оператор earth-distance недоступен, поэтому используется
// приближение по формуле гаверсинуса: сперва фильтруем прямоугольником, затем считаем точное расстояние в коде.
func (r *orderRepo) FindNearbyOrders(ctx context.Context, lat, lon float64, radiusMeters int) ([]*Order, error) {
	// Приблизительные градусы для ограничивающего прямоугольника: 1 градус широты ~ 111 км.
	deltaLat := float64(radiusMeters) / 111000.0
	deltaLon := float64(radiusMeters) / (111000.0 * math.Cos(lat*math.Pi/180.0))

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o
		 WHERE o.status = $1
		   AND o.pickup_lat BETWEEN $2 AND $3
		   AND o.pickup_lon BETWEEN $4 AND $5`,
		OrderStatusSearching,
		lat-deltaLat, lat+deltaLat,
		lon-deltaLon, lon+deltaLon,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []*Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		if o.PickupLat != nil && o.PickupLon != nil {
			dist := haversineDistance(lat, lon, *o.PickupLat, *o.PickupLon)
			if dist <= float64(radiusMeters) {
				result = append(result, &o)
			}
		}
	}
	return result, rows.Err()
}

// exec выбирает Querier: открытую транзакцию вызывающего, если она передана, и
// пул в противном случае. Каждый переход состояния ниже охраняется в SQL и
// сообщает ErrConflict, когда охрана не совпала, поэтому обновление вхолостую
// никогда не будет принято за успех.
func (r *orderRepo) exec(ctx context.Context, q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *orderRepo) Assign(ctx context.Context, q Querier, orderID, executorID uuid.UUID) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET executor_id = $1, status = $2, assigned_at = now() WHERE id = $3 AND status = $4 AND executor_id IS NULL`,
		executorID, OrderStatusAssigned, orderID, OrderStatusSearching,
	)
}

func (r *orderRepo) Execute(ctx context.Context, q Querier, orderID uuid.UUID) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET status = $1, executed_at = now() WHERE id = $2 AND status = $3`,
		OrderStatusExecuted, orderID, OrderStatusAssigned,
	)
}

func (r *orderRepo) SetExecutedAtDevice(ctx context.Context, q Querier, orderID uuid.UUID, at time.Time) error {
	_, err := r.exec(ctx, q).ExecContext(ctx, `UPDATE orders SET executed_at_device = $2 WHERE id = $1`, orderID, at)
	return err
}

func (r *orderRepo) MarkDisputed(ctx context.Context, q Querier, orderID uuid.UUID) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET status = $1 WHERE id = $2 AND status = $3`,
		OrderStatusDisputed, orderID, OrderStatusExecuted,
	)
}

func (r *orderRepo) Confirm(ctx context.Context, q Querier, orderID uuid.UUID, finalAmount money.Amount, isDowngraded bool) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET status = $1, final_amount = $2, is_downgraded = $3,
		    is_urgent = CASE WHEN $3 THEN FALSE ELSE is_urgent END,
		    is_asap = CASE WHEN $3 THEN FALSE ELSE is_asap END,
		    completed_at = now()
		 WHERE id = $4 AND status IN ($5, $6, $7)`,
		OrderStatusCompleted, finalAmount, isDowngraded, orderID, OrderStatusExecuted, OrderStatusAssigned, OrderStatusDisputed,
	)
}

func (r *orderRepo) SetCommission(ctx context.Context, q Querier, orderID uuid.UUID, percent float64, level int) error {
	_, err := r.exec(ctx, q).ExecContext(ctx,
		`UPDATE orders SET commission_percent = $2, commission_level = $3 WHERE id = $1`,
		orderID, percent, level)
	return err
}

// Cancel аннулирует незакрытый заказ. Из каких статусов отмена допустима в
// конкретном случае, решает слой сервисов (cancelTx получает список); здесь
// охрана лишь не даёт отменить уже закрытый заказ и второй параллельной отмене
// вернуть деньги дважды. DISPUTED отменяется признанием исполнителя и решением
// арбитра в пользу заказчика.
func (r *orderRepo) Cancel(ctx context.Context, q Querier, orderID uuid.UUID) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET status = $1, canceled_at = now() WHERE id = $2 AND status IN ($3, $4, $5, $6)`,
		OrderStatusCanceled, orderID, OrderStatusSearching, OrderStatusAssigned, OrderStatusExecuted, OrderStatusDisputed,
	)
}

func (r *orderRepo) Unassign(ctx context.Context, q Querier, orderID uuid.UUID) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET status = $1, executor_id = NULL, assigned_at = NULL WHERE id = $2 AND status = $3`,
		OrderStatusSearching, orderID, OrderStatusAssigned,
	)
}

// AssignWithHold назначает исполнителя и записывает согласованную цену одним
// оператором. Используется, когда заказчик принимает ставку аукциона, где цена
// известна только в этот момент.
func (r *orderRepo) AssignWithHold(ctx context.Context, q Querier, orderID, executorID uuid.UUID, holdAmount money.Amount) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET executor_id = $1, status = $2, assigned_at = now(),
		    hold_amount = $3, final_amount = $3
		 WHERE id = $4 AND status = $5 AND executor_id IS NULL`,
		executorID, OrderStatusAssigned, holdAmount, orderID, OrderStatusSearching,
	)
}

// LockForUpdate читает заказ внутри транзакции, беря блокировку строки, чтобы
// параллельные запросы подтверждения/отмены сериализовались, а не увидели оба
// одно и то же состояние до перехода.
func (r *orderRepo) LockForUpdate(ctx context.Context, q Querier, orderID uuid.UUID) (*Order, error) {
	row := r.exec(ctx, q).QueryRowContext(ctx, `SELECT `+orderColumns+` FROM orders o WHERE o.id = $1 FOR UPDATE`, orderID)
	o, err := scanOrderRow(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// SetHoldAmount корректирует сумму, удерживаемую сейчас с заказчика. Её надо
// держать в согласии с каждым возвратом, иначе выплата в момент подтверждения
// считается по устаревшему удержанию.
func (r *orderRepo) SetHoldAmount(ctx context.Context, q Querier, orderID uuid.UUID, holdAmount money.Amount) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE orders SET hold_amount = $1 WHERE id = $2`,
		holdAmount, orderID,
	)
}

// GetExecutorAssignedOrders возвращает заказы, назначенные конкретному исполнителю.
func (r *orderRepo) GetExecutorAssignedOrders(ctx context.Context, executorID uuid.UUID) ([]*Order, error) {
	orders, err := r.FindAssignedByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, nil
}

// GetCustomerOrders возвращает заказы, созданные заказчиком.
func (r *orderRepo) GetCustomerOrders(ctx context.Context, customerID uuid.UUID) ([]*Order, error) {
	orders, err := r.FindByCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, nil
}

func (r *orderRepo) FindOpenByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o
		 WHERE o.customer_id = $1 AND o.status IN ($2, $3, $4, $5)
		 ORDER BY o.created_at`,
		customerID, OrderStatusSearching, OrderStatusAssigned, OrderStatusExecuted, OrderStatusDisputed,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []*Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		order := o
		orders = append(orders, &order)
	}
	return orders, rows.Err()
}

// GetAvailableAuctionOrders возвращает открытые аукционные заказы.
func (r *orderRepo) GetAvailableAuctionOrders(ctx context.Context) ([]*Order, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+orderColumns+` FROM orders o
		 JOIN service_nodes sn ON sn.id = o.service_variant_id
		 WHERE sn.is_auction = TRUE AND o.status = $1`,
		OrderStatusSearching,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []*Order{}
	for rows.Next() {
		o, err := scanOrderRows(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, &o)
	}
	return orders, rows.Err()
}

func (r *orderRepo) CountActiveOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE executor_id = $1 AND status = 'ASSIGNED'`,
		executorID,
	).Scan(&count)
	return count, err
}

func (r *orderRepo) CountActiveOrdersByExecutors(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(executorIDs))
	placeholders, args := idList(executorIDs)
	if len(args) == 0 {
		return counts, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT executor_id, COUNT(*) FROM orders
		 WHERE status = 'ASSIGNED' AND executor_id IN (`+placeholders+`)
		 GROUP BY executor_id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}

func (r *orderRepo) CountExecutedUnconfirmedOrdersByExecutor(ctx context.Context, executorID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		// Оспоренный заказ тоже ждёт закрытия, и исполнитель, копящий споры, не
		// должен набирать новую работу сверх лимита.
		`SELECT COUNT(*) FROM orders WHERE executor_id = $1 AND status IN ('EXECUTED', 'DISPUTED')`,
		executorID,
	).Scan(&count)
	return count, err
}
