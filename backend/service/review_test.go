package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ratingFailingReviewRepo — отзывы пишутся, пересчёт рейтинга падает.
type ratingFailingReviewRepo struct {
	mockReviewRepo
	ratingErr error
}

func (m *ratingFailingReviewRepo) UpdateUserRating(ctx context.Context, q repository.Querier, userID uuid.UUID, role string) error {
	return m.ratingErr
}

// Сбой пересчёта рейтинга — ошибка отзыва, а не проглоченный лог: отзыв и
// рейтинг коммитятся вместе, и отзыв без пересчёта оставил бы их расходиться.
func TestCreateReviewReturnsRatingUpdateError(t *testing.T) {
	custID, execID, orderID := uuid.New(), uuid.New(), uuid.New()
	orderRepo := &mockOrderRepo{orders: []*repository.Order{{
		ID: orderID, CustomerID: custID, ExecutorID: &execID, Status: repository.OrderStatusCompleted,
	}}}
	boom := errors.New("profiles table is locked")
	reviews := &ratingFailingReviewRepo{ratingErr: boom}
	srv := NewReviewService(reviews, orderRepo)

	_, err := srv.CreateReview(context.Background(), orderID, custID, CreateReviewDTO{Rating: 5})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the rating error", err)
	}
}

// Второй отзыв от того же автора отклоняется классом состояния, а чужак —
// классом доступа: обработчик отвечает 409 и 403, а не одним кодом на всё.
func TestCreateReviewClassifiesRefusals(t *testing.T) {
	custID, execID, orderID := uuid.New(), uuid.New(), uuid.New()
	orderRepo := &mockOrderRepo{orders: []*repository.Order{{
		ID: orderID, CustomerID: custID, ExecutorID: &execID, Status: repository.OrderStatusCompleted,
	}}}
	srv := NewReviewService(&mockReviewRepo{}, orderRepo)
	ctx := context.Background()

	if _, err := srv.CreateReview(ctx, orderID, custID, CreateReviewDTO{Rating: 5}); err != nil {
		t.Fatalf("first review: %v", err)
	}
	if _, err := srv.CreateReview(ctx, orderID, custID, CreateReviewDTO{Rating: 4}); !errors.Is(err, ErrReviewAlreadySubmitted) || !errors.Is(err, ErrOrderState) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := srv.CreateReview(ctx, orderID, uuid.New(), CreateReviewDTO{Rating: 4}); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger: %v", err)
	}
	if _, err := srv.CreateReview(ctx, uuid.New(), custID, CreateReviewDTO{Rating: 4}); !errors.Is(err, ErrOrderNotFound) || !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("missing order: %v", err)
	}
	if _, err := srv.CreateReview(ctx, orderID, custID, CreateReviewDTO{Rating: 9}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad rating: %v", err)
	}
}
