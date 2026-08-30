package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/money"
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

func (m *mockRepo) FindByPhone(ctx context.Context, phone string) (*repository.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if u, ok := m.users[phone]; ok {
		return u, nil
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) Create(ctx context.Context, user *repository.User) error {
	if m.createErr != nil {
		return m.createErr
	}
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	m.users[user.Phone] = user
	return nil
}

func (m *mockRepo) FindByID(ctx context.Context, id uuid.UUID) (*repository.User, error) {
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

func (m *mockRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Status = status
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateVerified(ctx context.Context, id uuid.UUID, verified bool) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Verified = verified
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateRole(ctx context.Context, id uuid.UUID, role string) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Role = role
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) UpdateBalance(ctx context.Context, id uuid.UUID, balance money.Amount) error {
	for _, u := range m.users {
		if u.ID == id {
			u.Balance = balance
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) CreateCustomerProfile(ctx context.Context, userID uuid.UUID, fullName string) error {
	return nil
}

func (m *mockRepo) GetCustomerProfile(ctx context.Context, userID uuid.UUID) (*repository.CustomerProfile, error) {
	return &repository.CustomerProfile{UserID: userID}, nil
}

func (m *mockRepo) FindByEmail(ctx context.Context, email string) (*repository.User, error) {
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

func (m *mockRepo) FindByEmailVerificationToken(ctx context.Context, token string) (*repository.User, error) {
	for _, u := range m.users {
		if u.EmailVerificationToken == token {
			return u, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (m *mockRepo) VerifyEmailToken(ctx context.Context, token string) (*repository.User, error) {
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

func (m *mockRepo) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.PasswordResetCode = code
			u.PasswordResetExpiresAt = &expiresAt
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *mockRepo) ResetPasswordWithCode(ctx context.Context, email, code, newHashedPassword string) (*repository.User, error) {
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

func (m *mockRepo) UpdateUserEmail(ctx context.Context, userID uuid.UUID, email, verificationToken string, expiresAt time.Time) (*repository.User, error) {
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

func (m *mockRepo) UpdateUserBirthDate(ctx context.Context, userID uuid.UUID, birthDate time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			bd := birthDate
			u.BirthDate = &bd
			return nil
		}
	}
	return nil
}

func (m *mockRepo) UpdateUserName(ctx context.Context, userID uuid.UUID, lastName, firstName, patronymic string) error {
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

	user, err := svc.Register(context.Background(), phone, "test@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
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

	user, err := svc.Register(context.Background(), phone, "kursk@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Курск, улица Генерала Григорова, д. 40 кв. 12", "CUSTOMER")
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

	user, err := svc.Register(context.Background(), phone, "executor@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "EXECUTOR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.Role != "EXECUTOR" {
		t.Errorf("role mismatch: got %q want EXECUTOR", user.Role)
	}
}

func TestRegister_InvalidRoleAdmin(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register(context.Background(), "+79001234567", "admin@example.com", "Str0ngPassw0rd", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "ADMIN")
	if err == nil {
		t.Fatal("expected error for ADMIN role")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_InvalidRoleEmpty(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register(context.Background(), "+79001234567", "empty@example.com", "Str0ngPassw0rd", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "")
	if err == nil {
		t.Fatal("expected error for empty role")
	}
}

func TestRegister_EmptyPhone(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register(context.Background(), "", "emptyphone@example.com", "Str0ngPassw0rd", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error for empty phone")
	}
	if !strings.Contains(err.Error(), "phone and password are required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegister_EmptyPassword(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Register(context.Background(), "+79001234567", "emptypass@example.com", "", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestRegister_UserAlreadyExists(t *testing.T) {
	repo := newMockRepo()
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, nil)
	phone := "+79001234567"

	if _, err := svc.Register(context.Background(), phone, "exists@example.com", "password-one", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	_, err := svc.Register(context.Background(), phone, "exists2@example.com", "password-two", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
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

	_, err := svc.Register(context.Background(), "+79001234567", "dberr@example.com", "Str0ngPassw0rd", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
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

	_, err := svc.Register(context.Background(), "+79001234567", "createerr@example.com", "Str0ngPassw0rd", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER")
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

	if _, err := svc.Register(context.Background(), phone, "auth@example.com", password, "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	user, err := svc.Authenticate(context.Background(), phone, password)
	if err != nil {
		t.Fatalf("unexpected authentication error: %v", err)
	}
	if user.Phone != phone {
		t.Errorf("phone mismatch: got %q want %q", user.Phone, phone)
	}
}

func TestAuthenticate_EmptyPhone(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate(context.Background(), "", "password")
	if err == nil {
		t.Fatal("expected error for empty phone")
	}
}

func TestAuthenticate_EmptyPassword(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate(context.Background(), "+79001234567", "")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestAuthenticate_UserNotFound(t *testing.T) {
	svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
	_, err := svc.Authenticate(context.Background(), "+79001234567", "password")
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

	if _, err := svc.Register(context.Background(), phone, "wrongpass@example.com", "correct-password", "Иванов", "Иван", "Иванович", "Россия, Москва, Тверская улица, д. 1234 кв. 567", "CUSTOMER"); err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	_, err := svc.Authenticate(context.Background(), phone, "wrong-password")
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

	_, err := svc.Authenticate(context.Background(), "+79001234567", "password")
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

	token, err := svc.GenerateJWT(context.Background(), user)
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

	token, err := svc.GenerateJWT(context.Background(), user)
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

func (m *mockRepo) UpdatePassword(ctx context.Context, userID uuid.UUID, newHashedPassword string) error {
	for _, u := range m.users {
		if u.ID == userID {
			u.Password = newHashedPassword
			return nil
		}
	}
	return nil
}

// failingMailer stands in for a mail transport that is down.
type failingMailer struct{ calls int }

func (m *failingMailer) SendEmailVerification(string, string) error { return nil }
func (m *failingMailer) SendPasswordResetCode(string, string) error {
	m.calls++
	return errors.New("dial tcp: connection refused")
}

// TestRequestPasswordReset_DoesNotRevealAccounts pins the property that made
// the endpoint an account-existence oracle in production: an unknown address
// reported success while a known address whose mail could not be sent reported
// an error, so a 400 meant "this email has an account here".
func TestRequestPasswordReset_DoesNotRevealAccounts(t *testing.T) {
	mailer := &failingMailer{}
	repo := newMockRepo()
	known := &repository.User{ID: uuid.New(), Phone: "+79990000001", Email: "known@example.com", Role: "CUSTOMER"}
	repo.users[known.Phone] = known
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, mailer)

	if err := svc.RequestPasswordReset(context.Background(), "nobody@example.com"); err != nil {
		t.Fatalf("an unknown address must report success, got %v", err)
	}
	if err := svc.RequestPasswordReset(context.Background(), known.Email); err != nil {
		t.Fatalf("a known address must report the same thing when mail fails, got %v", err)
	}
	if mailer.calls != 1 {
		t.Errorf("expected exactly one send attempt for the known address, got %d", mailer.calls)
	}
}

// TestRegisterAcceptsRealBuildingNumbers is the user-visible half of the
// address change. Registration used to demand a purely numeric house number, so
// anyone living in a корпус or a строение could pick their address from the
// suggestion list and then be refused by the form. These are ordinary Russian
// addresses and they must go through.
func TestRegisterAcceptsRealBuildingNumbers(t *testing.T) {
	addresses := []string{
		"Россия, Москва, Тверская улица, д. 12к1",
		"Россия, Москва, Тверская улица, д. 10 стр. 2",
		"Россия, Курск, улица Ленина, д. 5А кв. 3",
		"Россия, Москва, Тверская улица, д. 7, кв. 35",
	}

	for i, address := range addresses {
		svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
		phone := fmt.Sprintf("+7900123%04d", 7000+i)
		email := fmt.Sprintf("building-%d@example.com", i)

		if _, err := svc.Register(context.Background(), phone, email, "strong-password",
			"Иванов", "Иван", "Иванович", address, "CUSTOMER"); err != nil {
			t.Errorf("%q must be accepted: %v", address, err)
		}
	}
}

// TestRegisterStillRequiresABuilding: relaxing the format must not turn into
// accepting anything. An address without a house cannot be delivered to.
func TestRegisterStillRequiresABuilding(t *testing.T) {
	rejected := []string{
		"Россия, Москва, Тверская улица",
		"Москва",
		"   ",
		"случайный текст",
	}

	for i, address := range rejected {
		svc := NewAuthServiceWithSecret(newMockRepo(), "test-secret", nil, nil)
		phone := fmt.Sprintf("+7900124%04d", 8000+i)
		email := fmt.Sprintf("nohouse-%d@example.com", i)

		if _, err := svc.Register(context.Background(), phone, email, "strong-password",
			"Иванов", "Иван", "Иванович", address, "CUSTOMER"); err == nil {
			t.Errorf("%q has no building and must be refused", address)
		}
	}
}

// recordingRepo notes what was written to the reset code, so a test can tell a
// stored code from a cleared one.
type resetCodeRecorder struct {
	*mockRepo
	lastCode string
	writes   int
}

func (r *resetCodeRecorder) SetPasswordResetCode(ctx context.Context, userID uuid.UUID, code string, expiresAt time.Time) error {
	r.lastCode = code
	r.writes++
	return r.mockRepo.SetPasswordResetCode(context.Background(), userID, code, expiresAt)
}

// TestUndeliveredResetCodeIsCleared covers what the production log exposed: the
// send failed and the next line still announced that a code had been sent. The
// code is now removed when it cannot be delivered, so a stored code always
// means a code somebody can actually receive — and the previous one is not left
// overwritten by a code that never arrived.
func TestUndeliveredResetCodeIsCleared(t *testing.T) {
	base := newMockRepo()
	known := &repository.User{ID: uuid.New(), Phone: "+79990000002", Email: "known@example.com", Role: "CUSTOMER"}
	base.users[known.Phone] = known

	repo := &resetCodeRecorder{mockRepo: base}
	svc := NewAuthServiceWithSecret(repo, "test-secret", nil, &failingMailer{})

	if err := svc.RequestPasswordReset(context.Background(), known.Email); err != nil {
		t.Fatalf("the caller must still see success: %v", err)
	}

	if repo.writes != 2 {
		t.Fatalf("expected the code to be written and then cleared, got %d writes", repo.writes)
	}
	if repo.lastCode != "" {
		t.Errorf("an undeliverable code must not stay stored, found %q", repo.lastCode)
	}
}

func (m *mockRepo) ListUserRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	return nil, nil
}

func (m *mockRepo) SetUserRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	return nil
}
