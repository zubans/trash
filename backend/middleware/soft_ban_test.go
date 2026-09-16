package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// stubUsers отдаёт пользователей из карты. Остальные методы интерфейса
// middleware не зовёт, и вызов любого из них уронит тест на nil.
type stubUsers struct {
	repository.UserRepository
	byID map[uuid.UUID]*repository.User
}

func (s *stubUsers) FindByID(_ context.Context, id uuid.UUID) (*repository.User, error) {
	u, ok := s.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	copied := *u
	return &copied, nil
}

const softBanTestSecret = "soft-ban-test-secret"

func signedToken(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID.String()})
	s, err := token.SignedString([]byte(softBanTestSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

// softBanRouter повторяет устройство main.go: те же маршруты под /api и в
// корне, RequireAuth на группе, вложения — отдельной группой на корневом роутере.
func softBanRouter(t *testing.T, users *stubUsers) http.Handler {
	t.Setenv("AUTH_CACHE_TTL_SEC", "0")
	auth := NewAuthMiddleware(users, nil, softBanTestSecret)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

	register := func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/auth/me", ok)
			r.Post("/customer/orders", ok)
			r.Post("/customer/orders/{id}/confirm", ok)
			r.Get("/chats/{order_id}/ws", ok)
		})
	}
	r := chi.NewRouter()
	r.Route("/api", register)
	register(r)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth)
		r.Get("/uploads/*", ok)
		r.Get("/api/uploads/*", ok)
	})
	return r
}

func TestRequireAuth_SoftBanned(t *testing.T) {
	active := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusActive}
	soft := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusSoftBanned}
	banned := &repository.User{ID: uuid.New(), Role: repository.RoleCustomer, Status: repository.UserStatusBanned}
	users := &stubUsers{byID: map[uuid.UUID]*repository.User{active.ID: active, soft.ID: soft, banned.ID: banned}}
	router := softBanRouter(t, users)

	cases := []struct {
		name   string
		user   *repository.User
		method string
		path   string
		want   int
	}{
		{"soft: me under /api", soft, http.MethodGet, "/api/auth/me", http.StatusOK},
		{"soft: me in legacy root", soft, http.MethodGet, "/auth/me", http.StatusOK},
		{"soft: confirm own order", soft, http.MethodPost, "/api/customer/orders/" + uuid.NewString() + "/confirm", http.StatusOK},
		{"soft: chat websocket", soft, http.MethodGet, "/api/chats/" + uuid.NewString() + "/ws", http.StatusOK},
		{"soft: attachment", soft, http.MethodGet, "/api/uploads/chat/a.jpg", http.StatusOK},
		{"soft: new order denied", soft, http.MethodPost, "/api/customer/orders", http.StatusForbidden},
		{"soft: new order denied in root", soft, http.MethodPost, "/customer/orders", http.StatusForbidden},
		{"active: new order", active, http.MethodPost, "/api/customer/orders", http.StatusOK},
		{"banned: me", banned, http.MethodGet, "/api/auth/me", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set("Authorization", "Bearer "+signedToken(t, c.user.ID))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d (body %q)", rec.Code, c.want, rec.Body.String())
			}
			if c.want == http.StatusForbidden {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != SoftBannedErrorCode {
					t.Fatalf("403 without %s code: %q", SoftBannedErrorCode, rec.Body.String())
				}
			}
		})
	}
}

// Каждый маршрут белого списка обязан существовать в коде: переименованный
// маршрут иначе молча закрылся бы для заблокированного, и он не смог бы,
// например, подтвердить заказ.
func TestSoftBanAllowedRoutes_Exist(t *testing.T) {
	var sources strings.Builder
	files, _ := filepath.Glob("../handler/*.go")
	// Маршруты регистрируют и обработчики, и модули со своей маршрутизацией
	// (photoproof), и сам main.
	modules, _ := filepath.Glob("../photoproof/*.go")
	files = append(files, modules...)
	files = append(files, "../main.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sources.Write(b)
	}
	code := sources.String()

	for key := range softBanAllowedRoutes {
		method, path, ok := strings.Cut(key, " ")
		if !ok || !strings.HasPrefix(path, "/") {
			t.Errorf("malformed entry %q", key)
			continue
		}
		verb := strings.ToUpper(method[:1]) + strings.ToLower(method[1:])
		re := regexp.MustCompile(`\.` + verb + `\("` + regexp.QuoteMeta(path) + `"`)
		if !re.MatchString(code) {
			t.Errorf("allowed route %q is not registered anywhere", key)
		}
	}
}
