package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// User represents a user record in the database.
type User struct {
	ID                     uuid.UUID  `json:"id"`
	Role                   string     `json:"role"`
	Phone                  string     `json:"phone"`
	Email                  string     `json:"email"`
	PendingEmail           string     `json:"pending_email,omitempty"`
	EmailVerified          bool       `json:"email_verified"`
	EmailVerificationToken string     `json:"-"`
	EmailTokenExpiresAt    *time.Time `json:"-"`
	PasswordResetCode      string     `json:"-"`
	PasswordResetExpiresAt *time.Time `json:"-"`
	Password               string     `json:"-"` // bcrypt hash, managed by the service layer
	Balance                float64    `json:"balance"`
	Status                 string     `json:"status"`
	CreatedAt              time.Time  `json:"created_at"`
	Address                string     `json:"address,omitempty"`
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
	FindByEmail(email string) (*User, error)
	FindByEmailVerificationToken(token string) (*User, error)
	Create(user *User) error
	FindByID(id uuid.UUID) (*User, error)
	UpdateStatus(id uuid.UUID, status string) error
	UpdateRole(id uuid.UUID, role string) error
	UpdateBalance(id uuid.UUID, balance float64) error
	UpdateLastGeo(id uuid.UUID, lastGeo string) error
	CreateCustomerProfile(userID uuid.UUID, address, lastGeo string) error
	GetCustomerProfile(userID uuid.UUID) (*CustomerProfile, error)
	UpdateCustomerAddress(userID uuid.UUID, address string) error
	VerifyEmailToken(token string) (*User, error)
	SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error
	ResetPasswordWithCode(email, code, newHashedPassword string) (*User, error)
	UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error)
}

// repo implements UserRepository using *sql.DB.
type repo struct {
	db *sql.DB
}

// New creates a new UserRepository backed by the provided database connection.
func New(db *sql.DB) UserRepository {
	_, _ = db.Exec(`
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(255) NULL;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT false;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verification_token VARCHAR(255) NULL;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_code VARCHAR(10) NULL;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS password_reset_expires_at TIMESTAMP WITH TIME ZONE NULL;
	`)
	return &repo{db: db}
}

func (r *repo) FindByPhone(phone string) (*User, error) {
	var u User
	var email, token, resetCode sql.NullString
	var resetExp sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE phone = $1`,
		phone,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	return &u, nil
}

func (r *repo) FindByEmail(email string) (*User, error) {
	var u User
	var em, token, resetCode sql.NullString
	var resetExp sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE LOWER(email) = LOWER($1)`,
		email,
	).Scan(&u.ID, &u.Role, &u.Phone, &em, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = em.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	return &u, nil
}

func (r *repo) FindByEmailVerificationToken(token string) (*User, error) {
	var u User
	var email, tok, resetCode sql.NullString
	var resetExp sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE email_verification_token = $1`,
		token,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.EmailVerified, &tok, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.EmailVerificationToken = tok.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	return &u, nil
}

func (r *repo) FindByID(id uuid.UUID) (*User, error) {
	var u User
	var email, token, resetCode sql.NullString
	var resetExp sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	return &u, nil
}

func (r *repo) Create(user *User) error {
	id := user.ID
	if id == uuid.Nil {
		id = uuid.New()
		user.ID = id
	}
	_, err := r.db.Exec(
		`INSERT INTO users (id, role, phone, email, pending_email, email_verified, email_verification_token, email_token_expires_at, password, balance, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		id, user.Role, user.Phone, user.Email, user.PendingEmail, user.EmailVerified, user.EmailVerificationToken, user.EmailTokenExpiresAt, user.Password, user.Balance, user.Status, time.Now(),
	)
	return err
}

func (r *repo) VerifyEmailToken(token string) (*User, error) {
	var userID uuid.UUID
	err := r.db.QueryRow(
		`UPDATE users 
		 SET email = COALESCE(NULLIF(pending_email, ''), email),
		     pending_email = NULL,
		     email_verified = true, 
		     email_verification_token = NULL,
		     email_token_expires_at = NULL
		 WHERE email_verification_token = $1 AND (email_token_expires_at IS NULL OR email_token_expires_at > now())
		 RETURNING id`,
		token,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var isExpired bool
			errExp := r.db.QueryRow(
				`SELECT EXISTS(SELECT 1 FROM users WHERE email_verification_token = $1 AND email_token_expires_at <= now())`,
				token,
			).Scan(&isExpired)
			if errExp == nil && isExpired {
				return nil, errors.New("verification_token_expired")
			}
			return nil, errors.New("invalid or expired verification token (valid 60m)")
		}
		return nil, err
	}
	return r.FindByID(userID)
}

func (r *repo) SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`UPDATE users SET password_reset_code = $1, password_reset_expires_at = $2 WHERE id = $3`,
		code, expiresAt, userID,
	)
	return err
}

func (r *repo) ResetPasswordWithCode(email, code, newHashedPassword string) (*User, error) {
	var userID uuid.UUID
	err := r.db.QueryRow(
		`UPDATE users 
		 SET password = $1, password_reset_code = NULL, password_reset_expires_at = NULL 
		 WHERE LOWER(email) = LOWER($2) AND password_reset_code = $3 AND password_reset_expires_at > now()
		 RETURNING id`,
		newHashedPassword, email, code,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid or expired reset code")
		}
		return nil, err
	}
	return r.FindByID(userID)
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

func (r *repo) CreateCustomerProfile(userID uuid.UUID, address, lastGeo string) error {
	_, err := r.db.Exec(
		`INSERT INTO customer_profiles (user_id, full_name, address, last_geo)
		 VALUES ($1, '', $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET address = $2, last_geo = $3`,
		userID, address, lastGeo,
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

func (r *repo) UpdateUserEmail(userID uuid.UUID, pendingEmail, verificationToken string, expiresAt time.Time) (*User, error) {
	row := r.db.QueryRow(
		`UPDATE users
		 SET pending_email = $1, email_verification_token = $2, email_token_expires_at = $3
		 WHERE id = $4
		 RETURNING id, role, phone, COALESCE(email, ''), email_verified, email_verification_token, balance, status, created_at`,
		pendingEmail, verificationToken, expiresAt, userID,
	)
	var u User
	var emailStr string
	err := row.Scan(
		&u.ID, &u.Role, &u.Phone, &emailStr, &u.EmailVerified,
		&u.EmailVerificationToken, &u.Balance, &u.Status, &u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.Email = emailStr
	u.PendingEmail = pendingEmail
	return &u, nil
}
