package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Виды писем. От вида зависит иконка и то, какую карточку рисует приложение:
// письмо о подарке несёт купон, письмо об акции — картинку и срок.
const (
	MailKindAchievement = "ACHIEVEMENT"
	MailKindGift        = "GIFT"
	MailKindPromo       = "PROMO"
	MailKindNews        = "NEWS"
	MailKindSystem      = "SYSTEM"
	// DIRECT — адресное письмо администрации одному человеку и ответы на него.
	// Единственный вид, у которого бывает продолжение: остальные письма пишет
	// ядро, и отвечать в них некому.
	MailKindDirect = "DIRECT"
)

// Направление письма внутри ящика. Ящик принадлежит пользователю всегда: и
// письмо администрации ему (IN), и его собственный ответ (OUT) лежат в одной
// ленте, иначе отвечающий не видел бы, что он написал.
const (
	MailDirectionIn  = "IN"
	MailDirectionOut = "OUT"
)

// Mail — письмо во внутреннем ящике пользователя.
//
// Ящик существует потому, что чат и e-mail для этого не годятся. Чат привязан к
// заказу и двусторонен; здесь лента односторонняя, она переживает закрытие
// заказа и есть у того, у кого заказов нет вовсе. Письмо наружу может не дойти,
// а купон обязан лежать там же, где приложение.
type Mail struct {
	ID      uuid.UUID `json:"id"`
	UserID  uuid.UUID `json:"user_id"`
	Kind    string    `json:"kind"`
	Subject string    `json:"subject"`
	Body    string    `json:"body"`
	// RefType и RefID указывают, о чём письмо: код ачивки, id подарка.
	// Приложение по ним открывает нужный экран, а не разбирает текст.
	RefType   string     `json:"ref_type,omitempty"`
	RefID     string     `json:"ref_id,omitempty"`
	SenderID  *uuid.UUID `json:"sender_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`

	// Direction различает письмо администрации и ответ владельца ящика.
	Direction string `json:"direction"`
	// ThreadID — id первого письма переписки; у корня равен собственному id.
	// У писем ядра и рассылок пуст: ветки у них нет.
	ThreadID *uuid.UUID `json:"thread_id,omitempty"`
	// AdminReadAt — когда ответ прочитала администрация. Отдельно от ReadAt:
	// тот принадлежит владельцу ящика, и отметка админа не должна гасить его
	// значок.
	AdminReadAt *time.Time `json:"admin_read_at,omitempty"`
	// Имя отправителя показывается в переписке: подпись «администратор Иванов»
	// отличает ответ живого человека от письма, которое написало ядро.
	SenderName string `json:"sender_name,omitempty"`

	// Поля ниже считаются только для корня ветки в списке ящика: сама лента
	// показывает переписку одной строкой, а не рассыпает ответы по ящику.
	Replies      int       `json:"replies,omitempty"`
	ThreadUnread int       `json:"thread_unread,omitempty"`
	LastAt       time.Time `json:"last_at,omitempty"`
}

// MailDialog — одна переписка в списке администратора: с кем, о чём и сколько
// непрочитанного. Собирается запросом, а не выбором всех писем в память:
// список переписок открывают, чтобы увидеть, кому не ответили.
type MailDialog struct {
	UserID   uuid.UUID `json:"user_id"`
	Phone    string    `json:"phone"`
	FullName string    `json:"full_name"`
	Role     string    `json:"role"`
	// ThreadID последней ветки — по нему открывается переписка и в неё же
	// отвечают, если админ отвечает прямо из списка.
	ThreadID      uuid.UUID `json:"thread_id"`
	Subject       string    `json:"subject"`
	LastBody      string    `json:"last_body"`
	LastAt        time.Time `json:"last_at"`
	LastDirection string    `json:"last_direction"`
	// Unread — ответы пользователя, не прочитанные администрацией. Это счётчик
	// долга: столько людей ждут ответа.
	Unread int `json:"unread"`
	// UserUnread — письма, которые не открыл сам пользователь.
	UserUnread int `json:"user_unread"`
	Total      int `json:"total"`
}

// MailRepository хранит внутреннюю почту.
type MailRepository interface {
	// Send кладёт письмо в ящик. Принимает Querier: письмо о выдаче пишется в
	// той же транзакции, что и сама выдача, чтобы не появилось сообщение о
	// подарке, которого нет.
	Send(ctx context.Context, q Querier, mail *Mail) error
	// Broadcast рассылает одно письмо многим — новость или акцию. Возвращает
	// число получателей.
	Broadcast(ctx context.Context, mail *Mail, userIDs []uuid.UUID) (int, error)
	// RecipientsByRole перечисляет получателей рассылки по роли.
	RecipientsByRole(ctx context.Context, role string) ([]uuid.UUID, error)
	// ListForUser возвращает ящик, свежие письма первыми.
	ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*Mail, error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, error)
	MarkRead(ctx context.Context, id, userID uuid.UUID) error
	MarkAllRead(ctx context.Context, userID uuid.UUID) error
	Delete(ctx context.Context, id, userID uuid.UUID) error

	// Get возвращает письмо по id — вместе с ящиком, которому оно принадлежит.
	// Отвечающий обязан доказать право на ветку, а доказывает он его тем, что
	// ящик его.
	Get(ctx context.Context, id uuid.UUID) (*Mail, error)
	// Thread возвращает переписку целиком, старые письма первыми: ветку читают
	// сверху вниз, как разговор.
	Thread(ctx context.Context, threadID uuid.UUID) ([]*Mail, error)
	// Reply дописывает письмо в существующую ветку.
	Reply(ctx context.Context, mail *Mail) error
	// ListDialogs перечисляет переписки для администратора: по одной строке на
	// собеседника, свежие первыми.
	ListDialogs(ctx context.Context, onlyUnanswered bool, limit int) ([]*MailDialog, error)
	// ListDirectForUser отдаёт всю адресную переписку с одним пользователем.
	ListDirectForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*Mail, error)
	// MarkThreadReadByAdmin гасит счётчик непрочитанного у администрации.
	MarkThreadReadByAdmin(ctx context.Context, threadID uuid.UUID) error
	// AdminUnreadCount — сколько ответов ждёт разбора. Значок в меню админки.
	AdminUnreadCount(ctx context.Context) (int, error)
}

type mailRepo struct {
	db *sql.DB
}

// NewMailRepository создаёт MailRepository.
func NewMailRepository(db *sql.DB) MailRepository {
	return &mailRepo{db: db}
}

func (r *mailRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *mailRepo) Send(ctx context.Context, q Querier, mail *Mail) error {
	if mail.ID == uuid.Nil {
		mail.ID = uuid.New()
	}
	if mail.Kind == "" {
		mail.Kind = MailKindSystem
	}
	if mail.Direction == "" {
		mail.Direction = MailDirectionIn
	}
	if mail.Kind == MailKindDirect && mail.ThreadID == nil {
		// Адресное письмо всегда начинает ветку — даже если ответа так и не
		// будет. Иначе ответить на него было бы некуда.
		id := mail.ID
		mail.ThreadID = &id
	}
	return r.exec(q).QueryRowContext(ctx, `
        INSERT INTO user_mail (id, user_id, kind, subject, body, ref_type, ref_id, sender_id, direction, thread_id, read_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING created_at
    `, mail.ID, mail.UserID, mail.Kind, mail.Subject, mail.Body,
		mail.RefType, mail.RefID, mail.SenderID, mail.Direction, mail.ThreadID,
		mail.ReadAt).Scan(&mail.CreatedAt)
}

func (r *mailRepo) Broadcast(ctx context.Context, mail *Mail, userIDs []uuid.UUID) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	// Одним оператором на всех: рассылка на десять тысяч человек не должна быть
	// десятью тысячами обращений к базе.
	result, err := r.db.ExecContext(ctx, `
        INSERT INTO user_mail (user_id, kind, subject, body, ref_type, ref_id, sender_id)
        SELECT unnest($1::uuid[]), $2, $3, $4, $5, $6, $7
    `, pq.Array(userIDs), mail.Kind, mail.Subject, mail.Body, mail.RefType, mail.RefID, mail.SenderID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

func (r *mailRepo) RecipientsByRole(ctx context.Context, role string) ([]uuid.UUID, error) {
	query := `SELECT u.id FROM users u WHERE u.status::text <> 'BANNED'`
	args := []interface{}{}
	if role != "" {
		// Роль может лежать и в основной колонке, и в таблице мультиролей
		// user_roles — отдельной колонки со списком ролей у users нет. Исполнитель,
		// заведённый заказчиком и получивший роль позже, записан только во второй.
		query += ` AND (u.role = $1::text OR EXISTS (
            SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = $1::text))`
		args = append(args, role)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *mailRepo) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*Mail, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// Лента показывает корни веток, а не каждое письмо: переписка из десяти
	// реплик — одна строка в ящике, а не десять одинаковых тем подряд. Поэтому
	// у корня тут же считаются ответы, непрочитанное внутри ветки и время
	// последней реплики — по нему лента и сортируется, иначе ответ поднял бы
	// переписку не выше, чем она была вчера.
	rows, err := r.db.QueryContext(ctx, `
        SELECT m.id, m.user_id, m.kind, m.subject, m.body, m.ref_type, m.ref_id, m.sender_id,
               m.direction, m.thread_id, m.created_at, m.read_at, m.admin_read_at,
               COALESCE(t.replies, 0), COALESCE(t.unread, 0),
               COALESCE(t.last_at, m.created_at) AS last_at
        FROM user_mail m
        LEFT JOIN LATERAL (
            SELECT COUNT(*) FILTER (WHERE r.id <> m.id)                                AS replies,
                   COUNT(*) FILTER (WHERE r.read_at IS NULL AND r.direction = 'IN')     AS unread,
                   MAX(r.created_at)                                                   AS last_at
            FROM user_mail r
            WHERE r.thread_id = m.id AND r.deleted_at IS NULL
        ) t ON true
        WHERE m.user_id = $1 AND m.deleted_at IS NULL
          AND (m.thread_id IS NULL OR m.thread_id = m.id)
        ORDER BY last_at DESC
        LIMIT $2
    `, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Mail, 0)
	for rows.Next() {
		var m Mail
		if err := rows.Scan(&m.ID, &m.UserID, &m.Kind, &m.Subject, &m.Body,
			&m.RefType, &m.RefID, &m.SenderID, &m.Direction, &m.ThreadID,
			&m.CreatedAt, &m.ReadAt, &m.AdminReadAt,
			&m.Replies, &m.ThreadUnread, &m.LastAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (r *mailRepo) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_mail WHERE user_id = $1 AND read_at IS NULL AND deleted_at IS NULL`,
		userID).Scan(&count)
	return count, err
}

func (r *mailRepo) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	// Прочитанным становится не письмо, а вся ветка: человек открыл переписку и
	// увидел её целиком, включая ответы. Оставить их непрочитанными значило бы
	// показывать значок над письмом, которое уже перед глазами.
	_, err := r.db.ExecContext(ctx, `
        UPDATE user_mail SET read_at = now()
        WHERE user_id = $2 AND read_at IS NULL
          AND (id = $1 OR thread_id = $1)`, id, userID)
	return err
}

func (r *mailRepo) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_mail SET read_at = now() WHERE user_id = $1 AND read_at IS NULL AND deleted_at IS NULL`, userID)
	return err
}

func (r *mailRepo) Delete(ctx context.Context, id, userID uuid.UUID) error {
	// Мягкое удаление: письмо о выданном подарке — след выдачи, и он не должен
	// исчезать из базы оттого, что получатель смахнул карточку.
	// Удаляется вся ветка: переписка — одна карточка в ленте, и смахнув её,
	// человек убирает разговор, а не первую реплику из него.
	_, err := r.db.ExecContext(ctx, `
        UPDATE user_mail SET deleted_at = now()
        WHERE user_id = $2 AND deleted_at IS NULL
          AND (id = $1 OR thread_id = $1)`, id, userID)
	return err
}

// --- Переписка ---------------------------------------------------------------

// mailColumns — набор колонок письма в порядке, который читает scanMail.
const mailColumns = `m.id, m.user_id, m.kind, m.subject, m.body, m.ref_type, m.ref_id,
       m.sender_id, m.direction, m.thread_id, m.created_at, m.read_at, m.admin_read_at,
       COALESCE(NULLIF(TRIM(CONCAT_WS(' ', s.last_name, s.first_name)), ''), '') AS sender_name`

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanMail(row scanner) (*Mail, error) {
	var m Mail
	if err := row.Scan(&m.ID, &m.UserID, &m.Kind, &m.Subject, &m.Body, &m.RefType, &m.RefID,
		&m.SenderID, &m.Direction, &m.ThreadID, &m.CreatedAt, &m.ReadAt, &m.AdminReadAt,
		&m.SenderName); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *mailRepo) Get(ctx context.Context, id uuid.UUID) (*Mail, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT `+mailColumns+`
        FROM user_mail m
        LEFT JOIN users s ON s.id = m.sender_id
        WHERE m.id = $1 AND m.deleted_at IS NULL`, id)
	return scanMail(row)
}

func (r *mailRepo) Thread(ctx context.Context, threadID uuid.UUID) ([]*Mail, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT `+mailColumns+`
        FROM user_mail m
        LEFT JOIN users s ON s.id = m.sender_id
        WHERE m.thread_id = $1 AND m.deleted_at IS NULL
        ORDER BY m.created_at`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Mail, 0)
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *mailRepo) Reply(ctx context.Context, mail *Mail) error {
	mail.Kind = MailKindDirect
	if mail.ThreadID == nil {
		return sql.ErrNoRows
	}
	if mail.Direction == MailDirectionOut && mail.ReadAt == nil {
		// Собственная реплика не может быть непрочитанной у того, кто её
		// написал: иначе ответ зажигал бы значок автору ответа.
		now := time.Now()
		mail.ReadAt = &now
	}
	return r.Send(ctx, nil, mail)
}

func (r *mailRepo) ListDirectForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*Mail, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	// Удалённые письма администратор видит. Мягкое удаление убирает переписку из
	// ящика её владельца, а не из истории обращений: иначе смахнув карточку,
	// человек стирал бы разговор и у того, кто ему отвечал.
	rows, err := r.db.QueryContext(ctx, `
        SELECT `+mailColumns+`
        FROM user_mail m
        LEFT JOIN users s ON s.id = m.sender_id
        WHERE m.user_id = $1 AND m.kind = $2
        ORDER BY m.created_at
        LIMIT $3`, userID, MailKindDirect, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Mail, 0)
	for rows.Next() {
		m, err := scanMail(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *mailRepo) ListDialogs(ctx context.Context, onlyUnanswered bool, limit int) ([]*MailDialog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// Одна строка на собеседника: администратор открывает список, чтобы увидеть,
	// кому он не ответил, а не чтобы пролистать все письма. Последнее письмо
	// берётся оконной функцией, счётчики — агрегатами по тому же набору, поэтому
	// вся сводка стоит одного прохода по адресной почте.
	//
	// Удалённые письма считаются наравне с остальными — см. ListDirectForUser.
	rows, err := r.db.QueryContext(ctx, `
        WITH direct AS (
            SELECT m.*,
                   ROW_NUMBER() OVER (PARTITION BY m.user_id ORDER BY m.created_at DESC) AS rn
            FROM user_mail m
            WHERE m.kind = $1
        ), agg AS (
            SELECT user_id,
                   COUNT(*)                                                                     AS total,
                   COUNT(*) FILTER (WHERE direction = 'OUT' AND admin_read_at IS NULL)          AS unread,
                   COUNT(*) FILTER (WHERE direction = 'IN' AND read_at IS NULL)                 AS user_unread
            FROM direct GROUP BY user_id
        )
        SELECT d.user_id, u.phone,
               COALESCE(NULLIF(TRIM(CONCAT_WS(' ', u.last_name, u.first_name)), ''), '') AS full_name,
               u.role, COALESCE(d.thread_id, d.id), d.subject, d.body, d.created_at, d.direction,
               agg.unread, agg.user_unread, agg.total
        FROM direct d
        JOIN agg ON agg.user_id = d.user_id
        JOIN users u ON u.id = d.user_id
        WHERE d.rn = 1 AND ($2 = false OR agg.unread > 0)
        ORDER BY d.created_at DESC
        LIMIT $3`, MailKindDirect, onlyUnanswered, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*MailDialog, 0)
	for rows.Next() {
		var d MailDialog
		if err := rows.Scan(&d.UserID, &d.Phone, &d.FullName, &d.Role, &d.ThreadID,
			&d.Subject, &d.LastBody, &d.LastAt, &d.LastDirection,
			&d.Unread, &d.UserUnread, &d.Total); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (r *mailRepo) MarkThreadReadByAdmin(ctx context.Context, threadID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
        UPDATE user_mail SET admin_read_at = now()
        WHERE thread_id = $1 AND direction = 'OUT' AND admin_read_at IS NULL`, threadID)
	return err
}

func (r *mailRepo) AdminUnreadCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM user_mail
        WHERE direction = 'OUT' AND admin_read_at IS NULL`).Scan(&count)
	return count, err
}
