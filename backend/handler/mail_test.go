package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
)

// fakeMail — ящик в памяти. Он ведёт себя как настоящий в том, что важно для
// переписки: помнит направление, ветку и кто чего не прочитал.
type fakeMail struct {
	letters []*repository.Mail
}

func (f *fakeMail) Send(ctx context.Context, q repository.Querier, mail *repository.Mail) error {
	if mail.ID == uuid.Nil {
		mail.ID = uuid.New()
	}
	if mail.Direction == "" {
		mail.Direction = repository.MailDirectionIn
	}
	if mail.Kind == repository.MailKindDirect && mail.ThreadID == nil {
		id := mail.ID
		mail.ThreadID = &id
	}
	mail.CreatedAt = time.Now()
	f.letters = append(f.letters, mail)
	return nil
}

func (f *fakeMail) Broadcast(ctx context.Context, mail *repository.Mail, userIDs []uuid.UUID) (int, error) {
	for range userIDs {
		copied := *mail
		copied.ID = uuid.New()
		f.letters = append(f.letters, &copied)
	}
	return len(userIDs), nil
}

func (f *fakeMail) RecipientsByRole(ctx context.Context, role string) ([]uuid.UUID, error) {
	return nil, nil
}

func (f *fakeMail) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Mail, error) {
	out := make([]*repository.Mail, 0)
	for _, m := range f.letters {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMail) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	count := 0
	for _, m := range f.letters {
		if m.UserID == userID && m.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

func (f *fakeMail) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	now := time.Now()
	for _, m := range f.letters {
		if m.UserID != userID || m.ReadAt != nil {
			continue
		}
		if m.ID == id || (m.ThreadID != nil && *m.ThreadID == id) {
			m.ReadAt = &now
		}
	}
	return nil
}

func (f *fakeMail) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, m := range f.letters {
		if m.UserID == userID && m.ReadAt == nil {
			m.ReadAt = &now
		}
	}
	return nil
}

func (f *fakeMail) Delete(ctx context.Context, id, userID uuid.UUID) error { return nil }

func (f *fakeMail) Get(ctx context.Context, id uuid.UUID) (*repository.Mail, error) {
	for _, m := range f.letters {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeMail) Thread(ctx context.Context, threadID uuid.UUID) ([]*repository.Mail, error) {
	out := make([]*repository.Mail, 0)
	for _, m := range f.letters {
		if m.ThreadID != nil && *m.ThreadID == threadID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMail) Reply(ctx context.Context, mail *repository.Mail) error {
	mail.Kind = repository.MailKindDirect
	if mail.ThreadID == nil {
		return sql.ErrNoRows
	}
	if mail.Direction == repository.MailDirectionOut && mail.ReadAt == nil {
		now := time.Now()
		mail.ReadAt = &now
	}
	return f.Send(ctx, nil, mail)
}

func (f *fakeMail) ListDialogs(ctx context.Context, onlyUnanswered bool, limit int) ([]*repository.MailDialog, error) {
	return nil, nil
}

func (f *fakeMail) ListDirectForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Mail, error) {
	out := make([]*repository.Mail, 0)
	for _, m := range f.letters {
		if m.UserID == userID && m.Kind == repository.MailKindDirect {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMail) MarkThreadReadByAdmin(ctx context.Context, threadID uuid.UUID) error {
	now := time.Now()
	for _, m := range f.letters {
		if m.ThreadID != nil && *m.ThreadID == threadID &&
			m.Direction == repository.MailDirectionOut && m.AdminReadAt == nil {
			m.AdminReadAt = &now
		}
	}
	return nil
}

func (f *fakeMail) AdminUnreadCount(ctx context.Context) (int, error) {
	count := 0
	for _, m := range f.letters {
		if m.Direction == repository.MailDirectionOut && m.AdminReadAt == nil {
			count++
		}
	}
	return count, nil
}

// mailRequest собирает запрос с параметром маршрута и вошедшим пользователем —
// так, как его собрал бы chi вместе с middleware аутентификации.
func mailRequest(method, target string, body interface{}, user *repository.User, params map[string]string) *http.Request {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != nil {
		ctx = context.WithValue(ctx, middleware.UserKey, user)
	}
	return req.WithContext(ctx)
}

func mailHarness() (*MailHandler, *fakeMail, *repository.User, *repository.User) {
	admin := &repository.User{ID: uuid.New(), Role: "ADMIN", Phone: "79990000002", LastName: "Петров", FirstName: "Пётр"}
	user := &repository.User{ID: uuid.New(), Role: "CUSTOMER", Phone: "79990000001", LastName: "Иванов", FirstName: "Иван"}
	users := &mockUserRepository{
		users:     map[uuid.UUID]*repository.User{admin.ID: admin, user.ID: user},
		addresses: map[uuid.UUID]string{},
	}
	mail := &fakeMail{}
	return NewMailHandler(mail, users), mail, admin, user
}

// Письмо администратора и ответ на него — одна ветка, и обе стороны видят её
// целиком. Это то, чего в ящике не было: он умел только доставлять.
func TestAdminMailAndUserReplyShareThread(t *testing.T) {
	h, box, admin, user := mailHarness()

	rec := httptest.NewRecorder()
	h.AdminSendMail(rec, mailRequest(http.MethodPost, "/admin/mail/users/x",
		map[string]string{"subject": "Проверка документов", "body": "Пришлите фото"},
		admin, map[string]string{"id": user.ID.String()}))
	if rec.Code != http.StatusOK {
		t.Fatalf("админское письмо не отправлено: %d %s", rec.Code, rec.Body.String())
	}
	var sent repository.Mail
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil {
		t.Fatalf("ответ не разобран: %v", err)
	}
	if sent.Kind != repository.MailKindDirect || sent.ThreadID == nil || *sent.ThreadID != sent.ID {
		t.Fatalf("адресное письмо должно начинать ветку: %+v", sent)
	}

	rec = httptest.NewRecorder()
	h.ReplyMail(rec, mailRequest(http.MethodPost, "/user/mail/x/reply",
		map[string]string{"body": "Отправил"}, user, map[string]string{"id": sent.ID.String()}))
	if rec.Code != http.StatusOK {
		t.Fatalf("ответ не отправлен: %d %s", rec.Code, rec.Body.String())
	}
	var reply repository.Mail
	_ = json.Unmarshal(rec.Body.Bytes(), &reply)
	if reply.Direction != repository.MailDirectionOut {
		t.Fatalf("ответ пользователя должен быть исходящим, а не %q", reply.Direction)
	}
	if reply.ThreadID == nil || *reply.ThreadID != sent.ID {
		t.Fatalf("ответ должен попасть в ту же ветку: %+v", reply)
	}
	if reply.Subject != "Re: Проверка документов" {
		t.Fatalf("тема ответа: %q", reply.Subject)
	}

	// Ответ ждёт разбора у администрации и не висит непрочитанным у автора.
	if count, _ := box.AdminUnreadCount(context.Background()); count != 1 {
		t.Fatalf("администрация должна увидеть один непрочитанный ответ, получено %d", count)
	}
	if count, _ := box.UnreadCount(context.Background(), user.ID); count != 1 {
		t.Fatalf("у пользователя непрочитанным остаётся только письмо админа, получено %d", count)
	}

	// Открыв ветку, пользователь прочитал её целиком.
	rec = httptest.NewRecorder()
	h.GetMailThread(rec, mailRequest(http.MethodGet, "/user/mail/x/thread", nil, user,
		map[string]string{"id": sent.ID.String()}))
	if rec.Code != http.StatusOK {
		t.Fatalf("ветка не открылась: %d %s", rec.Code, rec.Body.String())
	}
	var thread struct {
		Messages []repository.Mail `json:"messages"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &thread)
	if len(thread.Messages) != 2 {
		t.Fatalf("в ветке должно быть два письма, получено %d", len(thread.Messages))
	}
	if count, _ := box.UnreadCount(context.Background(), user.ID); count != 0 {
		t.Fatalf("после открытия ветки непрочитанных быть не должно, получено %d", count)
	}

	// Ответив, администратор ветку и разобрал.
	rec = httptest.NewRecorder()
	h.AdminSendMail(rec, mailRequest(http.MethodPost, "/admin/mail/users/x",
		map[string]string{"body": "Спасибо, принято", "thread_id": sent.ID.String()},
		admin, map[string]string{"id": user.ID.String()}))
	if rec.Code != http.StatusOK {
		t.Fatalf("ответ администратора не отправлен: %d %s", rec.Code, rec.Body.String())
	}
	if count, _ := box.AdminUnreadCount(context.Background()); count != 0 {
		t.Fatalf("после ответа непрочитанного у администрации быть не должно, получено %d", count)
	}
	if count, _ := box.UnreadCount(context.Background(), user.ID); count != 1 {
		t.Fatalf("ответ администратора должен зажечь конвертик, непрочитанных %d", count)
	}
}

// Ответить можно только в переписку: письмо о выданной ачивке написало ядро, и
// адресата у ответа на него нет.
func TestReplyRejectedForNonDirectMail(t *testing.T) {
	h, box, _, user := mailHarness()
	letter := &repository.Mail{UserID: user.ID, Kind: repository.MailKindAchievement, Subject: "Новый значок"}
	if err := box.Send(context.Background(), nil, letter); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ReplyMail(rec, mailRequest(http.MethodPost, "/user/mail/x/reply",
		map[string]string{"body": "спасибо"}, user, map[string]string{"id": letter.ID.String()}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("ожидался отказ 409, получено %d %s", rec.Code, rec.Body.String())
	}
}

// Чужая ветка не открывается и не отличима от несуществующей: перебор id не
// должен рассказывать, какие письма есть на свете.
func TestThreadOfAnotherUserIsNotFound(t *testing.T) {
	h, box, admin, user := mailHarness()
	letter := &repository.Mail{UserID: user.ID, Kind: repository.MailKindDirect, Subject: "Личное", SenderID: &admin.ID}
	if err := box.Send(context.Background(), nil, letter); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	stranger := &repository.User{ID: uuid.New(), Role: "CUSTOMER"}

	rec := httptest.NewRecorder()
	h.GetMailThread(rec, mailRequest(http.MethodGet, "/user/mail/x/thread", nil, stranger,
		map[string]string{"id": letter.ID.String()}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ожидался 404, получено %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ReplyMail(rec, mailRequest(http.MethodPost, "/user/mail/x/reply",
		map[string]string{"body": "чужое"}, stranger, map[string]string{"id": letter.ID.String()}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ожидался 404 на ответ в чужую ветку, получено %d", rec.Code)
	}
}

// Новое письмо без темы не отправляется: по теме письмо узнают в ящике.
func TestAdminMailRequiresSubjectAndBody(t *testing.T) {
	h, _, admin, user := mailHarness()

	rec := httptest.NewRecorder()
	h.AdminSendMail(rec, mailRequest(http.MethodPost, "/admin/mail/users/x",
		map[string]string{"body": "без темы"}, admin, map[string]string{"id": user.ID.String()}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 без темы, получено %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.AdminSendMail(rec, mailRequest(http.MethodPost, "/admin/mail/users/x",
		map[string]string{"subject": "Тема", "body": "   "}, admin, map[string]string{"id": user.ID.String()}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400 на пустой текст, получено %d", rec.Code)
	}
}
