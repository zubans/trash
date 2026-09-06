package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ErrAchievementAlreadyGranted сообщает, что выдача с этим ключом уже есть.
// Это нормальный исход — переотправленное событие, просящее уже сделанную
// выдачу, — и вызывающие пропускают её, а не падают.
var ErrAchievementAlreadyGranted = errors.New("achievement already granted")

// ErrAchievementExists сообщает, что код уже занят — в том числе
// заархивированной ачивкой: её код остаётся занятым, потому что на него
// ссылаются выданные экземпляры.
var ErrAchievementExists = errors.New("achievement with this code already exists")

// Источники баллов. Ачивки — не единственный: администратор вправе начислить
// вручную, а акции будут писать сюда же. Уровень считается по всей таблице
// баллов, а не по выдачам ачивок, именно поэтому.
const (
	PointSourceAchievement = "achievement"
	PointSourceAdmin       = "admin"
	PointSourcePromo       = "promo"
)

// Achievement — строка каталога: то, чем администратор управляет, не пересобирая
// образ. Правило живёт в скрипте с тем же кодом.
type Achievement struct {
	Code          string     `json:"code"`
	IsActive      bool       `json:"is_active"`
	AvailableFrom *time.Time `json:"available_from,omitempty"`
	AvailableTo   *time.Time `json:"available_to,omitempty"`
	// Weight переопределяет вес из скрипта. nil — вес берётся из манифеста.
	Weight    *int                   `json:"weight,omitempty"`
	Config    map[string]interface{} `json:"config"`
	SortOrder int                    `json:"sort_order"`
	// Constants и Source — собственный скрипт ачивки, написанный в админ-панели.
	// Пустые у поставляемой: её скрипт лежит в бинарнике, и править его отсюда
	// нельзя — правило, меняющее деньги, проходит ревью, а не правку в форме.
	Constants string     `json:"constants"`
	Source    string     `json:"source"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// HasOwnScript сообщает, несёт ли строка собственный скрипт. Именно это, а не
// происхождение кода, решает, что компилировать и что можно редактировать.
func (a *Achievement) HasOwnScript() bool {
	return a != nil && strings.TrimSpace(a.Source) != ""
}

// AvailableAt сообщает, попадает ли момент в окно акции. Окно проверяется здесь,
// а не в скрипте: «когда ачивку можно заслужить» — свойство строки каталога, и
// скрипт не должен иметь возможности его обойти.
func (a *Achievement) AvailableAt(t time.Time) bool {
	if a == nil || !a.IsActive || a.DeletedAt != nil {
		return false
	}
	if a.AvailableFrom != nil && t.Before(*a.AvailableFrom) {
		return false
	}
	if a.AvailableTo != nil && t.After(*a.AvailableTo) {
		return false
	}
	return true
}

// UserAchievement — одна выдача.
type UserAchievement struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	Code         string     `json:"code"`
	GrantKey     string     `json:"grant_key"`
	Points       int        `json:"points"`
	OrderID      *uuid.UUID `json:"order_id,omitempty"`
	GrantedAt    time.Time  `json:"granted_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
}

// GrantSummary — свод по коду для одного пользователя: столько раз выдана, на
// столько действующих баллов. Это то, что скрипт видит в f.granted, и то, что
// рисуется на карточке значка.
type GrantSummary struct {
	Count     int
	Points    int
	GrantedAt time.Time
	ExpiresAt *time.Time
}

// AchievementRepository хранит каталог ачивок, выдачи и баллы.
//
// Уникальность (user_id, code, grant_key) — не удобство, а защита: повторная
// доставка события проигрывает вставку в базе, а не в проверке сервиса, где две
// одновременные обработки одного события могли бы обе ничего не найти.
type AchievementRepository interface {
	// List возвращает живые ачивки; ListDeleted — заархивированные, чтобы их
	// можно было вернуть.
	List(ctx context.Context) ([]*Achievement, error)
	ListDeleted(ctx context.Context) ([]*Achievement, error)
	ListActive(ctx context.Context) ([]*Achievement, error)
	// ListWithScript возвращает ачивки с собственным скриптом — их компилирует
	// движок при старте и по таймеру.
	ListWithScript(ctx context.Context) ([]*Achievement, error)
	Get(ctx context.Context, code string) (*Achievement, error)
	// Create заводит новую ачивку. Возвращает ErrAchievementExists, когда код
	// уже занят, — в том числе заархивированной ачивкой: код остаётся занятым,
	// потому что на него ссылаются выданные экземпляры.
	Create(ctx context.Context, a *Achievement) error
	Upsert(ctx context.Context, a *Achievement) error
	// Delete архивирует ачивку. Выдачи и баллы остаются: у ачивки есть
	// полученные экземпляры, то есть чей-то уровень и чья-то ставка комиссии.
	Delete(ctx context.Context, code string) error
	// Restore возвращает заархивированную ачивку — выключенной.
	Restore(ctx context.Context, code string) error

	// Grant записывает выдачу внутри транзакции вызывающего. Возвращает
	// ErrAchievementAlreadyGranted, когда ключ уже занят.
	Grant(ctx context.Context, q Querier, grant *UserAchievement) error
	// AddPoints пишет строку в реестр баллов. Вызывается вместе с Grant, но
	// существует отдельно: у баллов будут и другие источники.
	AddPoints(ctx context.Context, q Querier, userID uuid.UUID, points int, sourceType, sourceCode string, sourceID *uuid.UUID, reason string, expiresAt *time.Time) error
	// ActivePoints — сумма действующих баллов. Один индексный запрос: он лежит
	// на пути подтверждения заказа, где считается ставка комиссии.
	ActivePoints(ctx context.Context, q Querier, userID uuid.UUID) (int, error)
	// PointsToday — сколько баллов начислено за сутки; суточный потолок.
	PointsToday(ctx context.Context, q Querier, userID uuid.UUID) (int, error)
	// BumpPointsToday увеличивает суточный счётчик и возвращает новое значение.
	// Инкремент и чтение — один оператор, поэтому два одновременных начисления
	// не пробьют потолок вдвоём.
	BumpPointsToday(ctx context.Context, q Querier, userID uuid.UUID, points int) (int, error)

	// ListForUser возвращает выдачи пользователя, свежие первыми.
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*UserAchievement, error)
	// SummaryForUser сводит выдачи по кодам — для фактов скрипта и для экрана.
	SummaryForUser(ctx context.Context, userID uuid.UUID) (map[string]GrantSummary, error)
	// RevokeByOrder отзывает всё, что было выдано за этот заказ, и гасит его
	// баллы. Отменённый или возвращённый заказ не должен оставлять после себя
	// заработанный уровень.
	RevokeByOrder(ctx context.Context, q Querier, orderID uuid.UUID, reason string) (int, error)
	// Revoke отзывает одну выдачу — решением администратора.
	Revoke(ctx context.Context, id uuid.UUID, reason string) error
}

type achievementRepo struct {
	db *sql.DB
}

// NewAchievementRepository создаёт AchievementRepository.
func NewAchievementRepository(db *sql.DB) AchievementRepository {
	return &achievementRepo{db: db}
}

func (r *achievementRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const achievementColumns = `code, is_active, available_from, available_to, weight, config, sort_order, constants, source, deleted_at, created_at, updated_at`

func (r *achievementRepo) List(ctx context.Context) ([]*Achievement, error) {
	return r.list(ctx, `WHERE deleted_at IS NULL`)
}

func (r *achievementRepo) ListDeleted(ctx context.Context) ([]*Achievement, error) {
	return r.list(ctx, `WHERE deleted_at IS NOT NULL`)
}

func (r *achievementRepo) ListActive(ctx context.Context) ([]*Achievement, error) {
	return r.list(ctx, `WHERE is_active AND deleted_at IS NULL`)
}

func (r *achievementRepo) ListWithScript(ctx context.Context) ([]*Achievement, error) {
	// Заархивированные не компилируются: их скрипт больше ничего не решает, а
	// занятое им место в движке пришлось бы освобождать отдельно.
	return r.list(ctx, `WHERE source <> '' AND deleted_at IS NULL`)
}

func (r *achievementRepo) list(ctx context.Context, where string) ([]*Achievement, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+achievementColumns+` FROM achievements `+where+` ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*Achievement, 0)
	for rows.Next() {
		a, err := scanAchievement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *achievementRepo) Get(ctx context.Context, code string) (*Achievement, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+achievementColumns+` FROM achievements WHERE code = $1`, code)
	return scanAchievement(row)
}

func scanAchievement(row rowScanner) (*Achievement, error) {
	var a Achievement
	var config []byte
	var weight sql.NullInt64
	if err := row.Scan(&a.Code, &a.IsActive, &a.AvailableFrom, &a.AvailableTo,
		&weight, &config, &a.SortOrder, &a.Constants, &a.Source,
		&a.DeletedAt, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if weight.Valid {
		value := int(weight.Int64)
		a.Weight = &value
	}
	if len(config) > 0 {
		_ = json.Unmarshal(config, &a.Config)
	}
	return &a, nil
}

func (r *achievementRepo) Upsert(ctx context.Context, a *Achievement) error {
	config := []byte("{}")
	if len(a.Config) > 0 {
		encoded, err := json.Marshal(a.Config)
		if err != nil {
			return err
		}
		config = encoded
	}
	var weight interface{}
	if a.Weight != nil {
		weight = *a.Weight
	}
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO achievements (code, is_active, available_from, available_to, weight, config, sort_order, constants, source)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (code) DO UPDATE SET
            is_active = EXCLUDED.is_active,
            available_from = EXCLUDED.available_from,
            available_to = EXCLUDED.available_to,
            weight = EXCLUDED.weight,
            config = EXCLUDED.config,
            sort_order = EXCLUDED.sort_order,
            constants = EXCLUDED.constants,
            source = EXCLUDED.source,
            updated_at = now()
    `, a.Code, a.IsActive, a.AvailableFrom, a.AvailableTo, weight, config, a.SortOrder, a.Constants, a.Source)
	return err
}

func (r *achievementRepo) Create(ctx context.Context, a *Achievement) error {
	config := []byte("{}")
	if len(a.Config) > 0 {
		encoded, err := json.Marshal(a.Config)
		if err != nil {
			return err
		}
		config = encoded
	}
	var weight interface{}
	if a.Weight != nil {
		weight = *a.Weight
	}
	// DO NOTHING вместо проверки «а есть ли такая?»: проверка и вставка порознь
	// позволили бы двум одновременным созданиям обеим пройти проверку.
	err := execExpectingOne(ctx, r.db, `
        INSERT INTO achievements (code, is_active, available_from, available_to, weight, config, sort_order, constants, source)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (code) DO NOTHING
    `, a.Code, a.IsActive, a.AvailableFrom, a.AvailableTo, weight, config, a.SortOrder, a.Constants, a.Source)
	if errors.Is(err, ErrConflict) {
		return ErrAchievementExists
	}
	return err
}

func (r *achievementRepo) Delete(ctx context.Context, code string) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE achievements SET deleted_at = now(), is_active = FALSE, updated_at = now()
		  WHERE code = $1 AND deleted_at IS NULL`, code)
}

func (r *achievementRepo) Restore(ctx context.Context, code string) error {
	// Возвращается выключенной: восстановление — это «верните строку», а не
	// «начните снова раздавать баллы».
	return execExpectingOne(ctx, r.db,
		`UPDATE achievements SET deleted_at = NULL, is_active = FALSE, updated_at = now()
		  WHERE code = $1 AND deleted_at IS NOT NULL`, code)
}

func (r *achievementRepo) Grant(ctx context.Context, q Querier, grant *UserAchievement) error {
	if grant.ID == uuid.Nil {
		grant.ID = uuid.New()
	}
	err := r.exec(q).QueryRowContext(ctx, `
        INSERT INTO user_achievements (id, user_id, code, grant_key, points, order_id, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        RETURNING granted_at
    `, grant.ID, grant.UserID, grant.Code, grant.GrantKey, grant.Points, grant.OrderID, grant.ExpiresAt).
		Scan(&grant.GrantedAt)
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAchievementAlreadyGranted
	}
	return err
}

func (r *achievementRepo) AddPoints(ctx context.Context, q Querier, userID uuid.UUID, points int, sourceType, sourceCode string, sourceID *uuid.UUID, reason string, expiresAt *time.Time) error {
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO user_points (user_id, points, source_type, source_code, source_id, reason, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, userID, points, sourceType, sourceCode, sourceID, reason, expiresAt)
	return err
}

func (r *achievementRepo) ActivePoints(ctx context.Context, q Querier, userID uuid.UUID) (int, error) {
	var points int
	// Действующий балл — не отозванный и не истёкший. Срок проверяется запросом,
	// а не сгоранием по расписанию: воркер, который «гасит» баллы, — это ещё
	// один способ разойтись с тем, что видит пользователь.
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT COALESCE(SUM(points), 0) FROM user_points
        WHERE user_id = $1 AND revoked_at IS NULL
          AND (expires_at IS NULL OR expires_at > now())
    `, userID).Scan(&points)
	return points, err
}

func (r *achievementRepo) PointsToday(ctx context.Context, q Querier, userID uuid.UUID) (int, error) {
	var points int
	err := r.exec(q).QueryRowContext(ctx,
		`SELECT COALESCE(points, 0) FROM user_points_daily WHERE user_id = $1 AND day = CURRENT_DATE`,
		userID).Scan(&points)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return points, err
}

func (r *achievementRepo) BumpPointsToday(ctx context.Context, q Querier, userID uuid.UUID, points int) (int, error) {
	var total int
	err := r.exec(q).QueryRowContext(ctx, `
        INSERT INTO user_points_daily (user_id, day, points)
        VALUES ($1, CURRENT_DATE, $2)
        ON CONFLICT (user_id, day) DO UPDATE SET points = user_points_daily.points + EXCLUDED.points
        RETURNING points
    `, userID, points).Scan(&total)
	return total, err
}

func (r *achievementRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]*UserAchievement, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, user_id, code, grant_key, points, order_id, granted_at, expires_at,
               revoked_at, COALESCE(revoke_reason, '')
        FROM user_achievements
        WHERE user_id = $1
        ORDER BY granted_at DESC
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*UserAchievement, 0)
	for rows.Next() {
		var g UserAchievement
		if err := rows.Scan(&g.ID, &g.UserID, &g.Code, &g.GrantKey, &g.Points, &g.OrderID,
			&g.GrantedAt, &g.ExpiresAt, &g.RevokedAt, &g.RevokeReason); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func (r *achievementRepo) SummaryForUser(ctx context.Context, userID uuid.UUID) (map[string]GrantSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT code,
               COUNT(*),
               COALESCE(SUM(points) FILTER (WHERE expires_at IS NULL OR expires_at > now()), 0),
               MAX(granted_at),
               MAX(expires_at)
        FROM user_achievements
        WHERE user_id = $1 AND revoked_at IS NULL
        GROUP BY code
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]GrantSummary{}
	for rows.Next() {
		var code string
		var summary GrantSummary
		if err := rows.Scan(&code, &summary.Count, &summary.Points, &summary.GrantedAt, &summary.ExpiresAt); err != nil {
			return nil, err
		}
		out[code] = summary
	}
	return out, rows.Err()
}

func (r *achievementRepo) RevokeByOrder(ctx context.Context, q Querier, orderID uuid.UUID, reason string) (int, error) {
	exec := r.exec(q)
	rows, err := exec.QueryContext(ctx, `
        UPDATE user_achievements
           SET revoked_at = now(), revoke_reason = $2
         WHERE order_id = $1 AND revoked_at IS NULL
        RETURNING id
    `, orderID, reason)
	if err != nil {
		return 0, err
	}
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// Баллы гасятся вместе с выдачей: отозванная ачивка, оставившая баллы,
	// оставила бы и уровень, то есть и сниженную комиссию.
	_, err = exec.ExecContext(ctx,
		`UPDATE user_points SET revoked_at = now() WHERE source_id = ANY($1) AND revoked_at IS NULL`,
		pq.Array(ids))
	return len(ids), err
}

func (r *achievementRepo) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := execExpectingOne(ctx, tx, `
        UPDATE user_achievements SET revoked_at = now(), revoke_reason = $2
        WHERE id = $1 AND revoked_at IS NULL
    `, id, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE user_points SET revoked_at = now() WHERE source_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit()
}
