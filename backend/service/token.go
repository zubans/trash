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
	// accessTokenTTL намеренно короткий: данные авторизации (роль, бан)
	// перечитываются из базы на каждом запросе, но украденный access-токен
	// остаётся годным до своего истечения.
	accessTokenTTL = 15 * time.Minute

	// refreshTokenTTL ограничивает, как долго клиент может оставаться в системе,
	// не вводя учётные данные заново.
	refreshTokenTTL = 30 * 24 * time.Hour

	refreshTokenBytes = 32

	// refreshReplayGrace — окно, внутри которого повторное предъявление уже
	// обменянного токена считается гонкой клиента, а не утечкой.
	//
	// Ротация разрешает обменять значение один раз, и два контекста одного
	// пользователя (две вкладки, экран и таймер обновления, мобильное
	// приложение, вернувшееся из фона сразу несколькими запросами) успевают
	// послать один и тот же токен почти одновременно. Наказывать за это
	// завершением всех сессий — значит выбрасывать человека из приложения за
	// собственный параллелизм. Настоящая утечка проявляется позже: у
	// атакующего нет причин попадать в те же секунды, что и законный клиент.
	refreshReplayGrace = 30 * time.Second
)

// ErrInvalidRefreshToken возвращается для всего, что делает refresh-токен
// непригодным: неизвестный, истёкший, уже использованный или отозванный.
// Вызывающему нельзя сообщать, что именно из этого произошло.
var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

// TokenPair — то, что клиент получает при входе и при каждом обновлении.
type TokenPair struct {
	AccessToken  string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// hashRefreshToken выводит значение, сохраняемое в базе. SHA-256 здесь
// достаточно (в отличие от паролей): токен — это 32 случайных байта, так что
// перебирать нечего.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newRefreshToken возвращает свежий непрозрачный токен и его хеш.
func newRefreshToken() (string, string, error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	return token, hashRefreshToken(token), nil
}

// IssueTokenPair создаёт access-токен и refresh-токен для пользователя.
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

// Refresh обменивает refresh-токен на новую пару и ротирует его.
//
// Именно ротация делает утёкший токен обнаружимым: каждый токен можно обменять
// один раз. Предъявление уже использованного токена означает, что значение
// находится в двух местах сразу, поэтому все сессии этого пользователя
// завершаются и ему приходится войти заново — но только если повтор пришёл
// позже refreshReplayGrace: внутри окна это параллелизм самого клиента.
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

	// Повтор: токен уже обменивали.
	if stored.UsedAt != nil {
		// Внутри окна это гонка самого клиента: отказываем в обмене, но пару,
		// которую уже получил победивший запрос, оставляем рабочей — иначе
		// параллелизм одного приложения заканчивался бы выходом из всех сессий.
		if time.Since(*stored.UsedAt) <= refreshReplayGrace {
			log.Printf("[AuthService] concurrent refresh for user %s; rejecting the duplicate without ending sessions", stored.UserID)
			return nil, ErrInvalidRefreshToken
		}
		log.Printf("[SECURITY] refresh token replay for user %s; revoking all sessions", stored.UserID)
		if err := s.refreshRepo.RevokeAllForUser(ctx, stored.UserID); err != nil {
			log.Printf("[SECURITY] failed to revoke sessions for user %s: %v", stored.UserID, err)
		}
		return nil, ErrInvalidRefreshToken
	}
	if !stored.IsUsable(time.Now()) {
		return nil, ErrInvalidRefreshToken
	}

	// Расходуем токен. Охраняемый UPDATE приводит два параллельных обновления
	// с одним токеном ровно к одному победителю.
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
	// Забаненная учётная запись не должна уметь продлевать свою сессию.
	if user.Status == "BANNED" {
		if err := s.refreshRepo.RevokeAllForUser(ctx, user.ID); err != nil {
			log.Printf("[AuthService] failed to revoke sessions for banned user %s: %v", user.ID, err)
		}
		return nil, ErrInvalidRefreshToken
	}

	return s.IssueTokenPair(ctx, user)
}

// RevokeAccessToken заносит access-токен в чёрный список до его истечения.
// Access-токены самодостаточны, поэтому завершить один досрочно можно, только запомнив его.
func (s *AuthService) RevokeAccessToken(ctx context.Context, tokenStr string) error {
	if s.tokenRepo == nil {
		return nil
	}

	// Разбираем без проверки: токен может быть уже истёкшим, а нужно лишь то,
	// сколько должна прожить запись в чёрном списке.
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

// IsAccessTokenRevoked сообщает, был ли токен занесён в чёрный список.
func (s *AuthService) IsAccessTokenRevoked(ctx context.Context, tokenStr string) (bool, error) {
	if s.tokenRepo == nil {
		return false, nil
	}
	return s.tokenRepo.IsTokenRevoked(ctx, hashRefreshToken(tokenStr))
}

// Logout завершает текущую сессию: предъявленный access-токен заносится в
// чёрный список до конца своего срока, а refresh-токен, если клиент его
// прислал, отзывается, чтобы его нельзя было обменять.
func (s *AuthService) Logout(ctx context.Context, accessToken, refreshToken string) error {
	if err := s.RevokeAccessToken(ctx, accessToken); err != nil {
		return err
	}
	return s.RevokeRefreshToken(ctx, refreshToken)
}

// RevokeRefreshToken завершает одну сессию. Используется при выходе;
// неизвестные значения игнорируются, чтобы выход не падал из-за старого клиента.
func (s *AuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if s.refreshRepo == nil || refreshToken == "" {
		return nil
	}
	return s.refreshRepo.Revoke(ctx, hashRefreshToken(refreshToken))
}

// RevokeAllSessions завершает все сессии пользователя. Используется при бане
// учётной записи или смене её роли.
func (s *AuthService) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	if s.refreshRepo == nil {
		return nil
	}
	return s.refreshRepo.RevokeAllForUser(ctx, userID)
}

// CleanupExpiredRefreshTokens удаляет строки, которые уже нельзя обменять.
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
