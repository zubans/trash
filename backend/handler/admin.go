package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AdminHandler хранит HTTP-обработчики административных операций.
type AdminHandler struct {
	adminService *service.AdminService
}

// NewAdminHandler создаёт новый AdminHandler.
func NewAdminHandler(adminService *service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

// GetUsersHandler отдаёт постраничный отфильтрованный список пользователей.
func (h *AdminHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	users, total, err := h.adminService.GetUsers(r.Context(), page, limit, role, status, search)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Убираем пароли из ответа
	for _, u := range users {
		u.Password = ""
	}

	resp := map[string]interface{}{
		"users": users,
		"total": total,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateUserStatusHandler блокирует или разблокирует пользователя.
func (h *AdminHandler) UpdateUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	admin, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserStatus(r.Context(), userID, admin.ID, req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "status updated successfully"})
}

// UpdateUserVerifiedHandler ставит или снимает флаг ручной верификации пользователя.
func (h *AdminHandler) UpdateUserVerifiedHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	admin, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Verified bool `json:"verified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.SetUserVerified(r.Context(), userID, admin.ID, req.Verified); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "verification updated successfully"})
}

// UpdateUserRoleHandler меняет роль пользователя.
func (h *AdminHandler) UpdateUserRoleHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	admin, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserRole(r.Context(), userID, admin.ID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "role updated successfully"})
}

// UpdateUserRolesHandler заменяет полный набор ролей пользователя
// (мультироль). Только для админов.
func (h *AdminHandler) UpdateUserRolesHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	admin, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserRoles(r.Context(), userID, admin.ID, req.Roles); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "roles updated successfully"})
}

// UpdateUserAddressHandler обновляет адрес подачи заказчика (только для админов).
func (h *AdminHandler) UpdateUserAddressHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserAddress(r.Context(), userID, req.Address); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "address updated successfully"})
}

// UpdateUserNameHandler обновляет ФИО пользователя (только для админов).
func (h *AdminHandler) UpdateUserNameHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		LastName   string `json:"last_name"`
		FirstName  string `json:"first_name"`
		Patronymic string `json:"patronymic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserName(r.Context(), userID, req.LastName, req.FirstName, req.Patronymic); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "name updated successfully"})
}

// UpdateUserBirthDateHandler исправляет дату рождения пользователя. Он отделён
// от обработчика имени, чтобы отклонённая дата не откатывала принятое имя.
func (h *AdminHandler) UpdateUserBirthDateHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		BirthDate string `json:"birth_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateUserBirthDate(r.Context(), userID, req.BirthDate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "birth date updated successfully"})
}

// TopUpUserBalanceHandler зачисляет средства прямо на баланс пользователя.
func (h *AdminHandler) TopUpUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.TopUpUserBalance(r.Context(), userID, adminUser.ID, req.Amount); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "balance topped up successfully"})
}

// pageParams читает limit/offset из строки запроса. Оба необязательны; сервис
// ограничивает их сверху.
func pageParams(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}

// GetTopUpRequestsHandler перечисляет ручные заявки на пополнение баланса.
func (h *AdminHandler) GetTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	reqs, err := h.adminService.GetTopUpRequests(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs)
}

// ApproveTopUpRequestsHandler одобряет заявку на пополнение баланса.
func (h *AdminHandler) ApproveTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid request ID", http.StatusBadRequest)
		return
	}

	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.adminService.ApproveTopUpRequest(r.Context(), reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "top-up request approved successfully"})
}

// RejectTopUpRequestsHandler отклоняет заявку на пополнение баланса.
func (h *AdminHandler) RejectTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid request ID", http.StatusBadRequest)
		return
	}

	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.adminService.RejectTopUpRequest(r.Context(), reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "top-up request rejected successfully"})
}

// CreateWithdrawalRequestHandler создаёт заявку на вывод для аутентифицированного пользователя.
func (h *AdminHandler) CreateWithdrawalRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	wReq, err := h.adminService.CreateWithdrawalRequest(r.Context(), user.ID, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(wReq)
}

// GetWithdrawalRequestsHandler перечисляет все ручные заявки на вывод средств.
func (h *AdminHandler) GetWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	reqs, err := h.adminService.GetWithdrawalRequests(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs)
}

// ApproveWithdrawalRequestsHandler одобряет заявку на вывод средств.
func (h *AdminHandler) ApproveWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid request ID", http.StatusBadRequest)
		return
	}

	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.adminService.ApproveWithdrawalRequest(r.Context(), reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "withdrawal request approved successfully"})
}

// RejectWithdrawalRequestsHandler отклоняет заявку на вывод средств.
func (h *AdminHandler) RejectWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	reqID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid request ID", http.StatusBadRequest)
		return
	}

	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.adminService.RejectWithdrawalRequest(r.Context(), reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "withdrawal request rejected successfully"})
}

// GetReconciliationHandler сообщает, сходятся ли ещё сохранённые балансы с
// журналом транзакций.
func (h *AdminHandler) GetReconciliationHandler(w http.ResponseWriter, r *http.Request) {
	tolerance := money.FromRubles(0.01)
	if raw := r.URL.Query().Get("tolerance"); raw != "" {
		parsed, err := money.ParseRubles(raw)
		if err != nil || parsed.IsNegative() {
			http.Error(w, "invalid tolerance", http.StatusBadRequest)
			return
		}
		tolerance = parsed
	}

	report, err := h.adminService.Reconcile(r.Context(), tolerance)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                        report.OK(),
		"summary":                   report.Summary(),
		"users_checked":             report.UsersChecked,
		"discrepancies":             report.Discrepancies,
		"hold_anomalies":            report.HoldAnomalies,
		"unknown_transaction_types": report.UnknownTypes,
		"books":                     report.Books,
		"books_open":                report.BooksOpen,
		"escrow_mismatch":           report.EscrowMismatch,
	})
}

// GetCommissionHandler сообщает собранную комиссию платформы и ставку, по
// которой она берётся.
func (h *AdminHandler) GetCommissionHandler(w http.ResponseWriter, r *http.Request) {
	commission, err := h.adminService.GetCommission(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commission)
}

// PayoutCommissionHandler выводит собранную комиссию из системы. Маршрут стоит
// за RequireAdmin, поэтому вызывающий всегда админ; именно админ из запроса и
// записывается против этой выплаты.
func (h *AdminHandler) PayoutCommissionHandler(w http.ResponseWriter, r *http.Request) {
	adminUser, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	commission, err := h.adminService.PayoutCommission(r.Context(), adminUser.ID, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commission)
}

// GetTransactionsHandler отдаёт аудит-логи транзакций.
func (h *AdminHandler) GetTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	txs, err := h.adminService.GetTransactions(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txs)
}

// GetPublicSettingsHandler возвращает публичные системные настройки (например, валюту).
func (h *AdminHandler) GetPublicSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.adminService.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"currency":                 settings["currency"],
		"shift_early_exit_penalty": settings["shift_early_exit_penalty"],
		"executor_location_send_interval_seconds": settings["executor_location_send_interval_seconds"],
		// Должны ли приложения исполнителей сообщать своё положение во время смены.
		// Именно эти отчёты держат сохранённую позицию свежей для карты и
		// автоматического подбора. Геозона, по которой это названо, исчезла; ключ
		// сохранён, чтобы у существующих установок осталась их настройка.
		"geofence_tracking_enabled": settings["geofence_tracking_enabled"],
	})
}

// GetSettingsHandler отдаёт системные настройки.
func (h *AdminHandler) GetSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.adminService.GetSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// UpdateSettingsHandler обновляет системные настройки.
//
// Значения приходят либо строками JSON, либо числами JSON. Настройки хранятся
// текстом, и раньше это декодировалось прямо в map[string]string — из-за чего
// одно числовое значение роняло весь запрос невнятным «invalid request body».
// Форма админки привязывает числовые поля к <input type="number">, а Vue
// возвращает их настоящими числами, так что правка тарифа или ставки комиссии
// отправляла число и вовсе не сохранялась.
func (h *AdminHandler) UpdateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	// Числа сохраняют присланные клиентом цифры, не проходя через float,
	// поэтому 8.50 хранится так, как записано, а не как 8.5.
	decoder.UseNumber()

	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req := make(map[string]string, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			req[key] = v
		case json.Number:
			req[key] = v.String()
		default:
			// Всё прочее — ошибка клиента, и назвать ключ лучше, чем заставлять
			// админа гадать, какое из десятка полей отвергли.
			http.Error(w, "setting "+key+" must be a string or a number", http.StatusBadRequest)
			return
		}
	}

	if err := h.adminService.UpdateSettings(r.Context(), req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "settings updated successfully"})
}

// CreateTopUpRequestHandler создаёт заявку на пополнение баланса (эндпоинт заказчика).
func (h *AdminHandler) CreateTopUpRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	topupReq, err := h.adminService.CreateTopUpRequest(r.Context(), user.ID, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(topupReq)
}

// GetProfileHandler возвращает профиль аутентифицированного пользователя, включая адрес заказчика.
func (h *AdminHandler) GetProfileHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	profile, err := h.adminService.GetProfile(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// writeAddresses отдаёт список сохранённых адресов в том виде, какого ждёт страница профиля.
func writeAddresses(w http.ResponseWriter, addresses []repository.Address, err error) {
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAddressLimitReached):
			http.Error(w, "можно сохранить не более 2 адресов", http.StatusConflict)
		case errors.Is(err, repository.ErrAddressNotFound):
			http.Error(w, "адрес не найден", http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"addresses": addresses})
}

// AddAddressHandler сохраняет адрес подачи для аутентифицированного заказчика.
func (h *AdminHandler) AddAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req addressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	addresses, err := h.adminService.AddAddress(r.Context(), user.ID, req.toAddress())
	writeAddresses(w, addresses, err)
}

// DeleteAddressHandler удаляет один из сохранённых адресов вызывающего. Клиент
// адресует его по id; позиционный индекс тоже принимается, потому что
// установленное приложение присылает номер строки.
func (h *AdminHandler) DeleteAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	raw := chi.URLParam(r, "id")
	addressID, err := uuid.Parse(raw)
	if err != nil {
		index, convErr := strconv.Atoi(raw)
		if convErr != nil {
			http.Error(w, "invalid address id", http.StatusBadRequest)
			return
		}
		current, listErr := h.adminService.ListAddresses(r.Context(), user.ID)
		if listErr != nil {
			http.Error(w, listErr.Error(), http.StatusInternalServerError)
			return
		}
		if index < 0 || index >= len(current) {
			http.Error(w, "адрес не найден", http.StatusNotFound)
			return
		}
		addressID = current[index].ID
	}

	addresses, err := h.adminService.DeleteAddress(r.Context(), user.ID, addressID)
	writeAddresses(w, addresses, err)
}

// SetDefaultAddressHandler отмечает, с какого сохранённого адреса начинаются новые заказы.
func (h *AdminHandler) SetDefaultAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var (
		addresses []repository.Address
		err       error
	)
	if id, parseErr := uuid.Parse(req.ID); parseErr == nil {
		addresses, err = h.adminService.SetDefaultAddress(r.Context(), user.ID, id)
	} else {
		addresses, err = h.adminService.SetDefaultAddressByValue(r.Context(), user.ID, req.Address)
	}
	writeAddresses(w, addresses, err)
}

// GetActiveShiftsHandler перечисляет все активные смены исполнителей.
func (h *AdminHandler) GetActiveShiftsHandler(w http.ResponseWriter, r *http.Request) {
	shifts, err := h.adminService.GetActiveShifts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shifts)
}

// GetActiveOrdersHandler перечисляет активные заказы заказчиков (в поиске или назначенные).
func (h *AdminHandler) GetActiveOrdersHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	orders, err := h.adminService.GetActiveOrders(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetCompletedOrdersHandler перечисляет завершённые заказы заказчиков.
func (h *AdminHandler) GetCompletedOrdersHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	q := r.URL.Query()
	orders, total, err := h.adminService.GetCompletedOrders(r.Context(), repository.CompletedOrdersFilter{
		Search:  q.Get("search"),
		Service: q.Get("service"),
		Period:  q.Get("period"),
		Sort:    q.Get("sort"),
		Desc:    q.Get("order") != "asc",
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []*repository.AdminOrder{}
	}
	// Фасеты едут вместе со страницей, чтобы выпадающие фильтры перечисляли все
	// существующие услуги и месяцы, а не только попавшие на экран.
	facets, err := h.adminService.CompletedOrderFacets(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orders":   orders,
		"total":    total,
		"services": facets.Services,
		"periods":  facets.Periods,
	})
}

// SendBroadcastEmailHandler рассылает письмо выбранным получателям.
func (h *AdminHandler) SendBroadcastEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req service.BroadcastEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	res, err := h.adminService.SendBroadcastEmail(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// addressRequest принимает обе формы, которые может прислать клиент: одну
// строку, которую до сих пор шлют установленные мобильные сборки, и части,
// приходящие прямо из списка подсказок. Именно отправка частей пропускает
// корпус или строение, ведь их не приходится выпарсивать обратно из строки.
type addressRequest struct {
	Address string   `json:"address"`
	Region  string   `json:"region"`
	City    string   `json:"city"`
	Street  string   `json:"street"`
	House   string   `json:"house"`
	Flat    string   `json:"flat"`
	FiasID  string   `json:"fias_id"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	Source  string   `json:"source"`
}

// toAddress предпочитает части и откатывается к разбору строки.
func (r addressRequest) toAddress() service.Address {
	if r.City == "" && r.Street == "" && r.House == "" {
		addr := service.ParseAddressLine(r.Address)
		// Квартира, присланная рядом с легаси-строкой, всё равно применяется: именно
		// так её отправляет старый экран регистрации.
		if r.Flat != "" {
			addr = addr.WithFlat(r.Flat)
		}
		return addr
	}

	return service.Address{
		Value:  r.Address,
		Region: r.Region,
		City:   r.City,
		Street: r.Street,
		House:  r.House,
		Flat:   r.Flat,
		FiasID: r.FiasID,
		Lat:    r.Lat,
		Lon:    r.Lon,
		Source: firstNonEmpty(r.Source, service.SourceDaData),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
