package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// Фейки чата. Встраивание интерфейса оставляет реализованными только методы,
// которые трогают проверяемые маршруты; прочие уронят тест nil-паникой, а не
// молча вернут пустой ответ. Каждый метод ведёт себя как database/sql: с
// отменённым контекстом запрос не выполняется, а возвращает ctx.Err().

type fakeChatRepo struct {
	repository.ChatRepository
	mu       sync.Mutex
	chat     *repository.Chat
	saved    []string
	owner    uuid.UUID
	banned   bool
	markRead int
}

func (f *fakeChatRepo) GetChatByOrderID(ctx context.Context, orderID uuid.UUID) (*repository.Chat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.chat != nil && f.chat.OrderID == orderID {
		return f.chat, nil
	}
	return nil, nil
}

func (f *fakeChatRepo) CreateChat(ctx context.Context, orderID uuid.UUID) (*repository.Chat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chat = &repository.Chat{ID: uuid.New(), OrderID: orderID, IsActive: true}
	return f.chat, nil
}

func (f *fakeChatRepo) SaveMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, text)
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text, CreatedAt: time.Now()}, nil
}

func (f *fakeChatRepo) MarkMessagesAsRead(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	return nil, ctx.Err()
}

func (f *fakeChatRepo) savedTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.saved...)
}

func (f *fakeChatRepo) GetSupportMessages(ctx context.Context, chatID uuid.UUID, q repository.MessageQuery) ([]*repository.Message, error) {
	return nil, ctx.Err()
}

func (f *fakeChatRepo) SupportChatOwner(ctx context.Context, chatID uuid.UUID) (uuid.UUID, error) {
	return f.owner, ctx.Err()
}

func (f *fakeChatRepo) MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markRead++
	return ctx.Err()
}

func (f *fakeChatRepo) IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error) {
	return f.banned, nil, ctx.Err()
}

func (f *fakeChatRepo) SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, text)
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text}, nil
}

func (f *fakeChatRepo) GetAdminSupportChatList(ctx context.Context, limit int) ([]*repository.SupportChatListItem, error) {
	return []*repository.SupportChatListItem{}, ctx.Err()
}

func (f *fakeChatRepo) GetAdminSupportUnreadCount(ctx context.Context) (int, error) {
	return 3, ctx.Err()
}

type fakeOrderRepo struct {
	repository.OrderRepository
	order *repository.Order
}

func (f *fakeOrderRepo) GetOrderByID(ctx context.Context, id uuid.UUID) (*repository.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.order != nil && f.order.ID == id {
		return f.order, nil
	}
	return nil, errors.New("order not found")
}

// withUser подкладывает пользователя в контекст, как это делает RequireAuth.
func withUser(user *repository.User, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r.WithContext(context.WithValue(r.Context(), middleware.UserKey, user)))
	}
}

// Сокет чата живёт много дольше запроса, который его открыл, а net/http
// отменяет контекст запроса сразу после возврата обработчика. Сообщение,
// присланное после этого, обязано дойти до базы: с контекстом запроса каждый
// запрос падал бы с context canceled и сообщение терялось бы молча.
func TestWebSocketHandler_ConnectionOutlivesRequestContext(t *testing.T) {
	customerID := uuid.New()
	order := &repository.Order{ID: uuid.New(), CustomerID: customerID, Status: "ASSIGNED"}
	chatRepo := &fakeChatRepo{}
	h := NewChatHandler(service.NewChatService(chatRepo, &fakeOrderRepo{order: order}))
	user := &repository.User{ID: customerID, Role: repository.RoleCustomer}

	// Контекст запроса, каким его видел обработчик: тест дождётся его отмены,
	// прежде чем слать кадр, — так проверяется именно жизнь после запроса.
	requestCtx := make(chan context.Context, 1)
	r := chi.NewRouter()
	r.Get("/chats/{order_id}/ws", withUser(user, func(w http.ResponseWriter, r *http.Request) {
		requestCtx <- r.Context()
		h.WebSocketHandler(w, r)
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/chats/"+order.ID.String()+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case ctx := <-requestCtx:
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("request context was not cancelled after the handler returned")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"text":"after the request"}`)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no echo of the message: %v (saved: %v)", err, chatRepo.savedTexts())
		}
		var frame struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &frame) == nil && frame.Text == "after the request" {
			break
		}
	}
	if got := chatRepo.savedTexts(); len(got) != 1 || got[0] != "after the request" {
		t.Fatalf("message sent after the request ended must be persisted, saved: %v", got)
	}
}

func newSupportChatHandler(repo *fakeChatRepo) *ChatHandler {
	return NewChatHandler(service.NewChatService(repo, &fakeOrderRepo{}))
}

// supportRequest собирает запрос к маршруту чата поддержки от имени user.
func supportRequest(method, target string, user *repository.User, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("chat_id", strings.Split(strings.TrimPrefix(target, "/support/chats/"), "/")[0])
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req.WithContext(context.WithValue(req.Context(), middleware.UserKey, user))
}

// Опрос переписки каждые несколько секунд не должен заново помечать её
// прочитанной: это делает только открытие (запрос без курсора).
func TestGetSupportMessagesHandler_MarksReadOnlyOnFirstPage(t *testing.T) {
	owner := uuid.New()
	chatID := uuid.New()
	repo := &fakeChatRepo{owner: owner}
	h := newSupportChatHandler(repo)
	user := &repository.User{ID: owner, Role: repository.RoleCustomer}
	base := "/support/chats/" + chatID.String() + "/messages"

	serve := func(target string) int {
		rec := httptest.NewRecorder()
		h.GetSupportMessagesHandler(rec, supportRequest(http.MethodGet, target, user, ""))
		return rec.Code
	}
	cursor := time.Now().UTC().Format(time.RFC3339Nano)

	if code := serve(base); code != http.StatusOK {
		t.Fatalf("first page: status %d", code)
	}
	if repo.markRead != 1 {
		t.Fatalf("opening the chat must mark it read once, got %d", repo.markRead)
	}
	if code := serve(base + "?after=" + cursor); code != http.StatusOK {
		t.Fatalf("poll: status %d", code)
	}
	if code := serve(base + "?before=" + cursor); code != http.StatusOK {
		t.Fatalf("older page: status %d", code)
	}
	if repo.markRead != 1 {
		t.Fatalf("poll and back-scroll must not mark the chat read again, got %d marks", repo.markRead)
	}
}

// Бан чата поддержки останавливает его владельца, но не администратора — в
// том числе того, у кого ADMIN лишь одна из ролей, а основная другая.
func TestSendSupportMessageHandler_BanSparesAdmins(t *testing.T) {
	owner := uuid.New()
	chatID := uuid.New()
	repo := &fakeChatRepo{owner: owner, banned: true}
	h := newSupportChatHandler(repo)
	target := "/support/chats/" + chatID.String() + "/messages"

	rec := httptest.NewRecorder()
	h.SendSupportMessageHandler(rec, supportRequest(http.MethodPost, target, &repository.User{ID: owner, Role: repository.RoleCustomer}, `{"text":"help"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner of a banned chat: status %d, want 403", rec.Code)
	}

	admin := &repository.User{ID: uuid.New(), Role: repository.RoleAdmin, Roles: []string{repository.RoleModerator, repository.RoleAdmin}}
	rec = httptest.NewRecorder()
	h.SendSupportMessageHandler(rec, supportRequest(http.MethodPost, target, admin, `{"text":"reply"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin writing to a banned chat: status %d, body %s", rec.Code, rec.Body.String())
	}
	if got := repo.savedTexts(); len(got) != 1 || got[0] != "reply" {
		t.Fatalf("admin reply must be saved, saved: %v", got)
	}
}

// Список чатов и счётчик непрочитанного охраняет право support_chats.view на
// маршруте; обработчик не должен дополнительно требовать роль ADMIN, иначе
// модератор с этим правом получает 401.
func TestAdminSupportHandlers_DoNotRequireAdminRole(t *testing.T) {
	h := newSupportChatHandler(&fakeChatRepo{})
	moderator := &repository.User{ID: uuid.New(), Role: repository.RoleModerator}

	for name, handle := range map[string]http.HandlerFunc{
		"list":   h.GetAdminSupportChatListHandler,
		"unread": h.GetAdminSupportUnreadSummaryHandler,
	} {
		rec := httptest.NewRecorder()
		handle(rec, supportRequest(http.MethodGet, "/admin/support/chats", moderator, ""))
		if rec.Code != http.StatusOK {
			t.Errorf("%s for a moderator: status %d, want 200", name, rec.Code)
		}
	}
}
