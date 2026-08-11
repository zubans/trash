package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Chat represents a chat room session between Customer and Executor for an order.
type Chat struct {
	ID       uuid.UUID `json:"id"`
	OrderID  uuid.UUID `json:"order_id"`
	IsActive bool      `json:"is_active"`
}

// Message represents an individual text message in a chat room.
type Message struct {
	ID        uuid.UUID  `json:"id"`
	ChatID    uuid.UUID  `json:"chat_id"`
	SenderID  uuid.UUID  `json:"sender_id"`
	Text      string     `json:"text"`
	Status    string     `json:"status"` // "sent", "delivered", "read"
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
}

// ChatRepository defines database operations for chats and messages.
type ChatRepository interface {
	GetChatByOrderID(orderID uuid.UUID) (*Chat, error)
	CreateChat(orderID uuid.UUID) (*Chat, error)
	SaveMessage(chatID, senderID uuid.UUID, text string) (*Message, error)
	GetMessages(chatID uuid.UUID) ([]*Message, error)
	DeactivateChat(chatID uuid.UUID) error
	MarkMessagesAsDelivered(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	MarkMessagesAsRead(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	GetUnreadOrderIDs(userID uuid.UUID) ([]uuid.UUID, error)
}

type chatRepo struct {
	db *sql.DB
}

// NewChatRepository creates a new ChatRepository.
func NewChatRepository(db *sql.DB) ChatRepository {
	return &chatRepo{db: db}
}

func (r *chatRepo) GetChatByOrderID(orderID uuid.UUID) (*Chat, error) {
	var c Chat
	err := r.db.QueryRow(`SELECT id, order_id, is_active FROM chats WHERE order_id = $1`, orderID).Scan(&c.ID, &c.OrderID, &c.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *chatRepo) CreateChat(orderID uuid.UUID) (*Chat, error) {
	var c Chat
	err := r.db.QueryRow(`
		INSERT INTO chats (order_id, is_active)
		VALUES ($1, TRUE)
		ON CONFLICT (order_id) DO UPDATE SET is_active = TRUE
		RETURNING id, order_id, is_active`, orderID).Scan(&c.ID, &c.OrderID, &c.IsActive)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *chatRepo) SaveMessage(chatID, senderID uuid.UUID, text string) (*Message, error) {
	var m Message
	err := r.db.QueryRow(`
		INSERT INTO messages (chat_id, sender_id, text, status, created_at)
		VALUES ($1, $2, $3, 'sent', now())
		RETURNING id, chat_id, sender_id, text, status, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) GetMessages(chatID uuid.UUID) ([]*Message, error) {
	query := `
		SELECT id, chat_id, sender_id, text, COALESCE(status, 'sent'), created_at, read_at
		FROM messages
		WHERE chat_id = $1
		ORDER BY created_at ASC`
	rows, err := r.db.Query(query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]*Message, 0)
	for rows.Next() {
		var m Message
		err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.CreatedAt, &m.ReadAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &m)
	}
	return messages, nil
}

func (r *chatRepo) DeactivateChat(chatID uuid.UUID) error {
	_, err := r.db.Exec(`UPDATE chats SET is_active = FALSE WHERE id = $1`, chatID)
	return err
}

func (r *chatRepo) MarkMessagesAsDelivered(chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		UPDATE messages
		SET status = 'delivered'
		WHERE chat_id = $1 AND sender_id != $2 AND status = 'sent'
		RETURNING id`
	rows, err := r.db.Query(query, chatID, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	updatedIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			updatedIDs = append(updatedIDs, id)
		}
	}
	return updatedIDs, nil
}

func (r *chatRepo) MarkMessagesAsRead(chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		UPDATE messages
		SET status = 'read', read_at = now()
		WHERE chat_id = $1 AND sender_id != $2 AND status IN ('sent', 'delivered')
		RETURNING id`
	rows, err := r.db.Query(query, chatID, recipientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	updatedIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			updatedIDs = append(updatedIDs, id)
		}
	}
	return updatedIDs, nil
}

func (r *chatRepo) GetUnreadOrderIDs(userID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT DISTINCT c.order_id
		FROM messages m
		JOIN chats c ON c.id = m.chat_id
		JOIN orders o ON o.id = c.order_id
		WHERE m.sender_id != $1
		  AND m.status != 'read'
		  AND (o.customer_id = $1 OR o.executor_id = $1)`
	rows, err := r.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orderIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			orderIDs = append(orderIDs, id)
		}
	}
	return orderIDs, nil
}
