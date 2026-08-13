package service

import (
	"encoding/json"
	"errors"
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

func (s *ReviewService) CreateReview(orderID, authorID uuid.UUID, dto CreateReviewDTO) (*repository.OrderReview, error) {
	if dto.Rating < 1 || dto.Rating > 5 {
		return nil, errors.New("rating must be between 1 and 5")
	}

	order, err := s.orderRepo.FindByID(orderID)
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

	existing, err := s.reviewRepo.GetReviewByOrderAndAuthor(orderID, authorID)
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

	if err := s.reviewRepo.CreateReview(review); err != nil {
		return nil, err
	}

	// Target role is target's role
	targetRole := "EXECUTOR"
	if authorRole == "EXECUTOR" {
		targetRole = "CUSTOMER"
	}

	_ = s.reviewRepo.UpdateUserRating(targetID, targetRole)

	return review, nil
}

func (s *ReviewService) GetReviewByOrderAndAuthor(orderID, authorID uuid.UUID) (*repository.OrderReview, error) {
	return s.reviewRepo.GetReviewByOrderAndAuthor(orderID, authorID)
}

func (s *ReviewService) GetReviewsForUser(targetID uuid.UUID, limit, offset int) ([]repository.OrderReview, error) {
	return s.reviewRepo.GetReviewsForUser(targetID, limit, offset)
}

func (s *ReviewService) GetUserRating(userID uuid.UUID, role string) (*repository.UserRatingSummary, error) {
	return s.reviewRepo.GetUserRating(userID, role)
}
