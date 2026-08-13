package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type OrderReview struct {
	ID         uuid.UUID       `json:"id"`
	OrderID    uuid.UUID       `json:"order_id"`
	AuthorID   uuid.UUID       `json:"author_id"`
	TargetID   uuid.UUID       `json:"target_id"`
	AuthorRole string          `json:"author_role"`
	Rating     int             `json:"rating"`
	Tags       json.RawMessage `json:"tags"`
	Comment    string          `json:"comment"`
	Photos     json.RawMessage `json:"photos"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type UserRatingSummary struct {
	UserID       uuid.UUID `json:"user_id"`
	Rating       float64   `json:"rating"`
	ReviewsCount int       `json:"reviews_count"`
}

type ReviewRepository interface {
	CreateReview(review *OrderReview) error
	GetReviewByOrderAndAuthor(orderID, authorID uuid.UUID) (*OrderReview, error)
	GetReviewsForUser(targetID uuid.UUID, limit, offset int) ([]OrderReview, error)
	UpdateUserRating(userID uuid.UUID, role string) error
	GetUserRating(userID uuid.UUID, role string) (*UserRatingSummary, error)
}

type reviewRepository struct {
	db *sql.DB
}

func NewReviewRepository(db *sql.DB) ReviewRepository {
	return &reviewRepository{db: db}
}

func (r *reviewRepository) CreateReview(review *OrderReview) error {
	if review.ID == uuid.Nil {
		review.ID = uuid.New()
	}
	now := time.Now()
	review.CreatedAt = now
	review.UpdatedAt = now

	tagsJSON := review.Tags
	if len(tagsJSON) == 0 {
		tagsJSON = json.RawMessage("[]")
	}
	photosJSON := review.Photos
	if len(photosJSON) == 0 {
		photosJSON = json.RawMessage("[]")
	}

	query := `
		INSERT INTO order_reviews (id, order_id, author_id, target_id, author_role, rating, tags, comment, photos, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := r.db.Exec(query, review.ID, review.OrderID, review.AuthorID, review.TargetID, review.AuthorRole, review.Rating, tagsJSON, review.Comment, photosJSON, review.CreatedAt, review.UpdatedAt)
	return err
}

func (r *reviewRepository) GetReviewByOrderAndAuthor(orderID, authorID uuid.UUID) (*OrderReview, error) {
	var rev OrderReview
	var tagsJSON, photosJSON []byte
	query := `
		SELECT id, order_id, author_id, target_id, author_role, rating, tags, comment, photos, created_at, updated_at
		FROM order_reviews
		WHERE order_id = $1 AND author_id = $2
	`
	err := r.db.QueryRow(query, orderID, authorID).Scan(
		&rev.ID, &rev.OrderID, &rev.AuthorID, &rev.TargetID, &rev.AuthorRole, &rev.Rating, &tagsJSON, &rev.Comment, &photosJSON, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rev.Tags = json.RawMessage(tagsJSON)
	rev.Photos = json.RawMessage(photosJSON)
	return &rev, nil
}

func (r *reviewRepository) GetReviewsForUser(targetID uuid.UUID, limit, offset int) ([]OrderReview, error) {
	query := `
		SELECT id, order_id, author_id, target_id, author_role, rating, tags, comment, photos, created_at, updated_at
		FROM order_reviews
		WHERE target_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(query, targetID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []OrderReview
	for rows.Next() {
		var rev OrderReview
		var tagsJSON, photosJSON []byte
		if err := rows.Scan(&rev.ID, &rev.OrderID, &rev.AuthorID, &rev.TargetID, &rev.AuthorRole, &rev.Rating, &tagsJSON, &rev.Comment, &photosJSON, &rev.CreatedAt, &rev.UpdatedAt); err != nil {
			return nil, err
		}
		rev.Tags = json.RawMessage(tagsJSON)
		rev.Photos = json.RawMessage(photosJSON)
		reviews = append(reviews, rev)
	}
	return reviews, nil
}

func (r *reviewRepository) UpdateUserRating(userID uuid.UUID, role string) error {
	// Calculate Bayesian Average Rating
	// m = 4.8, C = 5
	// R = (C*m + SUM(r_i)) / (C + n)
	var sumRating float64
	var count int
	err := r.db.QueryRow(`SELECT COALESCE(SUM(rating), 0), COUNT(id) FROM order_reviews WHERE target_id = $1`, userID).Scan(&sumRating, &count)
	if err != nil {
		return err
	}

	const C = 5.0
	const m = 4.8
	bayesianRating := (C*m + sumRating) / (C + float64(count))

	if role == "CUSTOMER" {
		query := `UPDATE customer_profiles SET rating = $1, reviews_count = $2 WHERE user_id = $3`
		_, err = r.db.Exec(query, bayesianRating, count, userID)
	} else {
		query := `UPDATE executor_profiles SET rating = $1, reviews_count = $2 WHERE user_id = $3`
		_, err = r.db.Exec(query, bayesianRating, count, userID)
	}
	return err
}

func (r *reviewRepository) GetUserRating(userID uuid.UUID, role string) (*UserRatingSummary, error) {
	var summary UserRatingSummary
	summary.UserID = userID

	if role == "CUSTOMER" {
		err := r.db.QueryRow(`SELECT rating, reviews_count FROM customer_profiles WHERE user_id = $1`, userID).Scan(&summary.Rating, &summary.ReviewsCount)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &UserRatingSummary{UserID: userID, Rating: 5.0, ReviewsCount: 0}, nil
			}
			return nil, err
		}
	} else {
		err := r.db.QueryRow(`SELECT rating, reviews_count FROM executor_profiles WHERE user_id = $1`, userID).Scan(&summary.Rating, &summary.ReviewsCount)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &UserRatingSummary{UserID: userID, Rating: 5.0, ReviewsCount: 0}, nil
			}
			return nil, err
		}
	}
	return &summary, nil
}
