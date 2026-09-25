package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// ReviewService принимает отзывы по завершённым заказам и держит рейтинг
// пользователей в такт с ними.
type ReviewService struct {
	reviewRepo repository.ReviewRepository
	orderRepo  repository.OrderRepository
	// tx выполняет проверку, запись и пересчёт рейтинга одной транзакцией.
	// Без него (в тестах) шаги идут на пуле соединений.
	tx TxRunner
	// stats копит агрегаты исполнителя, по которым решают ачивки. Необязателен:
	// без него серия пятёрок просто не ведётся.
	stats repository.ExecutorStatsRepository
}

// NewReviewService создаёт ReviewService.
func NewReviewService(reviewRepo repository.ReviewRepository, orderRepo repository.OrderRepository) *ReviewService {
	return &ReviewService{reviewRepo: reviewRepo, orderRepo: orderRepo}
}

// WithTx подключает транзакции: отзыв, пересчёт рейтинга и серия оценок
// коммитятся вместе.
func (s *ReviewService) WithTx(tx TxRunner) *ReviewService {
	s.tx = tx
	return s
}

// WithExecutorStats подключает счётчики исполнителя: оценка либо продолжает
// серию пятёрок, либо обрывает её, и ачивке «безупречный» нужна именно эта
// серия, а не средняя оценка.
func (s *ReviewService) WithExecutorStats(stats repository.ExecutorStatsRepository) *ReviewService {
	s.stats = stats
	return s
}

type CreateReviewDTO struct {
	Rating  int      `json:"rating"`
	Tags    []string `json:"tags"`
	Comment string   `json:"comment"`
	Photos  []string `json:"photos"`
}

// Ограничения на ввод отзыва. Без них отзыв — это неограниченная запись в базу
// и непроверенный URL, отрисованный в чужом браузере.
const (
	maxCommentRunes = 2000
	maxReviewPhotos = 10
)

func (s *ReviewService) runInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if s.tx == nil {
		return fn(nil)
	}
	return s.tx.RunInTx(ctx, fn)
}

// CreateReview записывает отзыв участника завершённого заказа о второй стороне
// и пересчитывает её рейтинг. Проверка «уже оставлял», запись и пересчёт —
// одна транзакция: два одновременных отзыва от одного автора не проходят оба, а
// рейтинг не может отстать от отзывов.
func (s *ReviewService) CreateReview(ctx context.Context, orderID, authorID uuid.UUID, dto CreateReviewDTO) (*repository.OrderReview, error) {
	if dto.Rating < 1 || dto.Rating > 5 {
		return nil, validationError("rating must be between 1 and 5")
	}
	dto.Comment = strings.TrimSpace(dto.Comment)
	if len([]rune(dto.Comment)) > maxCommentRunes {
		return nil, validationError("комментарий слишком длинный")
	}
	if len(dto.Photos) > maxReviewPhotos {
		return nil, validationError("слишком много фотографий")
	}
	for _, photo := range dto.Photos {
		// То же правило, что и для фото заказа: только наши собственные пути загрузки.
		if !strings.HasPrefix(photo, "/uploads/") || strings.Contains(photo, "..") {
			return nil, validationError("фотографии должны быть загружены через приложение")
		}
	}
	if len(dto.Tags) > 20 {
		return nil, validationError("слишком много тегов")
	}

	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return nil, orderNotFound(err)
	}
	if order == nil {
		return nil, ErrOrderNotFound
	}

	if order.Status != repository.OrderStatusCompleted {
		return nil, stateError("reviews can only be submitted for completed orders")
	}

	// Проверка 7-дневного SLA
	if order.CompletedAt != nil && time.Since(*order.CompletedAt) > ReviewWindow {
		return nil, stateError("review window has expired (7 days max after order completion)")
	}

	var authorRole string
	var targetID uuid.UUID

	if authorID == order.CustomerID {
		authorRole = "CUSTOMER"
		if order.ExecutorID == nil {
			return nil, ErrOrderHasNoExecutor
		}
		targetID = *order.ExecutorID
	} else if order.ExecutorID != nil && authorID == *order.ExecutorID {
		authorRole = "EXECUTOR"
		targetID = order.CustomerID
	} else {
		return nil, forbiddenError("user is not a participant of this order")
	}

	// Роль цели — это роль объекта отзыва
	targetRole := "EXECUTOR"
	if authorRole == "EXECUTOR" {
		targetRole = "CUSTOMER"
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

	if err := s.runInTx(ctx, func(tx *sql.Tx) error {
		existing, err := s.reviewRepo.GetReviewByOrderAndAuthor(ctx, tx, orderID, authorID)
		if err != nil {
			return err
		}
		if existing != nil {
			return ErrReviewAlreadySubmitted
		}
		if err := s.reviewRepo.CreateReview(ctx, tx, review); err != nil {
			return err
		}
		// Рейтинг — производная от отзывов; отзыв без пересчёта оставил бы их
		// расходиться до следующего отзыва.
		if err := s.reviewRepo.UpdateUserRating(ctx, tx, targetID, targetRole); err != nil {
			return err
		}
		// Серия считается только для исполнителя: ачивки уровня — его, и оценка,
		// которую он поставил заказчику, к ней отношения не имеет.
		if s.stats != nil && targetRole == "EXECUTOR" {
			if err := s.stats.RecordRating(ctx, tx, targetID, dto.Rating); err != nil {
				// Сбой счётчика не повод отклонить отзыв: агрегат восстанавливается
				// админским пересчётом.
				log.Printf("[review] cannot record rating for %s: %v", targetID, err)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return review, nil
}

func (s *ReviewService) GetReviewByOrderAndAuthor(ctx context.Context, orderID, authorID uuid.UUID) (*repository.OrderReview, error) {
	return s.reviewRepo.GetReviewByOrderAndAuthor(ctx, nil, orderID, authorID)
}

func (s *ReviewService) GetReviewsForUser(ctx context.Context, targetID uuid.UUID, limit, offset int) ([]repository.OrderReview, error) {
	return s.reviewRepo.GetReviewsForUser(ctx, targetID, limit, offset)
}

func (s *ReviewService) GetUserRating(ctx context.Context, userID uuid.UUID, role string) (*repository.UserRatingSummary, error) {
	return s.reviewRepo.GetUserRating(ctx, userID, role)
}
