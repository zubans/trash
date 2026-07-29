package service

import (
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/repository"
)

// AuthService handles user registration and authentication.
type AuthService struct {
	repo   repository.UserRepository
	secret []byte
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
func NewAuthService(repo repository.UserRepository) *AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return NewAuthServiceWithSecret(repo, secret)
}

// NewAuthServiceWithSecret creates an AuthService with an explicit JWT secret.
// Useful for tests and for environments where the secret is injected directly.
func NewAuthServiceWithSecret(repo repository.UserRepository, secret string) *AuthService {
	return &AuthService{repo: repo, secret: []byte(secret)}
}

// Register creates a new user with the given phone, password and pickup address.
// The password is hashed before persisting. Role defaults to CUSTOMER.
func (s *AuthService) Register(phone, password, address string) (*repository.User, error) {
	if phone == "" || password == "" {
		return nil, errors.New("phone and password are required")
	}
	if address == "" {
		return nil, errors.New("address is required")
	}

	existing, err := s.repo.FindByPhone(phone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("user already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &repository.User{
		Role:     "CUSTOMER",
		Phone:    phone,
		Password: string(hash),
		Balance:  0,
		Status:   "ACTIVE",
	}
	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	created, err := s.repo.FindByPhone(phone)
	if err != nil {
		return nil, err
	}

	if err := s.repo.CreateCustomerProfile(created.ID, address); err != nil {
		return nil, err
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
