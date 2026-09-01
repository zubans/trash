package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// BehaviorHandler serves the two endpoints scripted services need beyond the
// ordinary order flow: an executor submitting data for a check, and an
// administrator working through the cases a behaviour handed over.
type BehaviorHandler struct {
	dispatcher  *service.BehaviorDispatcher
	submissions repository.SubmissionRepository
}

// NewBehaviorHandler creates a BehaviorHandler.
func NewBehaviorHandler(dispatcher *service.BehaviorDispatcher, submissions repository.SubmissionRepository) *BehaviorHandler {
	return &BehaviorHandler{dispatcher: dispatcher, submissions: submissions}
}

// SubmitOrderData handles POST /executor/orders/{id}/submission.
//
// The body carries only what the executor typed. The values it is compared
// against stay on the server: this endpoint answers "did it match", never "what
// should it have been", so a wrong guess teaches the submitter nothing.
func (h *BehaviorHandler) SubmitOrderData(w http.ResponseWriter, r *http.Request) {
	orderID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}
	executor := userFromContext(r)
	if executor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var fields map[string]string
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.dispatcher.SubmitOrderData(r.Context(), orderID, executor.ID, fields)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSubmissionNotSupported):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrSubmissionEscalated):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	writeJSON(w, result)
}

// ListEscalations handles GET /admin/escalations. Open cases by default; the
// submitted attempts come with them, because comparing what the moderator read
// off the document with the account is the whole task on that screen.
func (h *BehaviorHandler) ListEscalations(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	escalations, err := h.submissions.ListEscalations(r.Context(), status, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, escalations)
}

// ResolveEscalation handles POST /admin/escalations/{id}/resolve. It closes the
// case and nothing else: verifying the customer, or cancelling the order, are
// the administrator's own decisions and have their own endpoints.
func (h *BehaviorHandler) ResolveEscalation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid escalation id", http.StatusBadRequest)
		return
	}
	admin := userFromContext(r)
	adminID := uuid.Nil
	if admin != nil {
		adminID = admin.ID
	}
	if err := h.submissions.ResolveEscalation(r.Context(), id, adminID); err != nil {
		if errors.Is(err, repository.ErrEscalationNotFound) {
			http.Error(w, "escalation not found or already resolved", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"message": "escalation resolved"})
}
