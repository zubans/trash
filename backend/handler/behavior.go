package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// BehaviorHandler обслуживает два эндпоинта, которые нужны скриптовым услугам
// сверх обычного потока заказа: отправку данных на проверку исполнителем и
// разбор администратором случаев, переданных поведением.
type BehaviorHandler struct {
	dispatcher  *service.BehaviorDispatcher
	submissions repository.SubmissionRepository
}

// NewBehaviorHandler создаёт BehaviorHandler.
func NewBehaviorHandler(dispatcher *service.BehaviorDispatcher, submissions repository.SubmissionRepository) *BehaviorHandler {
	return &BehaviorHandler{dispatcher: dispatcher, submissions: submissions}
}

// SubmitOrderData обслуживает POST /executor/orders/{id}/submission.
//
// Тело несёт только то, что набрал исполнитель. Значения, с которыми
// сравнивают, остаются на сервере: этот эндпоинт отвечает «совпало ли», но
// никогда «как должно было быть», поэтому неверная догадка ничему не учит.
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

// ListEscalations обслуживает GET /admin/escalations. По умолчанию открытые
// случаи; попытки отправки идут вместе с ними, потому что сравнить прочитанное
// модератором в документе с учётной записью — вся задача того экрана.
func (h *BehaviorHandler) ListEscalations(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	escalations, err := h.submissions.ListEscalations(r.Context(), status, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, escalations)
}

// ResolveEscalation обслуживает POST /admin/escalations/{id}/resolve. Он
// закрывает случай и только: верифицировать заказчика или отменить заказ —
// собственные решения администратора со своими эндпоинтами.
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
