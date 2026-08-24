package service

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
)

var addressRegex = regexp.MustCompile(`^Россия,\s*([^,]+?),\s*([^,]+?),\s*д\.\s*(\d+)(?:\s+кв\.\s*(\d+))?$`)

// normalizeAddress validates and canonicalizes the pickup address.
// Expected input: "Россия, Город, Улица, д.#### [кв. ###]" where # are digits.
// The flat number is optional. City and street can be any Russian city/street.
func normalizeAddress(address string) (string, error) {
	matches := addressRegex.FindStringSubmatch(address)
	if matches == nil {
		return "", errors.New("address must match format: Россия, Город, Улица, д.#### [кв. ###]")
	}
	city := strings.TrimSpace(matches[1])
	road := strings.TrimSpace(matches[2])
	house := matches[3]
	flat := matches[4]
	if flat != "" {
		return fmt.Sprintf("Россия, %s, %s, д. %s кв. %s", city, road, house, flat), nil
	}
	return fmt.Sprintf("Россия, %s, %s, д. %s", city, road, house), nil
}

// AuthService handles user registration and authentication.
type AuthService struct {
	repo     repository.UserRepository
	geocoder GeoCoder
	mailer   MailSender
	secret   []byte
}

// JWTClaims contains the data extracted from a validated access token.
type JWTClaims struct {
	UserID uuid.UUID
	Phone  string
	Role   string
}

// NewAuthService creates an AuthService using the provided repository.
// The JWT signing secret is read from JWT_SECRET; a development default is used
// if the variable is not set.
func NewAuthService(repo repository.UserRepository, geocoder GeoCoder) *AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return NewAuthServiceWithSecret(repo, secret, geocoder, NewSmtpMailSender())
}

// NewAuthServiceWithSecret creates an AuthService with an explicit JWT secret.
// Useful for tests and for environments where the secret is injected directly.
func NewAuthServiceWithSecret(repo repository.UserRepository, secret string, geocoder GeoCoder, mailer MailSender) *AuthService {
	if mailer == nil {
		mailer = NewSmtpMailSender()
	}
	return &AuthService{repo: repo, geocoder: geocoder, mailer: mailer, secret: []byte(secret)}
}

// minPasswordLength is the shortest password accepted at registration and at
// password reset.
const minPasswordLength = 8

// weakPasswords are the values seen most often in credential stuffing lists.
var weakPasswords = map[string]bool{
	"12345678": true, "123456789": true, "1234567890": true, "password": true,
	"qwerty123": true, "qwertyui": true, "11111111": true, "iloveyou": true,
	"admin123": true, "parol123": true, "password1": true,
}

// validatePassword enforces a minimum strength. Without it a single character
// password was accepted, which made the (unthrottled) login endpoint trivial.
func validatePassword(password string) error {
	if len([]rune(password)) < minPasswordLength {
		return fmt.Errorf("пароль должен быть не короче %d символов", minPasswordLength)
	}
	if weakPasswords[strings.ToLower(password)] {
		return errors.New("этот пароль слишком простой, выберите другой")
	}
	return nil
}

var phoneCleanup = regexp.MustCompile(`[^0-9+]`)

// normalizePhone reduces a Russian phone number to a single canonical form, so
// that "+7 999 …", "8999…" and "7999…" cannot become three separate accounts
// for the same person.
func normalizePhone(phone string) string {
	digits := phoneCleanup.ReplaceAllString(strings.TrimSpace(phone), "")
	digits = strings.TrimPrefix(digits, "+")
	switch {
	case len(digits) == 11 && strings.HasPrefix(digits, "8"):
		digits = "7" + digits[1:]
	case len(digits) == 10:
		digits = "7" + digits
	}
	return "+" + digits
}

// validRegistrationRole reports whether a role may be chosen during registration.
// ADMIN is explicitly forbidden; only CUSTOMER and EXECUTOR are allowed.
func validRegistrationRole(role string) bool {
	return role == "CUSTOMER" || role == "EXECUTOR"
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// Register creates a new user with the given phone, email, password, pickup address and role.
func (s *AuthService) Register(phone, email, password, lastName, firstName, patronymic, address, role string) (*repository.User, error) {
	return s.RegisterWithCoordinates(phone, email, password, lastName, firstName, patronymic, address, role, nil, nil)
}

// RegisterWithCoordinates creates a new user with email, phone, password and address.
func (s *AuthService) RegisterWithCoordinates(phone, email, password, lastName, firstName, patronymic, address, role string, lat, lon *float64) (*repository.User, error) {
	if phone == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	phone = normalizePhone(phone)
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	patronymic = strings.TrimSpace(patronymic)
	if lastName == "" || firstName == "" || patronymic == "" {
		return nil, errors.New("last_name, first_name, and patronymic are required")
	}
	email = strings.TrimSpace(email)
	if email == "" || !emailRegex.MatchString(email) {
		return nil, errors.New("a valid email is required")
	}
	if !validRegistrationRole(role) {
		return nil, errors.New("invalid role: must be CUSTOMER or EXECUTOR")
	}
	if address == "" {
		return nil, errors.New("address is required")
	}

	var normalizedAddress string
	if address != "" {
		var err error
		normalizedAddress, err = normalizeAddress(address)
		if err != nil {
			return nil, err
		}
	}

	existingPhone, err := s.repo.FindByPhone(phone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingPhone != nil {
		return nil, errors.New("user with this phone already exists")
	}

	existingEmail, err := s.repo.FindByEmail(email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingEmail != nil {
		return nil, errors.New("user with this email already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)

	user := &repository.User{
		Role:                   role,
		Phone:                  phone,
		Email:                  email,
		LastName:               lastName,
		FirstName:              firstName,
		Patronymic:             patronymic,
		EmailVerified:          false,
		EmailVerificationToken: verificationToken,
		EmailTokenExpiresAt:    &tokenExpiresAt,
		Password:               string(hash),
		Balance:                0,
		Status:                 "ACTIVE",
	}
	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	created, err := s.repo.FindByPhone(phone)
	if err != nil {
		return nil, err
	}

	// Save profile and base address location for both CUSTOMER and EXECUTOR
	// Note: client supplied coordinates are stored for this user only. They are
	// deliberately NOT written into the shared geocoding cache — anyone could
	// otherwise register with someone else's address and repoint it.
	var lastGeo string
	if lat != nil && lon != nil {
		lastGeo = formatGeo(*lat, *lon)
	} else if s.geocoder != nil {
		geo, err := s.geocoder.Geocode(normalizedAddress)
		if err == nil && geo != nil {
			lastGeo = fmt.Sprintf("%f,%f", geo.Lat, geo.Lon)
		}
	}

	if err := s.repo.CreateCustomerProfile(created.ID, normalizedAddress, lastGeo); err != nil {
		return nil, err
	}

	// Set initial executor location
	if role == "EXECUTOR" && lastGeo != "" {
		if err := s.repo.UpdateLastGeo(created.ID, lastGeo); err != nil {
			log.Printf("[AuthService] failed to store initial geo for %s: %v", created.ID, err)
		}
	}

	if s.mailer != nil {
		_ = s.mailer.SendEmailVerification(email, verificationToken)
	}

	return created, nil
}

// Authenticate verifies phone/password and returns the matching user.
func (s *AuthService) Authenticate(phone, password string) (*repository.User, error) {
	if phone == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}

	user, err := s.repo.FindByPhone(normalizePhone(phone))
	if err != nil || user == nil {
		// Hash anyway so a missing account is not distinguishable by timing.
		_, _ = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		return nil, errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	return user, nil
}

// GenerateJWT creates a signed JWT for the authenticated user.
func (s *AuthService) GenerateJWT(user *repository.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user.ID.String(),
		"phone": user.Phone,
		"role":  user.Role,
		"exp":   time.Now().Add(15 * time.Minute).Unix(),
	})
	return token.SignedString(s.secret)
}

// ParseJWT validates a token string and returns the extracted claims.
func (s *AuthService) ParseJWT(tokenStr string) (*JWTClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return nil, errors.New("missing sub claim")
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, err
	}

	phone, _ := claims["phone"].(string)
	role, _ := claims["role"].(string)

	return &JWTClaims{
		UserID: userID,
		Phone:  phone,
		Role:   role,
	}, nil
}

// VerifyEmail confirms user email by token.
func (s *AuthService) VerifyEmail(token string) (*repository.User, error) {
	if token == "" {
		return nil, errors.New("token is required")
	}
	return s.repo.VerifyEmailToken(token)
}

// RequestPasswordReset generates a 6-digit code for password reset and sends it via email.
func (s *AuthService) RequestPasswordReset(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("укажите Email")
	}
	user, err := s.repo.FindByEmail(email)
	if err != nil || user == nil {
		// Report success regardless: a different answer here tells an attacker
		// which email addresses have an account.
		log.Printf("[PASSWORD RESET] requested for unknown email")
		return nil
	}

	// Cryptographically secure 8-digit reset code. There is no time-based
	// fallback: a predictable code is worse than a failed request.
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return errors.New("не удалось сгенерировать код, попробуйте позже")
	}
	code := fmt.Sprintf("%08d", n.Int64())
	expiresAt := time.Now().Add(30 * time.Minute)

	if err := s.repo.SetPasswordResetCode(user.ID, code, expiresAt); err != nil {
		return err
	}

	if s.mailer != nil {
		if err := s.mailer.SendPasswordResetCode(email, code); err != nil {
			log.Printf("[PASSWORD RESET] Failed to send email to user %s: %v", user.ID, err)
			return errors.New("не удалось отправить письмо с кодом. Попробуйте позже.")
		}
	}

	// The code itself is never logged.
	log.Printf("[PASSWORD RESET] Code sent to user %s (expires %s)", user.ID, expiresAt.Format(time.RFC3339))
	return nil
}

// ResetPassword verifies the code and updates password.
func (s *AuthService) ResetPassword(email, code, newPassword string) error {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	if email == "" || code == "" || newPassword == "" {
		return errors.New("email, code and new password are required")
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.repo.ResetPasswordWithCode(email, code, string(hash))
	return err
}

// UpdateUserEmail updates the email for a user and triggers verification.
func (s *AuthService) UpdateUserEmail(userID uuid.UUID, newEmail string) (*repository.User, error) {
	newEmail = strings.TrimSpace(newEmail)
	if newEmail == "" || !emailRegex.MatchString(newEmail) {
		return nil, errors.New("a valid email is required")
	}

	currentUser, err := s.repo.FindByID(userID)
	if err != nil {
		return nil, err
	}

	existingUser, err := s.repo.FindByEmail(newEmail)
	if err == nil && existingUser != nil && existingUser.ID != userID {
		log.Printf("[SECURITY NOTICE] User with phone %s (ID: %s) attempted to attach email %s which is already bound to user with phone %s (ID: %s)",
			currentUser.Phone, currentUser.ID, newEmail, existingUser.Phone, existingUser.ID)
		return nil, errors.New("что-то пошло не так")
	}

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)
	user, err := s.repo.UpdateUserEmail(userID, newEmail, verificationToken, tokenExpiresAt)
	if err != nil {
		return nil, err
	}

	if s.mailer != nil {
		if err := s.mailer.SendEmailVerification(newEmail, verificationToken); err != nil {
			log.Printf("[AuthService] Failed to send email verification to %s: %v", newEmail, err)
		} else {
			log.Printf("[AuthService] Successfully triggered email verification to %s", newEmail)
		}
	}

	return user, nil
}

// UpdateUserBirthDate updates user's date of birth.
func (s *AuthService) UpdateUserBirthDate(userID uuid.UUID, birthDateStr string) (*repository.User, error) {
	t, err := time.Parse("2006-01-02", birthDateStr)
	if err != nil {
		return nil, errors.New("invalid birth date format, expected YYYY-MM-DD")
	}
	if err := s.repo.UpdateUserBirthDate(userID, t); err != nil {
		return nil, err
	}
	return s.repo.FindByID(userID)
}
