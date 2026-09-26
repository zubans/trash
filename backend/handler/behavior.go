package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/service"
)

// BehaviorHandler обслуживает два эндпоинта, которые нужны скриптовым услугам
// сверх обычного потока заказа: отправку данных на проверку исполнителем и
// разбор администратором случаев, переданных поведением.
type BehaviorHandler struct {
	orderData *service.OrderSubmissions
}

// NewBehaviorHandler создаёт BehaviorHandler.
func NewBehaviorHandler(orderData *service.OrderSubmissions) *BehaviorHandler {
	return &BehaviorHandler{orderData: orderData}
}

// RegisterExecutorRoutes — данные, которые исполнитель отправляет на проверку
// по скриптовой услуге, — проверка личности в заказе верификации.
func (h *BehaviorHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Post("/executor/orders/{id}/submission", h.SubmitOrderData)
}

// RegisterAdminRoutes — разбор случаев, переданных поведением.
func (h *BehaviorHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("escalations.view")).Get("/admin/escalations", h.ListEscalations)
	r.With(can("escalations.edit")).Post("/admin/escalations/{id}/resolve", h.ResolveEscalation)
}

// SubmitOrderData обслуживает POST /executor/orders/{id}/submission.
//
// Тело несёт только то, что набрал исполнитель. Значения, с которыми
// сравнивают, остаются на сервере: этот эндпоинт отвечает «совпало ли», но
// никогда «как должно было быть», поэтому неверная догадка ничему не учит.
func (h *BehaviorHandler) SubmitOrderData(w http.ResponseWriter, r *http.Request) {
	orderID, ok := parseIDParam(w, r, "id", "order id")
	if !ok {
		return
	}
	executor, ok := requireUser(w, r)
	if !ok {
		return
	}
	var fields map[string]string
	if !decodeBody(w, r, &fields) {
		return
	}
	result, err := h.orderData.SubmitOrderData(r.Context(), orderID, executor.ID, fields)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSubmissionNotSupported):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrPassportRequired):
			// Код, а не текст: приложение по нему досылает паспорт из очереди.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "passport_required", "message": err.Error()})
		default:
			// По классу: нет заказа — 404, не тот исполнитель или заказ не в
			// работе — 409, эскалирован — 409, сбой — 500 без текста.
			writeDomainError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ListEscalations обслуживает GET /admin/escalations?status=.
func (h *BehaviorHandler) ListEscalations(w http.ResponseWriter, r *http.Request) {
	escalations, err := h.orderData.ListEscalations(r.Context(), r.URL.Query().Get("status"), 0)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, escalations)
}

// ResolveEscalation обслуживает POST /admin/escalations/{id}/resolve.
func (h *BehaviorHandler) ResolveEscalation(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(w, r, "id", "escalation id")
	if !ok {
		return
	}
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := h.orderData.ResolveEscalation(r.Context(), admin.ID, id); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "escalation resolved"})
}
