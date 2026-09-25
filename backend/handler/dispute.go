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
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dispute)
}

// ConcedeDispute обслуживает POST /executor/orders/{id}/dispute/concede:
// исполнитель признаёт, что оспоренный заказ не выполнен.
func (h *DisputeHandler) ConcedeDispute(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
	evidence, err := h.disputes.DisputeEvidence(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, evidence)
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

	dispute, err := h.disputes.ResolveDispute(r.Context(), id, arbiter.ID, decision, req.Note)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, dispute)
}
