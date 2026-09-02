package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Chat представляет сессию чата между заказчиком и исполнителем по заказу.
type Chat struct {
	ID       uuid.UUID `json:"id"`
	OrderID  uuid.UUID `json:"order_id"`
	IsActive bool      `json:"is_active"`
}

// Message представляет отдельное текстовое или файловое сообщение в чат-комнате.
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

// SupportChat представляет переписку пользователя с админами в поддержке.
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

// SupportChatListItem представляет элемент чата пользователя в админском списке в стиле Telegram.
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

// ChatRepository описывает операции с базой для чатов и сообщений.
type ChatRepository interface {
	GetChatByOrderID(ctx context.Context, orderID uuid.UUID) (*Chat, error)
	CreateChat(ctx context.Context, orderID uuid.UUID) (*Chat, error)
	SaveMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*Message, error)
	SaveMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error)
	// GetMessages возвращает ограниченное окно истории чата заказа, всегда от
	// старых к новым. Как выбирается окно, см. в MessageQuery.
	GetMessages(ctx context.Context, chatID uuid.UUID, q MessageQuery) ([]*Message, error)
	DeactivateChat(ctx context.Context, chatID uuid.UUID) error
	MarkMessagesAsDelivered(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	MarkMessagesAsRead(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error)
	GetUnreadOrderIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	DeleteMessage(ctx context.Context, messageID, senderID uuid.UUID) error
	UpdateMessage(ctx context.Context, messageID, senderID uuid.UUID, newText string) (*Message, error)

	GetOrCreateSupportChat(ctx context.Context, userID uuid.UUID) (*SupportChat, error)
	// SupportChatOwner возвращает пользователя, которому принадлежит чат
	// поддержки, чтобы вызывающие могли проверить владение до чтения или записи.
	SupportChatOwner(ctx context.Context, chatID uuid.UUID) (uuid.UUID, error)
	// CanAccessAttachment сообщает, является ли пользователь участником чата,
	// которому принадлежит сохранённый файл.
	CanAccessAttachment(ctx context.Context, userID uuid.UUID, fileURL string) (bool, error)
	// GetSupportMessages — это GetMessages для переписки с поддержкой.
	GetSupportMessages(ctx context.Context, chatID uuid.UUID, q MessageQuery) ([]*Message, error)
	SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*Message, error)
	SaveSupportMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error)
	// GetAdminSupportChatList возвращает переписки поддержки для админского
	// ящика, сначала недавно активные, не более limit (см.
	// DefaultHistoryPageSize). Админский интерфейс опрашивает это, и каждая строка
	// стоит двух коррелированных подзапросов, поэтому расти без границ ей нельзя.
	GetAdminSupportChatList(ctx context.Context, limit int) ([]*SupportChatListItem, error)
	MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID) error
	BanSupportChat(ctx context.Context, chatID uuid.UUID, duration string) error
	UnbanSupportChat(ctx context.Context, chatID uuid.UUID) error
	IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error)
	GetAdminSupportUnreadCount(ctx context.Context) (int, error)
}

type chatRepo struct {
	db *sql.DB
}

// NewChatRepository создаёт новый ChatRepository. Изменения схемы — дело
// миграций, а не этого места.
func NewChatRepository(db *sql.DB) ChatRepository {
	return &chatRepo{db: db}
}

func (r *chatRepo) GetChatByOrderID(ctx context.Context, orderID uuid.UUID) (*Chat, error) {
	var c Chat
	err := r.db.QueryRowContext(ctx, `SELECT id, order_id, is_active FROM chats WHERE order_id = $1`, orderID).Scan(&c.ID, &c.OrderID, &c.IsActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *chatRepo) CreateChat(ctx context.Context, orderID uuid.UUID) (*Chat, error) {
	var c Chat
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO chats (order_id, is_active)
		VALUES ($1, TRUE)
		ON CONFLICT (order_id) DO UPDATE SET is_active = TRUE
		RETURNING id, order_id, is_active`, orderID).Scan(&c.ID, &c.OrderID, &c.IsActive)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *chatRepo) SaveMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*Message, error) {
	var m Message
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO messages (chat_id, sender_id, text, status, created_at)
		VALUES ($1, $2, $3, 'sent', now())
		RETURNING id, chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *chatRepo) SaveMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error) {
	var m Message
	err := r.db.QueryRowContext(ctx, `
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

// MessageQuery ограничивает чтение истории чата.
//
// Он существует потому, что раньше история возвращалась целиком, на эндпоинте,
// который клиенты опрашивают: долгая переписка заново пересылала каждое
// когда-либо хранившееся сообщение, по нескольку раз в минуту. Три формы ниже —
// это три вещи, которые на деле нужны клиенту чата.
type MessageQuery struct {
	// Limit ограничивает, сколько сообщений вернётся. Ноль означает
	// DefaultMessagePageSize, а всё сверх MaxMessagePageSize ужимается до него:
	// защита здесь — размер страницы, поэтому клиент не может от неё отказаться.
	Limit int
	// Before запрашивает самые новые сообщения старше этого момента: листание
	// вверх по истории, пока пользователь прокручивает назад.
	Before *time.Time
	// After запрашивает самые старые сообщения новее этого момента: то, что нужно
	// опросу, — только то, чего он ещё не видел.
	After *time.Time
}

const (
	// DefaultMessagePageSize — то, что получает клиент, не запросивший окна:
	// самые свежие сообщения, то есть то, что показывает только что открытый чат.
	DefaultMessagePageSize = 100
	// MaxMessagePageSize ограничивает то, что может вытянуть один запрос.
	MaxMessagePageSize = 500
)

func (q MessageQuery) limit() int {
	switch {
	case q.Limit <= 0:
		return DefaultMessagePageSize
	case q.Limit > MaxMessagePageSize:
		return MaxMessagePageSize
	default:
		return q.Limit
	}
}

// queryMessages читает одно окно переписки. У чатов заказов и чатов поддержки
// одинаковые таблицы сообщений и одинаковые правила листания, поэтому они делят
// это; table — внутренняя константа, никогда не то, что передал вызывающий.
//
// Результат всегда от старых к новым, в какую бы сторону ни бралось окно:
// клиенты рисуют по возрастанию и не должны об этом задумываться.
func (r *chatRepo) queryMessages(ctx context.Context, table string, chatID uuid.UUID, q MessageQuery) ([]*Message, error) {
	const columns = `id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, COALESCE(is_deleted, false), created_at, read_at, updated_at`

	where := "chat_id = $1 AND COALESCE(is_deleted, false) = false"
	args := []interface{}{chatID}

	// По возрастанию — только при листании вперёд от известной точки: это
	// единственный случай, когда нужны первые строки в порядке создания. Всем
	// прочим нужны самые новые, поэтому читается по убыванию и ниже переворачивается.
	ascending := false
	switch {
	case q.After != nil:
		args = append(args, *q.After)
		where += fmt.Sprintf(" AND created_at > $%d", len(args))
		ascending = true
	case q.Before != nil:
		args = append(args, *q.Before)
		where += fmt.Sprintf(" AND created_at < $%d", len(args))
	}

	direction := "DESC"
	if ascending {
		direction = "ASC"
	}
	args = append(args, q.limit())
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY created_at %s LIMIT $%d",
		columns, table, where, direction, len(args),
	)

	rows, err := r.db.QueryContext(ctx, query, args...)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if !ascending {
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	}
	return messages, nil
}

func (r *chatRepo) GetMessages(ctx context.Context, chatID uuid.UUID, q MessageQuery) ([]*Message, error) {
	return r.queryMessages(ctx, "messages", chatID, q)
}

func (r *chatRepo) DeactivateChat(ctx context.Context, chatID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE chats SET is_active = FALSE WHERE id = $1`, chatID)
	return err
}

func (r *chatRepo) MarkMessagesAsDelivered(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		UPDATE messages
		SET status = 'delivered'
		WHERE chat_id = $1 AND sender_id != $2 AND status = 'sent'
		RETURNING id`
	rows, err := r.db.QueryContext(ctx, query, chatID, recipientID)
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

func (r *chatRepo) MarkMessagesAsRead(ctx context.Context, chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		UPDATE messages
		SET status = 'read', read_at = now()
		WHERE chat_id = $1 AND sender_id != $2 AND status IN ('sent', 'delivered')
		RETURNING id`
	rows, err := r.db.QueryContext(ctx, query, chatID, recipientID)
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

func (r *chatRepo) GetUnreadOrderIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT DISTINCT c.order_id
		FROM messages m
		JOIN chats c ON c.id = m.chat_id
		JOIN orders o ON o.id = c.order_id
		WHERE m.sender_id != $1
		  AND m.status != 'read'
		  AND (o.customer_id = $1 OR o.executor_id = $1)`
	rows, err := r.db.QueryContext(ctx, query, userID)
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

func (r *chatRepo) DeleteMessage(ctx context.Context, messageID, senderID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `UPDATE messages SET is_deleted = TRUE WHERE id = $1 AND sender_id = $2`, messageID, senderID)
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

func (r *chatRepo) UpdateMessage(ctx context.Context, messageID, senderID uuid.UUID, newText string) (*Message, error) {
	var m Message
	err := r.db.QueryRowContext(ctx, `
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

func (r *chatRepo) GetOrCreateSupportChat(ctx context.Context, userID uuid.UUID) (*SupportChat, error) {
	var sc SupportChat
	var bannedUntil sql.NullTime
	var lastMsg sql.NullString
	err := r.db.QueryRowContext(ctx, `
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
			_, _ = r.db.ExecContext(ctx, `UPDATE support_chats SET is_banned = false, banned_until = NULL WHERE id = $1`, sc.ID)
		} else {
			sc.BannedUntil = &bannedUntil.Time
		}
	}
	if lastMsg.Valid {
		sc.LastMessage = &lastMsg.String
	}
	return &sc, nil
}

func (r *chatRepo) SupportChatOwner(ctx context.Context, chatID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT user_id FROM support_chats WHERE id = $1`, chatID).Scan(&userID)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// CanAccessAttachment проверяет оба источника вложений: чаты заказов (заказчик
// и назначенный исполнитель) и чаты поддержки (пользователь-владелец). Доступ
// админа обрабатывает вызывающий.
func (r *chatRepo) CanAccessAttachment(ctx context.Context, userID uuid.UUID, fileURL string) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `
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

func (r *chatRepo) GetSupportMessages(ctx context.Context, chatID uuid.UUID, q MessageQuery) ([]*Message, error) {
	return r.queryMessages(ctx, "support_messages", chatID, q)
}

func (r *chatRepo) SaveSupportMessage(ctx context.Context, chatID, senderID uuid.UUID, text string) (*Message, error) {
	var m Message
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO support_messages (chat_id, sender_id, text, status, created_at)
		VALUES ($1, $2, $3, 'sent', now())
		RETURNING id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text).Scan(&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE support_chats SET updated_at = now() WHERE id = $1`, chatID)
	return &m, nil
}

func (r *chatRepo) SaveSupportMessageWithAttachment(ctx context.Context, chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*Message, error) {
	var m Message
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO support_messages (chat_id, sender_id, text, status, file_url, file_name, file_type, file_size, created_at)
		VALUES ($1, $2, $3, 'sent', $4, $5, $6, $7, now())
		RETURNING id, chat_id, sender_id, COALESCE(text, ''), COALESCE(status, 'sent'), file_url, file_name, file_type, file_size, created_at`,
		chatID, senderID, text, fileURL, fileName, fileType, fileSize).Scan(
		&m.ID, &m.ChatID, &m.SenderID, &m.Text, &m.Status, &m.FileURL, &m.FileName, &m.FileType, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE support_chats SET updated_at = now() WHERE id = $1`, chatID)
	return &m, nil
}

func (r *chatRepo) GetAdminSupportChatList(ctx context.Context, limit int) ([]*SupportChatListItem, error) {
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
		LIMIT $1
	`
	rows, err := r.db.QueryContext(ctx, query, historyLimit(limit))
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

func (r *chatRepo) MarkSupportMessagesAsRead(ctx context.Context, chatID, readerID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE support_messages SET read_at = now(), status = 'read' WHERE chat_id = $1 AND sender_id != $2 AND read_at IS NULL`, chatID, readerID)
	return err
}

func (r *chatRepo) BanSupportChat(ctx context.Context, chatID uuid.UUID, duration string) error {
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
	_, err := r.db.ExecContext(ctx, `UPDATE support_chats SET is_banned = true, banned_until = $2, updated_at = now() WHERE id = $1`, chatID, until)
	return err
}

func (r *chatRepo) UnbanSupportChat(ctx context.Context, chatID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE support_chats SET is_banned = false, banned_until = NULL, updated_at = now() WHERE id = $1`, chatID)
	return err
}

func (r *chatRepo) IsSupportChatBanned(ctx context.Context, chatID uuid.UUID) (bool, *time.Time, error) {
	var isBanned bool
	var bannedUntil sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(is_banned, false), banned_until FROM support_chats WHERE id = $1`, chatID).Scan(&isBanned, &bannedUntil)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil, nil
		}
		return false, nil, err
	}
	if isBanned && bannedUntil.Valid {
		if time.Now().After(bannedUntil.Time) {
			_, _ = r.db.ExecContext(ctx, `UPDATE support_chats SET is_banned = false, banned_until = NULL WHERE id = $1`, chatID)
			return false, nil, nil
		}
		return true, &bannedUntil.Time, nil
	}
	return isBanned, nil, nil
}

func (r *chatRepo) GetAdminSupportUnreadCount(ctx context.Context) (int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(sm.id)
		FROM support_messages sm
		JOIN support_chats sc ON sc.id = sm.chat_id
		WHERE sm.sender_id = sc.user_id AND sm.read_at IS NULL
	`).Scan(&total)
	return total, err
}
