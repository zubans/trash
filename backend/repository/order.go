package repository

import (
	"database/sql"
	"math"
	"time"

	"github.com/google/uuid"
)

// OrderStatus represents the lifecycle status of an order.
type OrderStatus string

const (
	OrderStatusSearching OrderStatus = "SEARCHING"
	OrderStatusAssigned  OrderStatus = "ASSIGNED"
	OrderStatusCompleted OrderStatus = "COMPLETED"
	OrderStatusCanceled  OrderStatus = "CANCELED"
)

// Order represents a customer order.
type Order struct {
	ID               uuid.UUID    `json:"id"`
	CustomerID       uuid.UUID    `json:"customer_id"`
	ExecutorID       *uuid.UUID   `json:"executor_id,omitempty"`
	ServiceVariantID uuid.UUID    `json:"service_variant_id"`
	ServiceVariant   *ServiceNode `json:"service_variant,omitempty"`
	IsUrgent         bool         `json:"is_urgent"`
	IsAsap           bool         `json:"is_asap"`
	Status           OrderStatus  `json:"status"`
	HoldAmount       float64      `json:"hold_amount"`
	FinalAmount      float64      `json:"final_amount"`
	IsDowngraded     bool         `json:"is_downgraded"`
	PhotoURL         *string      `json:"photo_url,omitempty"`
	Address          *string      `json:"address,omitempty"`
	PickupLat        *float64     `json:"pickup_lat,omitempty"`
	PickupLon        *float64     `json:"pickup_lon,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
	AssignedAt       *time.Time   `json:"assigned_at,omitempty"`
	DeadlineAt       *time.Time   `json:"deadline_at,omitempty"`
	CompletedAt      *time.Time   `json:"completed_at,omitempty"`
	CanceledAt       *time.Time   `json:"canceled_at,omitempty"`
}

// OrderRepository defines storage operations for orders.
type OrderRepository interface {
	Create(order *Order) error
	FindByID(id uuid.UUID) (*Order, error)
	GetOrderByID(id uuid.UUID) (*Order, error)
	FindAssignedByExecutor(executorID uuid.UUID) ([]Order, error)
	FindAllByExecutor(executorID uuid.UUID) ([]Order, error)
	FindByCustomer(customerID uuid.UUID) ([]Order, error)
	GetPendingOrders() ([]*Order, error)
	FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*Order, error)
	Assign(orderID, executorID uuid.UUID) error
	AssignOrder(orderID, executorID uuid.UUID) error
	Confirm(orderID uuid.UUID, finalAmount float64, isDowngraded bool) error
	Cancel(orderID uuid.UUID) error
	Unassign(orderID uuid.UUID) error

	// Legacy/test-compatible methods
	CreateOrderWithHold(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, holdAmount float64, lastGeo string) (*Order, error)
	ConfirmOrderExecution(orderID uuid.UUID) error
	CancelOrder(orderID uuid.UUID) error
	GetExecutorAssignedOrders(executorID uuid.UUID) ([]*Order, error)
	GetCustomerOrders(customerID uuid.UUID) ([]*Order, error)
	CreateConstructionOrder(customerID uuid.UUID, serviceVariantID uuid.UUID, photoURL, lastGeo string) (*Order, error)
	GetAvailableAuctionOrders() ([]*Order, error)
}

// orderRepo implements OrderRepository using *sql.DB.
type orderRepo struct {
	db *sql.DB
}

// NewOrderRepository creates a new OrderRepository.
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
    o.created_at, o.assigned_at, o.deadline_at, o.completed_at, o.canceled_at
`

const orderInsertColumns = `
    id, customer_id, executor_id, service_variant_id, is_urgent, is_asap, status,
    hold_amount, final_amount, is_downgraded, photo_url, address, pickup_lat, pickup_lon,
    created_at, assigned_at, deadline_at, completed_at, canceled_at
`

func scanOrderRow(row *sql.Row) (Order, error) {
	var o Order
	err := row.Scan(
		&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
		&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address,
		&o.PickupLat, &o.PickupLon, &o.CreatedAt,
		&o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
	)
	return o, err
}

func scanOrderRows(rows *sql.Rows) (Order, error) {
	var o Order
	err := rows.Scan(
		&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
		&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address,
		&o.PickupLat, &o.PickupLon, &o.CreatedAt,
		&o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
	)
	return o, err
}

func (r *orderRepo) Create(order *Order) error {
	_, err := r.db.Exec(
		`INSERT INTO orders (`+orderInsertColumns+`)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		order.ID, order.CustomerID, order.ExecutorID, order.ServiceVariantID, order.IsUrgent, order.IsAsap,
		order.Status, order.HoldAmount, order.FinalAmount, order.IsDowngraded, order.PhotoURL,
		order.Address, order.PickupLat, order.PickupLon,
		order.CreatedAt, order.AssignedAt, order.DeadlineAt, order.CompletedAt, order.CanceledAt,
	)
	return err
}

func (r *orderRepo) FindByID(id uuid.UUID) (*Order, error) {
	row := r.db.QueryRow(
		`SELECT `+orderColumns+` FROM orders o WHERE o.id = $1`, id,
	)
	o, err := scanOrderRow(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *orderRepo) GetOrderByID(id uuid.UUID) (*Order, error) {
	return r.FindByID(id)
}

func (r *orderRepo) FindAssignedByExecutor(executorID uuid.UUID) ([]Order, error) {
	rows, err := r.db.Query(
		`SELECT `+orderColumns+` FROM orders o WHERE o.executor_id = $1 AND o.status = $2`,
		executorID, OrderStatusAssigned,
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

func (r *orderRepo) FindAllByExecutor(executorID uuid.UUID) ([]Order, error) {
	rows, err := r.db.Query(
		`SELECT `+orderColumns+` FROM orders o WHERE o.executor_id = $1 ORDER BY o.created_at DESC`,
		executorID,
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

func (r *orderRepo) FindByCustomer(customerID uuid.UUID) ([]Order, error) {
	rows, err := r.db.Query(
		`SELECT `+orderColumns+` FROM orders o WHERE o.customer_id = $1`,
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

func (r *orderRepo) GetPendingOrders() ([]*Order, error) {
	rows, err := r.db.Query(
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

// FindNearbyOrders returns searching orders with pickup coordinates within radiusMeters of (lat, lon).
// Uses the Haversine formula approximation via the earth-distance cube operator is not available,
// so we filter with a bounding box first and then compute exact distance in code.
func (r *orderRepo) FindNearbyOrders(lat, lon float64, radiusMeters int) ([]*Order, error) {
	// Approximate degrees for the bounding box: 1 degree lat ~ 111 km.
	deltaLat := float64(radiusMeters) / 111000.0
	deltaLon := float64(radiusMeters) / (111000.0 * math.Cos(lat*math.Pi/180.0))

	rows, err := r.db.Query(
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

func (r *orderRepo) Assign(orderID, executorID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE orders SET executor_id = $1, status = $2, assigned_at = now() WHERE id = $3 AND status = $4 AND executor_id IS NULL`,
		executorID, OrderStatusAssigned, orderID, OrderStatusSearching,
	)
	return err
}

func (r *orderRepo) AssignOrder(orderID, executorID uuid.UUID) error {
	return r.Assign(orderID, executorID)
}

func (r *orderRepo) Confirm(orderID uuid.UUID, finalAmount float64, isDowngraded bool) error {
	_, err := r.db.Exec(
		`UPDATE orders SET status = $1, final_amount = $2, is_downgraded = $3,
		    is_urgent = CASE WHEN $3 THEN FALSE ELSE is_urgent END,
		    is_asap = CASE WHEN $3 THEN FALSE ELSE is_asap END,
		    completed_at = now()
		 WHERE id = $4 AND status = $5`,
		OrderStatusCompleted, finalAmount, isDowngraded, orderID, OrderStatusAssigned,
	)
	return err
}

func (r *orderRepo) Cancel(orderID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE orders SET status = $1, canceled_at = now() WHERE id = $2 AND status IN ($3, $4)`,
		OrderStatusCanceled, orderID, OrderStatusSearching, OrderStatusAssigned,
	)
	return err
}

func (r *orderRepo) Unassign(orderID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE orders SET status = $1, executor_id = NULL, assigned_at = NULL WHERE id = $2 AND status = $3`,
		OrderStatusSearching, orderID, OrderStatusAssigned,
	)
	return err
}

// CreateOrderWithHold creates a standard order and blocks customer balance.
func (r *orderRepo) CreateOrderWithHold(customerID uuid.UUID, serviceVariantID uuid.UUID, isUrgent, isAsap bool, holdAmount float64, lastGeo string) (*Order, error) {
	order := &Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: serviceVariantID,
		IsUrgent:         isUrgent,
		IsAsap:           isAsap,
		Status:           OrderStatusSearching,
		HoldAmount:       holdAmount,
		FinalAmount:      holdAmount,
		CreatedAt:        time.Now(),
	}
	if err := r.Create(order); err != nil {
		return nil, err
	}
	return order, nil
}

// ConfirmOrderExecution marks an order as completed.
func (r *orderRepo) ConfirmOrderExecution(orderID uuid.UUID) error {
	return r.Confirm(orderID, 0, false)
}

// CancelOrder cancels an order.
func (r *orderRepo) CancelOrder(orderID uuid.UUID) error {
	return r.Cancel(orderID)
}

// GetExecutorAssignedOrders returns orders assigned to a specific executor.
func (r *orderRepo) GetExecutorAssignedOrders(executorID uuid.UUID) ([]*Order, error) {
	orders, err := r.FindAssignedByExecutor(executorID)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, nil
}

// GetCustomerOrders returns orders created by a customer.
func (r *orderRepo) GetCustomerOrders(customerID uuid.UUID) ([]*Order, error) {
	orders, err := r.FindByCustomer(customerID)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i := range orders {
		result[i] = &orders[i]
	}
	return result, nil
}

// CreateConstructionOrder creates a construction waste auction order.
func (r *orderRepo) CreateConstructionOrder(customerID uuid.UUID, serviceVariantID uuid.UUID, photoURL, lastGeo string) (*Order, error) {
	photo := &photoURL
	order := &Order{
		ID:               uuid.New(),
		CustomerID:       customerID,
		ServiceVariantID: serviceVariantID,
		IsUrgent:         false,
		IsAsap:           false,
		Status:           OrderStatusSearching,
		HoldAmount:       0,
		FinalAmount:      0,
		PhotoURL:         photo,
		CreatedAt:        time.Now(),
	}
	if err := r.Create(order); err != nil {
		return nil, err
	}
	return order, nil
}

// GetAvailableAuctionOrders returns open auction orders.
func (r *orderRepo) GetAvailableAuctionOrders() ([]*Order, error) {
	rows, err := r.db.Query(
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
