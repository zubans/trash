package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/service"
)

// maxReviewPageSize ограничивает публичный список отзывов.
const maxReviewPageSize = 100

type ReviewHandler struct {
	reviewService *service.ReviewService
}

func NewReviewHandler(reviewService *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{reviewService: reviewService}
}

func (h *ReviewHandler) CreateReview(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
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

	review, err := h.reviewService.CreateReview(r.Context(), orderID, user.ID, dto)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, review)
}

func (h *ReviewHandler) GetOrderReview(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderIDStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "Invalid order ID", http.StatusBadRequest)
		return
	}

	review, err := h.reviewService.GetReviewByOrderAndAuthor(r.Context(), orderID, user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	if review == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"has_reviewed": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"has_reviewed": true, "review": review})
}

func (h *ReviewHandler) GetUserReviews(w http.ResponseWriter, r *http.Request) {
	userIDStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Эндпоинт публичный, поэтому размер страницы ограничен: неограниченный limit
	// превращает виджет рейтинга в способ выкачать всю таблицу отзывов.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > maxReviewPageSize {
		limit = maxReviewPageSize
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	reviews, err := h.reviewService.GetReviewsForUser(r.Context(), userID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, reviews)
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

	rating, err := h.reviewService.GetUserRating(r.Context(), userID, role)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, rating)
}

// RegisterPublicRoutes — отзывы и рейтинг пользователя видны всем.
func (h *ReviewHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/users/{id}/reviews", h.GetUserReviews)
	r.Get("/users/{id}/rating", h.GetUserRating)
}

// RegisterUserRoutes — отзыв по заказу от его участника.
func (h *ReviewHandler) RegisterUserRoutes(r chi.Router) {
	r.Post("/orders/{id}/reviews", h.CreateReview)
	r.Get("/orders/{id}/reviews/mine", h.GetOrderReview)
}
