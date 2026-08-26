package service

import (
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
	// Only origins the API itself trusts may open a socket. Without this check
	// any web page could open an authenticated socket with the visitor's cookie
	// and read or write their chat (cross-site WebSocket hijacking) — the CORS
	// policy does not apply to WebSocket handshakes.
	CheckOrigin: IsAllowedOrigin,
}

// maxMessageRunes caps a single chat message so a client cannot push unbounded
// text into the database.
const maxMessageRunes = 4000

// ChatClient represents an active WebSocket client session.
type ChatClient struct {
	Conn   *websocket.Conn
	UserID uuid.UUID
	Role   string
	Send   chan []byte
}

// ChatRoom holds active client connections for a single order's chat.
type ChatRoom struct {
	ChatID     uuid.UUID
	OrderID    uuid.UUID
	Clients    map[*ChatClient]bool
	Register   chan *ChatClient
	Unregister chan *ChatClient
	Broadcast  chan []byte
}

// ChatService manages WebSocket session groups, message processing, and history retrieval.
type ChatService struct {
	chatRepo  repository.ChatRepository
	orderRepo repository.OrderRepository
	rooms     map[uuid.UUID]*ChatRoom // keyed by OrderID
	mu        sync.RWMutex
}

// NewChatService creates a new ChatService.
func NewChatService(chatRepo repository.ChatRepository, orderRepo repository.OrderRepository) *ChatService {
	return &ChatService{
		chatRepo:  chatRepo,
		orderRepo: orderRepo,
		rooms:     make(map[uuid.UUID]*ChatRoom),
	}
}

func (s *ChatService) getOrCreateRoom(orderID, chatID uuid.UUID) *ChatRoom {
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
		}
		s.rooms[orderID] = room
		go s.runRoom(room)
	}
	return room
}

func (s *ChatService) runRoom(room *ChatRoom) {
	for {
		select {
		case client := <-room.Register:
			room.Clients[client] = true
		case client := <-room.Unregister:
			if _, ok := room.Clients[client]; ok {
				delete(room.Clients, client)
				close(client.Send)
			}
			if len(room.Clients) == 0 {
				s.mu.Lock()
				delete(s.rooms, room.OrderID)
				s.mu.Unlock()
				return
			}
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

// WritePump pushes messages from the send channel up to the WebSocket client.
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

// ReadPump listens for messages from the WebSocket client and broadcasts them.
func (s *ChatService) ReadPump(client *ChatClient, room *ChatRoom) {
	defer func() {
		room.Unregister <- client
		client.Conn.Close()
	}()

	for {
		_, messageBytes, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}

		// 1. Fetch current order status to verify chat state
		order, err := s.orderRepo.GetOrderByID(room.OrderID)
		if err != nil {
			log.Printf("[ChatService] Failed to check order status: %v", err)
			continue
		}

		// If the order is already COMPLETED or CANCELED, shut down the chat
		if order.Status == "COMPLETED" || order.Status == "CANCELED" {
			_ = s.chatRepo.DeactivateChat(room.ChatID)
			sysMsg, _ := json.Marshal(map[string]string{
				"type":   "system",
				"action": "lock",
			})
			room.Broadcast <- sysMsg
			break
		}

		// Check if chat room is active in DB
		chat, err := s.chatRepo.GetChatByOrderID(room.OrderID)
		if err != nil || chat == nil || !chat.IsActive {
			warnMsg, _ := json.Marshal(map[string]string{
				"type":    "error",
				"message": "Chat is locked (read-only).",
			})
			_ = client.Conn.WriteMessage(websocket.TextMessage, warnMsg)
			continue
		}

		// Check if message is a status acknowledgment (delivery_ack / read_ack)
		var eventReq struct {
			Type   string   `json:"type"`
			Text   string   `json:"text"`
			MsgIDs []string `json:"message_ids"`
		}
		if err := json.Unmarshal(messageBytes, &eventReq); err == nil && eventReq.Type != "" {
			if eventReq.Type == "delivery_ack" {
				updatedIDs, err := s.chatRepo.MarkMessagesAsDelivered(room.ChatID, client.UserID)
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
				updatedIDs, err := s.chatRepo.MarkMessagesAsRead(room.ChatID, client.UserID)
				if err == nil && len(updatedIDs) > 0 {
					ackBytes, _ := json.Marshal(map[string]interface{}{
						"type":        "status_update",
						"message_ids": updatedIDs,
						"status":      "read",
					})
					room.Broadcast <- ackBytes
				}
				continue
			}
		}

		// Parse input text message
		var msgReq struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(messageBytes, &msgReq); err != nil || msgReq.Text == "" {
			continue
		}
		if len([]rune(msgReq.Text)) > maxMessageRunes {
			continue
		}

		// Save message to DB
		savedMsg, err := s.chatRepo.SaveMessage(room.ChatID, client.UserID, msgReq.Text)
		if err != nil {
			log.Printf("[ChatService] Failed to save message: %v", err)
			continue
		}

		// Broadcast message
		broadcastBytes, err := json.Marshal(savedMsg)
		if err == nil {
			room.Broadcast <- broadcastBytes
		}
	}
}

// MarkMessagesAsRead marks messages as read for an order.
func (s *ChatService) MarkMessagesAsRead(orderID, userID uuid.UUID) ([]uuid.UUID, error) {
	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil || chat == nil {
		return nil, errors.New("chat room not found")
	}
	updatedIDs, err := s.chatRepo.MarkMessagesAsRead(chat.ID, userID)
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

// GetUnreadOrderIDs returns order IDs with unread messages for a user.
func (s *ChatService) GetUnreadOrderIDs(userID uuid.UUID) ([]uuid.UUID, error) {
	return s.chatRepo.GetUnreadOrderIDs(userID)
}

// GetMessages retrieves all saved messages for an order's chat room (verifying participant access).
func (s *ChatService) GetMessages(orderID, userID uuid.UUID) ([]*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(orderID)
		if err != nil {
			return nil, err
		}
	}

	return s.chatRepo.GetMessages(chat.ID)
}

// SendMessage saves a chat message via REST and broadcasts it to active WS clients.
// This is the classic HTTP fallback used when the WebSocket send path is not
// available (e.g. on mobile WebViews where the bridge swallows ws.send()).
func (s *ChatService) SendMessage(orderID, userID uuid.UUID, text string) (*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, fmt.Errorf("%w: order completed or canceled", ErrChatLocked)
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, ErrChatLocked
	}

	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	savedMsg, err := s.chatRepo.SaveMessage(chat.ID, userID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("order")

	// Broadcast to any active WebSocket clients in the room.
	bytes, err := json.Marshal(savedMsg)
	if err == nil {
		s.mu.RLock()
		room, exists := s.rooms[orderID]
		s.mu.RUnlock()
		if exists {
			select {
			case room.Broadcast <- bytes:
			default:
				// drop if backbuffered; clients will refetch history
			}
		}
	}

	return savedMsg, nil
}

// SendMessageWithAttachment saves a chat message with a file attachment via REST and broadcasts it.
func (s *ChatService) SendMessageWithAttachment(orderID, userID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, err
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		return nil, ErrForbidden
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, fmt.Errorf("%w: order completed or canceled", ErrChatLocked)
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, ErrChatLocked
	}

	savedMsg, err := s.chatRepo.SaveMessageWithAttachment(chat.ID, userID, text, fileURL, fileName, fileType, fileSize)
	if err != nil {
		return nil, err
	}

	// Broadcast to active WebSocket clients.
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

// HandleWS handles upgrades, authorization, and loops.
func (s *ChatService) HandleWS(w http.ResponseWriter, r *http.Request, orderID, userID uuid.UUID, role string) {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if order.CustomerID != userID && (order.ExecutorID == nil || *order.ExecutorID != userID) {
		http.Error(w, "forbidden: you are not a participant in this order", http.StatusForbidden)
		return
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if chat == nil {
		chat, err = s.chatRepo.CreateChat(orderID)
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

	room := s.getOrCreateRoom(orderID, chat.ID)
	client := &ChatClient{
		Conn:   conn,
		UserID: userID,
		Role:   role,
		Send:   make(chan []byte, 256),
	}
	room.Register <- client

	go client.WritePump()

	// Automatically mark messages as read when user connects to room and notify partner
	if updatedIDs, err := s.chatRepo.MarkMessagesAsRead(chat.ID, userID); err == nil && len(updatedIDs) > 0 {
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

	// The gauge is paired here rather than inside ReadPump so a connection can
	// never be counted without its matching decrement.
	metrics.ChatConnected("order")
	go func() {
		defer metrics.ChatDisconnected("order")
		s.ReadPump(client, room)
	}()
}

// EditMessage updates message text if owned by sender and broadcasts message_edited event.
func (s *ChatService) EditMessage(messageID, senderID, orderID uuid.UUID, newText string) (*repository.Message, error) {
	if len([]rune(newText)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}

	msg, err := s.chatRepo.UpdateMessage(messageID, senderID, newText)
	if err != nil {
		return nil, err
	}
	// The room to notify is derived from the message itself: taking the order id
	// from the request would let a sender push an edit event into a chat they
	// are not part of.
	if err := s.assertMessageInOrder(msg, orderID); err != nil {
		return nil, err
	}

	// Broadcast edit event to room if active
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

// DeleteMessage deletes a message if owned by sender and broadcasts message_deleted event.
func (s *ChatService) DeleteMessage(messageID, senderID, orderID uuid.UUID) error {
	if err := s.chatRepo.DeleteMessage(messageID, senderID); err != nil {
		return err
	}

	// Broadcast deletion event to room if active
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

// assertMessageInOrder checks that a message really belongs to the chat of the
// given order.
func (s *ChatService) assertMessageInOrder(msg *repository.Message, orderID uuid.UUID) error {
	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil || chat == nil || chat.ID != msg.ChatID {
		return ErrForbidden
	}
	return nil
}

// BroadcastSystemMessage sends a custom message payload to all active
// connections of an order. The send never blocks: a room whose last client just
// disconnected has no reader left, and a blocking send would leak the caller's
// goroutine forever.
func (s *ChatService) BroadcastSystemMessage(orderID uuid.UUID, msg interface{}) {
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

// GetOrCreateSupportChat returns user's support chat.
func (s *ChatService) GetOrCreateSupportChat(userID uuid.UUID) (*repository.SupportChat, error) {
	return s.chatRepo.GetOrCreateSupportChat(userID)
}

// Errors the chat service returns. Handlers map them to status codes by
// identity: matching on the text of an error is how a rename silently turns a
// 403 into a 500.
var (
	// ErrForbidden reports that the caller is not a participant of the conversation.
	ErrForbidden = errors.New("forbidden: this chat does not belong to you")
	// ErrChatLocked reports that the conversation no longer accepts messages.
	ErrChatLocked = errors.New("chat is locked (read-only)")
)

// authorizeSupportChat allows the owner of the chat and any admin. Support
// conversations are addressed by chat id, so without this check any
// authenticated user could read or post into somebody else's chat.
func (s *ChatService) authorizeSupportChat(chatID, userID uuid.UUID, role string) error {
	if role == "ADMIN" {
		return nil
	}
	owner, err := s.chatRepo.SupportChatOwner(chatID)
	if err != nil {
		return ErrForbidden
	}
	if owner != userID {
		return ErrForbidden
	}
	return nil
}

// GetSupportMessages returns all messages for a support chat the caller owns.
func (s *ChatService) GetSupportMessages(chatID, userID uuid.UUID, role string) ([]*repository.Message, error) {
	if err := s.authorizeSupportChat(chatID, userID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.GetSupportMessages(chatID)
}

// SaveSupportMessage saves a new support text message.
func (s *ChatService) SaveSupportMessage(chatID, senderID uuid.UUID, role, text string) (*repository.Message, error) {
	if err := s.authorizeSupportChat(chatID, senderID, role); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("text is required")
	}
	if len([]rune(text)) > maxMessageRunes {
		return nil, errors.New("сообщение слишком длинное")
	}
	msg, err := s.chatRepo.SaveSupportMessage(chatID, senderID, text)
	if err != nil {
		return nil, err
	}
	metrics.ChatMessage("support")
	return msg, nil
}

// SaveSupportMessageWithAttachment saves a new support message with file attachment.
func (s *ChatService) SaveSupportMessageWithAttachment(chatID, senderID uuid.UUID, role, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	if err := s.authorizeSupportChat(chatID, senderID, role); err != nil {
		return nil, err
	}
	return s.chatRepo.SaveSupportMessageWithAttachment(chatID, senderID, text, fileURL, fileName, fileType, fileSize)
}

// CanAccessAttachment reports whether the user may download a stored file.
func (s *ChatService) CanAccessAttachment(userID uuid.UUID, role, fileURL string) (bool, error) {
	if role == "ADMIN" {
		return true, nil
	}
	return s.chatRepo.CanAccessAttachment(userID, fileURL)
}

// GetAdminSupportChatList returns all user support chats for Telegram-style admin UI.
func (s *ChatService) GetAdminSupportChatList() ([]*repository.SupportChatListItem, error) {
	return s.chatRepo.GetAdminSupportChatList()
}

// MarkSupportMessagesAsRead marks unread messages in a support chat as read.
func (s *ChatService) MarkSupportMessagesAsRead(chatID, readerID uuid.UUID, role string) error {
	if err := s.authorizeSupportChat(chatID, readerID, role); err != nil {
		return err
	}
	return s.chatRepo.MarkSupportMessagesAsRead(chatID, readerID)
}

// BanSupportChat bans a support chat for specified duration ("10m", "1h", "forever").
func (s *ChatService) BanSupportChat(chatID uuid.UUID, duration string) error {
	return s.chatRepo.BanSupportChat(chatID, duration)
}

// UnbanSupportChat unbans a support chat.
func (s *ChatService) UnbanSupportChat(chatID uuid.UUID) error {
	return s.chatRepo.UnbanSupportChat(chatID)
}

// IsSupportChatBanned checks if support chat is banned.
func (s *ChatService) IsSupportChatBanned(chatID uuid.UUID) (bool, *time.Time, error) {
	return s.chatRepo.IsSupportChatBanned(chatID)
}

// GetAdminSupportUnreadCount returns total unread messages count for admin.
func (s *ChatService) GetAdminSupportUnreadCount() (int, error) {
	return s.chatRepo.GetAdminSupportUnreadCount()
}
