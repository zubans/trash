package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

type mockChatRepo struct {
	// mu охраняет срезы: насос чтения сокета пишет в них из своей горутины,
	// а тест читает из своей.
	mu            sync.Mutex
	chats         []*repository.Chat
	messages      []*repository.Message
	supportOwners map[uuid.UUID]uuid.UUID
}

func (m *mockChatRepo) GetChatByOrderID(ctx context.Context, orderID uuid.UUID) (*repository.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.chats {
		if c.OrderID == orderID {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockChatRepo) CreateChat(ctx context.Context, orderID uuid.UUID) (*repository.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := &repository.Chat{
		ID:       uuid.New(),
		OrderID:  orderID,
		IsActive: true,
	}
	m.chats = append(m.chats, c)
	return c, nil
}

func (m *mockChatRepo) SaveMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := &repository.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      text,
		CreatedAt: time.Now(),
	}
	m.messages = append(m.messages, msg)
	return msg, nil
}

func (m *mockChatRepo) GetMessages(ctx context.Context, chatID uuid.UUID, q repository.MessageQuery) ([]*repository.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*repository.Message
	for _, msg := range m.messages {
		if msg.ChatID == chatID {
			list = append(list, msg)
		}
	}
	return list, nil
}

func (m *mockChatRepo) DeactivateChat(ctx context.Context, chatID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.chats {
		if c.ID == chatID {
			c.IsActive = false
			return nil
		}
	}
	return errors.New("chat not found")
}

func (m *mockChatRepo) GetUnreadOrderIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) MarkMessagesAsDelivered(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) MarkMessagesAsRead(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) SaveMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn := fileName
	msg := &repository.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      text,
		FileURL:   &fileURL,
		FileName:  &fn,
		FileType:  &fileType,
		FileSize:  &fileSize,
		CreatedAt: time.Now(),
	}
	m.messages = append(m.messages, msg)
	return msg, nil
}

func (m *mockChatRepo) DeleteMessage(ctx context.Context, messageID, senderID uuid.UUID) error {
	return nil
}

func (m *mockChatRepo) UpdateMessage(ctx context.Context, messageID, senderID uuid.UUID, newText string) (*repository.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.messages {
		if msg.ID == messageID && msg.SenderID == senderID {
			msg.Text = newText
			now := time.Now()
			msg.UpdatedAt = &now
			return msg, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockChatRepo) GetOrCreateSupportChat(ctx context.Context, userID uuid.UUID) (*repository.SupportChat, error) {
	return &repository.SupportChat{ID: uuid.New(), UserID: userID}, nil
}
func (m *mockChatRepo) GetSupportMessages(ctx context.Context, chatID uuid.UUID, q repository.MessageQuery) ([]*repository.Message, error) {
	return nil, nil
}
func (m *mockChatRepo) SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text}, nil
}
func (m *mockChatRepo) SaveSupportMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text}, nil
}
func (m *mockChatRepo) GetAdminSupportChatList(ctx context.Context, limit int) ([]*repository.SupportChatListItem, error) {
	return nil, nil
}
func (m *mockChatRepo) MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID) error {
	return nil
}

func TestChatService_GetMessagesAccessControl(t *testing.T) {
	chatRepo := &mockChatRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewChatService(chatRepo, orderRepo)

	customerID := uuid.New()
	executorID := uuid.New()
	strangerID := uuid.New()

	// Создаём заказ и назначаем исполнителя
	standardVariantID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	order, _ := orderRepo.CreateOrderWithHold(context.Background(), customerID, standardVariantID, false, false, 100.00, "")
	_ = orderRepo.AssignOrder(context.Background(), order.ID, executorID)

	// Создаём сессию чата
	chat, _ := chatRepo.CreateChat(context.Background(), order.ID)
	_, _ = chatRepo.SaveMessage(context.Background(), chat.ID, customerID, "Hello!")

	// Случай 1: заказчик должен получить доступ к сообщениям
	msgs, err := srv.GetMessages(context.Background(), order.ID, customerID, repository.MessageQuery{})
	if err != nil || len(msgs) != 1 {
		t.Errorf("expected customer to access messages, got err: %v, len: %d", err, len(msgs))
	}

	// Случай 2: исполнитель должен получить доступ к сообщениям
	msgs, err = srv.GetMessages(context.Background(), order.ID, executorID, repository.MessageQuery{})
	if err != nil || len(msgs) != 1 {
		t.Errorf("expected executor to access messages, got err: %v", err)
	}

	// Случай 3: посторонний НЕ должен получить доступ к сообщениям (должна вернуться ошибка)
	_, err = srv.GetMessages(context.Background(), order.ID, strangerID, repository.MessageQuery{})
	if err == nil {
		t.Error("expected error for stranger accessing messages")
	}
}

func TestChatService_EditAndDeleteMessage(t *testing.T) {
	chatRepo := &mockChatRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewChatService(chatRepo, orderRepo)

	customerID := uuid.New()
	orderID := uuid.New()
	chat, _ := chatRepo.CreateChat(context.Background(), orderID)
	msg, _ := chatRepo.SaveMessage(context.Background(), chat.ID, customerID, "Initial Message")

	// Проверяем EditMessage
	editedMsg, err := srv.EditMessage(context.Background(), msg.ID, customerID, orderID, "Edited Message Text")
	if err != nil {
		t.Fatalf("unexpected error editing message: %v", err)
	}
	if editedMsg.Text != "Edited Message Text" {
		t.Errorf("expected text 'Edited Message Text', got '%s'", editedMsg.Text)
	}
	if editedMsg.UpdatedAt == nil {
		t.Errorf("expected UpdatedAt timestamp to be set")
	}

	// Проверяем DeleteMessage
	err = srv.DeleteMessage(context.Background(), msg.ID, customerID, orderID)
	if err != nil {
		t.Fatalf("unexpected error deleting message: %v", err)
	}
}

// --- методы чата поддержки, требуемые repository.ChatRepository ---

func (m *mockChatRepo) SupportChatOwner(ctx context.Context, chatID uuid.UUID) (uuid.UUID, error) {
	if m.supportOwners == nil {
		return uuid.Nil, errors.New("support chat not found")
	}
	owner, ok := m.supportOwners[chatID]
	if !ok {
		return uuid.Nil, errors.New("support chat not found")
	}
	return owner, nil
}

func (m *mockChatRepo) CanAccessAttachment(ctx context.Context, userID uuid.UUID, fileURL string) (bool, error) {
	return false, nil
}

func (m *mockChatRepo) BanSupportChat(ctx context.Context, chatID uuid.UUID, duration string) error {
	return nil
}

func (m *mockChatRepo) UnbanSupportChat(ctx context.Context, chatID uuid.UUID) error { return nil }

func (m *mockChatRepo) IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error) {
	return false, nil, nil
}

func (m *mockChatRepo) GetAdminSupportUnreadCount(ctx context.Context) (int, error) { return 0, nil }

// TestChatService_SupportChatOwnership проверяет, что переписку с поддержкой
// может читать и писать только тот пользователь, которому она принадлежит (и админы).
func TestChatService_SupportChatOwnership(t *testing.T) {
	owner := uuid.New()
	stranger := uuid.New()
	chatID := uuid.New()

	chatRepo := &mockChatRepo{supportOwners: map[uuid.UUID]uuid.UUID{chatID: owner}}
	svc := NewChatService(chatRepo, &mockOrderRepo{})

	if _, err := svc.GetSupportMessages(context.Background(), chatID, stranger, "CUSTOMER", repository.MessageQuery{}); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger must not read the chat, got %v", err)
	}
	if _, err := svc.SaveSupportMessage(context.Background(), chatID, stranger, "CUSTOMER", "hi"); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger must not write to the chat, got %v", err)
	}
	if _, err := svc.GetSupportMessages(context.Background(), chatID, owner, "CUSTOMER", repository.MessageQuery{}); err != nil {
		t.Errorf("owner must be able to read the chat: %v", err)
	}
	if _, err := svc.GetSupportMessages(context.Background(), chatID, stranger, "ADMIN", repository.MessageQuery{}); err != nil {
		t.Errorf("admin must be able to read any chat: %v", err)
	}
}

// --- WebSocket: соединение, кадры, дедлайны ---

// countingOrderRepo считает обращения за заказом: так тест видит, какие кадры
// сокета стоят запроса к базе, а какие нет.
type countingOrderRepo struct {
	*mockOrderRepo
	lookups atomic.Int32
}

func (r *countingOrderRepo) FindByID(ctx context.Context, orderID uuid.UUID) (*repository.Order, error) {
	r.lookups.Add(1)
	return r.mockOrderRepo.FindByID(ctx, orderID)
}

// chatFixture — заказ с исполнителем и активным чатом, как их видит сервис.
func chatFixture(t *testing.T) (*mockChatRepo, *countingOrderRepo, uuid.UUID, uuid.UUID) {
	t.Helper()
	chatRepo := &mockChatRepo{}
	orderRepo := &countingOrderRepo{mockOrderRepo: &mockOrderRepo{}}
	customerID, executorID := uuid.New(), uuid.New()
	order, _ := orderRepo.CreateOrderWithHold(context.Background(), customerID, uuid.New(), false, false, 100, "")
	_ = orderRepo.AssignOrder(context.Background(), order.ID, executorID)
	if _, err := chatRepo.CreateChat(context.Background(), order.ID); err != nil {
		t.Fatal(err)
	}
	return chatRepo, orderRepo, order.ID, customerID
}

// dialChat поднимает сервер на HandleWS и открывает к нему сокет от имени userID.
func dialChat(t *testing.T, srv *ChatService, orderID, userID uuid.UUID) *websocket.Conn {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HandleWS(r.Context(), w, r, orderID, userID)
	}))
	t.Cleanup(ts.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readFrameOfType читает кадры, пока не встретит нужный тип (или текст), не
// дольше отведённого времени.
func readFrameOfType(t *testing.T, conn *websocket.Conn, want string, timeout time.Duration) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q frame: %v", want, err)
		}
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		if frame["type"] == want || frame["text"] == want {
			return frame
		}
	}
}

// waitFor опрашивает условие, пока оно не выполнится или не выйдет время.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Подтверждения и ping не пишут сообщений, поэтому статус заказа им ни к чему:
// единственное обращение за заказом — проверка участника при подключении.
// Обычное сообщение по-прежнему проверяет заказ перед записью.
func TestChatService_AckAndPingFramesSkipOrderLookup(t *testing.T) {
	chatRepo, orderRepo, orderID, customerID := chatFixture(t)
	srv := NewChatService(chatRepo, orderRepo)
	conn := dialChat(t, srv, orderID, customerID)

	for _, frame := range []string{`{"type":"delivery_ack"}`, `{"type":"read_ack"}`, `{"type":"ping"}`} {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	// Кадры обрабатываются по порядку: pong в ответ на последний значит, что
	// оба подтверждения уже прошли.
	readFrameOfType(t, conn, "pong", 2*time.Second)
	if got := orderRepo.lookups.Load(); got != 1 {
		t.Fatalf("ack/ping frames must not look the order up: %d lookups after connect + 3 frames, want 1", got)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	readFrameOfType(t, conn, "hi", 2*time.Second)
	if got := orderRepo.lookups.Load(); got != 2 {
		t.Fatalf("a message frame must check the order: %d lookups, want 2", got)
	}
	chatRepo.mu.Lock()
	saved := len(chatRepo.messages)
	chatRepo.mu.Unlock()
	if saved != 1 {
		t.Fatalf("message frame must be persisted: %d messages", saved)
	}
}

// chatConnectionsGauge — текущее значение датчика открытых сокетов чата заказов.
func chatConnectionsGauge(t *testing.T) float64 {
	t.Helper()
	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range families {
		if !strings.HasSuffix(mf.GetName(), "chat_websocket_connections") {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "kind" && l.GetValue() == "order" {
					return m.GetGauge().GetValue()
				}
			}
		}
	}
	return 0
}

// Клиент, который не отвечает на ping, рвётся по дедлайну чтения, а не висит
// вечно; за ним прибираются комната и датчик соединений — ровно на единицу.
func TestChatService_IdleClientIsDisconnected(t *testing.T) {
	chatRepo, orderRepo, orderID, customerID := chatFixture(t)
	srv := NewChatService(chatRepo, orderRepo)
	srv.wsTimeouts = wsTimeouts{writeWait: 200 * time.Millisecond, pongWait: 300 * time.Millisecond}

	baseline := chatConnectionsGauge(t)
	conn := dialChat(t, srv, orderID, customerID)
	// Глотаем ping-и вместо ответа pong — так ведёт себя полуоткрытое соединение.
	conn.SetPingHandler(func(string) error { return nil })
	waitFor(t, time.Second, "gauge to count the connection", func() bool {
		return chatConnectionsGauge(t) == baseline+1
	})

	started := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("idle client was dropped only after %v; pongWait is %v", elapsed, srv.wsTimeouts.pongWait)
	}

	waitFor(t, 2*time.Second, "room to be retired", func() bool {
		srv.mu.RLock()
		defer srv.mu.RUnlock()
		_, alive := srv.rooms[orderID]
		return !alive
	})
	waitFor(t, 2*time.Second, "gauge to return to baseline", func() bool {
		return chatConnectionsGauge(t) == baseline
	})
}

// Кадр длиннее предела рвёт соединение, а не буферизуется.
func TestChatService_OversizedFrameClosesConnection(t *testing.T) {
	chatRepo, orderRepo, orderID, customerID := chatFixture(t)
	srv := NewChatService(chatRepo, orderRepo)
	conn := dialChat(t, srv, orderID, customerID)

	huge := `{"text":"` + strings.Repeat("a", maxFrameBytes) + `"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(huge)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("server kept the connection after an oversized frame")
	}
	chatRepo.mu.Lock()
	defer chatRepo.mu.Unlock()
	if len(chatRepo.messages) != 0 {
		t.Fatal("oversized frame must not be persisted")
	}
}

// Правила записи в чат заказа — общий пролог REST-путей: посторонний,
// закрытый заказ и неактивный чат отвергаются одинаково и текстом, и вложением.
func TestChatService_WriteRules(t *testing.T) {
	chatRepo, orderRepo, orderID, customerID := chatFixture(t)
	srv := NewChatService(chatRepo, orderRepo)
	ctx := context.Background()
	stranger := uuid.New()

	send := func(userID uuid.UUID) error {
		_, err := srv.SendMessage(ctx, orderID, userID, "hi")
		return err
	}
	attach := func(userID uuid.UUID) error {
		_, err := srv.SendMessageWithAttachment(ctx, orderID, userID, "", "/uploads/a.jpg", "a.jpg", "image", 1)
		return err
	}

	if err := send(stranger); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger text: got %v, want ErrForbidden", err)
	}
	if err := attach(stranger); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger attachment: got %v, want ErrForbidden", err)
	}
	if err := send(customerID); err != nil {
		t.Errorf("participant text: %v", err)
	}
	if _, err := srv.SendMessage(ctx, orderID, customerID, strings.Repeat("я", maxMessageRunes+1)); err == nil {
		t.Error("over-long text must be rejected")
	}

	chat, _ := chatRepo.GetChatByOrderID(ctx, orderID)
	_ = chatRepo.DeactivateChat(ctx, chat.ID)
	if err := send(customerID); !errors.Is(err, ErrChatLocked) {
		t.Errorf("inactive chat text: got %v, want ErrChatLocked", err)
	}
	if err := attach(customerID); !errors.Is(err, ErrChatLocked) {
		t.Errorf("inactive chat attachment: got %v, want ErrChatLocked", err)
	}

	chat.IsActive = true
	_ = orderRepo.ConfirmOrderExecution(ctx, orderID)
	if err := send(customerID); !errors.Is(err, ErrChatLocked) {
		t.Errorf("completed order text: got %v, want ErrChatLocked", err)
	}
	if err := attach(customerID); !errors.Is(err, ErrChatLocked) {
		t.Errorf("completed order attachment: got %v, want ErrChatLocked", err)
	}
}
