package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// User represents a user record in the database.
type User struct {
	ID        uuid.UUID
	Role      string
	Phone     string
	Password  string // bcrypt hash, managed by the service layer
	Balance   float64
	Status    string
	CreatedAt time.Time
}

// UserRepository defines storage operations for users.
type UserRepository interface {
	FindByPhone(phone string) (*User, error)
	Create(user *User) error
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

func (r *repo) Create(user *User) error {
	_, err := r.db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New(), user.Role, user.Phone, user.Password, user.Balance, user.Status, time.Now(),
	)
	return err
}
