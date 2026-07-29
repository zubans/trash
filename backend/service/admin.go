package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// AdminService manages administrative business logic.
type AdminService struct {
	userRepo     repository.UserRepository
	adminRepo    repository.AdminRepository
	settingsRepo repository.SettingsRepository
	tokenRepo    repository.TokenRepository
	jwtSecret    []byte
}

// NewAdminService creates a new AdminService.
func NewAdminService(
	userRepo repository.UserRepository,
	adminRepo repository.AdminRepository,
	settingsRepo repository.SettingsRepository,
	tokenRepo repository.TokenRepository,
	jwtSecret string,
) *AdminService {
	secret := jwtSecret
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return &AdminService{
		userRepo:     userRepo,
		adminRepo:    adminRepo,
		settingsRepo: settingsRepo,
		tokenRepo:    tokenRepo,
		jwtSecret:    []byte(secret),
	}
}

// HashToken computes the SHA256 hash of a JWT token.
func (s *AdminService) HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// IsTokenRevoked checks if the token has been blacklisted.
func (s *AdminService) IsTokenRevoked(token string) (bool, error) {
	hash := s.HashToken(token)
	return s.tokenRepo.IsTokenRevoked(hash)
}

// RevokeToken adds a token to the blacklist.
func (s *AdminService) RevokeToken(tokenStr string) error {
	// Parse without validation first to get expiry time
	token, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return err
	}

	var expiresAt time.Time
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if expVal, ok := claims["exp"]; ok {
			switch exp := expVal.(type) {
			case float64:
				expiresAt = time.Unix(int64(exp), 0)
			case int64:
				expiresAt = time.Unix(exp, 0)
			}
		}
	}

	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour) // fallback
	}

	hash := s.HashToken(tokenStr)
	return s.tokenRepo.RevokeToken(hash, expiresAt)
}

// GetUsers retrieves a list of users with filters and search.
func (s *AdminService) GetUsers(page, limit int, role, status, search string) ([]*repository.User, int, error) {
	return s.adminRepo.GetUsers(page, limit, role, status, search)
}

// UpdateUserStatus updates user status (e.g., ACTIVE or BANNED).
func (s *AdminService) UpdateUserStatus(userID uuid.UUID, status string) error {
	if status != "ACTIVE" && status != "BANNED" {
		return errors.New("invalid status")
	}
	return s.userRepo.UpdateStatus(userID, status)
}

// UpdateUserRole updates a user's role.
func (s *AdminService) UpdateUserRole(userID uuid.UUID, role string) error {
	if role != "CUSTOMER" && role != "EXECUTOR" && role != "ADMIN" {
		return errors.New("invalid role")
	}
	return s.userRepo.UpdateRole(userID, role)
}

// UpdateUserAddress updates a customer's pickup address (admin-only).
func (s *AdminService) UpdateUserAddress(userID uuid.UUID, address string) error {
	if address == "" {
		return errors.New("address is required")
	}
	if _, err := s.userRepo.FindByID(userID); err != nil {
		return errors.New("user not found")
	}
	return s.userRepo.UpdateCustomerAddress(userID, address)
}

// TopUpUserBalance adds funds directly to a user's balance.
func (s *AdminService) TopUpUserBalance(userID, adminID uuid.UUID, amount float64) error {
	if amount <= 0 {
		return errors.New("amount must be greater than zero")
	}

	// Verify user exists
	_, err := s.userRepo.FindByID(userID)
	if err != nil {
		return errors.New("user not found")
	}

	return s.adminRepo.TopUpUserBalance(userID, adminID, amount)
}

// GetTopUpRequests lists all balance top-up requests.
func (s *AdminService) GetTopUpRequests() ([]*repository.TopUpRequest, error) {
	return s.adminRepo.GetTopUpRequests()
}

// CreateTopUpRequest creates a pending balance top-up request.
func (s *AdminService) CreateTopUpRequest(userID uuid.UUID, amount float64) (*repository.TopUpRequest, error) {
	if amount <= 0 {
		return nil, errors.New("amount must be greater than zero")
	}

	// Verify user exists
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user.Status == "BANNED" {
		return nil, errors.New("cannot request top-up for a banned user")
	}

	return s.adminRepo.CreateTopUpRequest(userID, amount)
}

// ApproveTopUpRequest approves a top-up request.
func (s *AdminService) ApproveTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	// Verify request is pending
	req, err := s.adminRepo.GetTopUpRequestByID(requestID)
	if err != nil {
		return err
	}
	if req.Status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	// Approve
	return s.adminRepo.ApproveTopUpRequest(requestID, adminID)
}

// RejectTopUpRequest rejects a top-up request.
func (s *AdminService) RejectTopUpRequest(requestID uuid.UUID, adminID uuid.UUID) error {
	// Verify request is pending
	req, err := s.adminRepo.GetTopUpRequestByID(requestID)
	if err != nil {
		return err
	}
	if req.Status != "PENDING" {
		return errors.New("request is not in PENDING status")
	}

	return s.adminRepo.RejectTopUpRequest(requestID, adminID)
}

// GetTransactions retrieves transaction history.
func (s *AdminService) GetTransactions() ([]*repository.Transaction, error) {
	return s.adminRepo.GetTransactions()
}

// GetProfile returns the authenticated user's profile including customer address.
func (s *AdminService) GetProfile(userID uuid.UUID) (map[string]interface{}, error) {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return nil, err
	}
	user.Password = ""

	profile := map[string]interface{}{
		"id":         user.ID,
		"role":       user.Role,
		"phone":      user.Phone,
		"balance":    user.Balance,
		"status":     user.Status,
		"created_at": user.CreatedAt,
		"address":    "",
	}

	cp, err := s.userRepo.GetCustomerProfile(userID)
	if err != nil {
		log.Printf("[GetProfile] failed to load customer profile for %s: %v", userID, err)
	} else if cp != nil {
		profile["address"] = cp.Address
	}

	return profile, nil
}

// GetSettings retrieves global settings.
func (s *AdminService) GetSettings() (map[string]string, error) {
	return s.settingsRepo.GetSettings()
}

// UpdateSettings updates global settings.
func (s *AdminService) UpdateSettings(settings map[string]string) error {
	// Numeric settings must be non-negative when applicable.
	numericKeys := map[string]bool{
		"standard_tariff_coeff":  true,
		"increased_tariff_coeff": true,
		"urgent_tariff_coeff":    true,
		"asap_tariff_coeff":      true,
		"geofence_fine_amount":   true,
	}
	for key, value := range settings {
		if numericKeys[key] {
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return errors.New("setting " + key + " must be numeric")
			}
			if v < 0 {
				return errors.New("setting " + key + " value cannot be negative")
			}
		}
	}
	return s.settingsRepo.UpdateSettings(settings)
}
