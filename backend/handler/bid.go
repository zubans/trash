package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/service"
)

// BidHandler хранит зависимости HTTP-эндпоинтов торгов.
type BidHandler struct {
	bidService   *service.BidService
	orderService *service.OrderService
}

// NewBidHandler создаёт новый BidHandler.
func NewBidHandler(bidService *service.BidService, orderService *service.OrderService) *BidHandler {
	return &BidHandler{
		bidService:   bidService,
		orderService: orderService,
	}
}

// CreateConstructionOrderHandler создаёт аукцион на вывоз строительного мусора.
func (h *BidHandler) CreateConstructionOrderHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req struct {
		PhotoURL string   `json:"photo_url"`
		Address  string   `json:"address"`
		Comment  string   `json:"comment,omitempty"`
		Lat      *float64 `json:"lat,omitempty"`
		Lon      *float64 `json:"lon,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	order, err := h.orderService.CreateConstructionOrder(r.Context(), user.ID, req.PhotoURL, req.Address, req.Comment, req.Lat, req.Lon)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, order)
}

// CreateBidHandler позволяет исполнителям делать ставки по строительным заказам.
func (h *BidHandler) CreateBidHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	var req struct {
		OfferedPrice money.Amount `json:"offered_price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	bid, err := h.bidService.CreateBid(r.Context(), orderID, user.ID, req.OfferedPrice)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, bid)
}

// AcceptBidHandler позволяет заказчикам принять конкретную ставку.
func (h *BidHandler) AcceptBidHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	bidID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid bid ID", http.StatusBadRequest)
		return
	}

	if err := h.bidService.AcceptBid(r.Context(), bidID, user.ID); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "bid accepted successfully"})
}

// GetBidsHandler перечисляет все ставки по конкретному строительному заказу.
func (h *BidHandler) GetBidsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	bids, err := h.bidService.GetBidsForOrder(r.Context(), orderID, user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bids)
}

// GetAvailableConstructionOrdersHandler перечисляет открытые строительные заказы для исполнителей.
func (h *BidHandler) GetAvailableConstructionOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orders, err := h.orderService.GetAvailableConstructionOrdersForExecutor(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

// RegisterCustomerRoutes — аукционные заказы заказчика.
func (h *BidHandler) RegisterCustomerRoutes(r chi.Router) {
	r.Post("/customer/orders/construction", h.CreateConstructionOrderHandler)
	r.Post("/customer/bids/{id}/accept", h.AcceptBidHandler)
	r.Get("/customer/orders/{id}/bids", h.GetBidsHandler)
}

// RegisterExecutorRoutes — ставки исполнителя.
func (h *BidHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Get("/executor/orders/available", h.GetAvailableConstructionOrdersHandler)
	r.Post("/executor/orders/{id}/bids", h.CreateBidHandler)
}
