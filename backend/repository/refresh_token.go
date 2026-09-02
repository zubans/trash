package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// RefreshToken — сохранённая запись refresh-токена. Само значение токена
// никогда не сохраняется, только его хеш.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// IsUsable сообщает, можно ли ещё обменять эту запись.
func (t *RefreshToken) IsUsable(now time.Time) bool {
	return t.UsedAt == nil && t.RevokedAt == nil && t.ExpiresAt.After(now)
}

// ErrRefreshTokenNotFound возвращается, когда предъявленному токену нет записи.
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenRepository хранит refresh-токены.
type RefreshTokenRepository interface {
	Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	FindByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	// MarkUsed расходует токен. Он сообщает ErrConflict, когда токен уже
	// использован или отозван, — так обнаруживается повтор.
	MarkUsed(ctx context.Context, tokenHash string) error
	// RevokeAllForUser завершает все сессии пользователя: применяется при выходе
	// отовсюду, при обнаруженном повторе и при бане учётной записи.
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
	Revoke(ctx context.Context, tokenHash string) error
	DeleteExpired(ctx context.Context) (int64, error)
}

type refreshTokenRepo struct {
	db *sql.DB
}

// NewRefreshTokenRepository создаёт RefreshTokenRepository.
func NewRefreshTokenRepository(db *sql.DB) RefreshTokenRepository {
	return &refreshTokenRepo{db: db}
}

func (r *refreshTokenRepo) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	return err
}

func (r *refreshTokenRepo) FindByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, created_at, used_at, revoked_at
		 FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.CreatedAt, &t.UsedAt, &t.RevokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, err
	}
	return &t, nil
}

// MarkUsed расходует токен одним охраняемым оператором, поэтому два
// параллельных запроса обновления с одним токеном не могут удаться оба.
func (r *refreshTokenRepo) MarkUsed(ctx context.Context, tokenHash string) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE refresh_tokens SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()`,
		tokenHash)
}

func (r *refreshTokenRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	return err
}

func (r *refreshTokenRepo) Revoke(ctx context.Context, tokenHash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	return err
}

// DeleteExpired удаляет строки, которые уже нельзя обменять. Использованные
// токены хранятся до истечения срока, потому что обнаружение повторов должно их узнавать.
func (r *refreshTokenRepo) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
