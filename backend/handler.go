package main

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/service"
)

// Handler holds HTTP handlers and their dependencies.
type Handler struct {
	authService *service.AuthService
}

// NewHandler creates a Handler with the provided AuthService.
func NewHandler(authService *service.AuthService) *Handler {
	return &Handler{authService: authService}
}

// AuthRequest is used for both registration and login.
type AuthRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
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
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]string{"status": "ok"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterHandler creates a new user account.
func (h *Handler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	user, err := h.authService.Register(req.Phone, req.Password)
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
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
