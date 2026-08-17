package service

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"healthlogin/backend/repository"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for simplicity in local docker/dev env
		return true
	},
}

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
			Type    string   `json:"type"`
			Text    string   `json:"text"`
			MsgIDs  []string `json:"message_ids"`
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
		return nil, errors.New("forbidden: you do not belong to this order")
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
		return nil, errors.New("forbidden: you are not a participant in this order")
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, errors.New("chat is locked (order completed or canceled)")
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, errors.New("chat is locked (read-only)")
	}

	savedMsg, err := s.chatRepo.SaveMessage(chat.ID, userID, text)
	if err != nil {
		return nil, err
	}

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
		return nil, errors.New("forbidden: you are not a participant in this order")
	}

	if order.Status == "COMPLETED" || order.Status == "CANCELED" {
		return nil, errors.New("chat is locked (order completed or canceled)")
	}

	chat, err := s.chatRepo.GetChatByOrderID(orderID)
	if err != nil {
		return nil, err
	}
	if chat == nil || !chat.IsActive {
		return nil, errors.New("chat is locked (read-only)")
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

	go s.ReadPump(client, room)
}

// EditMessage updates message text if owned by sender and broadcasts message_edited event.
func (s *ChatService) EditMessage(messageID, senderID, orderID uuid.UUID, newText string) (*repository.Message, error) {
	msg, err := s.chatRepo.UpdateMessage(messageID, senderID, newText)
	if err != nil {
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

// BroadcastSystemMessage sends a custom message payload to all active connections of an order.
func (s *ChatService) BroadcastSystemMessage(orderID uuid.UUID, msg interface{}) {
	s.mu.RLock()
	room, exists := s.rooms[orderID]
	s.mu.RUnlock()
	if exists {
		bytes, err := json.Marshal(msg)
		if err == nil {
			room.Broadcast <- bytes
		}
	}
}
