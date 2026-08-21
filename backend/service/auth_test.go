package service

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
)

// mockRepo is an in-memory implementation of repository.UserRepository for tests.
type mockRepo struct {
	users     map[string]*repository.User
	findErr   error
	createErr error
}

func newMockRepo() *mockRepo {
	return &mockRepo{users: make(map[string]*repository.User)}
}

func (m *mockRepo) FindByPhone(phone string) (*repository.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if u, ok := m.users[phone]; ok {
		return u, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) Create(user *repository.User) error {
	if m.createErr != nil {
		return m.createErr
	}
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	m.users[user.Phone] = user
	return nil
}

func (m *mockRepo) FindByID(id uuid.UUID) (*repository.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) UpdateStatus(id uuid.UUID, status string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Status = status
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateRole(id uuid.UUID, role string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Role = role
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateBalance(id uuid.UUID, balance float64) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Balance = balance
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateLastGeo(id uuid.UUID, lastGeo string) error {
	return nil
}

func (m *mockRepo) CreateCustomerProfile(userID uuid.UUID, address, lastGeo string) error {
	return nil
}

func (m *mockRepo) GetCustomerProfile(userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}

func (m *mockRepo) FindByEmail(email string) (*repository.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) FindByEmailVerificationToken(token string) (*repository.User, error) {
	for _, u := range m.users {
		if u.EmailVerificationToken == token {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) VerifyEmailToken(token string) (*repository.User, error) {
	for _, u := range m.users {
		if u.EmailVerificationToken == token {
			if u.PendingEmail != "" {
				u.Email = u.PendingEmail
				u.PendingEmail = ""
			}
			u.EmailVerified = true
			u.EmailVerificationToken = ""
			return u, nil
		}
	}
	return nil, errors.New("invalid or expired verification token")
}

func (m *mockRepo) SetPasswordResetCode(userID uuid.UUID, code string, expiresAt time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PasswordResetCode = code
			u.PasswordResetExpiresAt = &expiresAt
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) ResetPasswordWithCode(email, code, newHashedPassword string) (*repository.User, error) {
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) && u.PasswordResetCode == code {
			u.Password = newHashedPassword
			u.PasswordResetCode = ""
			u.PasswordResetExpiresAt = nil
			return u, nil
		}
	}
	return nil, errors.New("invalid or expired reset code")
}

func (m *mockRepo) UpdateUserEmail(userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
	for _, u := range m.users {
		if u.ID == userID {
			u.PendingEmail = email
			u.EmailVerified = false
			u.EmailVerificationToken = verificationToken
			return u, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *mockRepo) UpdateCustomerAddress(userID uuid.UUID, address string) error {
	return nil
}

func (m *mockRepo) UpdateUserName(userID uuid.UUID, lastName, firstName, patronymic string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.LastName = lastName
			u.FirstName = firstName
			u.Patronymic = patronymic
			return nil
		}
	}
	return errors.New("user not found")
}

func TestRegister_Success(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	phone := "+79001234567"
	password := "strong-password"

	user, err := svc.Register(phone, "test@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Phone != phone {
		t.Errorf("phone mismatch: got %q want %q", user.Phone, phone)
	}
	if user.Role != "CUSTOMER" {
		t.Errorf("role mismatch: got %q want CUSTOMER", user.Role)
	}
	if user.Status != "ACTIVE" {
		t.Errorf("status mismatch: got %q want ACTIVE", user.Status)
	}
	if user.Balance != 0 {
		t.Errorf("balance mismatch: got %v want 0", user.Balance)
	}
	if user.Password == password {
		t.Error("password must be hashed, got plain text")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		t.Errorf("stored hash does not match original password: %v", err)
	}
}

func TestRegister_NormalizesAddress(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	phone := "+79001234568"
	password := "strong-password"

	user, err := svc.Register(phone, "kursk@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Курск, улица Генерала Григорова, д. 40 кв. 12", "CUSTOMER")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Phone != phone {
		t.Errorf("phone mismatch: got %q want %q", user.Phone, phone)
	}
}

func TestRegister_ExecutorSuccess(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	phone := "+79001234569"
	password := "strong-password"

	user, err := svc.Register(phone, "executor@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "EXECUTOR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != "EXECUTOR" {
		t.Errorf("role mismatch: got %q want EXECUTOR", user.Role)
	}
}

func TestRegister_InvalidRoleAdmin(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register("+79001234567", "admin@example.com", "password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "ADMIN")
	if err == nil {
		t.Fatal("expected error for ADMIN role")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_InvalidRoleEmpty(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register("+79001234567", "empty@example.com", "password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "")
	if err == nil {
		t.Fatal("expected error for empty role")
	}
}

func TestRegister_EmptyPhone(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register("", "emptyphone@example.com", "password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error for empty phone")
	}
	if !strings.Contains(err.Error(), "phone and password are required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_EmptyPassword(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register("+79001234567", "emptypass@example.com", "", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestRegister_UserAlreadyExists(t *testing.T) {
	repo := newMockRepo()
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)
	phone := "+79001234567"

	if _, err := svc.Register(phone, "exists@example.com", "password-one", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	_, err := svc.Register(phone, "exists2@example.com", "password-two", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error when registering existing user")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_FindByPhoneError(t *testing.T) {
	repo := newMockRepo()
	repo.findErr = errors.New("db is down")
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)

	_, err := svc.Register("+79001234567", "dberr@example.com", "password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error from repository")
	}
	if err.Error() != "db is down" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_CreateError(t *testing.T) {
	repo := newMockRepo()
	repo.createErr = errors.New("insert failed")
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)

	_, err := svc.Register("+79001234567", "createerr@example.com", "password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error from Create")
	}
	if err.Error() != "insert failed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthenticate_Success(t *testing.T) {
	repo := newMockRepo()
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)
	phone := "+79001234567"
	password := "correct-password"

	if _, err := svc.Register(phone, "auth@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	user, err := svc.Authenticate(phone, password)
	if err != nil {
		t.Fatalf("unexpected authentication error: %v", err)
	}
	if user.Phone != phone {
		t.Errorf("phone mismatch: got %q want %q", user.Phone, phone)
	}
}

func TestAuthenticate_EmptyPhone(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate("", "password")
	if err == nil {
		t.Fatal("expected error for empty phone")
	}
}

func TestAuthenticate_EmptyPassword(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate("+79001234567", "")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestAuthenticate_UserNotFound(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate("+79001234567", "password")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
	if err.Error() != "invalid credentials" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthenticate_WrongPassword(t *testing.T) {
	repo := newMockRepo()
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)
	phone := "+79001234567"

	if _, err := svc.Register(phone, "wrongpass@example.com", "correct-password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	_, err := svc.Authenticate(phone, "wrong-password")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if err.Error() != "invalid credentials" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthenticate_RepositoryError(t *testing.T) {
	repo := newMockRepo()
	repo.findErr = errors.New("db error")
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)

	_, err := svc.Authenticate("+79001234567", "password")
	if err == nil {
		t.Fatal("expected error from repository")
	}
	if err.Error() != "invalid credentials" {
		t.Errorf("expected generic error message, got: %v", err)
	}
}

func TestGenerateJWT_Success(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	user := &repository.User{
		ID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Phone: "+79001234567",
		Role:  "CUSTOMER",
	}

	token, err := svc.GenerateJWT(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	parsed, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte("test-secret"), nil
	})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token must be valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}
	if claims["sub"] != user.ID.String() {
		t.Errorf("sub mismatch: got %v want %v", claims["sub"], user.ID.String())
	}
	if claims["phone"] != user.Phone {
		t.Errorf("phone mismatch: got %v want %v", claims["phone"], user.Phone)
	}
	if claims["role"] != user.Role {
		t.Errorf("role mismatch: got %v want %v", claims["role"], user.Role)
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("exp claim type mismatch: %T", claims["exp"])
	}
	wantExp := time.Now().Add(15 * time.Minute).Unix()
	if int64(exp) < wantExp-5 || int64(exp) > wantExp+5 {
		t.Errorf("exp mismatch: got %v want around %v", int64(exp), wantExp)
	}
}

func TestGenerateJWT_InvalidWithWrongSecret(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "secret-a", nil, nil)
	user := &repository.User{
		ID:    uuid.New(),
		Phone: "+79001234567",
		Role:  "CUSTOMER",
	}

	token, err := svc.GenerateJWT(user)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return []byte("secret-b"), nil
	})
	if err == nil {
		t.Fatal("token must be invalid when verified with a different secret")
	}
}
