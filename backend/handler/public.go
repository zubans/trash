package handler

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
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
	Phone    string   `json:"phone"`
	Password string   `json:"password"`
	Address  string   `json:"address"`
	Role     string   `json:"role"`
	Lat      *float64 `json:"lat,omitempty"`
	Lon      *float64 `json:"lon,omitempty"`
}

// AuthResponse returns a JWT after successful login.
type AuthResponse struct {
	Token string `json:"token"`
}

// RegisterResponse returns the created user without sensitive fields.
type RegisterResponse struct {
	ID    string `json:"id"`
	Phone string `json:"phone"`
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

	user, err := h.authService.RegisterWithCoordinates(req.Phone, req.Password, req.Address, req.Role, req.Lat, req.Lon)
	if err != nil {
		if err.Error() == "user already exists" {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := RegisterResponse{
		ID:    user.ID.String(),
		Phone: user.Phone,
		Role:  user.Role,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// LoginHandler authenticates a user and returns a JWT.
func (h *PublicHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	user, err := h.authService.Authenticate(req.Phone, req.Password)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := h.authService.GenerateJWT(user)
	if err != nil {
		http.Error(w, "Could not generate token", http.StatusInternalServerError)
		return
	}

	resp := AuthResponse{Token: token}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// MeHandler returns the current authenticated user details.
func (h *PublicHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       user.ID,
		"phone":    user.Phone,
		"role":     user.Role,
		"balance":  user.Balance,
		"status":   user.Status,
	})
}
