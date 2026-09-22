package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPerkNotFound возвращается для привилегии, которой нет или которая уже
// отозвана: отозвать её второй раз нечего.
var ErrPerkNotFound = errors.New("perk not found")

// UserPerk — привилегия пользователя на срок. Действует, пока
// starts_at ≤ now < expires_at и её не отозвали.
type UserPerk struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	// RuleCode — правило, которое считает ставку; RuleTitle — его название
	// для экрана.
	RuleCode  string `json:"rule_code"`
	RuleTitle string `json:"rule_title"`
	// RuleVersionID — версия собственного правила, проданная вместе с
	// привилегией. Пусто у поставляемых: их версионирует сборка.
	RuleVersionID *uuid.UUID `json:"rule_version_id,omitempty"`
	// Config — константы правила на момент покупки.
	Config    map[string]interface{} `json:"config"`
	StartsAt  time.Time              `json:"starts_at"`
	ExpiresAt time.Time              `json:"expires_at"`
	// ShopOrderID — покупка, которой выдана привилегия; nil — выдана админом.
	ShopOrderID *uuid.UUID `json:"shop_order_id,omitempty"`
	// ShopOrderNumber — номер той же покупки для людей, когда список читают
	// для экрана.
	ShopOrderNumber *int64     `json:"shop_order_number,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	RevokedBy       *uuid.UUID `json:"revoked_by,omitempty"`
	GrantedBy       *uuid.UUID `json:"granted_by,omitempty"`
	Reason          *string    `json:"reason,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// PerkRepository хранит привилегии и их общую очередь.
//
// Все привилегии меняют одно и то же — ставку комиссии, — поэтому очередь у
// пользователя одна на все правила.
type PerkRepository interface {
	// Active возвращает привилегии, действующие в момент now. Строк обычно ноль
	// или одна; самую выгодную из нескольких выбирает сервис по формуле.
	Active(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) ([]*UserPerk, error)
	// LockQueue блокирует строку пользователя до конца транзакции: две покупки
	// подряд иначе прочитали бы один и тот же конец очереди и начались бы в
	// один момент. Строк user_perks у новичка нет, блокировать их нечего, а
	// строка пользователя есть всегда.
	LockQueue(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error
	// NextStart — max(now, последний expires_at непросроченных и не отозванных
	// привилегий этих видов). Вызывается после LockQueue.
	NextStart(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) (time.Time, error)
	// CountQueued — сколько привилегий этих видов ещё не истекло: и
	// действующая, и ждущие в очереди. Это то, что ограничивает
	// max_active_per_user.
	CountQueued(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) (int, error)
	Create(ctx context.Context, q Querier, perk *UserPerk) error
	// Revoke отзывает привилегию. Следующие в очереди не сдвигаются: купленное
	// начинается тогда, когда было обещано на витрине.
	Revoke(ctx context.Context, q Querier, id, adminID uuid.UUID) (*UserPerk, error)
	// Get возвращает привилегию по id.
	Get(ctx context.Context, q Querier, id uuid.UUID) (*UserPerk, error)
	// ListByShopOrder — привилегии одной покупки, для отмены с возвратом.
	ListByShopOrder(ctx context.Context, q Querier, shopOrderID uuid.UUID) ([]*UserPerk, error)
	// ListQueue — действующая и ждущие своей очереди, в порядке начала.
	ListQueue(ctx context.Context, userID uuid.UUID, now time.Time) ([]*UserPerk, error)
	// ListForUser — вся история привилегий пользователя, свежие первыми, для
	// карточки в админке.
	ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*UserPerk, error)
	// DueReminders возвращает последние в очереди привилегии, истекающие до
	// deadline, о которых ещё не напоминали. Привилегия, за которой в очереди
	// стоит следующая, напоминания не вызывает: у человека ничего не кончается.
	DueReminders(ctx context.Context, now, deadline time.Time, limit int) ([]*UserPerk, error)
	// MarkReminded ставит отметку, что письмо о конце ушло. Возвращает false,
	// если отметку уже поставил кто-то другой.
	MarkReminded(ctx context.Context, q Querier, id uuid.UUID) (bool, error)
}

type perkRepo struct {
	db *sql.DB
}

// NewPerkRepository создаёт PerkRepository.
func NewPerkRepository(db *sql.DB) PerkRepository {
	return &perkRepo{db: db}
}

func (r *perkRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const perkColumns = `p.id, p.user_id, p.rule_code, r.title, p.rule_version_id, p.config,
	p.starts_at, p.expires_at, p.shop_order_id,
	so.number, p.revoked_at, p.revoked_by, p.granted_by, p.reason, p.created_at`

const perkFrom = ` FROM user_perks p
	JOIN perk_rules r ON r.code = p.rule_code
	LEFT JOIN shop_orders so ON so.id = p.shop_order_id`

func scanPerk(row rowScanner) (*UserPerk, error) {
	var p UserPerk
	var config []byte
	var number sql.NullInt64
	if err := row.Scan(&p.ID, &p.UserID, &p.RuleCode, &p.RuleTitle, &p.RuleVersionID, &config,
		&p.StartsAt, &p.ExpiresAt, &p.ShopOrderID,
		&number, &p.RevokedAt, &p.RevokedBy, &p.GrantedBy, &p.Reason, &p.CreatedAt); err != nil {
		return nil, err
	}
	p.Config = map[string]interface{}{}
	if len(config) > 0 {
		if err := json.Unmarshal(config, &p.Config); err != nil {
			return nil, err
		}
	}
	if number.Valid {
		n := number.Int64
		p.ShopOrderNumber = &n
	}
	return &p, nil
}

func (r *perkRepo) list(ctx context.Context, q Querier, query string, args ...interface{}) ([]*UserPerk, error) {
	rows, err := r.exec(q).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*UserPerk, 0)
	for rows.Next() {
		p, err := scanPerk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *perkRepo) Active(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) ([]*UserPerk, error) {
	return r.list(ctx, q, `SELECT `+perkColumns+perkFrom+`
		WHERE p.user_id = $1 AND p.revoked_at IS NULL
		  AND p.starts_at <= $2 AND p.expires_at > $2
		ORDER BY p.starts_at`, userID, now)
}

func (r *perkRepo) LockQueue(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	var id uuid.UUID
	return tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&id)
}

func (r *perkRepo) NextStart(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) (time.Time, error) {
	var last sql.NullTime
	err := r.exec(q).QueryRowContext(ctx, `
		SELECT MAX(expires_at) FROM user_perks
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
		userID, now).Scan(&last)
	if err != nil {
		return time.Time{}, err
	}
	if last.Valid && last.Time.After(now) {
		return last.Time, nil
	}
	return now, nil
}

func (r *perkRepo) CountQueued(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) (int, error) {
	var count int
	err := r.exec(q).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_perks
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2`,
		userID, now).Scan(&count)
	return count, err
}

func (r *perkRepo) Create(ctx context.Context, q Querier, perk *UserPerk) error {
	if perk.ID == uuid.Nil {
		perk.ID = uuid.New()
	}
	if perk.Config == nil {
		perk.Config = map[string]interface{}{}
	}
	config, err := json.Marshal(perk.Config)
	if err != nil {
		return err
	}
	return r.exec(q).QueryRowContext(ctx, `
		INSERT INTO user_perks (id, user_id, rule_code, rule_version_id, config, starts_at, expires_at,
		                        shop_order_id, granted_by, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at`,
		perk.ID, perk.UserID, perk.RuleCode, perk.RuleVersionID, config, perk.StartsAt, perk.ExpiresAt,
		perk.ShopOrderID, perk.GrantedBy, perk.Reason).Scan(&perk.CreatedAt)
}

func (r *perkRepo) Revoke(ctx context.Context, q Querier, id, adminID uuid.UUID) (*UserPerk, error) {
	var revokedID uuid.UUID
	err := r.exec(q).QueryRowContext(ctx, `
		UPDATE user_perks SET revoked_at = now(), revoked_by = $2
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id`, id, adminID).Scan(&revokedID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPerkNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, q, revokedID)
}

func (r *perkRepo) Get(ctx context.Context, q Querier, id uuid.UUID) (*UserPerk, error) {
	p, err := scanPerk(r.exec(q).QueryRowContext(ctx, `SELECT `+perkColumns+perkFrom+` WHERE p.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPerkNotFound
	}
	return p, err
}

func (r *perkRepo) ListByShopOrder(ctx context.Context, q Querier, shopOrderID uuid.UUID) ([]*UserPerk, error) {
	return r.list(ctx, q, `SELECT `+perkColumns+perkFrom+`
		WHERE p.shop_order_id = $1 ORDER BY p.starts_at`, shopOrderID)
}

func (r *perkRepo) ListQueue(ctx context.Context, userID uuid.UUID, now time.Time) ([]*UserPerk, error) {
	return r.list(ctx, nil, `SELECT `+perkColumns+perkFrom+`
		WHERE p.user_id = $1 AND p.revoked_at IS NULL AND p.expires_at > $2
		ORDER BY p.starts_at`, userID, now)
}

func (r *perkRepo) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]*UserPerk, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return r.list(ctx, nil, `SELECT `+perkColumns+perkFrom+`
		WHERE p.user_id = $1 ORDER BY p.starts_at DESC LIMIT $2`, userID, limit)
}

func (r *perkRepo) DueReminders(ctx context.Context, now, deadline time.Time, limit int) ([]*UserPerk, error) {
	if limit <= 0 {
		limit = 100
	}
	return r.list(ctx, nil, `SELECT `+perkColumns+perkFrom+`
		WHERE p.revoked_at IS NULL AND p.reminded_at IS NULL
		  AND p.expires_at > $1 AND p.expires_at <= $2
		  AND NOT EXISTS (
		      SELECT 1 FROM user_perks n
		      WHERE n.user_id = p.user_id AND n.revoked_at IS NULL
		        AND n.id <> p.id AND n.expires_at > p.expires_at)
		ORDER BY p.expires_at
		LIMIT $3`, now, deadline, limit)
}

func (r *perkRepo) MarkReminded(ctx context.Context, q Querier, id uuid.UUID) (bool, error) {
	res, err := r.exec(q).ExecContext(ctx,
		`UPDATE user_perks SET reminded_at = now() WHERE id = $1 AND reminded_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
