package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/lib/pq"
)

// ErrServiceAlreadyClaimed сообщает, что пользователь уже заказывал услугу,
// которую можно заказать только один раз.
var ErrServiceAlreadyClaimed = errors.New("service already ordered by this user")

// ServiceClaimRepository фиксирует, что пользователь воспользовался услугой
// «один раз на пользователя».
//
// Правило могло быть запросом — «есть ли у этого заказчика заказ по этому
// варианту?» — но запрос не может помешать двум одновременным обращениям обоим
// ничего не найти. Строка с первичным ключом может: claim вставляется в той же
// транзакции, что и заказ, и вторая вставка проигрывает.
type ServiceClaimRepository interface {
	// Claim записывает claim внутри транзакции вызывающего. Возвращает
	// ErrServiceAlreadyClaimed, когда у пользователя он уже есть.
	Claim(ctx context.Context, q Querier, userID, variantID, orderID uuid.UUID) error
	// ReleaseByOrder снимает claim, который держал отменённый заказ. Отменённый
	// заказ не должен навсегда закрывать пользователю доступ к услуге.
	ReleaseByOrder(ctx context.Context, q Querier, orderID uuid.UUID) error
	// CountForVariant сообщает, занимал ли этот пользователь этот вариант.
	CountForVariant(ctx context.Context, userID, variantID uuid.UUID) (int, error)
	// CountsForUser возвращает claim'ы пользователя по вариантам одним запросом —
	// для списков каталога, которые оценивают все узлы разом.
	CountsForUser(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error)
}

type serviceClaimRepo struct {
	db *sql.DB
}

// NewServiceClaimRepository создаёт ServiceClaimRepository.
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
