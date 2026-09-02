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

// PublicHandler хранит публичные HTTP-обработчики (health, регистрация, вход).
type PublicHandler struct {
	authService *service.AuthService
}

// NewPublicHandler создаёт PublicHandler с переданным AuthService.
func NewPublicHandler(authService *service.AuthService) *PublicHandler {
	return &PublicHandler{authService: authService}
}

// AuthRequest используется для входа.
type AuthRequest struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
}

// RegisterRequest дополняет AuthRequest обязательным адресом подачи, датой
// рождения, ролью и необязательными координатами. Роль должна быть CUSTOMER
// или EXECUTOR, а birth_date — в формате YYYY-MM-DD.
type RegisterRequest struct {
	Phone      string   `json:"phone"`
	Email      string   `json:"email"`
	Password   string   `json:"password"`
	LastName   string   `json:"last_name"`
	FirstName  string   `json:"first_name"`
	Patronymic string   `json:"patronymic"`
	BirthDate  string   `json:"birth_date"`
	Address    string   `json:"address"`
	Role       string   `json:"role"`
	Lat        *float64 `json:"lat,omitempty"`
	Lon        *float64 `json:"lon,omitempty"`
}

// AuthResponse возвращает пару токенов после успешного входа или обновления.
// Refresh-токен непрозрачен и одноразов: каждое обновление возвращает новый.
type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
}

// RegisterResponse возвращает созданного пользователя без чувствительных полей.
type RegisterResponse struct {
	ID    string `json:"id"`
	Phone string `json:"phone"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// HealthHandler возвращает состояние здоровья сервиса.
func (h *PublicHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]string{"status": "ok"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RegisterHandler создаёт новую учётную запись.
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

	user, err := h.authService.RegisterWithCoordinates(r.Context(), req.Phone, req.Email, req.Password, req.LastName, req.FirstName, req.Patronymic, req.BirthDate, req.Address, req.Role, req.Lat, req.Lon)
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

// LoginHandler аутентифицирует пользователя и возвращает JWT.
func (h *PublicHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	// Только POST: учётные данные не должны путешествовать в URL, а вход через
	// GET к тому же тривиально запускается с чужого сайта.
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
		// Считается отдельно от общей доли 401 в HTTP-метриках: всплеск отказанных
		// входов по действительным учёткам — сигнал подстановки учётных данных, а не
		// просто трафик.
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

// writeTokenPair отдаёт пару токенов. Ответы с учётными данными не должны
// кэшироваться ничем по дороге.
func writeTokenPair(w http.ResponseWriter, pair *service.TokenPair) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(AuthResponse{
		Token:        pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// RefreshHandler обменивает refresh-токен на новую пару.
//
// Он намеренно не аутентифицирован: к моменту, когда клиенту он нужен,
// access-токен уже истёк. Учётными данными здесь служит refresh-токен.
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
			// Один ответ на все виды отказа: неизвестный, истёкший, уже
			// использованный или отозванный не должны различаться.
			http.Error(w, "Invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Could not refresh session", http.StatusInternalServerError)
		return
	}

	metrics.AuthEvent("refresh", "ok")
	writeTokenPair(w, pair)
}

// LogoutHandler завершает текущую сессию. Access-токен заносится в чёрный
// список до конца своего срока, а refresh-токен, если клиент его прислал,
// отзывается: без этого выход оставил бы учётные данные, способные ещё 30
// дней штамповать свежие access-токены.
func (h *PublicHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	tokenStr, ok := r.Context().Value(middleware.TokenKey).(string)
	if !ok || tokenStr == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Тело необязательно: старые клиенты не присылают refresh-токен.
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

// MeHandler возвращает данные текущего аутентифицированного пользователя.
func (h *PublicHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
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
		"birth_date":    user.BirthDateString(),
		"age":           user.GetAge(),
		"is_verified":   user.IsVerified(),
		"pending_email": user.PendingEmail,
	})
}

// VerifyEmailHandler подтверждает почту по токену или перенаправляет старые переходы на страницу /login фронтенда.
func (h *PublicHandler) VerifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Если запрос пришёл прямо из клика в браузере (а не AJAX-запросом JSON), редиректим на /login?token=... фронтенда
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

// ForgotPasswordHandler отправляет код сброса пароля на указанную почту.
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

// ResetPasswordHandler сбрасывает пароль пользователя по коду подтверждения.
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

// UpdateEmailHandler меняет адрес почты пользователя и запускает письмо подтверждения.
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

	// Адрес меняется только после перехода по ссылке из письма, поэтому ответ
	// сообщает, что операция ожидает подтверждения, а не что она выполнена.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"email":         updatedUser.Email,
		"pending_email": updatedUser.PendingEmail,
		"message":       "Подтвердите новый адрес по ссылке в письме — до этого почта остаётся прежней",
	})
}

// ChangePasswordHandler заменяет пароль вызывающего.
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

	// Новая пара оставляет это устройство в системе; все прочие сессии завершены.
	writeTokenPair(w, pair)
}

// UpdateBirthDateHandler обновляет дату рождения пользователя.
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"birth_date": updatedUser.BirthDateString(),
		"age":        updatedUser.GetAge(),
	})
}
