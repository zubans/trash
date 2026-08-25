package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// AdminHandler holds HTTP handler functions for admin operations.
type AdminHandler struct {
	adminService *service.AdminService
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(adminService *service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

// GetUsersHandler retrieves a paginated and filtered list of users.
func (h *AdminHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	role := r.URL.Query().Get("role")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	users, total, err := h.adminService.GetUsers(page, limit, role, status, search)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Remove passwords from response
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

// UpdateUserStatusHandler blocks or unblocks a user.
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

	if err := h.adminService.UpdateUserStatus(userID, admin.ID, req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "status updated successfully"})
}

// UpdateUserRoleHandler changes a user's role.
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

	if err := h.adminService.UpdateUserRole(userID, admin.ID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "role updated successfully"})
}

// UpdateUserAddressHandler updates a customer's pickup address (admin-only).
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

	if err := h.adminService.UpdateUserAddress(userID, req.Address); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "address updated successfully"})
}

// UpdateUserNameHandler updates a user's full name (admin-only).
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

	if err := h.adminService.UpdateUserName(userID, req.LastName, req.FirstName, req.Patronymic); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "name updated successfully"})
}

// TopUpUserBalanceHandler adds funds directly to a user's balance.
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
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.TopUpUserBalance(userID, adminUser.ID, req.Amount); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "balance topped up successfully"})
}

// GetTopUpRequestsHandler lists all manual balance top-up requests.
func (h *AdminHandler) GetTopUpRequestsHandler(w http.ResponseWriter, r *http.Request) {
	reqs, err := h.adminService.GetTopUpRequests()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs)
}

// ApproveTopUpRequestsHandler approves a balance top-up request.
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

	if err := h.adminService.ApproveTopUpRequest(reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "top-up request approved successfully"})
}

// RejectTopUpRequestsHandler rejects a balance top-up request.
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

	if err := h.adminService.RejectTopUpRequest(reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "top-up request rejected successfully"})
}

// CreateWithdrawalRequestHandler creates a withdrawal request for the authenticated user.
func (h *AdminHandler) CreateWithdrawalRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	wReq, err := h.adminService.CreateWithdrawalRequest(user.ID, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(wReq)
}

// GetWithdrawalRequestsHandler lists all manual balance withdrawal requests.
func (h *AdminHandler) GetWithdrawalRequestsHandler(w http.ResponseWriter, r *http.Request) {
	reqs, err := h.adminService.GetWithdrawalRequests()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reqs)
}

// ApproveWithdrawalRequestsHandler approves a balance withdrawal request.
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

	if err := h.adminService.ApproveWithdrawalRequest(reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "withdrawal request approved successfully"})
}

// RejectWithdrawalRequestsHandler rejects a balance withdrawal request.
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

	if err := h.adminService.RejectWithdrawalRequest(reqID, adminUser.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "withdrawal request rejected successfully"})
}

// GetReconciliationHandler reports whether stored balances still agree with the
// transaction log.
func (h *AdminHandler) GetReconciliationHandler(w http.ResponseWriter, r *http.Request) {
	tolerance := 0.01
	if raw := r.URL.Query().Get("tolerance"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid tolerance", http.StatusBadRequest)
			return
		}
		tolerance = parsed
	}

	report, err := h.adminService.Reconcile(tolerance)
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
	})
}

// GetTransactionsHandler retrieves audit logs of transactions.
func (h *AdminHandler) GetTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	txs, err := h.adminService.GetTransactions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txs)
}

// GetPublicSettingsHandler returns public system settings (e.g. currency).
func (h *AdminHandler) GetPublicSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.adminService.GetSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"currency":                 settings["currency"],
		"shift_early_exit_penalty": settings["shift_early_exit_penalty"],
		"executor_location_send_interval_seconds": settings["executor_location_send_interval_seconds"],
	})
}

// GetSettingsHandler retrieves system settings.
func (h *AdminHandler) GetSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.adminService.GetSettings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// UpdateSettingsHandler updates system settings.
func (h *AdminHandler) UpdateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.adminService.UpdateSettings(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "settings updated successfully"})
}

// CreateTopUpRequestHandler creates a balance top-up request (Customer endpoint).
func (h *AdminHandler) CreateTopUpRequestHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	topupReq, err := h.adminService.CreateTopUpRequest(user.ID, req.Amount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(topupReq)
}

// GetProfileHandler returns the authenticated user's profile info including customer address.
func (h *AdminHandler) GetProfileHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	profile, err := h.adminService.GetProfile(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// GetActiveShiftsHandler lists all active executor shifts.
func (h *AdminHandler) GetActiveShiftsHandler(w http.ResponseWriter, r *http.Request) {
	shifts, err := h.adminService.GetActiveShifts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shifts)
}

// GetActiveOrdersHandler lists active customer orders (searching or assigned).
func (h *AdminHandler) GetActiveOrdersHandler(w http.ResponseWriter, r *http.Request) {
	orders, err := h.adminService.GetActiveOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetCompletedOrdersHandler lists completed customer orders.
func (h *AdminHandler) GetCompletedOrdersHandler(w http.ResponseWriter, r *http.Request) {
	orders, err := h.adminService.GetCompletedOrders()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// SendBroadcastEmailHandler sends an email broadcast to selected recipients.
func (h *AdminHandler) SendBroadcastEmailHandler(w http.ResponseWriter, r *http.Request) {
	var req service.BroadcastEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	res, err := h.adminService.SendBroadcastEmail(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
