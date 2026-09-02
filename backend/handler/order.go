package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// OrderHandler обслуживает HTTP-эндпоинты заказов.
type OrderHandler struct {
	orderService *service.OrderService
}

// NewOrderHandler создаёт OrderHandler.
func NewOrderHandler(orderService *service.OrderService) *OrderHandler {
	return &OrderHandler{orderService: orderService}
}

// writeOrderError сопоставляет доменные ошибки с кодами статуса. Конфликт
// означает, что заказ был не в том состоянии, какого ждал вызывающий, — обычно
// дубль или параллельный запрос, — и не должен выдаваться за успех.
func writeOrderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrConflict):
		http.Error(w, "заказ уже изменился, обновите страницу", http.StatusConflict)
	case errors.Is(err, repository.ErrInsufficientFunds):
		http.Error(w, "недостаточно средств", http.StatusUnprocessableEntity)
	default:
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	}
}

func userFromContext(r *http.Request) *repository.User {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		return nil
	}
	return user
}

// CreateOrder обслуживает POST /customer/orders.
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req service.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if req.Address == "" {
		http.Error(w, "address is required", http.StatusBadRequest)
		return
	}

	order, err := h.orderService.Create(r.Context(), user.ID, req)
	if err != nil {
		writeOrderError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// CancelOrder обслуживает POST /customer/orders/{id}/cancel.
func (h *OrderHandler) CancelOrder(w http.ResponseWriter, r *http.Request) {
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

	if err := h.orderService.Cancel(r.Context(), user.ID, orderID); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ConfirmOrder обслуживает POST /customer/orders/{id}/confirm.
func (h *OrderHandler) ConfirmOrder(w http.ResponseWriter, r *http.Request) {
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

	if err := h.orderService.Confirm(r.Context(), user.ID, orderID); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// TipOrder обслуживает POST /customer/orders/{id}/tip. Заказчик даёт чаевые
// исполнителю завершённого заказа; сумма приходит в рублях и списывается с
// баланса заказчика.
func (h *OrderHandler) TipOrder(w http.ResponseWriter, r *http.Request) {
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
		Amount money.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if err := h.orderService.TipOrder(r.Context(), user.ID, orderID, req.Amount); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// AcceptOrder обслуживает POST /executor/orders/{id}/accept.
func (h *OrderHandler) AcceptOrder(w http.ResponseWriter, r *http.Request) {
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

	if err := h.orderService.Accept(r.Context(), orderID, user.ID); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// RejectOrder обслуживает POST /executor/orders/{id}/reject.
func (h *OrderHandler) RejectOrder(w http.ResponseWriter, r *http.Request) {
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

	if err := h.orderService.RejectAssignedOrder(r.Context(), orderID, user.ID); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ExecuteOrder обслуживает POST /executor/orders/{id}/execute.
func (h *OrderHandler) ExecuteOrder(w http.ResponseWriter, r *http.Request) {
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

	if err := h.orderService.ExecuteOrder(r.Context(), orderID, user.ID); err != nil {
		writeOrderError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ListAssignedOrders обслуживает GET /executor/orders/assigned.
func (h *OrderHandler) ListAssignedOrders(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.ListAssigned(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetCustomerOrdersHandler обслуживает GET /customer/orders.
func (h *OrderHandler) GetCustomerOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.ListByCustomer(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetNearbyOrdersHandler обслуживает GET /executor/orders/nearby?lat=...&lon=...&radius=2000.
func (h *OrderHandler) GetNearbyOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	lat, lon, radius, err := parseCoords(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	orders, err := h.orderService.FindNearbyOrdersForExecutor(r.Context(), user.ID, lat, lon, radius)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

func parseCoords(r *http.Request) (float64, float64, int, error) {
	var lat, lon float64
	var radius int
	if _, err := fmt.Sscanf(r.URL.Query().Get("lat"), "%f", &lat); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid lat")
	}
	if _, err := fmt.Sscanf(r.URL.Query().Get("lon"), "%f", &lon); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid lon")
	}
	if _, err := fmt.Sscanf(r.URL.Query().Get("radius"), "%d", &radius); err != nil {
		radius = 2000
	}
	if radius <= 0 || radius > 50000 {
		radius = 2000
	}
	return lat, lon, radius, nil
}

// Псевдонимы имён методов, которых ожидает main.go.
func (h *OrderHandler) CreateOrderHandler(w http.ResponseWriter, r *http.Request) {
	h.CreateOrder(w, r)
}
func (h *OrderHandler) ConfirmOrderHandler(w http.ResponseWriter, r *http.Request) {
	h.ConfirmOrder(w, r)
}
func (h *OrderHandler) TipOrderHandler(w http.ResponseWriter, r *http.Request) {
	h.TipOrder(w, r)
}
func (h *OrderHandler) CancelOrderHandler(w http.ResponseWriter, r *http.Request) {
	h.CancelOrder(w, r)
}
func (h *OrderHandler) RejectOrderHandler(w http.ResponseWriter, r *http.Request) {
	h.RejectOrder(w, r)
}
func (h *OrderHandler) GetExecutorAssignedOrdersHandler(w http.ResponseWriter, r *http.Request) {
	h.ListAssignedOrders(w, r)
}
