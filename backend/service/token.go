package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

const (
	// accessTokenTTL is deliberately short: authorization data (role, ban) is
	// re-read from the database on every request, but a stolen access token
	// stays usable until it expires.
	accessTokenTTL = 15 * time.Minute

	// refreshTokenTTL bounds how long a client can stay signed in without
	// entering credentials again.
	refreshTokenTTL = 30 * 24 * time.Hour

	refreshTokenBytes = 32
)

// ErrInvalidRefreshToken is returned for anything that makes a refresh token
// unusable: unknown, expired, already used or revoked. The caller must not be
// told which of those it was.
var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

// TokenPair is what a client receives on login and on every refresh.
type TokenPair struct {
	AccessToken  string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// hashRefreshToken derives the value stored in the database. SHA-256 is
// sufficient here (unlike for passwords): the token is 32 random bytes, so
// there is nothing to brute force.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newRefreshToken returns a fresh opaque token and its hash.
func newRefreshToken() (string, string, error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	return token, hashRefreshToken(token), nil
}

// IssueTokenPair creates an access token and a refresh token for a user.
func (s *AuthService) IssueTokenPair(ctx context.Context, user *repository.User) (*TokenPair, error) {
	if s.refreshRepo == nil {
		return nil, errors.New("refresh token storage is not configured")
	}

	accessToken, err := s.GenerateJWT(ctx, user)
	if err != nil {
		return nil, err
	}

	refreshToken, hash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(refreshTokenTTL)
	if err := s.refreshRepo.Create(ctx, user.ID, hash, expiresAt); err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(accessTokenTTL),
	}, nil
}

// Refresh exchanges a refresh token for a new pair and rotates it.
//
// Rotation is what makes a leaked token detectable: each token may be exchanged
// once. Presenting a token that was already used means the value is in two
// places at once, so every session of that user is ended and the client has to
// sign in again.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if s.refreshRepo == nil {
		return nil, errors.New("refresh token storage is not configured")
	}
	if refreshToken == "" {
		return nil, ErrInvalidRefreshToken
	}

	hash := hashRefreshToken(refreshToken)
	stored, err := s.refreshRepo.FindByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, repository.ErrRefreshTokenNotFound) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}

	// Replay: the token was already exchanged. Treat it as a compromise.
	if stored.UsedAt != nil {
		log.Printf("[SECURITY] refresh token replay for user %s; revoking all sessions", stored.UserID)
		if err := s.refreshRepo.RevokeAllForUser(ctx, stored.UserID); err != nil {
			log.Printf("[SECURITY] failed to revoke sessions for user %s: %v", stored.UserID, err)
		}
		return nil, ErrInvalidRefreshToken
	}
	if !stored.IsUsable(time.Now()) {
		return nil, ErrInvalidRefreshToken
	}

	// Consume the token. The guarded UPDATE makes two parallel refreshes with
	// the same token resolve to exactly one winner.
	if err := s.refreshRepo.MarkUsed(ctx, hash); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrInvalidRefreshToken
		}
		return nil, err
	}

	user, err := s.repo.FindByID(ctx, stored.UserID)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	// A banned account must not be able to extend its session.
	if user.Status == "BANNED" {
		if err := s.refreshRepo.RevokeAllForUser(ctx, user.ID); err != nil {
			log.Printf("[AuthService] failed to revoke sessions for banned user %s: %v", user.ID, err)
		}
		return nil, ErrInvalidRefreshToken
	}

	return s.IssueTokenPair(ctx, user)
}

// RevokeAccessToken blacklists an access token until it expires. Access tokens
// are self-contained, so the only way to end one early is to remember it.
func (s *AuthService) RevokeAccessToken(ctx context.Context, tokenStr string) error {
	if s.tokenRepo == nil {
		return nil
	}

	// Parse without verifying: the token may already be expired, and all that
	// is needed is how long the blacklist entry has to live.
	expiresAt := time.Now().Add(accessTokenTTL)
	if parsed, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{}); err == nil {
		if claims, ok := parsed.Claims.(jwt.MapClaims); ok {
			if exp, ok := claims["exp"].(float64); ok {
				expiresAt = time.Unix(int64(exp), 0)
			}
		}
	}
	return s.tokenRepo.RevokeToken(ctx, hashRefreshToken(tokenStr), expiresAt)
}

// IsAccessTokenRevoked reports whether a token was blacklisted.
func (s *AuthService) IsAccessTokenRevoked(ctx context.Context, tokenStr string) (bool, error) {
	if s.tokenRepo == nil {
		return false, nil
	}
	return s.tokenRepo.IsTokenRevoked(ctx, hashRefreshToken(tokenStr))
}

// Logout ends the current session: the presented access token is blacklisted
// for the remainder of its lifetime and the refresh token, if the client sent
// one, is revoked so it cannot be exchanged.
func (s *AuthService) Logout(ctx context.Context, accessToken, refreshToken string) error {
	if err := s.RevokeAccessToken(ctx, accessToken); err != nil {
		return err
	}
	return s.RevokeRefreshToken(ctx, refreshToken)
}

// RevokeRefreshToken ends a single session. Used on logout; unknown values are
// ignored so a logout never fails because of a stale client.
func (s *AuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if s.refreshRepo == nil || refreshToken == "" {
		return nil
	}
	return s.refreshRepo.Revoke(ctx, hashRefreshToken(refreshToken))
}

// RevokeAllSessions ends every session of a user. Used when an account is
// banned or its role changes.
func (s *AuthService) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	if s.refreshRepo == nil {
		return nil
	}
	return s.refreshRepo.RevokeAllForUser(ctx, userID)
}

// CleanupExpiredRefreshTokens drops rows that can no longer be exchanged.
func (s *AuthService) CleanupExpiredRefreshTokens(ctx context.Context) {
	if s.refreshRepo == nil {
		return
	}
	removed, err := s.refreshRepo.DeleteExpired(ctx)
	if err != nil {
		log.Printf("[AuthService] failed to clean up refresh tokens: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("[AuthService] removed %d expired refresh tokens", removed)
	}
}
