package service

import (
	"context"
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

// AuthService handles user registration and authentication.
type AuthService struct {
	repo        repository.UserRepository
	addressRepo repository.AddressRepository
	execGeoRepo repository.ExecutorGeoRepository
	refreshRepo repository.RefreshTokenRepository
	tokenRepo   repository.TokenRepository
	resolver    AddressResolver
	mailer      MailSender
	secret      []byte
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
func NewAuthService(repo repository.UserRepository, resolver AddressResolver) *AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return NewAuthServiceWithSecret(repo, secret, resolver, NewSmtpMailSender())
}

// NewAuthServiceWithSecret creates an AuthService with an explicit JWT secret.
// Useful for tests and for environments where the secret is injected directly.
func NewAuthServiceWithSecret(repo repository.UserRepository, secret string, resolver AddressResolver, mailer MailSender) *AuthService {
	if mailer == nil {
		mailer = NewSmtpMailSender()
	}
	return &AuthService{repo: repo, resolver: resolver, mailer: mailer, secret: []byte(secret)}
}

// WithAddresses attaches the address repository used during registration.
func (s *AuthService) WithAddresses(addressRepo repository.AddressRepository) *AuthService {
	s.addressRepo = addressRepo
	return s
}

// WithExecutorGeo attaches the executor geo repository for setting initial location.
func (s *AuthService) WithExecutorGeo(execGeoRepo repository.ExecutorGeoRepository) *AuthService {
	s.execGeoRepo = execGeoRepo
	return s
}

// WithSessionStorage attaches the stores that back session handling: refresh
// tokens and the access-token blacklist. Without them the service can still
// issue access tokens, which is what the unit tests rely on.
func (s *AuthService) WithSessionStorage(refreshRepo repository.RefreshTokenRepository, tokenRepo repository.TokenRepository) *AuthService {
	s.refreshRepo = refreshRepo
	s.tokenRepo = tokenRepo
	return s
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
func (s *AuthService) Register(ctx context.Context, phone, email, password, lastName, firstName, patronymic, address, role string) (*repository.User, error) {
	return s.RegisterWithCoordinates(ctx, phone, email, password, lastName, firstName, patronymic, address, role, nil, nil)
}

// RegisterWithCoordinates creates a new user with email, phone, password and address.
func (s *AuthService) RegisterWithCoordinates(ctx context.Context, phone, email, password, lastName, firstName, patronymic, address, role string, lat, lon *float64) (*repository.User, error) {
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
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}

	// The address is checked for what it has to contain — a settlement, a
	// street and a building — rather than matched against a fixed spelling.
	// The old format check demanded a purely numeric house number, so a person
	// living at 12к1 could not register at all.
	parsedAddress := ParseAddressLine(address)
	if err := parsedAddress.Validate(); err != nil {
		return nil, err
	}
	normalizedAddress := parsedAddress.Compose()

	existingPhone, err := s.repo.FindByPhone(ctx, phone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingPhone != nil {
		return nil, errors.New("user with this phone already exists")
	}

	existingEmail, err := s.repo.FindByEmail(ctx, email)
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
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}

	created, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return nil, err
	}

	// Resolve coordinates for initial address if not provided
	var resLat, resLon *float64
	if lat != nil && lon != nil {
		resLat = lat
		resLon = lon
	} else if s.resolver != nil {
		if geo, err := s.resolver.Resolve(ctx, normalizedAddress); err == nil && geo != nil {
			l := geo.Lat
			ln := geo.Lon
			resLat = &l
			resLon = &ln
		}
	}

	fullName := strings.TrimSpace(fmt.Sprintf("%s %s %s", lastName, firstName, patronymic))
	if role == "CUSTOMER" {
		if err := s.repo.CreateCustomerProfile(ctx, created.ID, fullName); err != nil {
			return nil, err
		}
	}

	if s.addressRepo != nil {
		addrRecord := parsedAddress.ToRecord()
		addrRecord.UserID = created.ID
		addrRecord.IsDefault = true
		if resLat != nil && resLon != nil {
			addrRecord.Lat = resLat
			addrRecord.Lon = resLon
		}
		if _, err := s.addressRepo.Add(ctx, created.ID, addrRecord); err != nil {
			log.Printf("[AuthService] failed to save initial address for user %s: %v", created.ID, err)
		}
	}

	// Set initial executor location
	if role == "EXECUTOR" && resLat != nil && resLon != nil && s.execGeoRepo != nil {
		if err := s.execGeoRepo.UpdateExecutorLocation(ctx, created.ID, *resLat, *resLon, false); err != nil {
			log.Printf("[AuthService] failed to store initial executor geo for %s: %v", created.ID, err)
		}
	}

	if s.mailer != nil {
		_ = s.mailer.SendEmailVerification(email, verificationToken)
	}

	return created, nil
}

// Authenticate verifies phone/password or email/password and returns the matching user.
func (s *AuthService) Authenticate(ctx context.Context, phoneOrEmail, password string) (*repository.User, error) {
	if phoneOrEmail == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}

	input := strings.TrimSpace(phoneOrEmail)
	var user *repository.User
	var err error

	if strings.Contains(input, "@") {
		user, err = s.repo.FindByEmail(ctx, input)
	} else {
		user, err = s.repo.FindByPhone(ctx, normalizePhone(input))
		if (err != nil || user == nil) && input != "" {
			user, err = s.repo.FindByPhone(ctx, input)
		}
	}

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
func (s *AuthService) GenerateJWT(ctx context.Context, user *repository.User) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user.ID.String(),
		"phone": user.Phone,
		"role":  user.Role,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(accessTokenTTL).Unix(),
	})
	return token.SignedString(s.secret)
}

// ParseJWT validates a token string and returns the extracted claims.
func (s *AuthService) ParseJWT(ctx context.Context, tokenStr string) (*JWTClaims, error) {
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
func (s *AuthService) VerifyEmail(ctx context.Context, token string) (*repository.User, error) {
	if token == "" {
		return nil, errors.New("token is required")
	}
	return s.repo.VerifyEmailToken(ctx, token)
}

// RequestPasswordReset generates a 6-digit code for password reset and sends it via email.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("укажите Email")
	}
	user, err := s.repo.FindByEmail(ctx, email)
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

	if err := s.repo.SetPasswordResetCode(ctx, user.ID, code, expiresAt); err != nil {
		return err
	}

	if s.mailer != nil {
		if err := s.mailer.SendPasswordResetCode(email, code); err != nil {
			// The answer must not depend on whether the address has an account.
			// Returning an error here while an unknown address gets a cheerful
			// success turns this endpoint into an account-existence oracle —
			// the very thing the unknown-address branch above exists to avoid.
			// A transport that is down is an operator problem, visible in this
			// log, not something to report back to an anonymous caller.
			//
			// The stored code is cleared: it can never be delivered, and leaving
			// it in place would keep the previous code overwritten and the
			// attempt counter reset for nothing.
			log.Printf("[PASSWORD RESET] user %s: the code was NOT delivered: %v", user.ID, err)
			if clearErr := s.repo.SetPasswordResetCode(ctx, user.ID, "", time.Now()); clearErr != nil {
				log.Printf("[PASSWORD RESET] user %s: could not clear the undelivered code: %v", user.ID, clearErr)
			}
			return nil
		}
	}

	// The code itself is never logged.
	log.Printf("[PASSWORD RESET] Code sent to user %s (expires %s)", user.ID, expiresAt.Format(time.RFC3339))
	return nil
}

// ResetPassword verifies the code and updates password.
func (s *AuthService) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email = strings.TrimSpace(email)
	code = strings.TrimSpace(code)
	if email == "" || code == "" || newPassword == "" {
		return errors.New("укажите Email, код и новый пароль")
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	_, err = s.repo.ResetPasswordWithCode(ctx, email, code, string(hash))
	return err
}

// ChangePassword replaces a signed-in user's password after checking the
// current one, and ends every other session.
//
// The profile page has always offered this form; there was no endpoint behind
// it, so the only way to change a password was the forgot-password flow.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) (*TokenPair, error) {
	if oldPassword == "" || newPassword == "" {
		return nil, errors.New("укажите текущий и новый пароль")
	}
	if err := validatePassword(newPassword); err != nil {
		return nil, err
	}
	if oldPassword == newPassword {
		return nil, errors.New("новый пароль совпадает с текущим")
	}

	user, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, errors.New("user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return nil, errors.New("текущий пароль неверен")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return nil, err
	}

	// Whoever else was signed in with the old password is signed out. The caller
	// gets a fresh pair so the device that made the change stays usable.
	if err := s.RevokeAllSessions(ctx, userID); err != nil {
		log.Printf("[AuthService] failed to end sessions after password change for %s: %v", userID, err)
	}
	return s.IssueTokenPair(ctx, user)
}

// UpdateUserEmail updates the email for a user and triggers verification.
func (s *AuthService) UpdateUserEmail(ctx context.Context, userID uuid.UUID, newEmail string) (*repository.User, error) {
	newEmail = strings.TrimSpace(newEmail)
	if newEmail == "" || !emailRegex.MatchString(newEmail) {
		return nil, errors.New("a valid email is required")
	}

	currentUser, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	existingUser, err := s.repo.FindByEmail(ctx, newEmail)
	if err == nil && existingUser != nil && existingUser.ID != userID {
		log.Printf("[SECURITY NOTICE] User with phone %s (ID: %s) attempted to attach email %s which is already bound to user with phone %s (ID: %s)",
			currentUser.Phone, currentUser.ID, newEmail, existingUser.Phone, existingUser.ID)
		return nil, errors.New("что-то пошло не так")
	}

	verificationToken := uuid.New().String()
	tokenExpiresAt := time.Now().Add(60 * time.Minute)
	user, err := s.repo.UpdateUserEmail(ctx, userID, newEmail, verificationToken, tokenExpiresAt)
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
func (s *AuthService) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDateStr string) (*repository.User, error) {
	t, err := time.Parse("2006-01-02", birthDateStr)
	if err != nil {
		return nil, errors.New("invalid birth date format, expected YYYY-MM-DD")
	}
	if err := s.repo.UpdateUserBirthDate(ctx, userID, t); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, userID)
}
