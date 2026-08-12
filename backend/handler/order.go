package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// OrderHandler handles order-related HTTP endpoints.
type OrderHandler struct {
	orderService *service.OrderService
}

// NewOrderHandler creates an OrderHandler.
func NewOrderHandler(orderService *service.OrderService) *OrderHandler {
	return &OrderHandler{orderService: orderService}
}

func userFromContext(r *http.Request) *repository.User {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		return nil
	}
	return user
}

// CreateOrder handles POST /customer/orders.
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

	order, err := h.orderService.Create(user.ID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// CancelOrder handles POST /customer/orders/{id}/cancel.
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

	if err := h.orderService.Cancel(user.ID, orderID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ConfirmOrder handles POST /customer/orders/{id}/confirm.
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

	if err := h.orderService.Confirm(user.ID, orderID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// AcceptOrder handles POST /executor/orders/{id}/accept.
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

	if err := h.orderService.Accept(orderID, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// RejectOrder handles POST /executor/orders/{id}/reject.
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

	if err := h.orderService.RejectAssignedOrder(orderID, user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ListAssignedOrders handles GET /executor/orders/assigned.
func (h *OrderHandler) ListAssignedOrders(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.ListAssigned(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetCustomerOrdersHandler handles GET /customer/orders.
func (h *OrderHandler) GetCustomerOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.ListByCustomer(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// GetNearbyOrdersHandler handles GET /executor/orders/nearby?lat=...&lon=...&radius=2000.
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

	orders, err := h.orderService.FindNearbyOrders(lat, lon, radius)
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

// Alias method names expected by main.go.
func (h *OrderHandler) CreateOrderHandler(w http.ResponseWriter, r *http.Request)     { h.CreateOrder(w, r) }
func (h *OrderHandler) ConfirmOrderHandler(w http.ResponseWriter, r *http.Request)    { h.ConfirmOrder(w, r) }
func (h *OrderHandler) CancelOrderHandler(w http.ResponseWriter, r *http.Request)     { h.CancelOrder(w, r) }
func (h *OrderHandler) RejectOrderHandler(w http.ResponseWriter, r *http.Request)     { h.RejectOrder(w, r) }
func (h *OrderHandler) GetExecutorAssignedOrdersHandler(w http.ResponseWriter, r *http.Request) { h.ListAssignedOrders(w, r) }
