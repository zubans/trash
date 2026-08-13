package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/service"
)

type ReviewHandler struct {
	reviewService *service.ReviewService
}

func NewReviewHandler(reviewService *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{reviewService: reviewService}
}

func (h *ReviewHandler) CreateReview(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	var dto service.CreateReviewDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	review, err := h.reviewService.CreateReview(orderID, user.ID, dto)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(review)
}

func (h *ReviewHandler) GetOrderReview(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	review, err := h.reviewService.GetReviewByOrderAndAuthor(orderID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if review == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"has_reviewed": false})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"has_reviewed": true, "review": review})
}

func (h *ReviewHandler) GetUserReviews(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	reviews, err := h.reviewService.GetReviewsForUser(userID, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reviews)
}

func (h *ReviewHandler) GetUserRating(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	role := r.URL.Query().Get("role")
	if role != "CUSTOMER" && role != "EXECUTOR" {
		http.Error(w, "invalid or missing role parameter (must be CUSTOMER or EXECUTOR)", http.StatusBadRequest)
		return
	}

	rating, err := h.reviewService.GetUserRating(userID, role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rating)
}
