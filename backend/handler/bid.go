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
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// CreateBidHandler позволяет исполнителям делать ставки по строительным заказам.
func (h *BidHandler) CreateBidHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(bid)
}

// AcceptBidHandler позволяет заказчикам принять конкретную ставку.
func (h *BidHandler) AcceptBidHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "bid accepted successfully"})
}

// GetBidsHandler перечисляет все ставки по конкретному строительному заказу.
func (h *BidHandler) GetBidsHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	writeJSON(w, bids)
}

// GetAvailableConstructionOrdersHandler перечисляет открытые строительные заказы для исполнителей.
func (h *BidHandler) GetAvailableConstructionOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.GetAvailableConstructionOrdersForExecutor(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, orders)
}
