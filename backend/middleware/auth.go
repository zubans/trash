package middleware

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Ключи контекста, используемые middleware.
type contextKey string

const (
	// UserKey хранит аутентифицированного *repository.User в контексте запроса.
	UserKey contextKey = "user"
	// TokenKey хранит сырую строку JWT-токена в контексте запроса.
	TokenKey contextKey = "token"
	// RoleKey хранит строку роли пользователя в контексте запроса.
	RoleKey contextKey = "role"
)

// SessionChecker сообщает, занесён ли access-токен в чёрный список. Ему
// удовлетворяет *service.AuthService; middleware нужно ровно столько.
type SessionChecker interface {
	IsAccessTokenRevoked(ctx context.Context, token string) (bool, error)
}

// AuthMiddleware проверяет JWT и кладёт данные пользователя в контекст запроса.
type AuthMiddleware struct {
	userRepo repository.UserRepository
	sessions SessionChecker
	secret   []byte

	// Кэш аутентифицированного пользователя (с ролями) с коротким TTL. Раньше
	// каждый запрос дважды ходил в базу — строка пользователя и роли, — что
	// множится при всплеске клиентов (постоянный опрос от каждого клиента идёт
	// сюда). Кэш сводит это к одному чтению на пользователя за TTL.
	//
	// Бан по-прежнему мгновенный: бан отзывает сессию, а проверка отзыва
	// выполняется на каждом запросе ДО обращения к этому кэшу, поэтому старый
	// токен отвергается независимо от устаревшей записи. Смена роли или
	// верификации тоже отзывает сессию; остаётся лишь то, что после повторного
	// входа пользователя кэшированная строка может отставать на TTL, поэтому
	// только что изменённая роль или верификация (и поля профиля вроде баланса)
	// вступают в силу в пределах TTL, а не мгновенно. TTL держится заметно ниже
	// интервала опроса клиента, чтобы окно было крошечным; AUTH_CACHE_TTL_SEC=0 выключает кэш.
	userCacheTTL time.Duration
	userCache    sync.Map // userID -> cachedUser

	// permissions решает, что разрешено роли, отличной от ADMIN. nil означает
	// «справочник ролей не подключён»: тогда админские маршруты охраняет одна
	// только роль ADMIN, как было до него.
	permissions PermissionChecker
}

type cachedUser struct {
	user    *repository.User
	expires time.Time
}

// NewAuthMiddleware создаёт AuthMiddleware.
func NewAuthMiddleware(userRepo repository.UserRepository, sessions SessionChecker, jwtSecret string) *AuthMiddleware {
	if jwtSecret == "" {
		jwtSecret = "dev-secret-change-me"
	}
	m := &AuthMiddleware{
		userRepo:     userRepo,
		sessions:     sessions,
		secret:       []byte(jwtSecret),
		userCacheTTL: authCacheTTL(),
	}
	if m.userCacheTTL > 0 {
		go m.collectUserCache()
	}
	return m
}

// collectUserCache удаляет истёкшие записи.
//
// Без неё кэш — это карта, которая только растёт: запись создаётся для каждого
// пользователя, сделавшего запрос, и никогда не удаляется, поэтому процесс,
// проработавший месяцы, держит копию каждого, кто за это время входил.
// Одно истечение ничего не освобождает — устаревшая запись игнорируется при
// чтении, но продолжает занимать своё место.
func (m *AuthMiddleware) collectUserCache() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		now := time.Now()
		m.userCache.Range(func(key, value any) bool {
			if entry, ok := value.(cachedUser); ok && now.After(entry.expires) {
				m.userCache.Delete(key)
			}
			return true
		})
	}
}

// authCacheTTL читает AUTH_CACHE_TTL_SEC (по умолчанию 5 с). Установите 0,
// чтобы выключить кэш и читать пользователя заново на каждом запросе.
func authCacheTTL() time.Duration {
	if v := os.Getenv("AUTH_CACHE_TTL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}

// loadUser возвращает пользователя по id, по возможности из кэша с коротким
// TTL. Он отдаёт поверхностную копию, чтобы обработчик, меняющий возвращённую
// структуру, не портил кэшированную запись (срез Roles считается только для чтения).
func (m *AuthMiddleware) loadUser(ctx context.Context, id uuid.UUID) (*repository.User, error) {
	if m.userCacheTTL > 0 {
		if v, ok := m.userCache.Load(id); ok {
			if entry := v.(cachedUser); time.Now().Before(entry.expires) {
				u := *entry.user
				return &u, nil
			}
		}
	}
	user, err := m.userRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.userCacheTTL > 0 {
		stored := *user
		m.userCache.Store(id, cachedUser{user: &stored, expires: time.Now().Add(m.userCacheTTL)})
	}
	return user, nil
}

func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return parts[1]
		}
	}

	if cookie, err := r.Cookie("token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	return ""
}

// StripQueryToken переносит параметр ?token= в заголовок Authorization и
// убирает его из URL. Браузеры не умеют ставить заголовки на рукопожатии
// WebSocket, поэтому параметр приходится принимать, но он не должен попадать ни
// в логгер запросов, ни в лог доступа, ни в заголовок Referer.
func StripQueryToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		token := query.Get("token")
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		query.Del("token")
		r.URL.RawQuery = query.Encode()
		r.RequestURI = r.URL.RequestURI()

		next.ServeHTTP(w, r)
	})
}

// RequireAuth требует, чтобы запрос содержал действительный неотозванный JWT.
func (m *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			http.Error(w, "Authorization required", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.secret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Проверяем отзыв, если доступно хранилище сессий.
		if m.sessions != nil {
			revoked, err := m.sessions.IsAccessTokenRevoked(r.Context(), tokenStr)
			if err != nil {
				http.Error(w, "Token check failed", http.StatusInternalServerError)
				return
			}
			if revoked {
				http.Error(w, "Token has been revoked", http.StatusUnauthorized)
				return
			}
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		sub, ok := claims["sub"].(string)
		if !ok {
			http.Error(w, "Invalid token subject", http.StatusUnauthorized)
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			http.Error(w, "Invalid token subject", http.StatusUnauthorized)
			return
		}

		user, err := m.loadUser(r.Context(), userID)
		if err != nil {
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}
		if user.Status == "BANNED" {
			http.Error(w, "Account is banned", http.StatusUnauthorized)
			return
		}

		// Авторизация всегда следует роли, сохранённой в базе, и никогда — роли,
		// зафиксированной в токене: понижение или бан должны вступать в силу
		// немедленно, а не по истечении токена.
		role := user.Role

		ctx := r.Context()
		ctx = context.WithValue(ctx, UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, tokenStr)
		ctx = context.WithValue(ctx, RoleKey, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth кладёт аутентифицированного пользователя в контекст запроса,
// когда есть действительный неотозванный токен, но никогда не отвергает запрос,
// если токена нет или он неверен. Обработчики публичных в остальном эндпоинтов
// используют это, чтобы подогнать ответ под вызывающего (например, скрыть
// услуги «только для верифицированных»), продолжая обслуживать анонимов.
func (m *AuthMiddleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			next.ServeHTTP(w, r)
			return
		}

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return m.secret, nil
		})
		if err != nil || !token.Valid {
			next.ServeHTTP(w, r)
			return
		}

		if m.sessions != nil {
			if revoked, err := m.sessions.IsAccessTokenRevoked(r.Context(), tokenStr); err != nil || revoked {
				next.ServeHTTP(w, r)
				return
			}
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		sub, ok := claims["sub"].(string)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := uuid.Parse(sub)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		user, err := m.loadUser(r.Context(), userID)
		if err != nil || user == nil || user.Status == "BANNED" {
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, UserKey, user)
		ctx = context.WithValue(ctx, TokenKey, tokenStr)
		ctx = context.WithValue(ctx, RoleKey, user.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PermissionChecker отвечает, разрешено ли пользователю действие. Ему
// удовлетворяет *service.Permissions; middleware нужно ровно столько.
type PermissionChecker interface {
	Can(ctx context.Context, user *repository.User, permission string) bool
	CanAny(ctx context.Context, user *repository.User) bool
}

// WithPermissions подключает проверку прав. Без неё RequirePermission пускает
// только администратора — то же, что охраняло эти маршруты до появления ролей,
// поэтому процесс, поднятый без справочника ролей, теряет гибкость, но не
// открывается наружу.
func (m *AuthMiddleware) WithPermissions(checker PermissionChecker) *AuthMiddleware {
	m.permissions = checker
	return m
}

// RequirePermission пропускает запрос, если у пользователя есть это право.
//
// Право — это пара «раздел панели + действие» (users.edit, gifts.create);
// каталог живёт в service/permission.go. Администратор проходит всегда: он
// суперпользователь, и снятая где-то галочка не должна уметь запереть его
// снаружи собственной панели.
func (m *AuthMiddleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(UserKey).(*repository.User)
			if !ok || user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if user.HasRole(repository.RoleAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			if m.permissions == nil || !m.permissions.Can(r.Context(), user, permission) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdminPanel пускает к админским маршрутам всех, у кого есть хоть одно
// право в разделах панели, а не только роль ADMIN: роль «финансист» с одной
// галочкой «сверка» обязана дойти до своей страницы. Что именно ей там можно,
// решает RequirePermission на каждом маршруте.
func (m *AuthMiddleware) RequireAdminPanel(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(UserKey).(*repository.User)
		if !ok || user == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if user.HasRole(repository.RoleAdmin) {
			next.ServeHTTP(w, r)
			return
		}
		if m.permissions == nil || !m.permissions.CanAny(r.Context(), user) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFrom возвращает аутентифицированного пользователя, сохранённого RequireAuth.
func UserFrom(r *http.Request) *repository.User {
	user, _ := r.Context().Value(UserKey).(*repository.User)
	return user
}

// RequireRole ограничивает доступ пользователями с одной из разрешённых ролей.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, role := range allowedRoles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Авторизация следует полному набору ролей пользователя, загруженному из
			// базы в RequireAuth (никогда — утверждениям токена). Пользователь проходит,
			// если разрешена ЛЮБАЯ из его ролей, поэтому мультиролевая учётка (например,
			// EXECUTOR + MODERATOR) достаёт до всех поверхностей, что даёт любая из ролей.
			user, ok := r.Context().Value(UserKey).(*repository.User)
			if !ok || user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			granted := false
			for role := range allowed {
				if user.HasRole(role) {
					granted = true
					break
				}
			}
			if !granted {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
