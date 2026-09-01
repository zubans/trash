package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/lib/pq"
)

// ErrServiceAlreadyClaimed reports that the user has already ordered a service
// that may be ordered only once.
var ErrServiceAlreadyClaimed = errors.New("service already ordered by this user")

// ServiceClaimRepository records that a user has taken up a once-per-user
// service.
//
// The rule could have been a query — "does this customer already have an order
// for this variant?" — but a query cannot stop two simultaneous requests from
// both finding nothing. A row with a primary key can: the claim is inserted in
// the same transaction as the order, and the second insert loses.
type ServiceClaimRepository interface {
	// Claim records the claim inside the caller's transaction. Returns
	// ErrServiceAlreadyClaimed when the user already holds one.
	Claim(ctx context.Context, q Querier, userID, variantID, orderID uuid.UUID) error
	// ReleaseByOrder drops the claim a cancelled order held. A cancelled order
	// must not lock a user out of the service for good.
	ReleaseByOrder(ctx context.Context, q Querier, orderID uuid.UUID) error
	// CountForVariant reports whether this user has claimed this variant.
	CountForVariant(ctx context.Context, userID, variantID uuid.UUID) (int, error)
	// CountsForUser returns the user's claims per variant in one query, for the
	// catalog listings that judge every node at once.
	CountsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error)
}

type serviceClaimRepo struct {
	db *sql.DB
}

// NewServiceClaimRepository creates a ServiceClaimRepository.
func NewServiceClaimRepository(db *sql.DB) ServiceClaimRepository {
	return &serviceClaimRepo{db: db}
}

func (r *serviceClaimRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *serviceClaimRepo) Claim(ctx context.Context, q Querier, userID, variantID, orderID uuid.UUID) error {
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO user_service_claims (user_id, variant_id, order_id)
        VALUES ($1, $2, $3)
    `, userID, variantID, orderID)
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrServiceAlreadyClaimed
	}
	return err
}

func (r *serviceClaimRepo) ReleaseByOrder(ctx context.Context, q Querier, orderID uuid.UUID) error {
	_, err := r.exec(q).ExecContext(ctx,
		`DELETE FROM user_service_claims WHERE order_id = $1`, orderID)
	return err
}

func (r *serviceClaimRepo) CountForVariant(ctx context.Context, userID, variantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_service_claims WHERE user_id = $1 AND variant_id = $2`,
		userID, variantID).Scan(&count)
	return count, err
}

func (r *serviceClaimRepo) CountsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT variant_id, COUNT(*) FROM user_service_claims WHERE user_id = $1 GROUP BY variant_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}
