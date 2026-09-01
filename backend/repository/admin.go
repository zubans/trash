package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// TopUpRequest represents a manual balance top-up request.
type TopUpRequest struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Populated via JOIN
	Amount    money.Amount `json:"amount"`
	Status    string       `json:"status"`
	AdminID   *uuid.UUID   `json:"admin_id,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt *time.Time   `json:"updated_at,omitempty"`
}

// WithdrawalRequest represents a manual balance withdrawal request.
type WithdrawalRequest struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Populated via JOIN
	Amount    money.Amount `json:"amount"`
	Status    string       `json:"status"`
	AdminID   *uuid.UUID   `json:"admin_id,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt *time.Time   `json:"updated_at,omitempty"`
}

// Transaction represents a financial log entry.
type Transaction struct {
	ID        uuid.UUID    `json:"id"`
	UserID    uuid.UUID    `json:"user_id"`
	UserPhone string       `json:"user_phone"` // Populated via JOIN
	OrderID   *uuid.UUID   `json:"order_id,omitempty"`
	Type      string       `json:"type"`
	Amount    money.Amount `json:"amount"`
	// Counterparty is the system account on the other side of this entry.
	// Empty on rows written before system accounts existed.
	Counterparty string     `json:"counterparty,omitempty"`
	AdminID      *uuid.UUID `json:"admin_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AdminShift extends Shift with executor phone for admin views.
type AdminShift struct {
	Shift
	ExecutorPhone string `json:"executor_phone"`
}

// AdminOrder extends Order with customer/executor phone and service variant name for admin views.
type AdminOrder struct {
	Order
	CustomerPhone      string `json:"customer_phone"`
	ExecutorPhone      string `json:"executor_phone,omitempty"`
	ServiceVariantName string `json:"service_variant_name"`
}

// AdminRepository defines admin database operations.
type AdminRepository interface {
	GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*User, int, error)
	GetTopUpRequests(ctx context.Context, limit, offset int) ([]*TopUpRequest, error)
	GetTopUpRequestByID(ctx context.Context, id uuid.UUID) (*TopUpRequest, error)
	CreateTopUpRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*TopUpRequest, error)
	LockTopUpRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*TopUpRequest, error)
	SetTopUpStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error
	GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*WithdrawalRequest, error)
	GetWithdrawalRequestByID(ctx context.Context, id uuid.UUID) (*WithdrawalRequest, error)
	// Withdrawals are a money workflow and live in AdminService; the repository
	// provides the locked read and the individual writes it needs.
	CreateWithdrawalRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*WithdrawalRequest, error)
	LockWithdrawalRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*WithdrawalRequest, error)
	SetWithdrawalStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error
	HasPendingWithdrawal(ctx context.Context, userID uuid.UUID) (bool, error)
	CountAdmins(ctx context.Context) (int, error)
	GetTransactions(ctx context.Context, limit, offset int) ([]*Transaction, error)
	GetActiveShifts(ctx context.Context) ([]*AdminShift, error)
	GetActiveOrders(ctx context.Context, limit, offset int) ([]*AdminOrder, error)
	GetCompletedOrders(ctx context.Context, f CompletedOrdersFilter) ([]*AdminOrder, int, error)
	CompletedOrderFacets(ctx context.Context) (CompletedOrderFacets, error)
}

// CompletedOrderFacets are the values the completed-orders filters can be set
// to. They are computed over every completed order rather than over the current
// page, so choosing one filter never empties the other.
type CompletedOrderFacets struct {
	Services []string `json:"services"`
	Periods  []string `json:"periods"`
}

// CompletedOrdersFilter describes one page of the completed-orders listing.
// Search, service and period narrow the set; Sort picks the column. All of it
// runs in SQL, so what the admin sees and exports covers every completed order,
// not just the rows that happened to be loaded.
type CompletedOrdersFilter struct {
	Search  string // phone, order id or service name, matched loosely
	Service string // exact service name
	Period  string // YYYY-MM over completed_at
	Sort    string // one of completedOrderSorts; anything else falls back
	Desc    bool
	Limit   int
	Offset  int
}

// completedOrderSorts whitelists what may reach ORDER BY. The key arrives from
// the client, so it must never be interpolated: only these fixed expressions
// can be selected.
var completedOrderSorts = map[string]string{
	"completed_at": "o.completed_at",
	"final_amount": "o.final_amount",
	"service":      "COALESCE(sn.name->>'ru', sn.code)",
	"customer":     "cu.phone",
	"executor":     "eu.phone",
}

type adminRepo struct {
	db *sql.DB
}

// NewAdminRepository creates a repository for admin operations.
func NewAdminRepository(db *sql.DB) AdminRepository {
	return &adminRepo{db: db}
}

func (r *adminRepo) GetUsers(ctx context.Context, page, limit int, role, status, search string) ([]*User, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	offset := (page - 1) * limit

	whereClause := "WHERE 1=1"
	var args []interface{}
	argCount := 1

	if role != "" {
		whereClause += fmt.Sprintf(" AND role = $%d", argCount)
		args = append(args, role)
		argCount++
	}
	if status != "" {
		whereClause += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, status)
		argCount++
	}
	if search != "" {
		whereClause += fmt.Sprintf(" AND phone LIKE $%d", argCount)
		args = append(args, "%"+search+"%")
		argCount++
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated list with the customer's default address. The address now
	// lives in the unified `addresses` table (migration 037 dropped
	// customer_profiles.address), so it is read from there. The multi-role set is
	// fetched separately (attachRoles) so a missing user_roles table can never
	// take down the whole admin listing.
	listQuery := fmt.Sprintf(
		`SELECT u.id, u.role, u.phone, u.balance, u.status, u.is_verified, u.created_at,
		        COALESCE((SELECT a.address FROM addresses a WHERE a.user_id = u.id AND a.is_default LIMIT 1), '') AS address,
		        COALESCE(u.last_name, ''), COALESCE(u.first_name, ''), COALESCE(u.patronymic, ''), u.birth_date
		 FROM users u
		 %s ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argCount, argCount+1,
	)
	queryArgs := append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, listQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var birthDate sql.NullTime
		// The password hash is deliberately not selected: it has no use in an
		// admin listing and must not travel through the application at all.
		err := rows.Scan(&u.ID, &u.Role, &u.Phone, &u.Balance, &u.Status, &u.Verified, &u.CreatedAt, &u.Address,
			&u.LastName, &u.FirstName, &u.Patronymic, &birthDate)
		if err != nil {
			return nil, 0, err
		}
		if birthDate.Valid {
			bd := birthDate.Time
			u.BirthDate = &bd
		}
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	r.attachRoles(ctx, users)

	return users, total, nil
}

// attachRoles populates each user's multi-role set from user_roles. It is
// deliberately best-effort: any failure (most importantly the table not existing
// yet because migration 039 has not run) is swallowed and leaves Roles empty, so
// the admin user list still renders — the client falls back to the primary role.
func (r *adminRepo) attachRoles(ctx context.Context, users []*User) {
	if len(users) == 0 {
		return
	}
	placeholders := make([]string, len(users))
	args := make([]interface{}, len(users))
	byID := make(map[uuid.UUID]*User, len(users))
	for i, u := range users {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = u.ID
		byID[u.ID] = u
	}
	query := "SELECT user_id, role FROM user_roles WHERE user_id IN (" + strings.Join(placeholders, ", ") + ") ORDER BY role"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid uuid.UUID
		var role string
		if err := rows.Scan(&uid, &role); err != nil {
			return
		}
		if u, ok := byID[uid]; ok {
			u.Roles = append(u.Roles, role)
		}
	}
}

func (r *adminRepo) GetTopUpRequests(ctx context.Context, limit, offset int) ([]*TopUpRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_topup_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []*TopUpRequest
	for rows.Next() {
		var req TopUpRequest
		err := rows.Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, &req)
	}
	return reqs, rows.Err()
}

func (r *adminRepo) GetTopUpRequestByID(ctx context.Context, id uuid.UUID) (*TopUpRequest, error) {
	var req TopUpRequest
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_topup_requests r
		JOIN users u ON r.user_id = u.id
		WHERE r.id = $1`
	err := r.db.QueryRowContext(ctx, query, id).Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *adminRepo) CreateTopUpRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*TopUpRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_topup_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req TopUpRequest
	err := r.exec(ctx, q).QueryRowContext(ctx, query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// LockTopUpRequest reads a request taking a row lock, so two admins deciding at
// the same time serialise instead of both crediting the balance.
func (r *adminRepo) LockTopUpRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*TopUpRequest, error) {
	var req TopUpRequest
	err := r.exec(ctx, q).QueryRowContext(ctx, `
		SELECT id, user_id, amount, status, admin_id, created_at, updated_at
		FROM balance_topup_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(
		&req.ID, &req.UserID, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// SetTopUpStatus decides a pending request; the guard keeps a second decision
// from crediting the balance twice.
func (r *adminRepo) SetTopUpStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error {
	return execExpectingOne(ctx, r.exec(ctx, q), `
		UPDATE balance_topup_requests
		SET status = $1::topup_status, admin_id = $2, updated_at = now()
		WHERE id = $3 AND status = 'PENDING'`, status, adminID, requestID)
}

func (r *adminRepo) GetWithdrawalRequests(ctx context.Context, limit, offset int) ([]*WithdrawalRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_withdrawal_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []*WithdrawalRequest
	for rows.Next() {
		var req WithdrawalRequest
		err := rows.Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, &req)
	}
	return reqs, rows.Err()
}

func (r *adminRepo) GetWithdrawalRequestByID(ctx context.Context, id uuid.UUID) (*WithdrawalRequest, error) {
	var req WithdrawalRequest
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_withdrawal_requests r
		JOIN users u ON r.user_id = u.id
		WHERE r.id = $1`
	err := r.db.QueryRowContext(ctx, query, id).Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *adminRepo) exec(ctx context.Context, q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *adminRepo) CreateWithdrawalRequest(ctx context.Context, q Querier, userID uuid.UUID, amount money.Amount) (*WithdrawalRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_withdrawal_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req WithdrawalRequest
	err := r.exec(ctx, q).QueryRowContext(ctx, query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// LockWithdrawalRequest reads a request taking a row lock, so two admins acting
// at the same time serialise instead of both seeing it as PENDING.
func (r *adminRepo) LockWithdrawalRequest(ctx context.Context, q Querier, requestID uuid.UUID) (*WithdrawalRequest, error) {
	var req WithdrawalRequest
	err := r.exec(ctx, q).QueryRowContext(ctx, `
		SELECT id, user_id, amount, status, admin_id, created_at, updated_at
		FROM balance_withdrawal_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(
		&req.ID, &req.UserID, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// SetWithdrawalStatus decides a pending request. The guard makes a second
// decision on the same request fail instead of overwriting the first.
func (r *adminRepo) SetWithdrawalStatus(ctx context.Context, q Querier, requestID, adminID uuid.UUID, status string) error {
	return execExpectingOne(ctx, r.exec(ctx, q), `
		UPDATE balance_withdrawal_requests
		SET status = $1::withdrawal_status, admin_id = $2, updated_at = now()
		WHERE id = $3 AND status = 'PENDING'`, status, adminID, requestID)
}

// HasPendingWithdrawal reports whether the user already has an open request.
// Requests do not reserve funds, so several open ones for the same balance
// would leave the admin approving payouts that cannot all be honoured.
func (r *adminRepo) HasPendingWithdrawal(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM balance_withdrawal_requests WHERE user_id = $1 AND status = 'PENDING')`,
		userID,
	).Scan(&exists)
	return exists, err
}

// CountAdmins is used to keep the last administrator from being demoted.
func (r *adminRepo) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'ADMIN'`).Scan(&count)
	return count, err
}

func (r *adminRepo) GetTransactions(ctx context.Context, limit, offset int) ([]*Transaction, error) {
	query := `
		SELECT t.id, t.user_id, u.phone, t.order_id, t.type, t.amount, t.admin_id, t.created_at
		FROM transactions t
		JOIN users u ON t.user_id = u.id
		ORDER BY t.created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []*Transaction
	for rows.Next() {
		var tx Transaction
		err := rows.Scan(&tx.ID, &tx.UserID, &tx.UserPhone, &tx.OrderID, &tx.Type, &tx.Amount, &tx.AdminID, &tx.CreatedAt)
		if err != nil {
			return nil, err
		}
		txs = append(txs, &tx)
	}
	return txs, rows.Err()
}

func (r *adminRepo) GetActiveShifts(ctx context.Context) ([]*AdminShift, error) {
	query := `
		SELECT s.id, s.executor_id, s.duration_hours, s.started_at, s.planned_end_at, s.actual_end_at, s.status, s.fine_amount,
		       u.phone
		FROM shifts s
		JOIN users u ON s.executor_id = u.id
		WHERE s.status = $1
		ORDER BY s.started_at DESC`

	rows, err := r.db.QueryContext(ctx, query, ShiftStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shifts []*AdminShift
	for rows.Next() {
		var s AdminShift
		err := rows.Scan(
			&s.ID, &s.ExecutorID, &s.DurationHours, &s.StartedAt, &s.PlannedEndAt, &s.ActualEndAt, &s.Status, &s.FineAmount,
			&s.ExecutorPhone,
		)
		if err != nil {
			return nil, err
		}
		shifts = append(shifts, &s)
	}
	return shifts, rows.Err()
}

func (r *adminRepo) GetActiveOrders(ctx context.Context, limit, offset int) ([]*AdminOrder, error) {
	query := `
		SELECT o.id, o.customer_id, o.executor_id, o.service_variant_id, o.is_urgent, o.is_asap, o.status,
		       o.hold_amount, o.final_amount, o.is_downgraded, o.photo_url, o.address, o.pickup_lat, o.pickup_lon,
		       o.created_at, o.assigned_at, o.deadline_at, o.completed_at, o.canceled_at,
		       cu.phone, eu.phone, COALESCE(sn.name->>'ru', sn.code)
		FROM orders o
		JOIN users cu ON o.customer_id = cu.id
		LEFT JOIN users eu ON o.executor_id = eu.id
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.status IN ($1, $2)
		ORDER BY o.created_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.db.QueryContext(ctx, query, OrderStatusSearching, OrderStatusAssigned, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []*AdminOrder
	for rows.Next() {
		var o AdminOrder
		err := rows.Scan(
			&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
			&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address, &o.PickupLat, &o.PickupLon,
			&o.CreatedAt, &o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
			&o.CustomerPhone, &o.ExecutorPhone, &o.ServiceVariantName,
		)
		if err != nil {
			return nil, err
		}
		orders = append(orders, &o)
	}
	return orders, rows.Err()
}

func (r *adminRepo) GetCompletedOrders(ctx context.Context, f CompletedOrdersFilter) ([]*AdminOrder, int, error) {
	where := "WHERE o.status = $1"
	args := []interface{}{OrderStatusCompleted}

	if search := strings.TrimSpace(f.Search); search != "" {
		// A phone is stored as +79997454656 but typed as "+7 (999) 745-46-56"
		// or just "9997": both sides are reduced to digits before comparing, so
		// the admin does not have to reproduce the stored spelling. Digits are
		// only used when the term actually has some — otherwise every row would
		// match the empty string.
		digits := digitsOnly(search)
		args = append(args, "%"+search+"%")
		like := fmt.Sprintf("$%d", len(args))
		conds := []string{
			fmt.Sprintf("o.id::text ILIKE %s", like),
			fmt.Sprintf("COALESCE(sn.name->>'ru', sn.code) ILIKE %s", like),
			fmt.Sprintf("COALESCE(o.address, '') ILIKE %s", like),
		}
		if digits != "" {
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

	if period := strings.TrimSpace(f.Period); period != "" {
		args = append(args, period)
		where += fmt.Sprintf(" AND to_char(o.completed_at, 'YYYY-MM') = $%d", len(args))
	}

	from := `
		FROM orders o
		JOIN users cu ON o.customer_id = cu.id
		LEFT JOIN users eu ON o.executor_id = eu.id
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		` + where

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) "+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortExpr, ok := completedOrderSorts[f.Sort]
	if !ok {
		sortExpr = completedOrderSorts["completed_at"]
	}
	direction := "ASC"
	if f.Desc {
		direction = "DESC"
	}

	args = append(args, f.Limit, f.Offset)
	query := fmt.Sprintf(`
		SELECT o.id, o.customer_id, o.executor_id, o.service_variant_id, o.is_urgent, o.is_asap, o.status,
		       o.hold_amount, o.final_amount, o.is_downgraded, o.photo_url, o.address, o.pickup_lat, o.pickup_lon,
		       o.created_at, o.assigned_at, o.deadline_at, o.completed_at, o.canceled_at,
		       cu.phone, eu.phone, COALESCE(sn.name->>'ru', sn.code)
		%s
		ORDER BY %s %s NULLS LAST, o.created_at DESC
		LIMIT $%d OFFSET $%d`, from, sortExpr, direction, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []*AdminOrder
	for rows.Next() {
		var o AdminOrder
		err := rows.Scan(
			&o.ID, &o.CustomerID, &o.ExecutorID, &o.ServiceVariantID, &o.IsUrgent, &o.IsAsap, &o.Status,
			&o.HoldAmount, &o.FinalAmount, &o.IsDowngraded, &o.PhotoURL, &o.Address, &o.PickupLat, &o.PickupLon,
			&o.CreatedAt, &o.AssignedAt, &o.DeadlineAt, &o.CompletedAt, &o.CanceledAt,
			&o.CustomerPhone, &o.ExecutorPhone, &o.ServiceVariantName,
		)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, &o)
	}
	return orders, total, rows.Err()
}

func (r *adminRepo) CompletedOrderFacets(ctx context.Context) (CompletedOrderFacets, error) {
	facets := CompletedOrderFacets{Services: []string{}, Periods: []string{}}

	serviceRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT COALESCE(sn.name->>'ru', sn.code) AS name
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.status = $1
		ORDER BY name`, OrderStatusCompleted)
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
		SELECT DISTINCT to_char(completed_at, 'YYYY-MM') AS period
		FROM orders
		WHERE status = $1 AND completed_at IS NOT NULL
		ORDER BY period DESC`, OrderStatusCompleted)
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

// digitsOnly keeps the digits of a search term so a typed phone matches a
// stored one whatever punctuation either side uses.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
