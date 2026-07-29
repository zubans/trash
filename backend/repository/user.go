package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// User represents a user record in the database.
type User struct {
	ID        uuid.UUID `json:"id"`
	Role      string    `json:"role"`
	Phone     string    `json:"phone"`
	Password  string    `json:"-"` // bcrypt hash, managed by the service layer
	Balance   float64   `json:"balance"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	Address   string    `json:"address,omitempty"`
}

// CustomerProfile holds customer-specific profile data.
type CustomerProfile struct {
	UserID   uuid.UUID      `json:"user_id"`
	FullName string         `json:"full_name"`
	Address  string         `json:"address"`
	LastGeo  sql.NullString `json:"last_geo"`
}

// UserRepository defines storage operations for users.
type UserRepository interface {
	FindByPhone(phone string) (*User, error)
	Create(user *User) error
	FindByID(id uuid.UUID) (*User, error)
	UpdateStatus(id uuid.UUID, status string) error
	UpdateRole(id uuid.UUID, role string) error
	UpdateBalance(id uuid.UUID, balance float64) error
	UpdateLastGeo(id uuid.UUID, lastGeo string) error
	CreateCustomerProfile(userID uuid.UUID, address string) error
	GetCustomerProfile(userID uuid.UUID) (*CustomerProfile, error)
	UpdateCustomerAddress(userID uuid.UUID, address string) error
}

// repo implements UserRepository using *sql.DB.
type repo struct {
	db *sql.DB
}

// New creates a new UserRepository backed by the provided database connection.
func New(db *sql.DB) UserRepository {
	return &repo{db: db}
}

func (r *repo) FindByPhone(phone string) (*User, error) {
	var u User
	err := r.db.QueryRow(
		`SELECT id, role, phone, password, balance, status, created_at FROM users WHERE phone = $1`,
		phone,
	).Scan(&u.ID, &u.Role, &u.Phone, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repo) FindByID(id uuid.UUID) (*User, error) {
	var u User
	err := r.db.QueryRow(
		`SELECT id, role, phone, password, balance, status, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Role, &u.Phone, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *repo) Create(user *User) error {
	_, err := r.db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), user.Role, user.Phone, user.Password, user.Balance, user.Status, time.Now(),
	)
	return err
}

func (r *repo) UpdateStatus(id uuid.UUID, status string) error {
	_, err := r.db.Exec(`UPDATE users SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (r *repo) UpdateRole(id uuid.UUID, role string) error {
	_, err := r.db.Exec(`UPDATE users SET role = $1 WHERE id = $2`, role, id)
	return err
}

func (r *repo) UpdateBalance(id uuid.UUID, balance float64) error {
	_, err := r.db.Exec(`UPDATE users SET balance = $1 WHERE id = $2`, balance, id)
	return err
}

func (r *repo) UpdateLastGeo(id uuid.UUID, lastGeo string) error {
	_, err := r.db.Exec(
		`INSERT INTO customer_profiles (user_id, full_name, address, last_geo)
		 VALUES ($1, '', '', $2)
		 ON CONFLICT (user_id) DO UPDATE SET last_geo = $2`,
		id, lastGeo,
	)
	return err
}

func (r *repo) CreateCustomerProfile(userID uuid.UUID, address string) error {
	_, err := r.db.Exec(
		`INSERT INTO customer_profiles (user_id, full_name, address)
		 VALUES ($1, '', $2)
		 ON CONFLICT (user_id) DO UPDATE SET address = $2`,
		userID, address,
	)
	return err
}

func (r *repo) GetCustomerProfile(userID uuid.UUID) (*CustomerProfile, error) {
	var p CustomerProfile
	err := r.db.QueryRow(
		`SELECT user_id, full_name, address, last_geo FROM customer_profiles WHERE user_id = $1`,
		userID,
	).Scan(&p.UserID, &p.FullName, &p.Address, &p.LastGeo)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &CustomerProfile{UserID: userID}, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *repo) UpdateCustomerAddress(userID uuid.UUID, address string) error {
	_, err := r.db.Exec(
		`INSERT INTO customer_profiles (user_id, full_name, address)
		 VALUES ($1, '', $2)
		 ON CONFLICT (user_id) DO UPDATE SET address = $2`,
		userID, address,
	)
	return err
}
