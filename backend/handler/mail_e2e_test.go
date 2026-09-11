package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"healthlogin/backend/handler"
	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// E2E внутренней почты: настоящий Postgres со всеми миграциями, настоящий вход
// по паролю, настоящие JWT, middleware авторизации и прав и та же разводка
// маршрутов, что в main.go (MailHandler.RegisterUserRoutes/RegisterAdminRoutes).
// Подменено только одно — HTTP-сервер: httptest вместо nginx.
//
// Запуск — как у остальных e2e: `make test-db`, либо TEST_DATABASE_URL на
// одноразовую базу. Без базы тест пропускается.

const mailE2EPassword = "Password123!"

func mailE2EDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	// Под `make test-db` база обязана быть: пропуск там — молча зелёный прогон.
	skip := t.Skipf
	if os.Getenv("TEST_DB_REQUIRED") != "" {
		skip = t.Fatalf
	}
	if dsn == "" {
		skip("e2e database test: DATABASE_URL / TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		skip("cannot ping database: %v", err)
	}
	if err := repository.Migrate(db, "../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// mailApp — поднятый сервер и то, что нужно тесту, чтобы готовить данные.
type mailApp struct {
	t      *testing.T
	server *httptest.Server
	db     *sql.DB
	users  repository.UserRepository
	roles  repository.RoleRepository
	mail   repository.MailRepository
	// permissions — кэш прав middleware. Тест правит роли в обход RoleService,
	// поэтому сбрасывает его сам, как это делает сервис после правки роли.
	permissions *service.Permissions
}

func startMailApp(t *testing.T, db *sql.DB) *mailApp {
	t.Helper()
	// Без кэша пользователя в middleware: тест меняет роли на лету и хочет видеть
	// их со следующего же запроса, а не через TTL.
	t.Setenv("AUTH_CACHE_TTL_SEC", "0")

	const secret = "mail-e2e-secret"
	users := repository.New(db)
	roles := repository.NewRoleRepository(db)
	mail := repository.NewMailRepository(db)

	auth := service.NewAuthServiceWithSecret(users, secret, nil, nil).
		WithSessionStorage(repository.NewRefreshTokenRepository(db), repository.NewTokenRepository(db))
	permissions := service.NewPermissions(roles)
	authMiddleware := middleware.NewAuthMiddleware(users, auth, secret).WithPermissions(permissions)

	ph := handler.NewPublicHandler(auth).WithPermissions(permissions)
	mh := handler.NewMailHandler(mail, users)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Post("/login", ph.LoginHandler)
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(middleware.RequireRole("CUSTOMER", "EXECUTOR", "ADMIN"))
			mh.RegisterUserRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth)
			r.Use(authMiddleware.RequireAdminPanel)
			mh.RegisterAdminRoutes(r, authMiddleware.RequirePermission)
		})
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &mailApp{t: t, server: server, db: db, users: users, roles: roles, mail: mail, permissions: permissions}
}

// newUser заводит учётку с паролем. Телефон уникален на прогон: база общая с
// остальными e2e и не чистится между ними.
func (a *mailApp) newUser(role, lastName, firstName string) *repository.User {
	a.t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(mailE2EPassword), bcrypt.MinCost)
	if err != nil {
		a.t.Fatalf("bcrypt: %v", err)
	}
	phone := fmt.Sprintf("+7%010d", rand.Int63n(1e10))
	user := &repository.User{
		Role: role, Phone: phone, Email: strings.TrimPrefix(phone, "+") + "@mail-e2e.test",
		LastName: lastName, FirstName: firstName, Patronymic: "Тестович",
		Password: string(hash), Status: "ACTIVE",
	}
	if err := a.users.Create(context.Background(), user); err != nil {
		a.t.Fatalf("create user %s: %v", role, err)
	}
	return user
}

// client — вошедший пользователь: ходит в API со своим токеном.
type client struct {
	app   *mailApp
	user  *repository.User
	token string
}

func (a *mailApp) login(user *repository.User) *client {
	a.t.Helper()
	body, _ := json.Marshal(map[string]string{"phone": user.Phone, "password": mailE2EPassword})
	resp, err := http.Post(a.server.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		a.t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		a.t.Fatalf("login %s: %d %s", user.Phone, resp.StatusCode, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		a.t.Fatalf("login %s: no token (%v)", user.Phone, err)
	}
	return &client{app: a, user: user, token: out.Token}
}

// do выполняет запрос и возвращает код и тело. into, если задан, заполняется
// из JSON-ответа с кодом 200.
func (c *client) do(method, path string, payload interface{}, into interface{}) (int, string) {
	c.app.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.app.server.URL+"/api"+path, body)
	if err != nil {
		c.app.t.Fatalf("request %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.app.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, into); err != nil {
			c.app.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode, strings.TrimSpace(string(raw))
}

// must — do, который валит тест на любом коде, кроме ожидаемого.
func (c *client) must(want int, method, path string, payload interface{}, into interface{}) {
	c.app.t.Helper()
	if code, body := c.do(method, path, payload, into); code != want {
		c.app.t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, code, body)
	}
}

// unread — то, что рисует конвертик у телефона.
func (c *client) unread() int {
	c.app.t.Helper()
	var out struct {
		Unread int `json:"unread"`
	}
	c.must(http.StatusOK, http.MethodGet, "/user/mail/unread", nil, &out)
	return out.Unread
}

type inbox struct {
	Messages []repository.Mail `json:"messages"`
	Unread   int               `json:"unread"`
}

func (c *client) inbox() inbox {
	c.app.t.Helper()
	var out inbox
	c.must(http.StatusOK, http.MethodGet, "/user/mail", nil, &out)
	return out
}

// dialogWith находит переписку с пользователем в списке администратора.
func (c *client) dialogWith(userID uuid.UUID) *repository.MailDialog {
	c.app.t.Helper()
	var out struct {
		Dialogs []*repository.MailDialog `json:"dialogs"`
	}
	c.must(http.StatusOK, http.MethodGet, "/admin/mail/dialogs", nil, &out)
	for _, d := range out.Dialogs {
		if d.UserID == userID {
			return d
		}
	}
	return nil
}

func findLetter(messages []repository.Mail, id uuid.UUID) *repository.Mail {
	for i := range messages {
		if messages[i].ID == id {
			return &messages[i]
		}
	}
	return nil
}

// Полный круг переписки: администратор пишет, у пользователя загорается
// конвертик, он читает и отвечает, ответ приходит администратору, тот отвечает
// снова. Именно этого не было: ящик умел только доставлять.
func TestE2E_MailCorrespondence(t *testing.T) {
	db := mailE2EDB(t)
	defer db.Close()
	app := startMailApp(t, db)

	admin := app.login(app.newUser("ADMIN", "Петров", "Пётр"))
	customer := app.login(app.newUser("CUSTOMER", "Иванов", "Иван"))

	// 0. Писем нет — конвертика нет.
	if n := customer.unread(); n != 0 {
		t.Fatalf("новому пользователю не может быть непрочитанных писем, получено %d", n)
	}

	// 1. Администратор пишет пользователю.
	var letter repository.Mail
	admin.must(http.StatusOK, http.MethodPost, "/admin/mail/users/"+customer.user.ID.String(),
		map[string]string{"subject": "Проверка документов", "body": "Пришлите, пожалуйста, фото паспорта"},
		&letter)
	if letter.Kind != repository.MailKindDirect || letter.ThreadID == nil || *letter.ThreadID != letter.ID {
		t.Fatalf("адресное письмо должно начинать собственную ветку: %+v", letter)
	}

	// 2. У пользователя загорается конвертик, письмо лежит в ящике.
	if n := customer.unread(); n != 1 {
		t.Fatalf("после письма администратора ждали 1 непрочитанное, получено %d", n)
	}
	box := customer.inbox()
	root := findLetter(box.Messages, letter.ID)
	if root == nil {
		t.Fatalf("письма администратора нет в ящике: %+v", box.Messages)
	}
	if root.ReadAt != nil || root.ThreadUnread != 1 {
		t.Fatalf("письмо должно быть непрочитанным: read_at=%v thread_unread=%d", root.ReadAt, root.ThreadUnread)
	}

	// 3. Пользователь открывает ветку — прочитал, конвертик гаснет.
	var thread struct {
		ThreadID uuid.UUID         `json:"thread_id"`
		Messages []repository.Mail `json:"messages"`
	}
	customer.must(http.StatusOK, http.MethodGet, "/user/mail/"+letter.ID.String()+"/thread", nil, &thread)
	if thread.ThreadID != letter.ID || len(thread.Messages) != 1 {
		t.Fatalf("ветка: thread_id=%s, писем %d", thread.ThreadID, len(thread.Messages))
	}
	if thread.Messages[0].SenderName != "Петров Пётр" {
		t.Fatalf("в переписке должна быть подпись администратора, получено %q", thread.Messages[0].SenderName)
	}
	if n := customer.unread(); n != 0 {
		t.Fatalf("после открытия ветки конвертик должен погаснуть, непрочитанных %d", n)
	}

	// 4. Пользователь отвечает.
	var reply repository.Mail
	customer.must(http.StatusOK, http.MethodPost, "/user/mail/"+letter.ID.String()+"/reply",
		map[string]string{"body": "Отправил фото на почту"}, &reply)
	if reply.Direction != repository.MailDirectionOut || reply.ThreadID == nil || *reply.ThreadID != letter.ID {
		t.Fatalf("ответ должен уйти исходящим в ту же ветку: %+v", reply)
	}
	if reply.Subject != "Re: Проверка документов" {
		t.Fatalf("тема ответа: %q", reply.Subject)
	}
	// Собственный ответ конвертик не зажигает.
	if n := customer.unread(); n != 0 {
		t.Fatalf("свой ответ не может быть непрочитанным у автора, получено %d", n)
	}

	// 5. Ответ дошёл до администратора: переписка в списке, один непрочитанный.
	var adminUnread struct {
		Unread int `json:"unread"`
	}
	admin.must(http.StatusOK, http.MethodGet, "/admin/mail/unread", nil, &adminUnread)
	if adminUnread.Unread < 1 {
		t.Fatalf("счётчик неотвеченного у администрации должен вырасти, получено %d", adminUnread.Unread)
	}
	dialog := admin.dialogWith(customer.user.ID)
	if dialog == nil {
		t.Fatal("переписки нет в списке администратора")
	}
	if dialog.Unread != 1 || dialog.Total != 2 || dialog.LastDirection != repository.MailDirectionOut {
		t.Fatalf("строка переписки: unread=%d total=%d last=%s", dialog.Unread, dialog.Total, dialog.LastDirection)
	}
	if dialog.LastBody != "Отправил фото на почту" || dialog.Phone != customer.user.Phone {
		t.Fatalf("строка переписки должна показывать последний ответ и телефон: %+v", dialog)
	}
	// Фильтр «только без ответа» её тоже показывает.
	var unanswered struct {
		Dialogs []*repository.MailDialog `json:"dialogs"`
	}
	admin.must(http.StatusOK, http.MethodGet, "/admin/mail/dialogs?unanswered=1", nil, &unanswered)
	found := false
	for _, d := range unanswered.Dialogs {
		found = found || d.UserID == customer.user.ID
	}
	if !found {
		t.Fatal("переписка с непрочитанным ответом должна попасть в фильтр «без ответа»")
	}

	// 6. Администратор открывает переписку — видит оба письма, долг гаснет.
	var conversation struct {
		Messages []repository.Mail `json:"messages"`
		User     struct {
			FullName string `json:"full_name"`
			Phone    string `json:"phone"`
		} `json:"user"`
	}
	admin.must(http.StatusOK, http.MethodGet, "/admin/mail/users/"+customer.user.ID.String(), nil, &conversation)
	if len(conversation.Messages) != 2 || conversation.User.FullName != "Иванов Иван" {
		t.Fatalf("переписка у администратора: писем %d, собеседник %q", len(conversation.Messages), conversation.User.FullName)
	}
	if conversation.Messages[0].ID != letter.ID || conversation.Messages[1].ID != reply.ID {
		t.Fatal("переписка должна идти по времени: письмо, затем ответ")
	}
	if d := admin.dialogWith(customer.user.ID); d == nil || d.Unread != 0 {
		t.Fatalf("открытая переписка не должна числиться неотвеченной: %+v", d)
	}

	// 7. Администратор отвечает в ту же ветку — конвертик загорается снова.
	var answer repository.Mail
	admin.must(http.StatusOK, http.MethodPost, "/admin/mail/users/"+customer.user.ID.String(),
		map[string]string{"body": "Получили, спасибо!", "thread_id": letter.ID.String()}, &answer)
	if answer.ThreadID == nil || *answer.ThreadID != letter.ID || answer.Subject != "Re: Проверка документов" {
		t.Fatalf("ответ администратора должен продолжить ветку: %+v", answer)
	}
	if n := customer.unread(); n != 1 {
		t.Fatalf("ответ администратора должен зажечь конвертик, непрочитанных %d", n)
	}

	// 8. В ящике переписка — одна карточка, а не три письма подряд.
	box = customer.inbox()
	directRoots := 0
	for _, m := range box.Messages {
		if m.Kind == repository.MailKindDirect {
			directRoots++
		}
	}
	root = findLetter(box.Messages, letter.ID)
	if directRoots != 1 || root == nil {
		t.Fatalf("переписка должна быть одной карточкой, адресных карточек %d", directRoots)
	}
	if root.Replies != 2 || root.ThreadUnread != 1 {
		t.Fatalf("карточка переписки: ответов %d (ждали 2), непрочитанных %d (ждали 1)", root.Replies, root.ThreadUnread)
	}
	if !root.LastAt.After(root.CreatedAt) && !root.LastAt.Equal(answer.CreatedAt) {
		t.Fatalf("карточка должна подниматься по последней реплике: last_at=%s created_at=%s", root.LastAt, root.CreatedAt)
	}

	// 9. «Прочитать все» гасит конвертик.
	customer.must(http.StatusNoContent, http.MethodPost, "/user/mail/read-all", nil, nil)
	if n := customer.unread(); n != 0 {
		t.Fatalf("после «Прочитать все» непрочитанных быть не должно, получено %d", n)
	}

	// 10. Пользователь удаляет переписку: из его ящика она уходит, у
	// администрации остаётся — смахнув карточку, историю обращения не стирают.
	customer.must(http.StatusNoContent, http.MethodDelete, "/user/mail/"+letter.ID.String(), nil, nil)
	if findLetter(customer.inbox().Messages, letter.ID) != nil {
		t.Fatal("удалённая переписка не должна показываться в ящике")
	}
	admin.must(http.StatusOK, http.MethodGet, "/admin/mail/users/"+customer.user.ID.String(), nil, &conversation)
	if len(conversation.Messages) != 3 {
		t.Fatalf("после удаления у получателя администрация должна видеть всю переписку, писем %d", len(conversation.Messages))
	}
}

// Границы: кто что может в переписке. Проверяется через настоящие middleware —
// ровно то, что стоит на проде перед обработчиками.
func TestE2E_MailAccessRules(t *testing.T) {
	db := mailE2EDB(t)
	defer db.Close()
	app := startMailApp(t, db)
	ctx := context.Background()

	admin := app.login(app.newUser("ADMIN", "Петров", "Пётр"))
	customer := app.login(app.newUser("CUSTOMER", "Иванов", "Иван"))
	stranger := app.login(app.newUser("EXECUTOR", "Сидоров", "Сидор"))

	var letter repository.Mail
	admin.must(http.StatusOK, http.MethodPost, "/admin/mail/users/"+customer.user.ID.String(),
		map[string]string{"subject": "Личное", "body": "Только для Ивана"}, &letter)
	id := letter.ID.String()

	t.Run("без токена почта закрыта", func(t *testing.T) {
		resp, err := http.Get(app.server.URL + "/api/user/mail/unread")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ждали 401, получено %d", resp.StatusCode)
		}
	})

	t.Run("чужая ветка неотличима от несуществующей", func(t *testing.T) {
		stranger.must(http.StatusNotFound, http.MethodGet, "/user/mail/"+id+"/thread", nil, nil)
		stranger.must(http.StatusNotFound, http.MethodPost, "/user/mail/"+id+"/reply",
			map[string]string{"body": "влезаю"}, nil)
		stranger.must(http.StatusNotFound, http.MethodGet, "/user/mail/"+uuid.NewString()+"/thread", nil, nil)
		// Чужое письмо не стало прочитанным от попытки его открыть.
		if n := customer.unread(); n != 1 {
			t.Fatalf("попытка постороннего не должна гасить конвертик владельца, непрочитанных %d", n)
		}
	})

	t.Run("пользователь не попадает в админскую почту", func(t *testing.T) {
		customer.must(http.StatusForbidden, http.MethodGet, "/admin/mail/dialogs", nil, nil)
		customer.must(http.StatusForbidden, http.MethodPost, "/admin/mail/users/"+stranger.user.ID.String(),
			map[string]string{"subject": "спам", "body": "спам"}, nil)
		customer.must(http.StatusForbidden, http.MethodPost, "/admin/mail/broadcast",
			map[string]string{"subject": "спам", "body": "спам"}, nil)
	})

	t.Run("на письмо ядра ответить нельзя", func(t *testing.T) {
		system := &repository.Mail{UserID: customer.user.ID, Kind: repository.MailKindAchievement,
			Subject: "Новый значок", Body: "Поздравляем"}
		if err := app.mail.Send(ctx, nil, system); err != nil {
			t.Fatalf("подготовка: %v", err)
		}
		customer.must(http.StatusConflict, http.MethodPost, "/user/mail/"+system.ID.String()+"/reply",
			map[string]string{"body": "спасибо"}, nil)
	})

	t.Run("пустой ответ и новое письмо без темы отвергаются", func(t *testing.T) {
		customer.must(http.StatusBadRequest, http.MethodPost, "/user/mail/"+id+"/reply",
			map[string]string{"body": "   "}, nil)
		admin.must(http.StatusBadRequest, http.MethodPost, "/admin/mail/users/"+customer.user.ID.String(),
			map[string]string{"body": "без темы"}, nil)
		admin.must(http.StatusNotFound, http.MethodPost, "/admin/mail/users/"+uuid.NewString(),
			map[string]string{"subject": "Кому-то", "body": "некому"}, nil)
		// Продолжить можно только ветку этого же пользователя.
		admin.must(http.StatusNotFound, http.MethodPost, "/admin/mail/users/"+stranger.user.ID.String(),
			map[string]string{"body": "подмена ветки", "thread_id": id}, nil)
	})

	t.Run("право mail.view даёт читать, но не писать", func(t *testing.T) {
		code := fmt.Sprintf("MAIL_E2E_%d", time.Now().UnixNano())
		if err := app.roles.Create(ctx, &repository.Role{Code: code, Name: "Читатель почты"}); err != nil {
			t.Fatalf("роль: %v", err)
		}
		if err := app.roles.SetPermissions(ctx, code, []string{"mail.view"}); err != nil {
			t.Fatalf("права роли: %v", err)
		}
		reader := app.newUser("CUSTOMER", "Читатель", "Почты")
		if err := app.roles.AssignUser(ctx, code, reader.ID); err != nil {
			t.Fatalf("назначение роли: %v", err)
		}
		// Кэш прав уже прочитан предыдущими запросами — сбрасываем его, как
		// RoleService после правки роли.
		app.permissions.Invalidate()
		readerClient := app.login(reader)

		readerClient.must(http.StatusOK, http.MethodGet, "/admin/mail/dialogs", nil, nil)
		readerClient.must(http.StatusOK, http.MethodGet, "/admin/mail/users/"+customer.user.ID.String(), nil, nil)
		readerClient.must(http.StatusForbidden, http.MethodPost, "/admin/mail/users/"+customer.user.ID.String(),
			map[string]string{"subject": "Можно?", "body": "Нельзя"}, nil)
		readerClient.must(http.StatusForbidden, http.MethodPost, "/admin/mail/broadcast",
			map[string]string{"subject": "Можно?", "body": "Нельзя"}, nil)
	})
}

// Рассылка по роли доходит и до тех, у кого роль записана только в user_roles.
// Регрессия прод-бага: запрос получателей искал несуществующую колонку
// users.roles и отвечал «cannot resolve recipients» на любую рассылку по роли.
func TestE2E_MailBroadcastReachesAdditionalRoles(t *testing.T) {
	db := mailE2EDB(t)
	defer db.Close()
	app := startMailApp(t, db)
	ctx := context.Background()

	admin := app.login(app.newUser("ADMIN", "Петров", "Пётр"))
	executor := app.login(app.newUser("EXECUTOR", "Исполнитель", "Основной"))

	// Заказчик, ставший исполнителем позже: основная роль CUSTOMER, EXECUTOR
	// только в user_roles.
	convertedUser := app.newUser("CUSTOMER", "Исполнитель", "Дополнительный")
	if err := app.roles.AssignUser(ctx, "EXECUTOR", convertedUser.ID); err != nil {
		t.Fatalf("назначение роли: %v", err)
	}
	converted := app.login(convertedUser)
	customer := app.login(app.newUser("CUSTOMER", "Заказчик", "Только"))

	subject := "Все комиссии отменены на 3 месяца " + uuid.NewString()[:8]
	var result struct {
		Sent int `json:"sent"`
	}
	admin.must(http.StatusOK, http.MethodPost, "/admin/mail/broadcast", map[string]string{
		"kind": "PROMO", "role": "EXECUTOR", "subject": subject,
		"body": "В честь открытия приложения в вашем регионе все комиссии отменены.",
	}, &result)
	if result.Sent < 2 {
		t.Fatalf("рассылка исполнителям должна дойти хотя бы до двух тестовых, отправлено %d", result.Sent)
	}

	gotPromo := func(c *client) *repository.Mail {
		for _, m := range c.inbox().Messages {
			if m.Subject == subject {
				m := m
				return &m
			}
		}
		return nil
	}
	for name, c := range map[string]*client{"основной исполнитель": executor, "дополнительная роль": converted} {
		m := gotPromo(c)
		if m == nil {
			t.Fatalf("%s: акция не пришла", name)
		}
		if m.Kind != repository.MailKindPromo || m.ThreadID != nil {
			t.Fatalf("%s: рассылка — акция без ветки, получено kind=%s thread=%v", name, m.Kind, m.ThreadID)
		}
		// На рассылку не отвечают.
		c.must(http.StatusConflict, http.MethodPost, "/user/mail/"+m.ID.String()+"/reply",
			map[string]string{"body": "а мне?"}, nil)
	}
	if gotPromo(customer) != nil {
		t.Fatal("рассылка исполнителям не должна приходить заказчику")
	}
}
