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
	ID        uuid.UUID `json:"id"`
	ChatID    uuid.UUID `json:"chat_id"`
	SenderID  uuid.UUID `json:"sender_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// ChatRepository defines database operations for chats and messages.
type ChatRepository interface {
	GetChatByOrderID(orderID uuid.UUID) (*Chat, error)
	CreateChat(orderID uuid.UUID) (*Chat, error)
	SaveMessage(chatID, senderID uuid.UUID, text string) (*Message, error)
	GetMessages(chatID uuid.UUID) ([]*Message, error)
	DeactivateChat(chatID uuid.UUID) error
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
		INSERT INTO messages (chat_id, sender_id, text, created_at)
		VALUES ($1, $2, $3, now())
		RETURNING id, chat_id, sender_id, text, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) GetMessages(chatID uuid.UUID) ([]*Message, error) {
	query := `
		SELECT id, chat_id, sender_id, text, created_at
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
		err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.CreatedAt)
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
