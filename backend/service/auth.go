package service

import (
	"database/sql"
	"errors"
	"fmt"
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
		Email:                  "", // Email is empty until verified via token
		LastName:               lastName,
		FirstName:              firstName,
		Patronymic:             patronymic,
		PendingEmail:           email,
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
	var lastGeo string
	if lat != nil && lon != nil {
		lastGeo = fmt.Sprintf("%f,%f", *lat, *lon)
		// Cache the coordinates for the normalized address so the geocoder knows this point.
		if gc, ok := s.geocoder.(*Geocoder); ok && gc != nil {
			_ = gc.saveCache(normalizedAddress, *lat, *lon)
		}
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
	if role == "EXECUTOR" && (lat != nil && lon != nil || lastGeo != "") {
		var eLat, eLon float64
		if lat != nil && lon != nil {
			eLat, eLon = *lat, *lon
		} else {
			fmt.Sscanf(lastGeo, "%f,%f", &eLat, &eLon)
		}
		_ = s.repo.UpdateLastGeo(created.ID, lastGeo)
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

	user, err := s.repo.FindByPhone(phone)
	if err != nil {
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

// RequestPasswordReset generates a 6-digit code for password reset.
func (s *AuthService) RequestPasswordReset(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", errors.New("email is required")
	}
	user, err := s.repo.FindByEmail(email)
	if err != nil || user == nil {
		return "", errors.New("user with this email not found")
	}

	// Generate 6-digit numeric reset code
	code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	expiresAt := time.Now().Add(30 * time.Minute)

	if err := s.repo.SetPasswordResetCode(user.ID, code, expiresAt); err != nil {
		return "", err
	}

	if s.mailer != nil {
		_ = s.mailer.SendPasswordResetCode(email, code)
	}

	// Development/production logging of code
	fmt.Printf("[PASSWORD RESET] Code for %s: %s (Expires: %s)\n", email, code, expiresAt.Format(time.RFC3339))
	return code, nil
}

// ResetPassword verifies the code and updates password.
func (s *AuthService) ResetPassword(email, code, newPassword string) error {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	if email == "" || code == "" || newPassword == "" {
		return errors.New("email, code and new password are required")
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

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)
	user, err := s.repo.UpdateUserEmail(userID, newEmail, verificationToken, tokenExpiresAt)
	if err != nil {
		return nil, err
	}

	if s.mailer != nil {
		_ = s.mailer.SendEmailVerification(newEmail, verificationToken)
	}

	return user, nil
}
