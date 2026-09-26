package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AdminHandler хранит HTTP-обработчики административных операций. Ошибки
// сервиса переводятся в коды через writeDomainError: по классу ошибки, а не по
// тексту и не одним 400 на всё.
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
		writeDomainError(w, err)
		return
	}

	// Убираем пароли из ответа
	for _, u := range users {
		u.Password = ""
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"total": total,
	})
}

// UpdateUserStatusHandler блокирует или разблокирует пользователя.
func (h *AdminHandler) UpdateUserStatusHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Status string `json:"status"`
		// Reason — причина мягкого бана; для других статусов не используется.
		Reason string `json:"reason"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserStatus(r.Context(), userID, admin.ID, req.Status, req.Reason); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "status updated successfully")
}

// UpdateUserVerifiedHandler ставит или снимает флаг ручной верификации пользователя.
func (h *AdminHandler) UpdateUserVerifiedHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Verified bool `json:"verified"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.SetUserVerified(r.Context(), userID, admin.ID, req.Verified); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "verification updated successfully")
}

// UpdateUserRoleHandler меняет роль пользователя.
func (h *AdminHandler) UpdateUserRoleHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserRole(r.Context(), userID, admin.ID, req.Role); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "role updated successfully")
}

// UpdateUserRolesHandler заменяет полный набор ролей пользователя (мультироль).
func (h *AdminHandler) UpdateUserRolesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Roles []string `json:"roles"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserRoles(r.Context(), userID, admin.ID, req.Roles); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "roles updated successfully")
}

// UpdateUserAddressHandler обновляет адрес подачи заказчика (только для админов).
func (h *AdminHandler) UpdateUserAddressHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}

	var req struct {
		Address string `json:"address"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserAddress(r.Context(), userID, req.Address); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "address updated successfully")
}

// UpdateUserNameHandler обновляет ФИО пользователя (только для админов).
func (h *AdminHandler) UpdateUserNameHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}

	var req struct {
		LastName   string `json:"last_name"`
		FirstName  string `json:"first_name"`
		Patronymic string `json:"patronymic"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserName(r.Context(), userID, req.LastName, req.FirstName, req.Patronymic); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "name updated successfully")
}

// UpdateUserBirthDateHandler исправляет дату рождения пользователя. Он отделён
// от обработчика имени, чтобы отклонённая дата не откатывала принятое имя.
func (h *AdminHandler) UpdateUserBirthDateHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}

	var req struct {
		BirthDate string `json:"birth_date"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.UpdateUserBirthDate(r.Context(), userID, req.BirthDate); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "birth date updated successfully")
}

// TopUpUserBalanceHandler зачисляет средства прямо на баланс пользователя.
// Маршрут стоит за правом topups.edit: это движение денег, а не правка карточки.
func (h *AdminHandler) TopUpUserBalanceHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	adminUser, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	if err := h.adminService.TopUpUserBalance(r.Context(), userID, adminUser.ID, req.Amount); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "balance topped up successfully")
}

// pageParams читает limit/offset из строки запроса. Оба необязательны; сервис
// ограничивает их сверху.
func pageParams(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}

// listExtras решает, что класть в ответ списка сверх самой страницы.
//
// Общий счётчик и значения фильтров (фасеты) нужны один раз — при первом
// показе списка; дальше клиент листает и выгружает, держа их у себя. Поэтому
// первая страница (offset = 0) несёт и total, и фасеты всегда, а остальные —
// только по явной просьбе: total=1 и facets=1 в строке запроса. Раньше и
// COUNT(*) по всей выборке, и два DISTINCT-прохода по таблице выполнялись на
// каждой странице.
func listExtras(r *http.Request, offset int) (withTotal, withFacets bool) {
	q := r.URL.Query()
	first := offset <= 0
	return first || q.Get("total") == "1", first || q.Get("facets") == "1"
}

// GetTopUpRequestsHandler перечисляет ручные заявки на пополнение баланса.
func (h *AdminHandler) GetTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	reqs, err := h.adminService.GetTopUpRequests(r.Context(), limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// decideRequest — общий каркас одобрения/отклонения заявки: id из маршрута,
// админ из контекста, решение сервиса, сообщение.
func decideRequest(w http.ResponseWriter, r *http.Request, decide func(reqID, adminID uuid.UUID) error, message string) {
	reqID, ok := parseIDParam(w, r, "id", "request ID")
	if !ok {
		return
	}
	adminUser, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := decide(reqID, adminUser.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	writeMessage(w, message)
}

// ApproveTopUpRequestsHandler одобряет заявку на пополнение баланса.
func (h *AdminHandler) ApproveTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	decideRequest(w, r, func(reqID, adminID uuid.UUID) error {
		return h.adminService.ApproveTopUpRequest(r.Context(), reqID, adminID)
	}, "top-up request approved successfully")
}

// RejectTopUpRequestsHandler отклоняет заявку на пополнение баланса.
func (h *AdminHandler) RejectTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	decideRequest(w, r, func(reqID, adminID uuid.UUID) error {
		return h.adminService.RejectTopUpRequest(r.Context(), reqID, adminID)
	}, "top-up request rejected successfully")
}

// GetWithdrawalRequestsHandler перечисляет все ручные заявки на вывод средств.
func (h *AdminHandler) GetWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	reqs, err := h.adminService.GetWithdrawalRequests(r.Context(), limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// ApproveWithdrawalRequestsHandler одобряет заявку на вывод средств.
func (h *AdminHandler) ApproveWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	decideRequest(w, r, func(reqID, adminID uuid.UUID) error {
		return h.adminService.ApproveWithdrawalRequest(r.Context(), reqID, adminID)
	}, "withdrawal request approved successfully")
}

// RejectWithdrawalRequestsHandler отклоняет заявку на вывод средств.
func (h *AdminHandler) RejectWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	decideRequest(w, r, func(reqID, adminID uuid.UUID) error {
		return h.adminService.RejectWithdrawalRequest(r.Context(), reqID, adminID)
	}, "withdrawal request rejected successfully")
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
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, commission)
}

// PayoutCommissionHandler выводит собранную комиссию из системы. Маршрут стоит
// за правом commission.edit, поэтому вызывающий всегда аутентифицирован; именно
// он из запроса и записывается против этой выплаты.
func (h *AdminHandler) PayoutCommissionHandler(w http.ResponseWriter, r *http.Request) {
	adminUser, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Amount money.Amount `json:"amount"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	commission, err := h.adminService.PayoutCommission(r.Context(), adminUser.ID, req.Amount)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, commission)
}

// GetUserTransactionsHandler отдаёт проводки одного пользователя — историю,
// которую открывают с его карточки.
func (h *AdminHandler) GetUserTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	txs, total, err := h.adminService.GetUserTransactions(r.Context(), userID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if txs == nil {
		txs = []*repository.Transaction{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"total":        total,
	})
}

// GetUserOrdersHandler отдаёт заказы пользователя в обеих ролях — историю,
// которую открывают с его карточки.
func (h *AdminHandler) GetUserOrdersHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user ID")
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	orders, total, err := h.adminService.GetUserOrders(r.Context(), userID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if orders == nil {
		orders = []*repository.AdminOrder{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"orders": orders,
		"total":  total,
	})
}

// GetTransactionsHandler обслуживает GET /admin/transactions: страница журнала
// проводок. total и фасеты (types, periods) — на первой странице и по
// параметрам total=1 / facets=1, см. listExtras.
func (h *AdminHandler) GetTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	withTotal, withFacets := listExtras(r, offset)
	q := r.URL.Query()
	txs, total, err := h.adminService.GetTransactions(r.Context(), repository.TransactionsFilter{
		Search: q.Get("search"),
		Type:   q.Get("type"),
		Period: q.Get("period"),
		Sort:   q.Get("sort"),
		Desc:   q.Get("order") != "asc",
		Page:   repository.PageRequest{Limit: limit, Offset: offset, WithTotal: withTotal},
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if txs == nil {
		txs = []*repository.Transaction{}
	}

	resp := map[string]interface{}{"transactions": txs}
	if withTotal {
		resp["total"] = total
	}
	if withFacets {
		facets, err := h.adminService.TransactionFacets(r.Context())
		if err != nil {
			writeDomainError(w, err)
			return
		}
		resp["types"] = facets.Types
		resp["periods"] = facets.Periods
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetSettingsHandler отдаёт системные настройки.
func (h *AdminHandler) GetSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.adminService.GetSettings(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
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
		writeDomainError(w, err)
		return
	}
	writeMessage(w, "settings updated successfully")
}

// GetActiveShiftsHandler перечисляет все активные смены исполнителей.
func (h *AdminHandler) GetActiveShiftsHandler(w http.ResponseWriter, r *http.Request) {
	shifts, err := h.adminService.GetActiveShifts(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shifts)
}

// GetOrdersHandler обслуживает GET /admin/orders: один список заказов с
// фильтром по группе статусов (status = active | review | completed |
// canceled | all). Неизвестная группа читается как all. total и фасеты
// (services, periods) — на первой странице и по параметрам total=1 /
// facets=1, см. listExtras.
func (h *AdminHandler) GetOrdersHandler(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	withTotal, withFacets := listExtras(r, offset)
	q := r.URL.Query()
	statuses := repository.OrderStatusGroup(q.Get("status"))
	orders, total, err := h.adminService.GetOrders(r.Context(), repository.OrdersFilter{
		Statuses: statuses,
		Search:   q.Get("search"),
		Service:  q.Get("service"),
		Period:   q.Get("period"),
		Sort:     q.Get("sort"),
		Desc:     q.Get("order") != "asc",
		Page:     repository.PageRequest{Limit: limit, Offset: offset, WithTotal: withTotal},
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if orders == nil {
		orders = []*repository.AdminOrder{}
	}

	resp := map[string]interface{}{"orders": orders}
	if withTotal {
		resp["total"] = total
	}
	if withFacets {
		// Фасеты перечисляют все существующие услуги и месяцы группы, а не
		// только попавшие на экран.
		facets, err := h.adminService.OrderFacets(r.Context(), statuses)
		if err != nil {
			writeDomainError(w, err)
			return
		}
		resp["services"] = facets.Services
		resp["periods"] = facets.Periods
	}
	writeJSON(w, http.StatusOK, resp)
}

// SendBroadcastEmailHandler рассылает письмо выбранным получателям.
func (h *AdminHandler) SendBroadcastEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req service.BroadcastEmailRequest
	if !decodeBody(w, r, &req) {
		return
	}

	res, err := h.adminService.SendBroadcastEmail(r.Context(), req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// RegisterAdminRoutes — пользователи, деньги, настройки и мониторинг. Что
// именно можно, решает право на каждом маршруте — раздел плюс действие из
// каталога service/permission.go.
func (h *AdminHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("users.view")).Get("/admin/users", h.GetUsersHandler)
	r.With(can("users.edit")).Post("/admin/users/{id}/status", h.UpdateUserStatusHandler)
	r.With(can("users.edit")).Post("/admin/users/{id}/verified", h.UpdateUserVerifiedHandler)
	// Роль пользователя меняется на его карточке, но это раздача прав,
	// поэтому охраняется правом на роли, а не правом на пользователей.
	r.With(can("roles.edit")).Post("/admin/users/{id}/role", h.UpdateUserRoleHandler)
	r.With(can("roles.edit")).Post("/admin/users/{id}/roles", h.UpdateUserRolesHandler)
	r.With(can("users.edit")).Post("/admin/users/{id}/address", h.UpdateUserAddressHandler)
	r.With(can("users.edit")).Post("/admin/users/{id}/name", h.UpdateUserNameHandler)
	r.With(can("users.edit")).Post("/admin/users/{id}/birth-date", h.UpdateUserBirthDateHandler)
	// Прямое зачисление с карточки — движение денег, а не правка карточки:
	// охраняется тем же правом, что одобрение заявок на пополнение.
	r.With(can("topups.edit")).Post("/admin/users/{id}/balance", h.TopUpUserBalanceHandler)
	// Истории с карточки пользователя. Охраняются правом на тот раздел,
	// который они показывают, а не правом на пользователей: кто не допущен
	// к журналу проводок, не должен читать его и здесь.
	r.With(can("transactions.view")).Get("/admin/users/{id}/transactions", h.GetUserTransactionsHandler)
	r.With(can("orders.view")).Get("/admin/users/{id}/orders", h.GetUserOrdersHandler)

	r.With(can("topups.view")).Get("/admin/finances/topups", h.GetTopUpRequestsHandler)
	r.With(can("topups.edit")).Post("/admin/finances/topups/{id}/approve", h.ApproveTopUpRequestsHandler)
	r.With(can("topups.edit")).Post("/admin/finances/topups/{id}/reject", h.RejectTopUpRequestsHandler)
	r.With(can("withdrawals.view")).Get("/admin/finances/withdrawals", h.GetWithdrawalRequestsHandler)
	r.With(can("withdrawals.edit")).Post("/admin/finances/withdrawals/{id}/approve", h.ApproveWithdrawalRequestsHandler)
	r.With(can("withdrawals.edit")).Post("/admin/finances/withdrawals/{id}/reject", h.RejectWithdrawalRequestsHandler)
	r.With(can("commission.view")).Get("/admin/finances/commission", h.GetCommissionHandler)
	r.With(can("commission.edit")).Post("/admin/finances/commission/payout", h.PayoutCommissionHandler)
	r.With(can("transactions.view")).Get("/admin/transactions", h.GetTransactionsHandler)
	r.With(can("reconciliation.view")).Get("/admin/finances/reconciliation", h.GetReconciliationHandler)
	r.With(can("settings.view")).Get("/admin/settings", h.GetSettingsHandler)
	r.With(can("settings.edit")).Post("/admin/settings", h.UpdateSettingsHandler)
	r.With(can("shifts.view")).Get("/admin/shifts/active", h.GetActiveShiftsHandler)
	r.With(can("orders.view")).Get("/admin/orders", h.GetOrdersHandler)
	r.With(can("broadcasts.create")).Post("/admin/broadcast-email", h.SendBroadcastEmailHandler)
}
