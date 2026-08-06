package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// Context keys used by the middleware.
type contextKey string

const (
	// UserKey stores the authenticated *repository.User in the request context.
	UserKey contextKey = "user"
	// TokenKey stores the raw JWT token string in the request context.
	TokenKey contextKey = "token"
	// RoleKey stores the user role string in the request context.
	RoleKey contextKey = "role"
)

// AuthMiddleware validates JWTs and injects user information into the request context.
type AuthMiddleware struct {
	userRepo    repository.UserRepository
	adminService *service.AdminService
	secret      []byte
}

// NewAuthMiddleware creates an AuthMiddleware.
func NewAuthMiddleware(userRepo repository.UserRepository, adminService *service.AdminService, jwtSecret string) *AuthMiddleware {
	if jwtSecret == "" {
		jwtSecret = "dev-secret-change-me"
	}
	return &AuthMiddleware{
		userRepo:     userRepo,
		adminService: adminService,
		secret:       []byte(jwtSecret),
	}
}

func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return parts[1]
		}
	}

	if cookie, err := r.Cookie("token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// RequireAuth ensures the request contains a valid non-revoked JWT.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			http.Error(w, "Authorization required", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.secret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Check revocation if admin service is available.
		if m.adminService != nil {
			revoked, err := m.adminService.IsTokenRevoked(tokenStr)
			if err != nil {
				http.Error(w, "Token check failed", http.StatusInternalServerError)
				return
			}
			if revoked {
				http.Error(w, "Token has been revoked", http.StatusUnauthorized)
				return
			}
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		sub, ok := claims["sub"].(string)
		if !ok {
			http.Error(w, "Invalid token subject", http.StatusUnauthorized)
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			http.Error(w, "Invalid token subject", http.StatusUnauthorized)
			return
		}

		user, err := m.userRepo.FindByID(userID)
		if err != nil {
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}

		role, _ := claims["role"].(string)

		ctx := r.Context()
		ctx = context.WithValue(ctx, UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, tokenStr)
		ctx = context.WithValue(ctx, RoleKey, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin restricts access to users with the ADMIN role.
func (m *AuthMiddleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := r.Context().Value(RoleKey).(string)
		if !ok || role != "ADMIN" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole restricts access to users with one of the allowed roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, role := range allowedRoles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := r.Context().Value(RoleKey).(string)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if _, ok := allowed[role]; !ok {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
