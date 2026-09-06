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
	return r.exec(q).QueryRowContext(ctx, `
        INSERT INTO user_mail (id, user_id, kind, subject, body, ref_type, ref_id, sender_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING created_at
    `, mail.ID, mail.UserID, mail.Kind, mail.Subject, mail.Body,
		mail.RefType, mail.RefID, mail.SenderID).Scan(&mail.CreatedAt)
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
	query := `SELECT id FROM users WHERE status <> 'BANNED'`
	args := []interface{}{}
	if role != "" {
		// Роль может лежать и в основной колонке, и в списке ролей — так же, как
		// её читает HasRole.
		query += ` AND (role = $1 OR $1 = ANY(roles))`
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
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, user_id, kind, subject, body, ref_type, ref_id, sender_id, created_at, read_at
        FROM user_mail
        WHERE user_id = $1 AND deleted_at IS NULL
        ORDER BY created_at DESC
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
			&m.RefType, &m.RefID, &m.SenderID, &m.CreatedAt, &m.ReadAt); err != nil {
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_mail SET read_at = now() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`, id, userID)
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
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_mail SET deleted_at = now() WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}
