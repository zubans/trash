package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"healthlogin/backend/metrics"
	"healthlogin/backend/middleware"
	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
)

// PublicHandler хранит публичные HTTP-обработчики (health, регистрация, вход).
type PublicHandler struct {
	authService *service.AuthService
	// permissions даёт /auth/me список действующих прав пользователя. Интерфейс
	// строит по нему меню и прячет кнопки, а не гадает по названию роли.
	permissions *service.Permissions
	// passports — согласие на обработку персональных данных, паспорт при
	// регистрации и статус «проверенный» в /auth/me.
	passports *service.PassportService
}

// NewPublicHandler создаёт PublicHandler с переданным AuthService.
func NewPublicHandler(authService *service.AuthService) *PublicHandler {
	return &PublicHandler{authService: authService}
}

// WithPassports подключает паспорт и согласие к регистрации и /auth/me.
func (h *PublicHandler) WithPassports(passports *service.PassportService) *PublicHandler {
	h.passports = passports
	return h
}

// WithPermissions подключает службу прав к ответу /auth/me.
func (h *PublicHandler) WithPermissions(permissions *service.Permissions) *PublicHandler {
	h.permissions = permissions
	return h
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
	// PDConsent — галочка согласия на обработку персональных данных,
	// обязательна.
	PDConsent bool `json:"pd_consent"`
	// Passport — необязательный паспорт; фото прикладывается после входа.
	Passport *service.PassportData `json:"passport,omitempty"`
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
	// PassportSaved — паспорт из формы регистрации записан.
	PassportSaved bool `json:"passport_saved,omitempty"`
}

// HealthHandler возвращает состояние здоровья сервиса.
func (h *PublicHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"status": "ok"}
	writeJSON(w, http.StatusOK, resp)
}

// RegisterHandler создаёт новую учётную запись.
func (h *PublicHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if h.passports != nil {
		if !req.PDConsent {
			metrics.AuthEvent("register", "denied")
			http.Error(w, "pd_consent_required", http.StatusBadRequest)
			return
		}
		// Паспорт проверяется до создания учётной записи: иначе она создалась
		// бы, а паспорт — нет, и человек не понял бы почему.
		if req.Passport != nil {
			if err := h.passports.ValidateData(*req.Passport); err != nil {
				writePassportError(w, err)
				return
			}
		}
	}

	user, err := h.authService.RegisterWithCoordinates(r.Context(), req.Phone, req.Email, req.Password, req.LastName, req.FirstName, req.Patronymic, req.BirthDate, req.Address, req.Role, req.Lat, req.Lon)
	if err != nil {
		metrics.AuthEvent("register", "denied")
		// По классу, а не по тексту: занятый телефон или почта — 409, негодные
		// данные формы — 400 с текстом, всё прочее — сбой, о котором клиенту
		// знать нечего.
		switch {
		case errors.Is(err, service.ErrPhoneTaken), errors.Is(err, service.ErrEmailTaken):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, service.ErrValidation):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			log.Printf("[auth] register: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	metrics.AuthEvent("register", "ok")

	resp := RegisterResponse{
		ID:    user.ID.String(),
		Phone: user.Phone,
		Email: user.Email,
		Role:  user.Role,
	}
	if h.passports != nil && req.Passport != nil {
		// Сбой записи паспорта регистрацию не отменяет: учётная запись уже есть,
		// а паспорт можно заполнить в профиле. Клиент узнаёт об этом по флагу.
		if err := h.passports.SaveAtRegistration(r.Context(), user.ID, *req.Passport); err != nil {
			log.Printf("[passport] registration of %s: %v", user.ID, err)
		} else {
			resp.PassportSaved = true
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

// LoginHandler аутентифицирует пользователя и возвращает JWT.
func (h *PublicHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, AuthResponse{
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
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out successfully"})
}

// MeHandler возвращает данные текущего аутентифицированного пользователя.
func (h *PublicHandler) MeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	// Права отдаются вместе с профилем, а не отдельным запросом: меню рисуется
	// в тот же момент, что и остальная шапка, и второй круг ожидания сделал бы
	// его мигающим.
	permissions := []string{}
	if h.permissions != nil {
		if effective := h.permissions.Effective(r.Context(), user); effective != nil {
			permissions = effective
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":            user.ID,
		"permissions":   permissions,
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
		"is_checked":    user.Checked,
		"pending_email": user.PendingEmail,
		// Окно согласия показывается, пока пользователь не принял текущую
		// редакцию: зарегистрировавшимся до галочки и после её правки.
		"pd_consent_required": h.passports != nil && h.passports.ConsentRequired(r.Context(), user),
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
		switch {
		case errors.Is(err, service.ErrVerificationTokenExpired):
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":     "Срок действия ссылки истек (60 минут). Пожалуйста, запросите изменение почты заново.",
				"code":      "TOKEN_EXPIRED",
				"can_retry": true,
			})
		case errors.Is(err, service.ErrValidation):
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		default:
			// Сбой базы — не «неверная ссылка»: 500 без внутреннего текста.
			log.Printf("[auth] verify email: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
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
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	metrics.AuthEvent("password_reset_request", "ok")
	writeJSON(w, http.StatusOK, map[string]string{
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
		// Неверный код и негодный пароль — ErrValidation (422), сбой — 500 без текста.
		writeDomainError(w, err)
		return
	}
	metrics.AuthEvent("password_reset", "ok")

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Password reset successfully. You can now login with your new password.",
	})
}

// UpdateEmailHandler меняет адрес почты пользователя и запускает письмо подтверждения.
func (h *PublicHandler) UpdateEmailHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
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
		writeDomainError(w, err)
		return
	}

	// Адрес меняется только после перехода по ссылке из письма, поэтому ответ
	// сообщает, что операция ожидает подтверждения, а не что она выполнена.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":        "ok",
		"email":         updatedUser.Email,
		"pending_email": updatedUser.PendingEmail,
		"message":       "Подтвердите новый адрес по ссылке в письме — до этого почта остаётся прежней",
	})
}

// ChangePasswordHandler заменяет пароль вызывающего.
func (h *PublicHandler) ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
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
		writeDomainError(w, err)
		return
	}

	// Новая пара оставляет это устройство в системе; все прочие сессии завершены.
	writeTokenPair(w, pair)
}

// UpdateBirthDateHandler обновляет дату рождения пользователя.
func (h *PublicHandler) UpdateBirthDateHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
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
		// ErrBirthDateLocked — класс ErrForbidden (403), негодная дата — 422.
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"birth_date": updatedUser.BirthDateString(),
		"age":        updatedUser.GetAge(),
	})
}

// AuthLimiters — ограничители частоты для эндпоинтов с учётными данными,
// которые есть смысл перебирать. Собираются в composition root.
type AuthLimiters struct {
	Register      func(http.Handler) http.Handler
	Login         func(http.Handler) http.Handler
	Refresh       func(http.Handler) http.Handler
	PasswordReset func(http.Handler) http.Handler
}

// RegisterPublicRoutes — вход, регистрация и восстановление доступа.
func (h *PublicHandler) RegisterPublicRoutes(r chi.Router, limits AuthLimiters) {
	r.Get("/health", h.HealthHandler)
	r.With(limits.Register).Post("/register", h.RegisterHandler)
	r.With(limits.Login).Post("/login", h.LoginHandler)
	// Обновление намеренно без аутентификации: к моменту, когда клиенту это нужно,
	// access-токен уже истёк. Учётными данными служит refresh-токен, поэтому
	// эндпоинт ограничен по частоте, как и прочие эндпоинты с учётными данными.
	r.With(limits.Refresh).Post("/auth/refresh", h.RefreshHandler)
	r.Get("/auth/verify-email", h.VerifyEmailHandler)
	r.With(limits.PasswordReset).Post("/auth/forgot-password", h.ForgotPasswordHandler)
	r.With(limits.PasswordReset).Post("/auth/reset-password", h.ResetPasswordHandler)
}

// RegisterUserRoutes — профиль сессии: кто я, смена почты, даты рождения и
// пароля, выход. passwordReset — тот же ограничитель, что у восстановления.
func (h *PublicHandler) RegisterUserRoutes(r chi.Router, passwordReset func(http.Handler) http.Handler) {
	r.Get("/auth/me", h.MeHandler)
	r.Post("/user/email", h.UpdateEmailHandler)
	r.Post("/user/birth-date", h.UpdateBirthDateHandler)
	r.With(passwordReset).Post("/user/change-password", h.ChangePasswordHandler)
	r.Post("/logout", h.LogoutHandler)
}
