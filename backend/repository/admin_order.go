package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Админский список заказов: все заказы с телефонами сторон и названием услуги,
// фильтр по группе статусов, фасеты фильтров и история на карточке
// пользователя. Заказом как таковым владеет OrderRepository; здесь только
// чтение для панели.

// AdminOrder дополняет Order телефонами заказчика/исполнителя и названием варианта услуги для админских представлений.
type AdminOrder struct {
	Order
	CustomerPhone      string `json:"customer_phone"`
	ExecutorPhone      string `json:"executor_phone,omitempty"`
	ServiceVariantName string `json:"service_variant_name"`
}

// AdminOrderRepository — чтение заказов для панели.
type AdminOrderRepository interface {
	// GetOrders — страница списка заказов админки с фильтрами. Общий счётчик
	// считается только при f.Page.WithTotal.
	GetOrders(ctx context.Context, f OrdersFilter) ([]*AdminOrder, int, error)
	// OrderFacets — значения фильтров услуги и периода для группы статусов.
	OrderFacets(ctx context.Context, statuses []OrderStatus) (OrderFacets, error)
	// GetUserOrders — заказы пользователя в обеих ролях и всех статусах, новые
	// сверху. Тот, кто и заказывает, и исполняет, видит здесь одну общую ленту:
	// на карточке спрашивают «что у этого человека было», а не «что у него было
	// в роли заказчика».
	GetUserOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*AdminOrder, int, error)
}

// OrderFacets — значения, в которые можно выставить фильтры списка заказов.
// Они считаются по всем заказам выбранной группы статусов, а не по текущей
// странице, поэтому выбор одного фильтра никогда не опустошает другой.
type OrderFacets struct {
	Services []string `json:"services"`
	Periods  []string `json:"periods"`
}

// Группы статусов списка заказов админки. «На проверке» — исполнитель отметил
// заказ исполненным и ждёт подтверждения заказчика.
const (
	OrderGroupActive    = "active"
	OrderGroupReview    = "review"
	OrderGroupCompleted = "completed"
	OrderGroupCanceled  = "canceled"
	OrderGroupAll       = "all"
)

// orderStatusGroups — статусы каждой группы. Спор (DISPUTED) — в активных: его
// разбирают, и заказ не должен пропадать из списка, пока спор открыт.
var orderStatusGroups = map[string][]OrderStatus{
	OrderGroupActive:    {OrderStatusSearching, OrderStatusAssigned, OrderStatusDisputed},
	OrderGroupReview:    {OrderStatusExecuted},
	OrderGroupCompleted: {OrderStatusCompleted},
	OrderGroupCanceled:  {OrderStatusCanceled},
}

// OrderStatusGroup отдаёт статусы группы списка заказов; для неизвестной группы
// (в том числе OrderGroupAll) — nil, что фильтр читает как «все статусы».
// Возвращается копия: карта — общее состояние пакета, и вызывающий не должен
// уметь его переписать.
func OrderStatusGroup(group string) []OrderStatus {
	statuses, ok := orderStatusGroups[group]
	if !ok {
		return nil
	}
	return append([]OrderStatus(nil), statuses...)
}

// OrdersFilter описывает одну страницу списка заказов. Statuses сужает набор до
// группы (пусто — все статусы); search, service и period сужают дальше; Sort
// выбирает колонку. Всё выполняется в SQL, поэтому то, что админ видит и
// выгружает, покрывает все подходящие заказы, а не загруженные строки.
type OrdersFilter struct {
	Statuses []OrderStatus
	// Search — телефон стороны (по цифрам), название услуги или адрес
	// (нестрого) либо полный uuid заказа (точно).
	Search  string
	Service string // точное название услуги
	Period  string // YYYY-MM по дате последнего события заказа (orderEventAt)
	Sort    string // один из orderSorts; всё прочее откатывается к умолчанию
	Desc    bool
	Page    PageRequest
}

// orderEventAt — дата последнего события заказа: завершения, отмены, отметки
// исполнителя или создания. По ней список показывает дату, сортирует и
// группирует периоды, какой бы ни была группа статусов.
const orderEventAt = "COALESCE(o.completed_at, o.canceled_at, o.executed_at, o.created_at)"

// orderSorts — белый список того, что может дойти до ORDER BY. Ключ приходит от
// клиента, поэтому его нельзя подставлять в запрос: выбрать можно только эти
// фиксированные выражения.
var orderSorts = map[string]string{
	"date":         orderEventAt,
	"final_amount": "o.final_amount",
	"service":      "COALESCE(sn.name->>'ru', sn.code)",
	"customer":     "cu.phone",
	"executor":     "eu.phone",
	"status":       "o.status",
}

// statusArgs дописывает условие по статусам в where и аргументы.
func statusArgs(where string, args []interface{}, statuses []OrderStatus) (string, []interface{}) {
	if len(statuses) == 0 {
		return where, args
	}
	placeholders := make([]string, len(statuses))
	for i, st := range statuses {
		args = append(args, st)
		placeholders[i] = fmt.Sprintf("$%d", len(args))
	}
	return where + " AND o.status IN (" + strings.Join(placeholders, ", ") + ")", args
}

// adminOrderColumns — заказ целиком плюс телефоны сторон и название услуги.
// Таблицы обязаны идти под псевдонимами o, cu, eu и sn. Телефон исполнителя —
// через COALESCE: у заказа в поиске исполнителя нет, и NULL из LEFT JOIN,
// прочитанный в строку, — ошибка драйвера, а не пустое значение.
const adminOrderColumns = orderColumns + `, cu.phone, COALESCE(eu.phone, ''), COALESCE(sn.name->>'ru', sn.code)`

// scanAdminOrder читает строку adminOrderColumns.
func scanAdminOrder(row rowScanner) (*AdminOrder, error) {
	var a AdminOrder
	o, err := scanOrder(row, &a.CustomerPhone, &a.ExecutorPhone, &a.ServiceVariantName)
	if err != nil {
		return nil, err
	}
	a.Order = o
	return &a, nil
}

type adminOrderRepo struct {
	db *sql.DB
}

// NewAdminOrderRepository создаёт AdminOrderRepository.
func NewAdminOrderRepository(db *sql.DB) AdminOrderRepository {
	return &adminOrderRepo{db: db}
}

func (r *adminOrderRepo) GetOrders(ctx context.Context, f OrdersFilter) ([]*AdminOrder, int, error) {
	where, args := statusArgs("WHERE TRUE", nil, f.Statuses)

	if search := strings.TrimSpace(f.Search); search != "" {
		var conds []string
		if id, ok := searchUUID(search); ok {
			args = append(args, id)
			conds = append(conds, fmt.Sprintf("o.id = $%d", len(args)))
		}
		args = append(args, "%"+search+"%")
		like := fmt.Sprintf("$%d", len(args))
		conds = append(conds,
			fmt.Sprintf("COALESCE(sn.name->>'ru', sn.code) ILIKE %s", like),
			fmt.Sprintf("COALESCE(o.address, '') ILIKE %s", like),
		)
		// Телефон хранится как +79997454656, а набирают его как
		// «+7 (999) 745-46-56» или просто «9997»: обе стороны перед сравнением
		// сводятся к цифрам, поэтому админу не нужно воспроизводить сохранённое
		// написание. Цифры используются, только когда они в запросе реально
		// есть, — иначе пустой строке соответствовала бы каждая строка.
		if digits := digitsOnly(search); digits != "" {
			args = append(args, "%"+digits+"%")
			digitsLike := fmt.Sprintf("$%d", len(args))
			conds = append(conds,
				fmt.Sprintf("regexp_replace(cu.phone, '[^0-9]', '', 'g') LIKE %s", digitsLike),
				fmt.Sprintf("regexp_replace(COALESCE(eu.phone, ''), '[^0-9]', '', 'g') LIKE %s", digitsLike),
			)
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}

	if service := strings.TrimSpace(f.Service); service != "" {
		args = append(args, service)
		where += fmt.Sprintf(" AND COALESCE(sn.name->>'ru', sn.code) = $%d", len(args))
	}

	where, args = periodArgs(where, args, orderEventAt, f.Period)

	from := `
		FROM orders o
		JOIN users cu ON o.customer_id = cu.id
		LEFT JOIN users eu ON o.executor_id = eu.id
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		` + where

	var total int
	if f.Page.WithTotal {
		if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+from, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}

	sortExpr, ok := orderSorts[f.Sort]
	if !ok {
		sortExpr = orderSorts["date"]
	}
	direction := "ASC"
	if f.Desc {
		direction = "DESC"
	}

	limit, offset := clampPage(f.Page.Limit, f.Page.Offset)
	args = append(args, limit, offset)
	query := fmt.Sprintf(`
		SELECT `+adminOrderColumns+`
		%s
		ORDER BY %s %s NULLS LAST, o.created_at DESC
		LIMIT $%d OFFSET $%d`, from, sortExpr, direction, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	orders := []*AdminOrder{}
	for rows.Next() {
		o, err := scanAdminOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, rows.Err()
}

// OrderFacets читает все заказы группы дважды (DISTINCT по услуге и по месяцу),
// поэтому вызывающий обязан кэшировать результат: см. service.facetCache.
func (r *adminOrderRepo) OrderFacets(ctx context.Context, statuses []OrderStatus) (OrderFacets, error) {
	facets := OrderFacets{Services: []string{}, Periods: []string{}}
	where, args := statusArgs("WHERE TRUE", nil, statuses)

	serviceRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT COALESCE(sn.name->>'ru', sn.code) AS name
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		`+where+`
		ORDER BY name`, args...)
	if err != nil {
		return facets, err
	}
	defer serviceRows.Close()
	for serviceRows.Next() {
		var name string
		if err := serviceRows.Scan(&name); err != nil {
			return facets, err
		}
		facets.Services = append(facets.Services, name)
	}
	if err := serviceRows.Err(); err != nil {
		return facets, err
	}

	periodRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT to_char(`+orderEventAt+`, 'YYYY-MM') AS period
		FROM orders o
		`+where+`
		ORDER BY period DESC`, args...)
	if err != nil {
		return facets, err
	}
	defer periodRows.Close()
	for periodRows.Next() {
		var period string
		if err := periodRows.Scan(&period); err != nil {
			return facets, err
		}
		facets.Periods = append(facets.Periods, period)
	}
	return facets, periodRows.Err()
}

// GetUserOrders отдаёт заказы пользователя в обеих ролях и всех статусах.
// Счётчик считается всегда: выборка идёт по индексам customer_id/executor_id,
// и карточка листает страницы без первой.
func (r *adminOrderRepo) GetUserOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*AdminOrder, int, error) {
	limit, offset = clampPage(limit, offset)

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE customer_id = $1 OR executor_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+adminOrderColumns+`
		FROM orders o
		JOIN users cu ON o.customer_id = cu.id
		LEFT JOIN users eu ON o.executor_id = eu.id
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.customer_id = $1 OR o.executor_id = $1
		ORDER BY o.created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	orders := make([]*AdminOrder, 0, limit)
	for rows.Next() {
		o, err := scanAdminOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, rows.Err()
}
