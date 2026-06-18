package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

type contextKey string

const (
	UserKey   contextKey = "user"
	TokenKey  contextKey = "token"
	ClaimsKey contextKey = "claims"
)

// AuthMiddleware holds dependencies for auth checking.
type AuthMiddleware struct {
	userRepo     repository.UserRepository
	adminService *service.AdminService
	jwtSecret    []byte
}

// NewAuthMiddleware creates a new AuthMiddleware.
func NewAuthMiddleware(userRepo repository.UserRepository, adminService *service.AdminService, jwtSecret string) *AuthMiddleware {
	secret := jwtSecret
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	return &AuthMiddleware{
		userRepo:     userRepo,
		adminService: adminService,
		jwtSecret:    []byte(secret),
	}
}

// CustomClaims defines JWT claims structure.
type CustomClaims struct {
	UserID string `json:"sub"`
	Phone  string `json:"phone"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Authenticate verifies the token and user status, returning claims and user.
func (m *AuthMiddleware) Authenticate(r *http.Request) (*CustomClaims, *repository.User, string, error) {
	var tokenStr string
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		cookie, err := r.Cookie("token")
		if err == nil {
			tokenStr = cookie.Value
		}
	}

	if tokenStr == "" {
		return nil, nil, "", errors.New("unauthorized: missing token")
	}

	// 1. Check if token is revoked in blacklist
	revoked, err := m.adminService.IsTokenRevoked(tokenStr)
	if err != nil {
		return nil, nil, "", err
	}
	if revoked {
		return nil, nil, "", errors.New("unauthorized: token is blacklisted")
	}

	// 2. Parse and validate JWT
	token, err := jwt.ParseWithClaims(tokenStr, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, nil, "", errors.New("unauthorized: invalid token")
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return nil, nil, "", errors.New("unauthorized: invalid claims")
	}

	userUUID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, nil, "", errors.New("unauthorized: invalid user ID format")
	}

	// 3. Fetch user status from DB (covers instant ban revocation)
	user, err := m.userRepo.FindByID(userUUID)
	if err != nil {
		return nil, nil, "", errors.New("unauthorized: user not found")
	}

	if user.Status == "BANNED" {
		return nil, nil, "", errors.New("forbidden: account is banned")
	}

	return claims, user, tokenStr, nil
}

// RequireAuth ensures a valid user token is present.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, user, token, err := m.Authenticate(r)
		if err != nil {
			if strings.Contains(err.Error(), "forbidden") {
				http.Error(w, err.Error(), http.StatusForbidden)
			} else {
				http.Error(w, err.Error(), http.StatusUnauthorized)
			}
			return
		}

		ctx := context.WithValue(r.Context(), UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, token)
		ctx = context.WithValue(ctx, ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin restricts route to ADMIN role only.
func (m *AuthMiddleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, user, token, err := m.Authenticate(r)
		if err != nil {
			if strings.Contains(err.Error(), "forbidden") {
				http.Error(w, err.Error(), http.StatusForbidden)
			} else {
				http.Error(w, err.Error(), http.StatusUnauthorized)
			}
			return
		}

		if claims.Role != "ADMIN" {
			http.Error(w, "forbidden: admin role required", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, token)
		ctx = context.WithValue(ctx, ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
