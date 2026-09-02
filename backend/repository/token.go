package repository

import (
	"context"
	"database/sql"
	"time"
)

// TokenRepository описывает операции с базой для отозванных токенов.
type TokenRepository interface {
	IsTokenRevoked(ctx context.Context, tokenHash string) (bool, error)
	RevokeToken(ctx context.Context, tokenHash string, expiresAt time.Time) error
}

type tokenRepo struct {
	db *sql.DB
}

// NewTokenRepository создаёт репозиторий для чёрного списка токенов.
func NewTokenRepository(db *sql.DB) TokenRepository {
	return &tokenRepo{db: db}
}

func (r *tokenRepo) IsTokenRevoked(ctx context.Context, tokenHash string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM revoked_tokens WHERE token_hash = $1 AND expires_at > now())`
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *tokenRepo) RevokeToken(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	query := `
		INSERT INTO revoked_tokens (token_hash, expires_at) 
		VALUES ($1, $2)
		ON CONFLICT (token_hash) DO UPDATE SET expires_at = EXCLUDED.expires_at`
	_, err := r.db.ExecContext(ctx, query, tokenHash, expiresAt)
	return err
}
