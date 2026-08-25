package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// RefreshToken is a stored refresh-token record. The token value itself is
// never persisted, only its hash.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	CreatedAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// IsUsable reports whether the record may still be exchanged.
func (t *RefreshToken) IsUsable(now time.Time) bool {
	return t.UsedAt == nil && t.RevokedAt == nil && t.ExpiresAt.After(now)
}

// ErrRefreshTokenNotFound is returned when a presented token has no record.
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenRepository stores refresh tokens.
type RefreshTokenRepository interface {
	Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	FindByHash(tokenHash string) (*RefreshToken, error)
	// MarkUsed consumes a token. It reports ErrConflict when the token was
	// already used or revoked, which is how a replay is detected.
	MarkUsed(tokenHash string) error
	// RevokeAllForUser ends every session of a user: used on logout-everywhere,
	// on a detected replay, and when an account is banned.
	RevokeAllForUser(userID uuid.UUID) error
	Revoke(tokenHash string) error
	DeleteExpired() (int64, error)
}

type refreshTokenRepo struct {
	db *sql.DB
}

// NewRefreshTokenRepository creates a RefreshTokenRepository.
func NewRefreshTokenRepository(db *sql.DB) RefreshTokenRepository {
	return &refreshTokenRepo{db: db}
}

func (r *refreshTokenRepo) Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
	return err
}

func (r *refreshTokenRepo) FindByHash(tokenHash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.QueryRow(
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

// MarkUsed consumes the token in one guarded statement, so two parallel refresh
// requests with the same token cannot both succeed.
func (r *refreshTokenRepo) MarkUsed(tokenHash string) error {
	return execExpectingOne(r.db,
		`UPDATE refresh_tokens SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()`,
		tokenHash)
}

func (r *refreshTokenRepo) RevokeAllForUser(userID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	return err
}

func (r *refreshTokenRepo) Revoke(tokenHash string) error {
	_, err := r.db.Exec(
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	return err
}

// DeleteExpired drops rows that can no longer be exchanged. Used tokens are kept
// until they expire, because replay detection needs to recognise them.
func (r *refreshTokenRepo) DeleteExpired() (int64, error) {
	res, err := r.db.Exec(`DELETE FROM refresh_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
