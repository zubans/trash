package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/service"
)

// DisputeHandler обслуживает споры: открытие заказчиком, признание
// исполнителем и очередь арбитража в админке.
type DisputeHandler struct {
	disputes *service.DisputeService
}

// NewDisputeHandler создаёт DisputeHandler.
func NewDisputeHandler(disputes *service.DisputeService) *DisputeHandler {
	return &DisputeHandler{disputes: disputes}
}

// OpenDispute обслуживает POST /customer/orders/{id}/dispute: заказчик
// заявляет, что исполненный заказ не выполнен. Тело — {"claim": "..."}.
func (h *DisputeHandler) OpenDispute(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid order id", http.StatusBadRequest)
		return
	}

	var req struct {
		Claim string `json:"claim"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	dispute, err := h.disputes.OpenDispute(r.Context(), user.ID, orderID, req.Claim)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, dispute)
}

// ConcedeDispute обслуживает POST /executor/orders/{id}/dispute/concede:
// исполнитель признаёт, что оспоренный заказ не выполнен.
func (h *DisputeHandler) ConcedeDispute(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid order id", http.StatusBadRequest)
		return
	}

	if err := h.disputes.ConcedeDispute(r.Context(), user.ID, orderID); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ListDisputes обслуживает GET /admin/disputes?status=OPEN|CLOSED&limit=&offset=.
// Без статуса — все споры, открытые первыми.
func (h *DisputeHandler) ListDisputes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	disputes, err := h.disputes.ListDisputes(r.Context(), q.Get("status"), limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, disputes)
}

// DisputeEvidence обслуживает GET /admin/disputes/{id}/evidence — карточку
// доказательств спора: снимки, жест, сверку времени и координат, трек.
func (h *DisputeHandler) DisputeEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid dispute id", http.StatusBadRequest)
		return
	}
	evidence, err := h.disputes.DisputeEvidence(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, evidence)
}

// ResolveDispute обслуживает POST /admin/disputes/{id}/resolve.
// Тело — {"decision": "executor" | "customer" | "unknown", "note": "..."}.
func (h *DisputeHandler) ResolveDispute(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid dispute id", http.StatusBadRequest)
		return
	}
	arbiter, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	decision, err := service.ParseDisputeDecision(req.Decision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dispute, err := h.disputes.ResolveDispute(r.Context(), id, arbiter.ID, decision, req.Note)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dispute)
}

// RegisterCustomerRoutes — открытие спора заказчиком.
func (h *DisputeHandler) RegisterCustomerRoutes(r chi.Router) {
	r.Post("/customer/orders/{id}/dispute", h.OpenDispute)
}

// RegisterExecutorRoutes — признание спора исполнителем.
func (h *DisputeHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Post("/executor/orders/{id}/dispute/concede", h.ConcedeDispute)
}

// RegisterAdminRoutes — арбитраж.
func (h *DisputeHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("disputes.view")).Get("/admin/disputes", h.ListDisputes)
	r.With(can("disputes.view")).Get("/admin/disputes/{id}/evidence", h.DisputeEvidence)
	r.With(can("disputes.edit")).Post("/admin/disputes/{id}/resolve", h.ResolveDispute)
}
