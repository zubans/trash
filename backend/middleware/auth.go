package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
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

// SessionChecker reports whether an access token has been blacklisted. It is
// satisfied by *service.AuthService; the middleware only needs this much.
type SessionChecker interface {
	IsAccessTokenRevoked(ctx context.Context, token string) (bool, error)
}

// AuthMiddleware validates JWTs and injects user information into the request context.
type AuthMiddleware struct {
	userRepo repository.UserRepository
	sessions SessionChecker
	secret   []byte
}

// NewAuthMiddleware creates an AuthMiddleware.
func NewAuthMiddleware(userRepo repository.UserRepository, sessions SessionChecker, jwtSecret string) *AuthMiddleware {
	if jwtSecret == "" {
		jwtSecret = "dev-secret-change-me"
	}
	return &AuthMiddleware{
		userRepo: userRepo,
		sessions: sessions,
		secret:   []byte(jwtSecret),
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

	return ""
}

// StripQueryToken moves a ?token= parameter into the Authorization header and
// removes it from the URL. Browsers cannot set headers on a WebSocket handshake,
// so the parameter has to be accepted, but it must never reach the request
// logger, the access log or a Referer header.
func StripQueryToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		token := query.Get("token")
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		query.Del("token")
		r.URL.RawQuery = query.Encode()
		r.RequestURI = r.URL.RequestURI()

		next.ServeHTTP(w, r)
	})
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

		// Check revocation if session storage is available.
		if m.sessions != nil {
			revoked, err := m.sessions.IsAccessTokenRevoked(r.Context(), tokenStr)
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

		user, err := m.userRepo.FindByID(r.Context(), userID)
		if err != nil {
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}
		if user.Status == "BANNED" {
			http.Error(w, "Account is banned", http.StatusUnauthorized)
			return
		}

		// Authorization always follows the role stored in the database, never the
		// role captured in the token: a demotion or a ban must take effect
		// immediately instead of at token expiry.
		role := user.Role

		ctx := r.Context()
		ctx = context.WithValue(ctx, UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, tokenStr)
		ctx = context.WithValue(ctx, RoleKey, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth populates the request context with the authenticated user when a
// valid, non-revoked token is present, but never rejects the request when it is
// absent or invalid. Handlers on otherwise-public endpoints use it to tailor the
// response to the caller (e.g. hiding verification-only services from unverified
// customers) while still serving anonymous visitors.
func (m *AuthMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			next.ServeHTTP(w, r)
			return
		}

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.secret, nil
		})
		if err != nil || !token.Valid {
			next.ServeHTTP(w, r)
			return
		}

		if m.sessions != nil {
			if revoked, err := m.sessions.IsAccessTokenRevoked(r.Context(), tokenStr); err != nil || revoked {
				next.ServeHTTP(w, r)
				return
			}
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		sub, ok := claims["sub"].(string)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := uuid.Parse(sub)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		user, err := m.userRepo.FindByID(r.Context(), userID)
		if err != nil || user == nil || user.Status == "BANNED" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, tokenStr)
		ctx = context.WithValue(ctx, RoleKey, user.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin restricts access to users with the ADMIN role.
func (m *AuthMiddleware) RequireAdmin(next http.Handler) http.Handler {
	return RequireRole("ADMIN")(next)
}

// UserFrom returns the authenticated user stored by RequireAuth.
func UserFrom(r *http.Request) *repository.User {
	user, _ := r.Context().Value(UserKey).(*repository.User)
	return user
}

// RequireRole restricts access to users with one of the allowed roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, role := range allowedRoles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The role in the context comes from the database record loaded by
			// RequireAuth, not from the token claims.
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
