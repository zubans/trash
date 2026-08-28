package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"healthlogin/backend/repository"
)

type ReviewService struct {
	reviewRepo repository.ReviewRepository
	orderRepo  repository.OrderRepository
}

func NewReviewService(reviewRepo repository.ReviewRepository, orderRepo repository.OrderRepository) *ReviewService {
	return &ReviewService{reviewRepo: reviewRepo, orderRepo: orderRepo}
}

type CreateReviewDTO struct {
	Rating  int      `json:"rating"`
	Tags    []string `json:"tags"`
	Comment string   `json:"comment"`
	Photos  []string `json:"photos"`
}

// Review input limits. Without them a review is an unbounded write into the
// database and an unchecked URL rendered in somebody else's browser.
const (
	maxCommentRunes = 2000
	maxReviewPhotos = 10
)

func (s *ReviewService) CreateReview(ctx context.Context, orderID, authorID uuid.UUID, dto CreateReviewDTO) (*repository.OrderReview, error) {
	if dto.Rating < 1 || dto.Rating > 5 {
		return nil, errors.New("rating must be between 1 and 5")
	}
	dto.Comment = strings.TrimSpace(dto.Comment)
	if len([]rune(dto.Comment)) > maxCommentRunes {
		return nil, errors.New("комментарий слишком длинный")
	}
	if len(dto.Photos) > maxReviewPhotos {
		return nil, errors.New("слишком много фотографий")
	}
	for _, photo := range dto.Photos {
		// Same rule as order photos: only our own upload paths.
		if !strings.HasPrefix(photo, "/uploads/") || strings.Contains(photo, "..") {
			return nil, errors.New("фотографии должны быть загружены через приложение")
		}
	}
	if len(dto.Tags) > 20 {
		return nil, errors.New("слишком много тегов")
	}

	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil || order == nil {
		return nil, errors.New("order not found")
	}

	if order.Status != repository.OrderStatusCompleted {
		return nil, errors.New("reviews can only be submitted for completed orders")
	}

	// 7 days SLA check
	if order.CompletedAt != nil && time.Since(*order.CompletedAt) > 7*24*time.Hour {
		return nil, errors.New("review window has expired (7 days max after order completion)")
	}

	var authorRole string
	var targetID uuid.UUID

	if authorID == order.CustomerID {
		authorRole = "CUSTOMER"
		if order.ExecutorID == nil {
			return nil, errors.New("executor not assigned to this order")
		}
		targetID = *order.ExecutorID
	} else if order.ExecutorID != nil && authorID == *order.ExecutorID {
		authorRole = "EXECUTOR"
		targetID = order.CustomerID
	} else {
		return nil, errors.New("user is not a participant of this order")
	}

	existing, err := s.reviewRepo.GetReviewByOrderAndAuthor(ctx, orderID, authorID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("you have already submitted a review for this order")
	}

	tagsJSON, _ := json.Marshal(dto.Tags)
	photosJSON, _ := json.Marshal(dto.Photos)

	review := &repository.OrderReview{
		OrderID:    orderID,
		AuthorID:   authorID,
		TargetID:   targetID,
		AuthorRole: authorRole,
		Rating:     dto.Rating,
		Tags:       json.RawMessage(tagsJSON),
		Comment:    dto.Comment,
		Photos:     json.RawMessage(photosJSON),
	}

	if err := s.reviewRepo.CreateReview(ctx, review); err != nil {
		return nil, err
	}

	// Target role is target's role
	targetRole := "EXECUTOR"
	if authorRole == "EXECUTOR" {
		targetRole = "CUSTOMER"
	}

	_ = s.reviewRepo.UpdateUserRating(ctx, targetID, targetRole)

	return review, nil
}

func (s *ReviewService) GetReviewByOrderAndAuthor(ctx context.Context, orderID, authorID uuid.UUID) (*repository.OrderReview, error) {
	return s.reviewRepo.GetReviewByOrderAndAuthor(ctx, orderID, authorID)
}

func (s *ReviewService) GetReviewsForUser(ctx context.Context, targetID uuid.UUID, limit, offset int) ([]repository.OrderReview, error) {
	return s.reviewRepo.GetReviewsForUser(ctx, targetID, limit, offset)
}

func (s *ReviewService) GetUserRating(ctx context.Context, userID uuid.UUID, role string) (*repository.UserRatingSummary, error) {
	return s.reviewRepo.GetUserRating(ctx, userID, role)
}
