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

// AdminRepository defines admin database operations.
type AdminRepository interface {
	GetUsers(page, limit int, role, status, search string) ([]*User, int, error)
	GetTopUpRequests() ([]*TopUpRequest, error)
	GetTopUpRequestByID(id uuid.UUID) (*TopUpRequest, error)
	CreateTopUpRequest(userID uuid.UUID, amount float64) (*TopUpRequest, error)
	ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error
	RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error
	GetTransactions() ([]*Transaction, error)
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

	// Get paginated list
	listQuery := fmt.Sprintf(
		"SELECT id, role, phone, password, balance, status, created_at FROM users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
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
		err := rows.Scan(&u.ID, &u.Role, &u.Phone, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
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
	return reqs, nil
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
	return txs, nil
}
