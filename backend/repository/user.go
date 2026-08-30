package repository

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// User represents a user record in the database.
type User struct {
	ID                     uuid.UUID    `json:"id"`
	Role                   string       `json:"role"`
	Phone                  string       `json:"phone"`
	Email                  string       `json:"email"`
	LastName               string       `json:"last_name"`
	FirstName              string       `json:"first_name"`
	Patronymic             string       `json:"patronymic"`
	BirthDate              *time.Time   `json:"birth_date,omitempty"`
	PendingEmail           string       `json:"pending_email,omitempty"`
	EmailVerified          bool         `json:"email_verified"`
	Verified               bool         `json:"is_verified"`
	EmailVerificationToken string       `json:"-"`
	EmailTokenExpiresAt    *time.Time   `json:"-"`
	PasswordResetCode      string       `json:"-"`
	PasswordResetExpiresAt *time.Time   `json:"-"`
	Password               string       `json:"-"` // bcrypt hash, managed by the service layer
	Balance                money.Amount `json:"balance"`
	Status                 string       `json:"status"`
	CreatedAt              time.Time    `json:"created_at"`
	Address                string       `json:"address,omitempty"`
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

// IsVerified reports whether an admin has manually verified this user. It is
// deliberately independent of EmailVerified: confirming an email proves address
// ownership, not that the account is trusted. Every eligibility gate — customer
// order visibility and service variants that set requires_verification — reads
// this single flag.
func (u *User) IsVerified() bool {
	return u.Verified
}

// CustomerProfile holds customer-specific profile data.
type CustomerProfile struct {
	UserID   uuid.UUID `json:"user_id"`
	FullName string    `json:"full_name"`
	DeviceOS string    `json:"device_os,omitempty"`
	DeviceID string    `json:"device_id,omitempty"`
	DeviceIP string    `json:"device_ip,omitempty"`
}

// UserRepository defines storage operations for users.
type UserRepository interface {
	FindByPhone(ctx context.Context, phone string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByEmailVerificationToken(ctx context.Context, token string) (*User, error)
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateRole(ctx context.Context, id uuid.UUID, role string) error
	UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error
	UpdateBalance(ctx context.Context, id uuid.UUID, balance money.Amount) error
	CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error
	GetCustomerProfile(ctx context.Context, userID uuid.UUID) (*CustomerProfile, error)
	VerifyEmailToken(ctx context.Context, token string) (*User, error)
	UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error
	SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error
	ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*User, error)
	UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error)
	UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error
	UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error
}

// repo implements UserRepository using *sql.DB.
type repo struct {
	db *sql.DB
}

// New creates a new UserRepository backed by the provided database connection.
func New(db *sql.DB) UserRepository {
	return &repo{db: db}
}

func (r *repo) FindByPhone(ctx context.Context, phone string) (*User, error) {
	var u User
	var email, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	cleanDigits := regexp.MustCompile(`[^0-9]`).ReplaceAllString(phone, "")
	err := r.db.QueryRowContext(ctx,
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, is_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at
		 FROM users
		 WHERE phone = $1
		    OR ($2 != '' AND REGEXP_REPLACE(phone, '[^0-9]', '', 'g') = $2)
		 ORDER BY created_at ASC LIMIT 1`,
		phone,
		cleanDigits,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &u.Verified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
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

func (r *repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	var em, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, is_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE LOWER(email) = LOWER($1)`,
		email,
	).Scan(&u.ID, &u.Role, &u.Phone, &em, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &u.Verified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
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

func (r *repo) FindByEmailVerificationToken(ctx context.Context, token string) (*User, error) {
	var u User
	var email, tok, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, email_verified, is_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE email_verification_token = $1`,
		token,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &u.EmailVerified, &u.Verified, &tok, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
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

func (r *repo) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	var email, pendingEmail, token, resetCode sql.NullString
	var resetExp, birthDate sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, role, phone, COALESCE(email, ''), COALESCE(last_name, ''), COALESCE(first_name, ''), COALESCE(patronymic, ''), birth_date, COALESCE(pending_email, ''), email_verified, is_verified, COALESCE(email_verification_token, ''), COALESCE(password_reset_code, ''), password_reset_expires_at, password, balance, status, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Role, &u.Phone, &email, &u.LastName, &u.FirstName, &u.Patronymic, &birthDate, &pendingEmail, &u.EmailVerified, &u.Verified, &token, &resetCode, &resetExp, &u.Password, &u.Balance, &u.Status, &u.CreatedAt)
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

func (r *repo) Create(ctx context.Context, user *User) error {
	id := user.ID
	if id == uuid.Nil {
		id = uuid.New()
		user.ID = id
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, role, phone, email, last_name, first_name, patronymic, pending_email, email_verified, is_verified, email_verification_token, email_token_expires_at, password, balance, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		id, user.Role, user.Phone, user.Email, user.LastName, user.FirstName, user.Patronymic, user.PendingEmail, user.EmailVerified, user.Verified, user.EmailVerificationToken, user.EmailTokenExpiresAt, user.Password, user.Balance, user.Status, time.Now(),
	)
	return err
}

func (r *repo) VerifyEmailToken(ctx context.Context, token string) (*User, error) {
	var userID uuid.UUID
	err := r.db.QueryRowContext(ctx,
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
			errExp := r.db.QueryRowContext(ctx,
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
	return r.FindByID(ctx, userID)
}

// UpdatePassword replaces the stored hash and clears any pending reset code, so
// a code issued before the change cannot be used afterwards.
func (r *repo) UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE users SET password = $1, password_reset_code = NULL,
		    password_reset_expires_at = NULL, password_reset_attempts = 0
		 WHERE id = $2`,
		newHashedPassword, userID)
}

func (r *repo) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	// A fresh code resets the attempt counter.
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password_reset_code = $1, password_reset_expires_at = $2, password_reset_attempts = 0 WHERE id = $3`,
		code, expiresAt, userID,
	)
	return err
}

// maxResetAttempts limits how many codes may be tried for one reset request.
// Without it a numeric code can simply be enumerated inside its validity window.
const maxResetAttempts = 5

// ResetPasswordWithCode verifies the code under a row lock and counts failed
// attempts, invalidating the code once the limit is reached.
func (r *repo) ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		userID     uuid.UUID
		storedCode sql.NullString
		expiresAt  sql.NullTime
		attempts   int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, password_reset_code, password_reset_expires_at, COALESCE(password_reset_attempts, 0)
		 FROM users WHERE LOWER(email) = LOWER($1) FOR UPDATE`,
		email,
	).Scan(&userID, &storedCode, &expiresAt, &attempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("неверный или истекший код сброса")
		}
		return nil, err
	}

	invalidate := func() error {
		_, err := tx.ExecContext(ctx,
			`UPDATE users SET password_reset_code = NULL, password_reset_expires_at = NULL, password_reset_attempts = 0 WHERE id = $1`,
			userID,
		)
		return err
	}

	if !storedCode.Valid || storedCode.String == "" || !expiresAt.Valid || expiresAt.Time.Before(time.Now()) {
		return nil, errors.New("неверный или истекший код сброса")
	}

	if attempts >= maxResetAttempts {
		if err := invalidate(); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errors.New("превышено число попыток, запросите новый код")
	}

	if subtle.ConstantTimeCompare([]byte(storedCode.String), []byte(code)) != 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_reset_attempts = COALESCE(password_reset_attempts, 0) + 1 WHERE id = $1`, userID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errors.New("неверный или истекший код сброса")
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users
		 SET password = $1, password_reset_code = NULL, password_reset_expires_at = NULL, password_reset_attempts = 0
		 WHERE id = $2`,
		newHashedPassword, userID,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.FindByID(ctx, userID)
}

func (r *repo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (r *repo) UpdateRole(ctx context.Context, id uuid.UUID, role string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, id)
	return err
}

// UpdateVerified sets the manual verification flag. This is the only writer of
// users.is_verified; it is reached exclusively through the admin endpoint.
func (r *repo) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET is_verified = $1 WHERE id = $2`, verified, id)
	return err
}

func (r *repo) UpdateBalance(ctx context.Context, id uuid.UUID, balance money.Amount) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET balance = $1 WHERE id = $2`, balance, id)
	return err
}

func (r *repo) CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO customer_profiles (user_id, full_name)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET full_name = $2`,
		userID, fullName,
	)
	return err
}

func (r *repo) GetCustomerProfile(ctx context.Context, userID uuid.UUID) (*CustomerProfile, error) {
	var p CustomerProfile
	var devOS, devID, devIP sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id, full_name, device_os, device_id, device_ip FROM customer_profiles WHERE user_id = $1`,
		userID,
	).Scan(&p.UserID, &p.FullName, &devOS, &devID, &devIP)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &CustomerProfile{UserID: userID}, nil
		}
		return nil, err
	}
	p.DeviceOS = devOS.String
	p.DeviceID = devID.String
	p.DeviceIP = devIP.String
	return &p, nil
}

// UpdateUserEmail records a requested address as pending and sends the user a
// verification link. The current address stays in place until the new one is
// confirmed: writing it straight into email meant an unverified address became
// the account's address immediately, which let somebody occupy an address that
// its real owner had not registered yet, and dropped the working address of a
// user who mistyped.
func (r *repo) UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*User, error) {
	row := r.db.QueryRowContext(ctx,
		`UPDATE users
		 SET pending_email = $1, email_verification_token = $2, email_token_expires_at = $3
		 WHERE id = $4
		 RETURNING id, role, phone, COALESCE(email, ''), COALESCE(pending_email, ''), email_verified, email_verification_token, balance, status, created_at`,
		email, verificationToken, expiresAt, userID,
	)
	var u User
	var emailStr, pendingStr string
	err := row.Scan(
		&u.ID, &u.Role, &u.Phone, &emailStr, &pendingStr, &u.EmailVerified,
		&u.EmailVerificationToken, &u.Balance, &u.Status, &u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.Email = emailStr
	u.PendingEmail = pendingStr
	return &u, nil
}

func (r *repo) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_name = $1, first_name = $2, patronymic = $3 WHERE id = $4`,
		lastName, firstName, patronymic, userID,
	)
	return err
}

func (r *repo) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET birth_date = $1 WHERE id = $2`,
		birthDate, userID,
	)
	return err
}
