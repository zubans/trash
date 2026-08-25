package repository

import (
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Chat represents a chat room session between Customer and Executor for an order.
type Chat struct {
	ID       uuid.UUID `json:"id"`
	OrderID  uuid.UUID `json:"order_id"`
	IsActive bool      `json:"is_active"`
}

// Message represents an individual text or file message in a chat room.
type Message struct {
	ID        uuid.UUID  `json:"id"`
	ChatID    uuid.UUID  `json:"chat_id"`
	SenderID  uuid.UUID  `json:"sender_id"`
	Text      string     `json:"text"`
	Status    string     `json:"status"` // "sent", "delivered", "read"
	FileURL   *string    `json:"file_url,omitempty"`
	FileName  *string    `json:"file_name,omitempty"`
	FileType  *string    `json:"file_type,omitempty"` // "image", "document"
	FileSize  *int64     `json:"file_size,omitempty"`
	IsDeleted bool       `json:"is_deleted"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// SupportChat represents a support conversation between a user and admins.
type SupportChat struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	IsBanned    bool       `json:"is_banned"`
	BannedUntil *time.Time `json:"banned_until,omitempty"`
	UnreadCount int        `json:"unread_count"`
	LastMessage *string    `json:"last_message,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// SupportChatListItem represents a user chat item in the admin Telegram-style list.
type SupportChatListItem struct {
	ChatID      uuid.UUID  `json:"chat_id"`
	UserID      uuid.UUID  `json:"user_id"`
	Phone       string     `json:"phone"`
	FirstName   string     `json:"first_name"`
	LastName    string     `json:"last_name"`
	Patronymic  string     `json:"patronymic"`
	FullName    string     `json:"full_name"`
	Role        string     `json:"role"`
	UnreadCount int        `json:"unread_count"`
	IsBanned    bool       `json:"is_banned"`
	BannedUntil *time.Time `json:"banned_until,omitempty"`
	LastMessage *string    `json:"last_message,omitempty"`
	LastTime    *time.Time `json:"last_time,omitempty"`
}

// ChatRepository defines database operations for chats and messages.
type ChatRepository interface {
	GetChatByOrderID(orderID uuid.UUID) (*Chat, error)
	CreateChat(orderID uuid.UUID) (*Chat, error)
	SaveMessage(chatID, senderID uuid.UUID, text string) (*Message, error)
	SaveMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error)
	GetMessages(chatID uuid.UUID) ([]*Message, error)
	DeactivateChat(chatID uuid.UUID) error
	MarkMessagesAsDelivered(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	MarkMessagesAsRead(chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	GetUnreadOrderIDs(userID uuid.UUID) ([]uuid.UUID, error)
	DeleteMessage(messageID, senderID uuid.UUID) error
	UpdateMessage(messageID, senderID uuid.UUID, newText string) (*Message, error)

	GetOrCreateSupportChat(userID uuid.UUID) (*SupportChat, error)
	// SupportChatOwner returns the user a support chat belongs to, so callers
	// can verify ownership before reading or writing messages.
	SupportChatOwner(chatID uuid.UUID) (uuid.UUID, error)
	// CanAccessAttachment reports whether a user is a participant of the chat
	// that a stored file belongs to.
	CanAccessAttachment(userID uuid.UUID, fileURL string) (bool, error)
	GetSupportMessages(chatID uuid.UUID) ([]*Message, error)
	SaveSupportMessage(chatID, senderID uuid.UUID, text string) (*Message, error)
	SaveSupportMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error)
	GetAdminSupportChatList() ([]*SupportChatListItem, error)
	MarkSupportMessagesAsRead(chatID, readerID uuid.UUID) error
	BanSupportChat(chatID uuid.UUID, duration string) error
	UnbanSupportChat(chatID uuid.UUID) error
	IsSupportChatBanned(chatID uuid.UUID) (bool, *time.Time, error)
	GetAdminSupportUnreadCount() (int, error)
}

type chatRepo struct {
	db *sql.DB
}

// NewChatRepository creates a new ChatRepository. Schema changes belong in
// migrations, not here.
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
		RETURNING id, chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) SaveMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error) {
	var m Message
	err := r.db.QueryRow(`
		INSERT INTO messages (chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at)
		VALUES ($1, $2, $3, 'sent', $4, $5, $6, $7, now())
		RETURNING id, chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text, fileURL, fileName, fileType, fileSize).Scan(
		&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) GetMessages(chatID uuid.UUID) ([]*Message, error) {
	query := `
		SELECT id, chat_id, sender_id, text, COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, COALESCE(is_deleted, false), created_at, read_at, updated_at
		FROM messages
		WHERE chat_id = $1 AND COALESCE(is_deleted, false) = false
		ORDER BY created_at ASC`
	rows, err := r.db.Query(query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]*Message, 0)
	for rows.Next() {
		var m Message
		err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.IsDeleted, &m.CreatedAt, &m.ReadAt, &m.UpdatedAt)
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

func (r *chatRepo) DeleteMessage(messageID, senderID uuid.UUID) error {
	res, err := r.db.Exec(`UPDATE messages SET is_deleted = TRUE WHERE id = $1 AND sender_id = $2`, messageID, senderID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *chatRepo) UpdateMessage(messageID, senderID uuid.UUID, newText string) (*Message, error) {
	var m Message
	err := r.db.QueryRow(`
		UPDATE messages
		SET text = $3, updated_at = now()
		WHERE id = $1 AND sender_id = $2 AND COALESCE(is_deleted, false) = false
		RETURNING id, chat_id, sender_id, text, COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, COALESCE(is_deleted, false), created_at, read_at, updated_at`,
		messageID, senderID, newText).Scan(
		&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.IsDeleted, &m.CreatedAt, &m.ReadAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) GetOrCreateSupportChat(userID uuid.UUID) (*SupportChat, error) {
	var sc SupportChat
	var bannedUntil sql.NullTime
	var lastMsg sql.NullString
	err := r.db.QueryRow(`
		WITH sc_row AS (
			INSERT INTO support_chats (user_id, updated_at)
			VALUES ($1, now())
			ON CONFLICT (user_id) DO UPDATE SET updated_at = support_chats.updated_at
			RETURNING id, user_id, COALESCE(is_banned, false) as is_banned, banned_until, created_at, updated_at
		)
		SELECT 
			id, user_id, is_banned, banned_until, created_at, updated_at,
			(SELECT COUNT(*) FROM support_messages sm WHERE sm.chat_id = sc_row.id AND sm.sender_id != sc_row.user_id AND sm.read_at IS NULL) as unread_count,
			(SELECT COALESCE(NULLIF(text, ''), file_name, 'Вложение') FROM support_messages WHERE chat_id = sc_row.id ORDER BY created_at DESC LIMIT 1) as last_message
		FROM sc_row`, userID).Scan(
		&sc.ID, &sc.UserID, &sc.IsBanned, &bannedUntil, &sc.CreatedAt, &sc.UpdatedAt,
		&sc.UnreadCount, &lastMsg,
	)
	if err != nil {
		return nil, err
	}
	if bannedUntil.Valid {
		if time.Now().After(bannedUntil.Time) {
			sc.IsBanned = false
			_, _ = r.db.Exec(`UPDATE support_chats SET is_banned = false, banned_until = NULL WHERE id = $1`, sc.ID)
		} else {
			sc.BannedUntil = &bannedUntil.Time
		}
	}
	if lastMsg.Valid {
		sc.LastMessage = &lastMsg.String
	}
	return &sc, nil
}

func (r *chatRepo) SupportChatOwner(chatID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := r.db.QueryRow(`SELECT user_id FROM support_chats WHERE id = $1`, chatID).Scan(&userID)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// CanAccessAttachment checks both attachment sources: order chats (customer and
// assigned executor) and support chats (the owning user). Admin access is
// handled by the caller.
func (r *chatRepo) CanAccessAttachment(userID uuid.UUID, fileURL string) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM messages m
			JOIN chats c ON c.id = m.chat_id
			JOIN orders o ON o.id = c.order_id
			WHERE m.file_url = $2
			  AND (o.customer_id = $1 OR o.executor_id = $1)
		) OR EXISTS(
			SELECT 1
			FROM support_messages sm
			JOIN support_chats sc ON sc.id = sm.chat_id
			WHERE sm.file_url = $2 AND sc.user_id = $1
		)`, userID, fileURL).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (r *chatRepo) GetSupportMessages(chatID uuid.UUID) ([]*Message, error) {
	query := `
		SELECT id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, COALESCE(is_deleted, false), created_at, read_at, updated_at
		FROM support_messages
		WHERE chat_id = $1 AND COALESCE(is_deleted, false) = false
		ORDER BY created_at ASC`
	rows, err := r.db.Query(query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]*Message, 0)
	for rows.Next() {
		var m Message
		err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.IsDeleted, &m.CreatedAt, &m.ReadAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &m)
	}
	return messages, nil
}

func (r *chatRepo) SaveSupportMessage(chatID, senderID uuid.UUID, text string) (*Message, error) {
	var m Message
	err := r.db.QueryRow(`
		INSERT INTO support_messages (chat_id, sender_id, text, status, created_at)
		VALUES ($1, $2, $3, 'sent', now())
		RETURNING id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = r.db.Exec(`UPDATE support_chats SET updated_at = now() WHERE id = $1`, chatID)
	return &m, nil
}

func (r *chatRepo) SaveSupportMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error) {
	var m Message
	err := r.db.QueryRow(`
		INSERT INTO support_messages (chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at)
		VALUES ($1, $2, $3, 'sent', $4, $5, $6, $7, now())
		RETURNING id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text, fileURL, fileName, fileType, fileSize).Scan(
		&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = r.db.Exec(`UPDATE support_chats SET updated_at = now() WHERE id = $1`, chatID)
	return &m, nil
}

func (r *chatRepo) GetAdminSupportChatList() ([]*SupportChatListItem, error) {
	query := `
		SELECT 
			sc.id,
			sc.user_id,
			u.phone,
			COALESCE(u.first_name, ''),
			COALESCE(u.last_name, ''),
			COALESCE(u.patronymic, ''),
			u.role,
			COUNT(sm.id) FILTER (WHERE sm.sender_id = u.id AND sm.read_at IS NULL) as unread_count,
			COALESCE(sc.is_banned, false) as is_banned,
			sc.banned_until,
			(SELECT COALESCE(NULLIF(text, ''), file_name, 'Вложение') FROM support_messages WHERE chat_id = sc.id ORDER BY created_at DESC LIMIT 1) as last_message,
			(SELECT created_at FROM support_messages WHERE chat_id = sc.id ORDER BY created_at DESC LIMIT 1) as last_time
		FROM support_chats sc
		JOIN users u ON u.id = sc.user_id
		LEFT JOIN support_messages sm ON sm.chat_id = sc.id AND sm.sender_id = u.id AND sm.read_at IS NULL
		GROUP BY sc.id, sc.user_id, u.phone, u.first_name, u.last_name, u.patronymic, u.role, sc.is_banned, sc.banned_until
		ORDER BY COALESCE((SELECT created_at FROM support_messages WHERE chat_id = sc.id ORDER BY created_at DESC LIMIT 1), sc.created_at) DESC
	`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*SupportChatListItem, 0)
	for rows.Next() {
		var item SupportChatListItem
		var lastMsg sql.NullString
		var lastTime sql.NullTime
		var bannedUntil sql.NullTime
		err := rows.Scan(
			&item.ChatID, &item.UserID, &item.Phone,
			&item.FirstName, &item.LastName, &item.Patronymic,
			&item.Role, &item.UnreadCount, &item.IsBanned, &bannedUntil, &lastMsg, &lastTime,
		)
		if err != nil {
			return nil, err
		}
		if bannedUntil.Valid {
			if time.Now().After(bannedUntil.Time) {
				item.IsBanned = false
			} else {
				item.BannedUntil = &bannedUntil.Time
			}
		}
		fullName := strings.TrimSpace(item.LastName + " " + item.FirstName + " " + item.Patronymic)
		if fullName == "" {
			fullName = item.Phone
		}
		item.FullName = fullName
		if lastMsg.Valid {
			item.LastMessage = &lastMsg.String
		}
		if lastTime.Valid {
			item.LastTime = &lastTime.Time
		}
		items = append(items, &item)
	}
	return items, nil
}

func (r *chatRepo) MarkSupportMessagesAsRead(chatID, readerID uuid.UUID) error {
	_, err := r.db.Exec(`UPDATE support_messages SET read_at = now(), status = 'read' WHERE chat_id = $1 AND sender_id != $2 AND read_at IS NULL`, chatID, readerID)
	return err
}

func (r *chatRepo) BanSupportChat(chatID uuid.UUID, duration string) error {
	var until time.Time
	now := time.Now()
	switch duration {
	case "10m":
		until = now.Add(10 * time.Minute)
	case "1h":
		until = now.Add(1 * time.Hour)
	case "forever":
		until = now.AddDate(100, 0, 0)
	default:
		until = now.Add(10 * time.Minute)
	}
	_, err := r.db.Exec(`UPDATE support_chats SET is_banned = true, banned_until = $2, updated_at = now() WHERE id = $1`, chatID, until)
	return err
}

func (r *chatRepo) UnbanSupportChat(chatID uuid.UUID) error {
	_, err := r.db.Exec(`UPDATE support_chats SET is_banned = false, banned_until = NULL, updated_at = now() WHERE id = $1`, chatID)
	return err
}

func (r *chatRepo) IsSupportChatBanned(chatID uuid.UUID) (bool, *time.Time, error) {
	var isBanned bool
	var bannedUntil sql.NullTime
	err := r.db.QueryRow(`SELECT COALESCE(is_banned, false), banned_until FROM support_chats WHERE id = $1`, chatID).Scan(&isBanned, &bannedUntil)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil, nil
		}
		return false, nil, err
	}
	if isBanned && bannedUntil.Valid {
		if time.Now().After(bannedUntil.Time) {
			_, _ = r.db.Exec(`UPDATE support_chats SET is_banned = false, banned_until = NULL WHERE id = $1`, chatID)
			return false, nil, nil
		}
		return true, &bannedUntil.Time, nil
	}
	return isBanned, nil, nil
}

func (r *chatRepo) GetAdminSupportUnreadCount() (int, error) {
	var total int
	err := r.db.QueryRow(`
		SELECT COUNT(sm.id)
		FROM support_messages sm
		JOIN support_chats sc ON sc.id = sm.chat_id
		WHERE sm.sender_id = sc.user_id AND sm.read_at IS NULL
	`).Scan(&total)
	return total, err
}
