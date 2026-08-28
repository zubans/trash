package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// BidHandler holds dependencies for bidding HTTP endpoints.
type BidHandler struct {
	bidService   *service.BidService
	orderService *service.OrderService
}

// NewBidHandler creates a new BidHandler.
func NewBidHandler(bidService *service.BidService, orderService *service.OrderService) *BidHandler {
	return &BidHandler{
		bidService:   bidService,
		orderService: orderService,
	}
}

// CreateConstructionOrderHandler creates a construction waste order auction.
func (h *BidHandler) CreateConstructionOrderHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// CreateBidHandler allows executors to bid on construction orders.
func (h *BidHandler) CreateBidHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(idStr)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(bid)
}

// AcceptBidHandler allows customers to accept a specific bid.
func (h *BidHandler) AcceptBidHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	bidID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid bid ID", http.StatusBadRequest)
		return
	}

	err = h.bidService.AcceptBid(r.Context(), bidID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "bid accepted successfully"})
}

// GetBidsHandler lists all bids on a specific construction order.
func (h *BidHandler) GetBidsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	bids, err := h.bidService.GetBidsForOrder(r.Context(), orderID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bids)
}

// GetAvailableConstructionOrdersHandler lists open construction orders for executors.
func (h *BidHandler) GetAvailableConstructionOrdersHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.orderService.GetAvailableConstructionOrdersForExecutor(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}
