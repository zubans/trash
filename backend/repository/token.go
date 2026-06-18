package repository

import (
	"database/sql"
	"time"
)

// TokenRepository defines database operations for revoked tokens.
type TokenRepository interface {
	IsTokenRevoked(tokenHash string) (bool, error)
	RevokeToken(tokenHash string, expiresAt time.Time) error
}

type tokenRepo struct {
	db *sql.DB
}

// NewTokenRepository creates a repository for token blacklisting.
func NewTokenRepository(db *sql.DB) TokenRepository {
	return &tokenRepo{db: db}
}

func (r *tokenRepo) IsTokenRevoked(tokenHash string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM revoked_tokens WHERE token_hash = $1 AND expires_at > now())`
	err := r.db.QueryRow(query, tokenHash).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *tokenRepo) RevokeToken(tokenHash string, expiresAt time.Time) error {
	query := `
		INSERT INTO revoked_tokens (token_hash, expires_at) 
		VALUES ($1, $2)
		ON CONFLICT (token_hash) DO UPDATE SET expires_at = EXCLUDED.expires_at`
	_, err := r.db.Exec(query, tokenHash, expiresAt)
	return err
}
