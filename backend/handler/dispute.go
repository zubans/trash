package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"healthlogin/backend/service"
)

// DisputeHandler обслуживает очередь арбитража в админке.
type DisputeHandler struct {
	orders *service.OrderService
}

// NewDisputeHandler создаёт DisputeHandler.
func NewDisputeHandler(orders *service.OrderService) *DisputeHandler {
	return &DisputeHandler{orders: orders}
}

// ListDisputes обслуживает GET /admin/disputes?status=OPEN|CLOSED&limit=&offset=.
// Без статуса — все споры, открытые первыми.
func (h *DisputeHandler) ListDisputes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	disputes, err := h.orders.ListDisputes(r.Context(), q.Get("status"), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, disputes)
}

// DisputeEvidence обслуживает GET /admin/disputes/{id}/evidence — карточку
// доказательств спора: снимки, жест, сверку времени и координат, трек.
func (h *DisputeHandler) DisputeEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid dispute id", http.StatusBadRequest)
		return
	}
	evidence, err := h.orders.DisputeEvidence(r.Context(), id)
	switch {
	case err == nil:
		writeJSON(w, evidence)
	case errors.Is(err, service.ErrDisputeNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ResolveDispute обслуживает POST /admin/disputes/{id}/resolve.
// Тело — {"decision": "executor" | "customer" | "unknown", "note": "..."}.
func (h *DisputeHandler) ResolveDispute(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid dispute id", http.StatusBadRequest)
		return
	}
	arbiter := userFromContext(r)
	if arbiter == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	dispute, err := h.orders.ResolveDispute(r.Context(), id, arbiter.ID, decision, req.Note)
	switch {
	case err == nil:
		writeJSON(w, dispute)
	case errors.Is(err, service.ErrDisputeNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrDisputeClosed):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		writeOrderError(w, err)
	}
}
