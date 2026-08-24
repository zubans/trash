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
	LastName               string     `json:"last_name"`
	FirstName              string     `json:"first_name"`
	Patronymic             string     `json:"patronymic"`
	BirthDate              *time.Time `json:"birth_date,omitempty"`
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

func (u *User) GetAge() int {
	if u.BirthDate == nil {
		return 0
	}
	now := time.Now()
	age := now.Year() - u.BirthDate.Year()
	if now.YearDay() < u.BirthDate.YearDay() {
		age--
	}
	return age
}

func (u *User) IsVerified() bool {
	return u.Status == "VERIFIED" || u.EmailVerified
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
	UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error
	UpdateUserBirthDate(userID uuid.UUID, birthDate time.Time) error
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
		ALTER TABLE users ADD COLUMN IF NOT EXISTS last_name VARCHAR(100) NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS first_name VARCHAR(100) NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS patronymic VARCHAR(100) NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS birth_date DATE NULL;
	`)
	return &repo{db: db}
}

func (r *repo) FindByPhone(phone string) (*User, error) {
	var u User
	var email, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE phone = $1`,
		phone,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	if birthDate.Valid {
		u.BirthDate = &birthDate.Time
	}
	return &u, nil
}

func (r *repo) FindByEmail(email string) (*User, error) {
	var u User
	var em, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE LOWER(email) = LOWER($1)`,
		email,
	).Scan(&u.ID, &u.Role, &u.Phone, &em, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = em.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	if birthDate.Valid {
		u.BirthDate = &birthDate.Time
	}
	return &u, nil
}

func (r *repo) FindByEmailVerificationToken(token string) (*User, error) {
	var u User
	var email, tok, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE email_verification_token = $1`,
		token,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &tok, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.EmailVerificationToken = tok.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	if birthDate.Valid {
		u.BirthDate = &birthDate.Time
	}
	return &u, nil
}

func (r *repo) FindByID(id uuid.UUID) (*User, error) {
	var u User
	var email, pendingEmail, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRow(
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, COALESCE(pending_email, ''), email_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &pendingEmail, &u.EmailVerified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.PendingEmail = pendingEmail.String
	u.EmailVerificationToken = token.String
	u.PasswordResetCode = resetCode.String
	if resetExp.Valid {
		u.PasswordResetExpiresAt = &resetExp.Time
	}
	if birthDate.Valid {
		u.BirthDate = &birthDate.Time
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
		`INSERT INTO users (id, role, phone, email, last_name, first_name, patronymic, pending_email, email_verified, email_verification_token, email_token_expires_at, password, balance, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		id, user.Role, user.Phone, user.Email, user.LastName, user.FirstName, user.Patronymic, user.PendingEmail, user.EmailVerified, user.EmailVerificationToken, user.EmailTokenExpiresAt, user.Password, user.Balance, user.Status, time.Now(),
	)
	return err
}

func (r *repo) VerifyEmailToken(token string) (*User, error) {
	var userID uuid.UUID
	err := r.db.QueryRow(
		`UPDATE users 
		 SET email_verified = true, 
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

func (r *repo) UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error) {
	row := r.db.QueryRow(
		`UPDATE users
		 SET email = $1, pending_email = NULL, email_verified = false, email_verification_token = $2, email_token_expires_at = $3
		 WHERE id = $4
		 RETURNING id, role, phone, COALESCE(email, ''), email_verified, email_verification_token, balance, status, created_at`,
		email, verificationToken, expiresAt, userID,
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
	return &u, nil
}

func (r *repo) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error {
	_, err := r.db.Exec(
		`UPDATE users SET last_name = $1, first_name = $2, patronymic = $3 WHERE id = $4`,
		lastName, firstName, patronymic, userID,
	)
	return err
}

func (r *repo) UpdateUserBirthDate(userID uuid.UUID, birthDate time.Time) error {
	_, err := r.db.Exec(
		`UPDATE users SET birth_date = $1 WHERE id = $2`,
		birthDate, userID,
	)
	return err
}
