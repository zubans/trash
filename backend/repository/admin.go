package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TopUpRequest represents a manual balance top-up request.
type TopUpRequest struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	UserPhone string     `json:"user_phone"` // Populated via JOIN
	Amount    float64    `json:"amount"`
	Status    string     `json:"status"`
	AdminID   *uuid.UUID `json:"admin_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// WithdrawalRequest represents a manual balance withdrawal request.
type WithdrawalRequest struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	UserPhone string     `json:"user_phone"` // Populated via JOIN
	Amount    float64    `json:"amount"`
	Status    string     `json:"status"`
	AdminID   *uuid.UUID `json:"admin_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// Transaction represents a financial log entry.
type Transaction struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	UserPhone string     `json:"user_phone"` // Populated via JOIN
	OrderID   *uuid.UUID `json:"order_id,omitempty"`
	Type      string     `json:"type"`
	Amount    float64    `json:"amount"`
	AdminID   *uuid.UUID `json:"admin_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
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
	GetUsers(page, limit int, role, status, search string) ([]*User, int, error)
	GetTopUpRequests() ([]*TopUpRequest, error)
	GetTopUpRequestByID(id uuid.UUID) (*TopUpRequest, error)
	CreateTopUpRequest(userID uuid.UUID, amount float64) (*TopUpRequest, error)
	ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error
	RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error
	GetWithdrawalRequests() ([]*WithdrawalRequest, error)
	GetWithdrawalRequestByID(id uuid.UUID) (*WithdrawalRequest, error)
	CreateWithdrawalRequest(userID uuid.UUID, amount float64) (*WithdrawalRequest, error)
	HasPendingWithdrawal(userID uuid.UUID) (bool, error)
	CountAdmins() (int, error)
	ApproveWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error
	RejectWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error
	TopUpUserBalance(userID, adminID uuid.UUID, amount float64) error
	GetTransactions() ([]*Transaction, error)
	GetActiveShifts() ([]*AdminShift, error)
	GetActiveOrders() ([]*AdminOrder, error)
	GetCompletedOrders() ([]*AdminOrder, error)
}

type adminRepo struct {
	db *sql.DB
}

// NewAdminRepository creates a repository for admin operations.
func NewAdminRepository(db *sql.DB) AdminRepository {
	return &adminRepo{db: db}
}

func (r *adminRepo) GetUsers(page, limit int, role, status, search string) ([]*User, int, error) {
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
	err := r.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated list with customer address
	listQuery := fmt.Sprintf(
		`SELECT u.id, u.role, u.phone, u.balance, u.status, u.created_at, COALESCE(cp.address, '') as address
		 FROM users u
		 LEFT JOIN customer_profiles cp ON cp.user_id = u.id
		 %s ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, argCount, argCount+1,
	)
	queryArgs := append(args, limit, offset)

	rows, err := r.db.Query(listQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		// The password hash is deliberately not selected: it has no use in an
		// admin listing and must not travel through the application at all.
		err := rows.Scan(&u.ID, &u.Role, &u.Phone, &u.Balance, &u.Status, &u.CreatedAt, &u.Address)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, &u)
	}

	return users, total, nil
}

func (r *adminRepo) GetTopUpRequests() ([]*TopUpRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_topup_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC`

	rows, err := r.db.Query(query)
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

func (r *adminRepo) GetTopUpRequestByID(id uuid.UUID) (*TopUpRequest, error) {
	var req TopUpRequest
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_topup_requests r
		JOIN users u ON r.user_id = u.id
		WHERE r.id = $1`
	err := r.db.QueryRow(query, id).Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *adminRepo) CreateTopUpRequest(userID uuid.UUID, amount float64) (*TopUpRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_topup_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req TopUpRequest
	err := r.db.QueryRow(query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *adminRepo) ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Lock request row for update and check status and amount
	var status string
	var amount float64
	var userID uuid.UUID
	queryLock := `
		SELECT status, amount, user_id
		FROM balance_topup_requests
		WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(queryLock, requestID).Scan(&status, &amount, &userID)
	if err != nil {
		return err
	}

	if status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	// 2. Update status of the request
	queryUpdateReq := `
		UPDATE balance_topup_requests
		SET status = 'APPROVED', admin_id = $1, updated_at = now()
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateReq, adminID, requestID)
	if err != nil {
		return err
	}

	// 3. Update user's balance
	queryUpdateUser := `
		UPDATE users
		SET balance = balance + $1
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateUser, amount, userID)
	if err != nil {
		return err
	}

	// 4. Log the transaction
	queryLogTx := `
		INSERT INTO transactions (user_id, type, amount, admin_id, created_at)
		VALUES ($1, 'TOP_UP', $2, $3, now())`
	_, err = tx.Exec(queryLogTx, userID, amount, adminID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *adminRepo) RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	queryLock := `
		SELECT status
		FROM balance_topup_requests
		WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(queryLock, requestID).Scan(&status)
	if err != nil {
		return err
	}

	if status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	queryUpdateReq := `
		UPDATE balance_topup_requests
		SET status = 'REJECTED', admin_id = $1, updated_at = now()
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateReq, adminID, requestID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *adminRepo) GetWithdrawalRequests() ([]*WithdrawalRequest, error) {
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_withdrawal_requests r
		JOIN users u ON r.user_id = u.id
		ORDER BY r.created_at DESC`

	rows, err := r.db.Query(query)
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

func (r *adminRepo) GetWithdrawalRequestByID(id uuid.UUID) (*WithdrawalRequest, error) {
	var req WithdrawalRequest
	query := `
		SELECT r.id, r.user_id, u.phone, r.amount, r.status, r.admin_id, r.created_at, r.updated_at
		FROM balance_withdrawal_requests r
		JOIN users u ON r.user_id = u.id
		WHERE r.id = $1`
	err := r.db.QueryRow(query, id).Scan(&req.ID, &req.UserID, &req.UserPhone, &req.Amount, &req.Status, &req.AdminID, &req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *adminRepo) CreateWithdrawalRequest(userID uuid.UUID, amount float64) (*WithdrawalRequest, error) {
	id := uuid.New()
	query := `
		INSERT INTO balance_withdrawal_requests (id, user_id, amount, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		RETURNING id, user_id, amount, status, created_at`

	var req WithdrawalRequest
	err := r.db.QueryRow(query, id, userID, amount).Scan(&req.ID, &req.UserID, &req.Amount, &req.Status, &req.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// HasPendingWithdrawal reports whether the user already has an open request.
// Requests do not reserve funds, so several open ones for the same balance
// would leave the admin approving payouts that cannot all be honoured.
func (r *adminRepo) HasPendingWithdrawal(userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM balance_withdrawal_requests WHERE user_id = $1 AND status = 'PENDING')`,
		userID,
	).Scan(&exists)
	return exists, err
}

// CountAdmins is used to keep the last administrator from being demoted.
func (r *adminRepo) CountAdmins() (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'ADMIN'`).Scan(&count)
	return count, err
}

func (r *adminRepo) ApproveWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Lock request row and check status/amount
	var status string
	var amount float64
	var userID uuid.UUID
	queryLock := `
		SELECT status, amount, user_id
		FROM balance_withdrawal_requests
		WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(queryLock, requestID).Scan(&status, &amount, &userID)
	if err != nil {
		return err
	}

	if status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	// 2. Lock user balance to ensure sufficient funds
	var userBalance float64
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&userBalance)
	if err != nil {
		return err
	}
	if userBalance < amount {
		return errors.New("insufficient balance for withdrawal")
	}

	// 3. Update status of the request
	queryUpdateReq := `
		UPDATE balance_withdrawal_requests
		SET status = 'APPROVED', admin_id = $1, updated_at = now()
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateReq, adminID, requestID)
	if err != nil {
		return err
	}

	// 4. Deduct user's balance
	queryUpdateUser := `
		UPDATE users
		SET balance = balance - $1
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateUser, amount, userID)
	if err != nil {
		return err
	}

	// 5. Log the transaction
	queryLogTx := `
		INSERT INTO transactions (user_id, type, amount, admin_id, created_at)
		VALUES ($1, 'WITHDRAWAL', $2, $3, now())`
	_, err = tx.Exec(queryLogTx, userID, amount, adminID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *adminRepo) RejectWithdrawalRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	queryLock := `
		SELECT status
		FROM balance_withdrawal_requests
		WHERE id = $1 FOR UPDATE`
	err = tx.QueryRow(queryLock, requestID).Scan(&status)
	if err != nil {
		return err
	}

	if status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	queryUpdateReq := `
		UPDATE balance_withdrawal_requests
		SET status = 'REJECTED', admin_id = $1, updated_at = now()
		WHERE id = $2`
	_, err = tx.Exec(queryUpdateReq, adminID, requestID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *adminRepo) TopUpUserBalance(userID, adminID uuid.UUID, amount float64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update user balance
	_, err = tx.Exec(`UPDATE users SET balance = balance + $1 WHERE id = $2`, amount, userID)
	if err != nil {
		return err
	}

	// Log the transaction
	_, err = tx.Exec(`
		INSERT INTO transactions (user_id, type, amount, admin_id, created_at)
		VALUES ($1, 'TOP_UP', $2, $3, now())`, userID, amount, adminID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *adminRepo) GetTransactions() ([]*Transaction, error) {
	query := `
		SELECT t.id, t.user_id, u.phone, t.order_id, t.type, t.amount, t.admin_id, t.created_at
		FROM transactions t
		JOIN users u ON t.user_id = u.id
		ORDER BY t.created_at DESC`

	rows, err := r.db.Query(query)
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

func (r *adminRepo) GetActiveShifts() ([]*AdminShift, error) {
	query := `
		SELECT s.id, s.executor_id, s.duration_hours, s.started_at, s.planned_end_at, s.actual_end_at, s.status, s.fine_amount,
		       u.phone
		FROM shifts s
		JOIN users u ON s.executor_id = u.id
		WHERE s.status = $1
		ORDER BY s.started_at DESC`

	rows, err := r.db.Query(query, ShiftStatusActive)
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

func (r *adminRepo) GetActiveOrders() ([]*AdminOrder, error) {
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
		ORDER BY o.created_at DESC`

	rows, err := r.db.Query(query, OrderStatusSearching, OrderStatusAssigned)
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

func (r *adminRepo) GetCompletedOrders() ([]*AdminOrder, error) {
	query := `
		SELECT o.id, o.customer_id, o.executor_id, o.service_variant_id, o.is_urgent, o.is_asap, o.status,
		       o.hold_amount, o.final_amount, o.is_downgraded, o.photo_url, o.address, o.pickup_lat, o.pickup_lon,
		       o.created_at, o.assigned_at, o.deadline_at, o.completed_at, o.canceled_at,
		       cu.phone, eu.phone, COALESCE(sn.name->>'ru', sn.code)
		FROM orders o
		JOIN users cu ON o.customer_id = cu.id
		LEFT JOIN users eu ON o.executor_id = eu.id
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.status = $1
		ORDER BY o.completed_at DESC, o.created_at DESC`

	rows, err := r.db.Query(query, OrderStatusCompleted)
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
