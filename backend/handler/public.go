package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/middleware"
	"healthlogin/backend/service"
)

// PublicHandler holds public HTTP handlers (health, registration, login).
type PublicHandler struct {
	authService *service.AuthService
}

// NewPublicHandler creates a PublicHandler with the provided AuthService.
func NewPublicHandler(authService *service.AuthService) *PublicHandler {
	return &PublicHandler{authService: authService}
}

// AuthRequest is used for login.
type AuthRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// RegisterRequest extends AuthRequest with the required pickup address,
// role and optional coordinates. Role must be CUSTOMER or EXECUTOR.
type RegisterRequest struct {
	Phone      string   `json:"phone"`
	Email      string   `json:"email"`
	Password   string   `json:"password"`
	LastName   string   `json:"last_name"`
	FirstName  string   `json:"first_name"`
	Patronymic string   `json:"patronymic"`
	Address    string   `json:"address"`
	Role       string   `json:"role"`
	Lat        *float64 `json:"lat,omitempty"`
	Lon        *float64 `json:"lon,omitempty"`
}

// AuthResponse returns the token pair after a successful login or refresh.
// The refresh token is opaque and single-use: every refresh returns a new one.
type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// RegisterResponse returns the created user without sensitive fields.
type RegisterResponse struct {
	ID    string `json:"id"`
	Phone string `json:"phone"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// HealthHandler returns the service health status.
func (h *PublicHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]string{"status": "ok"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterHandler creates a new user account.
func (h *PublicHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	user, err := h.authService.RegisterWithCoordinates(r.Context(), req.Phone, req.Email, req.Password, req.LastName, req.FirstName, req.Patronymic, req.Address, req.Role, req.Lat, req.Lon)
	if err != nil {
		metrics.AuthEvent("register", "denied")
		if err.Error() == "user with this phone already exists" || err.Error() == "user with this email already exists" {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.AuthEvent("register", "ok")

	resp := RegisterResponse{
		ID:    user.ID.String(),
		Phone: user.Phone,
		Email: user.Email,
		Role:  user.Role,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// LoginHandler authenticates a user and returns a JWT.
func (h *PublicHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	// POST only: credentials must never travel in a URL, and a GET login is
	// also trivially triggerable cross-site.
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	user, err := h.authService.Authenticate(r.Context(), req.Phone, req.Password)
	if err != nil {
		// Counted separately from the 401 rate in the HTTP metrics: a burst of
		// denied logins against valid accounts is a credential-stuffing signal,
		// not just traffic.
		metrics.AuthEvent("login", "denied")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	pair, err := h.authService.IssueTokenPair(r.Context(), user)
	if err != nil {
		metrics.AuthEvent("login", "error")
		http.Error(w, "Could not generate token", http.StatusInternalServerError)
		return
	}

	metrics.AuthEvent("login", "ok")
	writeTokenPair(w, pair)
}

// writeTokenPair renders a token pair. Responses carrying credentials must not
// be stored by any cache along the way.
func writeTokenPair(w http.ResponseWriter, pair *service.TokenPair) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(AuthResponse{
		Token:        pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// RefreshHandler exchanges a refresh token for a new pair.
//
// It is intentionally unauthenticated: by the time a client needs it, the
// access token has already expired. The refresh token is the credential.
func (h *PublicHandler) RefreshHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	pair, err := h.authService.Refresh(r.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		metrics.AuthEvent("refresh", "denied")
		if errors.Is(err, service.ErrInvalidRefreshToken) {
			// One answer for every failure mode: unknown, expired, already
			// used or revoked must not be distinguishable.
			http.Error(w, "Invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Could not refresh session", http.StatusInternalServerError)
		return
	}

	metrics.AuthEvent("refresh", "ok")
	writeTokenPair(w, pair)
}

// LogoutHandler ends the current session. The access token is blacklisted for
// the rest of its lifetime and the refresh token, when the client sends one, is
// revoked — without that, logging out would leave a credential that can mint
// fresh access tokens for another 30 days.
func (h *PublicHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	tokenStr, ok := r.Context().Value(middleware.TokenKey).(string)
	if !ok || tokenStr == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// The body is optional: older clients do not send the refresh token.
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if err := h.authService.Logout(r.Context(), tokenStr, strings.TrimSpace(req.RefreshToken)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "logged out successfully"})
}

// MeHandler returns the current authenticated user details.
func (h *PublicHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var birthDateStr string
	if user.BirthDate != nil {
		birthDateStr = user.BirthDate.Format("2006-01-02")
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            user.ID,
		"phone":         user.Phone,
		"email":         user.Email,
		"role":          user.Role,
		"roles":         user.Roles,
		"balance":       user.Balance,
		"status":        user.Status,
		"first_name":    user.FirstName,
		"last_name":     user.LastName,
		"patronymic":    user.Patronymic,
		"birth_date":    birthDateStr,
		"age":           user.GetAge(),
		"is_verified":   user.IsVerified(),
		"pending_email": user.PendingEmail,
	})
}

// VerifyEmailHandler verifies email by token or redirects legacy clicks to frontend /login page.
func (h *PublicHandler) VerifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// If request comes directly from a browser click (not AJAX JSON request), redirect to frontend /login?token=...
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/login?token="+token, http.StatusFound)
		return
	}

	user, err := h.authService.VerifyEmail(r.Context(), token)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err.Error() == "verification_token_expired" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":     "Срок действия ссылки истек (60 минут). Пожалуйста, запросите изменение почты заново.",
				"code":      "TOKEN_EXPIRED",
				"can_retry": true,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Email успешно подтверждён!",
		"email":   user.Email,
	})
}

// ForgotPasswordHandler sends a password reset code to the specified email.
func (h *PublicHandler) ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := h.authService.RequestPasswordReset(r.Context(), req.Email); err != nil {
		metrics.AuthEvent("password_reset_request", "denied")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": err.Error(),
		})
		return
	}

	metrics.AuthEvent("password_reset_request", "ok")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Код восстановления отправлен на ваш Email",
	})
}

// ResetPasswordHandler resets user password with verification code.
func (h *PublicHandler) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if err := h.authService.ResetPassword(r.Context(), req.Email, req.Code, req.NewPassword); err != nil {
		metrics.AuthEvent("password_reset", "denied")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics.AuthEvent("password_reset", "ok")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Password reset successfully. You can now login with your new password.",
	})
}

// UpdateEmailHandler updates user's email address and triggers a verification email.
func (h *PublicHandler) UpdateEmailHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	updatedUser, err := h.authService.UpdateUserEmail(r.Context(), user.ID, req.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// The address only changes once the link in the mail is followed, so the
	// response says what is pending rather than claiming it is done.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"email":         updatedUser.Email,
		"pending_email": updatedUser.PendingEmail,
		"message":       "Подтвердите новый адрес по ссылке в письме — до этого почта остаётся прежней",
	})
}

// ChangePasswordHandler replaces the caller's password.
func (h *PublicHandler) ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	pair, err := h.authService.ChangePassword(r.Context(), user.ID, req.OldPassword, req.NewPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// The new pair keeps this device signed in; every other session was ended.
	writeTokenPair(w, pair)
}

// UpdateBirthDateHandler updates user's birth date.
func (h *PublicHandler) UpdateBirthDateHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		BirthDate string `json:"birth_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	updatedUser, err := h.authService.UpdateUserBirthDate(r.Context(), user.ID, req.BirthDate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var birthDateStr string
	if updatedUser.BirthDate != nil {
		birthDateStr = updatedUser.BirthDate.Format("2006-01-02")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"birth_date": birthDateStr,
		"age":        updatedUser.GetAge(),
	})
}
