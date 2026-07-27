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

		// Parse input message
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
		return nil, errors.New("chat room not found for this order")
	}

	return s.chatRepo.GetMessages(chat.ID)
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
		http.Error(w, "chat room does not exist yet", http.StatusNotFound)
		return
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
	go s.ReadPump(client, room)
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
