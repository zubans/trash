package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Открывать сокет могут только те источники, которым доверяет сам API. Без
	// этой проверки любая веб-страница могла бы открыть аутентифицированный сокет с
	// cookie посетителя и читать или писать его чат (межсайтовый перехват
	// WebSocket) — политика CORS на рукопожатия WebSocket не распространяется.
	CheckOrigin: IsAllowedOrigin,
}

// maxMessageRunes ограничивает одно сообщение чата, чтобы клиент не мог
// заталкивать в базу неограниченный текст.
const maxMessageRunes = 4000

// ChatClient представляет активную клиентскую сессию WebSocket.
type ChatClient struct {
	Conn   *websocket.Conn
	UserID uuid.UUID
	Role   string
	Send   chan []byte
}

// ChatRoom хранит активные клиентские соединения чата одного заказа.
type ChatRoom struct {
	ChatID     uuid.UUID
	OrderID    uuid.UUID
	Clients    map[*ChatClient]bool
	Register   chan *ChatClient
	Unregister chan *ChatClient
	Broadcast  chan []byte

	// refs считает соединения, держащие эту комнату, и охраняется мьютексом
	// сервиса — тем же, который эту комнату и выдаёт.
	//
	// Раньше комната сама удаляла себя из сервиса, когда отписывался её последний
	// клиент. Соединение, только что взявшее указатель на комнату и ещё не
	// добравшееся до блокирующей отправки в Register, оставалось после этого
	// отправляющим в горутину, которая уже вернулась: обработчик блокировался
	// навсегда, удерживая сокет и горутину, которые никто уже не освободит. Подсчёт
	// держателей под тем же замком, что выдаёт комнату, закрывает это окно, потому
	// что комнату нельзя списать, пока кто-то ещё идёт к тому, чтобы в ней
	// зарегистрироваться.
	refs int
	// done закрывается, когда уходит последний держатель, — именно это
	// останавливает горутину комнаты.
	done chan struct{}
}

// ChatService управляет группами WebSocket-сессий, обработкой сообщений и получением истории.
type ChatService struct {
	chatRepo  repository.ChatRepository
	orderRepo repository.OrderRepository
	rooms     map[uuid.UUID]*ChatRoom // с ключом OrderID
	mu        sync.RWMutex
}

// NewChatService создаёт новый ChatService.
func NewChatService(chatRepo repository.ChatRepository, orderRepo repository.OrderRepository) *ChatService {
	return &ChatService{
		chatRepo:  chatRepo,
		orderRepo: orderRepo,
		rooms:     make(map[uuid.UUID]*ChatRoom),
	}
}

func (s *ChatService) getOrCreateRoom(ctx context.Context, orderID, chatID uuid.UUID) *ChatRoom {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, exists := s.rooms[orderID]
	if !exists {
		room = &ChatRoom{
			ChatID:     chatID,
			OrderID:    orderID,
			Clients:    make(map[*ChatClient]bool),
			Register:   make(chan *ChatClient),
			Unregister: make(chan *ChatClient),
			Broadcast:  make(chan []byte),
			done:       make(chan struct{}),
		}
		s.rooms[orderID] = room
		go s.runRoom(ctx, room)
	}
	// Вызывающий теперь держит комнату и обязан вызвать releaseRoom, когда его
	// соединение закончится. Взято под тем же замком, что её создал или нашёл,
	// поэтому комнату нельзя списать между этими двумя действиями.
	room.refs++
	return room
}

// releaseRoom отпускает один захват комнаты, списывая её, когда уходит последний.
func (s *ChatService) releaseRoom(room *ChatRoom) {
	s.mu.Lock()
	defer s.mu.Unlock()

	room.refs--
	if room.refs > 0 {
		return
	}
	// Убираем запись, только если это всё ещё та же комната: комната, списанная
	// здесь и заново созданная более поздним соединением, иначе была бы выдернута
	// из-под своих новых держателей.
	if current, ok := s.rooms[room.OrderID]; ok && current == room {
		delete(s.rooms, room.OrderID)
	}
	close(room.done)
}

func (s *ChatService) runRoom(ctx context.Context, room *ChatRoom) {
	for {
		select {
		case client := <-room.Register:
			room.Clients[client] = true
		case client := <-room.Unregister:
			if _, ok := room.Clients[client]; ok {
				delete(room.Clients, client)
				close(client.Send)
			}
		case <-room.done:
			// Последний держатель ушёл, и комната уже вне карты сервиса. Всё, что
			// здесь ещё зарегистрировано, — клиент, чей читатель исчез; закрываем его
			// писателя, чтобы горутина завершилась.
			for client := range room.Clients {
				delete(room.Clients, client)
				close(client.Send)
			}
			return
		case message := <-room.Broadcast:
			for client := range room.Clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(room.Clients, client)
				}
			}
		}
	}
}

// WritePump проталкивает сообщения из канала отправки клиенту WebSocket.
func (c *ChatClient) WritePump() {
	defer func() {
		c.Conn.Close()
	}()
	for msg := range c.Send {
		err := c.Conn.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			return
		}
	}
}

// ReadPump слушает сообщения от клиента WebSocket и рассылает их.
func (s *ChatService) ReadPump(ctx context.Context, client *ChatClient, room *ChatRoom) {
	defer func() {
		// Сначала отписка, потом отпускание: пока это соединение всё ещё держит
		// комнату, её горутина гарантированно работает и это примет.
		room.Unregister <- client
		s.releaseRoom(room)
		client.Conn.Close()
	}()

	for {
		_, messageBytes, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		// 1. Читаем текущий статус заказа, чтобы проверить состояние чата
		order, err := s.orderRepo.GetOrderByID(ctx, room.OrderID)
		if err != nil {
			log.Printf("[ChatService] Failed to check order status: %v", err)
			continue
		}

		// Если заказ уже COMPLETED или CANCELED, гасим чат
		if order.Status == "COMPLETED" || order.Status == "CANCELED" {
			_ = s.chatRepo.DeactivateChat(ctx, room.ChatID)
			sysMsg, _ := json.Marshal(map[string]string{
				"type":   "system",
				"action": "lock",
			})
			room.Broadcast <- sysMsg
			break
		}

		// Проверяем, активна ли чат-комната в базе
		chat, err := s.chatRepo.GetChatByOrderID(ctx, room.OrderID)
		if err != nil || chat == nil || !chat.IsActive {
			warnMsg, _ := json.Marshal(map[string]string{
				"type":    "error",
				"message": "Chat is locked (read-only).",
			})
			_ = client.Conn.WriteMessage(websocket.TextMessage, warnMsg)
			continue
		}

		// Проверяем, не является ли сообщение подтверждением статуса (delivery_ack / read_ack)
		var eventReq struct {
			Type   string   `json:"type"`
			Text   string   `json:"text"`
			MsgIDs []string `json:"message_ids"`
		}
		if err := json.Unmarshal(messageBytes, &eventReq); err == nil && eventReq.Type != "" {
			if eventReq.Type == "delivery_ack" {
				updatedIDs, err := s.chatRepo.MarkMessagesAsDelivered(ctx, room.ChatID, client.UserID)
				if err == nil && len(updatedIDs) > 0 {
					ackBytes, _ := json.Marshal(map[string]interface{}{
						"type":        "status_update",
						"message_ids": updatedIDs,
						"status":      "delivered",
					})
					room.Broadcast <- ackBytes
				}
				continue
			} else if eventReq.Type == "read_ack" {
				updatedIDs, err := s.chatRepo.MarkMessagesAsRead(ctx, room.ChatID, client.UserID)
				if err == nil && len(updatedIDs) > 0 {
					ackBytes, _ := json.Marshal(map[string]interface{}{
						"type":        "status_update",
						"message_ids": updatedIDs,
						"status":      "read",
					})
					room.Broadcast <- ackBytes
				}
				continue
			} else if eventReq.Type == "ping" {
				// Диагностический round-trip: доказательство, что кадр, отправленный
				// клиентом, реально дошёл до сервера. Pong рассылается через писателя
				// комнаты (и никогда не пишется в сокет прямо отсюда — это гонка с
				// насосом записи). Клиенты игнорируют pong в интерфейсе и только логируют.
				pongBytes, _ := json.Marshal(map[string]interface{}{
					"type": "pong",
					"ts":   time.Now().UnixMilli(),
				})
				room.Broadcast <- pongBytes
				continue
			}
		}

		// Разбираем входящее текстовое сообщение
		var msgReq struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(messageBytes, &msgReq); err != nil || msgReq.Text == "" {
			continue
		}
		if len([]rune(msgReq.Text)) > maxMessageRunes {
			continue
		}

		// Сохраняем сообщение в базу
		savedMsg, err := s.chatRepo.SaveMessage(ctx, room.ChatID, client.UserID, msgReq.Text)
		if err != nil {
			log.Printf("[ChatService] Failed to save message: %v", err)
			continue
		}

		// Рассылаем сообщение
		broadcastBytes, err := json.Marshal(savedMsg)
		if err == nil {
			room.Broadcast <- broadcastBytes
		}
	}
}

// MarkMessagesAsRead помечает сообщения заказа прочитанными.
func (s *ChatService) MarkMessagesAsRead(ctx context.Context, orderID, userID uuid.UUID) ([]uuid.UUID, error) {
	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil || chat == nil {
		return nil, errors.New("chat room not found")
	}
	updatedIDs, err := s.chatRepo.MarkMessagesAsRead(ctx, chat.ID, userID)
	if err == nil && len(updatedIDs) > 0 {
		s.mu.RLock()
		room, exists := s.rooms[orderID]
		s.mu.RUnlock()
		if exists {
			ackBytes, _ := json.Marshal(map[string]interface{}{
				"type":        "status_update",
				"message_ids": updatedIDs,
				"status":      "read",
			})
			select {
			case room.Broadcast <- ackBytes:
			default:
			}
		}
	}
	return updatedIDs, err
}

// GetUnreadOrderIDs возвращает ID заказов с непрочитанными сообщениями для пользователя.
func (s *ChatService) GetUnreadOrderIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return s.chatRepo.GetUnreadOrderIDs(ctx, userID)
}

// GetMessages отдаёт окно истории чата заказа, проверяя, что вызывающий
// участвует в переписке. Окно выбирает вызывающий (см. repository.MessageQuery);
// пустой запрос даёт самую свежую страницу.
func (s *ChatService) GetMessages(ctx context.Context, orderID, userID uuid.UUID, q repository.MessageQuery) ([]*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(ctx, orderID)
		if err != nil {
			return nil, err
		}
	}

	return s.chatRepo.GetMessages(ctx, chat.ID, q)
}

// SendMessage сохраняет сообщение чата через REST и рассылает его активным
// WS-клиентам. Это классический запасной путь по HTTP, используемый, когда путь
// отправки по WebSocket недоступен (например, в WebView, где мост глотает ws.send()).
func (s *ChatService) SendMessage(ctx context.Context, orderID, userID uuid.UUID, text string) (*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, fmt.Errorf("%w: order completed or canceled", ErrChatLocked)
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, ErrChatLocked
	}

	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	savedMsg, err := s.chatRepo.SaveMessage(ctx, chat.ID, userID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("order")

	// Рассылаем всем активным клиентам WebSocket в комнате.
	bytes, err := json.Marshal(savedMsg)
	if err == nil {
		s.mu.RLock()
		room, exists := s.rooms[orderID]
		s.mu.RUnlock()
		if exists {
			select {
			case room.Broadcast <- bytes:
			default:
				// отбрасываем, если буфер переполнен; клиенты перезапросят историю
			}
		}
	}

	return savedMsg, nil
}

// SendMessageWithAttachment сохраняет сообщение чата с вложением через REST и рассылает его.
func (s *ChatService) SendMessageWithAttachment(ctx context.Context, orderID, userID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, fmt.Errorf("%w: order completed or canceled", ErrChatLocked)
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, ErrChatLocked
	}

	savedMsg, err := s.chatRepo.SaveMessageWithAttachment(ctx, chat.ID, userID, text, fileURL, fileName, fileType, fileSize)
	if err != nil {
		return nil, err
	}

	// Рассылаем активным клиентам WebSocket.
	bytes, err := json.Marshal(savedMsg)
	if err == nil {
		s.mu.RLock()
		room, exists := s.rooms[orderID]
		s.mu.RUnlock()
		if exists {
			select {
			case room.Broadcast <- bytes:
			default:
			}
		}
	}

	return savedMsg, nil
}

// HandleWS обрабатывает апгрейды, авторизацию и циклы.
func (s *ChatService) HandleWS(ctx context.Context, w http.ResponseWriter, r *http.Request, orderID, userID uuid.UUID, role string) {
	order, err := s.orderRepo.GetOrderByID(ctx, orderID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		http.Error(w, "forbidden: you are not a participant in this order", http.StatusForbidden)
		return
	}

	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(ctx, orderID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ChatService] Upgrade error: %v", err)
		return
	}

	room := s.getOrCreateRoom(ctx, orderID, chat.ID)
	client := &ChatClient{
		Conn:   conn,
		UserID: userID,
		Role:   role,
		Send:   make(chan []byte, 256),
	}
	room.Register <- client

	go client.WritePump()

	// Автоматически помечаем сообщения прочитанными, когда пользователь подключается к комнате, и уведомляем собеседника
	if updatedIDs, err := s.chatRepo.MarkMessagesAsRead(ctx, chat.ID, userID); err == nil && len(updatedIDs) > 0 {
		ackBytes, _ := json.Marshal(map[string]interface{}{
			"type":        "status_update",
			"message_ids": updatedIDs,
			"status":      "read",
		})
		select {
		case room.Broadcast <- ackBytes:
		default:
		}
	}

	// Датчик парно ставится здесь, а не внутри ReadPump, чтобы соединение никогда
	// не было посчитано без своего парного уменьшения.
	metrics.ChatConnected("order")
	go func() {
		defer metrics.ChatDisconnected("order")
		s.ReadPump(ctx, client, room)
	}()
}

// EditMessage меняет текст сообщения, если оно принадлежит отправителю, и рассылает событие message_edited.
func (s *ChatService) EditMessage(ctx context.Context, messageID, senderID, orderID uuid.UUID, newText string) (*repository.Message, error) {
	if len([]rune(newText)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	msg, err := s.chatRepo.UpdateMessage(ctx, messageID, senderID, newText)
	if err != nil {
		return nil, err
	}
	// Комната, которую надо уведомить, выводится из самого сообщения: взятие id
	// заказа из запроса позволило бы отправителю протолкнуть событие правки в чат,
	// участником которого он не является.
	if err := s.assertMessageInOrder(ctx, msg, orderID); err != nil {
		return nil, err
	}

	// Рассылаем событие правки в комнату, если она активна
	editPayload, _ := json.Marshal(map[string]interface{}{
		"type":       "message_edited",
		"message_id": messageID,
		"order_id":   orderID,
		"text":       msg.Text,
		"updated_at": msg.UpdatedAt,
		"message":    msg,
	})

	s.mu.RLock()
	room, exists := s.rooms[orderID]
	s.mu.RUnlock()
	if exists {
		select {
		case room.Broadcast <- editPayload:
		default:
		}
	}

	return msg, nil
}

// DeleteMessage удаляет сообщение, если оно принадлежит отправителю, и рассылает событие message_deleted.
func (s *ChatService) DeleteMessage(ctx context.Context, messageID, senderID, orderID uuid.UUID) error {
	if err := s.chatRepo.DeleteMessage(ctx, messageID, senderID); err != nil {
		return err
	}

	// Рассылаем событие удаления в комнату, если она активна
	deletePayload, _ := json.Marshal(map[string]interface{}{
		"type":       "message_deleted",
		"message_id": messageID,
		"order_id":   orderID,
	})

	s.mu.RLock()
	room, exists := s.rooms[orderID]
	s.mu.RUnlock()
	if exists {
		select {
		case room.Broadcast <- deletePayload:
		default:
		}
	}

	return nil
}

// assertMessageInOrder проверяет, что сообщение действительно принадлежит чату
// указанного заказа.
func (s *ChatService) assertMessageInOrder(ctx context.Context, msg *repository.Message, orderID uuid.UUID) error {
	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err != nil || chat == nil || chat.ID != msg.ChatID {
		return ErrForbidden
	}
	return nil
}

// BroadcastSystemMessage отправляет произвольную нагрузку всем активным
// соединениям заказа. Отправка никогда не блокируется: у комнаты, чей последний
// клиент только что отключился, не осталось читателя, а блокирующая отправка
// навсегда утекла бы горутиной вызывающего.
func (s *ChatService) BroadcastSystemMessage(ctx context.Context, orderID uuid.UUID, msg interface{}) {
	s.mu.RLock()
	room, exists := s.rooms[orderID]
	s.mu.RUnlock()
	if !exists {
		return
	}
	bytes, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case room.Broadcast <- bytes:
	case <-time.After(time.Second):
		log.Printf("[ChatService] dropped system message for order %s: room is not reading", orderID)
	}
}

// GetOrCreateSupportChat возвращает чат поддержки пользователя.
func (s *ChatService) GetOrCreateSupportChat(ctx context.Context, userID uuid.UUID) (*repository.SupportChat, error) {
	return s.chatRepo.GetOrCreateSupportChat(ctx, userID)
}

// Ошибки, которые возвращает сервис чата. Обработчики сопоставляют их с кодами
// статуса по тождеству: сопоставление по тексту ошибки — способ молча
// превратить 403 в 500 при переименовании.
var (
	// ErrForbidden сообщает, что вызывающий не участник переписки.
	ErrForbidden = errors.New("forbidden: this chat does not belong to you")
	// ErrChatLocked сообщает, что переписка больше не принимает сообщений.
	ErrChatLocked = errors.New("chat is locked (read-only)")
)

// authorizeSupportChat пропускает владельца чата и любого админа. Переписки
// поддержки адресуются по id чата, поэтому без этой проверки любой
// аутентифицированный пользователь мог бы читать или писать в чужой чат.
func (s *ChatService) authorizeSupportChat(ctx context.Context, chatID, userID uuid.UUID, role string) error {
	if role == "ADMIN" {
		return nil
	}
	owner, err := s.chatRepo.SupportChatOwner(ctx, chatID)
	if err != nil {
		return ErrForbidden
	}
	if owner != userID {
		return ErrForbidden
	}
	return nil
}

// GetSupportMessages возвращает окно чата поддержки, которым владеет вызывающий.
func (s *ChatService) GetSupportMessages(ctx context.Context, chatID, userID uuid.UUID, role string, q repository.MessageQuery) ([]*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, userID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.GetSupportMessages(ctx, chatID, q)
}

// SaveSupportMessage сохраняет новое текстовое сообщение поддержки.
func (s *ChatService) SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, role, text string) (*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, senderID, role); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("text is required")
	}
	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}
	msg, err := s.chatRepo.SaveSupportMessage(ctx, chatID, senderID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("support")
	return msg, nil
}

// SaveSupportMessageWithAttachment сохраняет новое сообщение поддержки с вложением.
func (s *ChatService) SaveSupportMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, role, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	if err := s.authorizeSupportChat(ctx, chatID, senderID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.SaveSupportMessageWithAttachment(ctx, chatID, senderID, text, fileURL, fileName, fileType, fileSize)
}

// CanAccessAttachment сообщает, может ли пользователь скачать сохранённый файл.
func (s *ChatService) CanAccessAttachment(ctx context.Context, userID uuid.UUID, role, fileURL string) (bool, error) {
	if role == "ADMIN" {
		return true, nil
	}
	return s.chatRepo.CanAccessAttachment(ctx, userID, fileURL)
}

// GetAdminSupportChatList возвращает недавно активные чаты поддержки для
// админского интерфейса в стиле Telegram, ограниченные размером страницы репозитория.
func (s *ChatService) GetAdminSupportChatList(ctx context.Context) ([]*repository.SupportChatListItem, error) {
	return s.chatRepo.GetAdminSupportChatList(ctx, 0)
}

// MarkSupportMessagesAsRead помечает непрочитанные сообщения чата поддержки прочитанными.
func (s *ChatService) MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID, role string) error {
	if err := s.authorizeSupportChat(ctx, chatID, readerID, role); err != nil {
		return err
	}
	return s.chatRepo.MarkSupportMessagesAsRead(ctx, chatID, readerID)
}

// BanSupportChat банит чат поддержки на указанный срок («10m», «1h», «forever»).
func (s *ChatService) BanSupportChat(ctx context.Context, chatID uuid.UUID, duration string) error {
	return s.chatRepo.BanSupportChat(ctx, chatID, duration)
}

// UnbanSupportChat снимает бан с чата поддержки.
func (s *ChatService) UnbanSupportChat(ctx context.Context, chatID uuid.UUID) error {
	return s.chatRepo.UnbanSupportChat(ctx, chatID)
}

// IsSupportChatBanned проверяет, забанен ли чат поддержки.
func (s *ChatService) IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error) {
	return s.chatRepo.IsSupportChatBanned(ctx, chatID)
}

// GetAdminSupportUnreadCount возвращает общее число непрочитанных сообщений для админа.
func (s *ChatService) GetAdminSupportUnreadCount(ctx context.Context) (int, error) {
	return s.chatRepo.GetAdminSupportUnreadCount(ctx)
}
