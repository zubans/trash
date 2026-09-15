package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// PenaltyPoint — одна строка журнала штрафных баллов (penalty_points).
type PenaltyPoint struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Role       string     `json:"role"`
	OrderID    *uuid.UUID `json:"order_id,omitempty"`
	DisputeID  *uuid.UUID `json:"dispute_id,omitempty"`
	AssignedBy *uuid.UUID `json:"assigned_by,omitempty"`
	Reason     string     `json:"reason"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	RevokedBy  *uuid.UUID `json:"revoked_by,omitempty"`
	ExpiredAt  *time.Time `json:"expired_at,omitempty"`
}

// Live сообщает, действует ли балл: не отменён и не сгорел.
func (p *PenaltyPoint) Live() bool {
	return p.RevokedAt == nil && p.ExpiredAt == nil
}

// PenaltyStatus — состояние одной роли пользователя, свёрнутое из журнала
// (user_penalty_status).
type PenaltyStatus struct {
	UserID               uuid.UUID  `json:"user_id"`
	Role                 string     `json:"role"`
	ActivePoints         int        `json:"active_points"`
	PhotoRequiredUntil   *time.Time `json:"photo_required_until,omitempty"`
	SilentBlockStartedAt *time.Time `json:"silent_block_started_at,omitempty"`
	SilentBlockEndsAt    *time.Time `json:"silent_block_ends_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// LivePoints — сводка действующих баллов роли.
type LivePoints struct {
	Count int
	// Last — время последнего действующего балла; nil, если баллов нет.
	Last *time.Time
}

// PenaltyFlags — факты уровня пользователя, переживающие баллы любой роли
// (user_penalty_flags, миграция 053).
type PenaltyFlags struct {
	UserID uuid.UUID `json:"user_id"`
	// HadSilentBlockAt — когда с пользователя сняли тихую блокировку. Пока флаг
	// стоит, любой новый балл — рецидив.
	HadSilentBlockAt *time.Time `json:"had_silent_block_at,omitempty"`
	SoftBannedAt     *time.Time `json:"soft_banned_at,omitempty"`
	// SoftBannedBy — администратор, поставивший мягкий бан вручную. nil, когда
	// его поставила система при рецидиве.
	SoftBannedBy  *uuid.UUID `json:"soft_banned_by,omitempty"`
	SoftBanReason string     `json:"soft_ban_reason,omitempty"`
}

// PenaltyRepository хранит штрафное состояние пользователей.
type PenaltyRepository interface {
	// AddPoint записывает балл и заполняет ID и CreatedAt. Возвращает
	// ErrConflict, если по этому спору эта сторона балл уже получила.
	AddPoint(ctx context.Context, q Querier, p *PenaltyPoint) error
	// RevokePoint отменяет действующий балл и возвращает его. ErrConflict —
	// балл уже отменён или сгорел; sql.ErrNoRows — балла нет.
	RevokePoint(ctx context.Context, q Querier, pointID, revokedBy uuid.UUID) (*PenaltyPoint, error)
	// ListPoints — весь журнал пользователя по всем ролям, новые первыми.
	ListPoints(ctx context.Context, q Querier, userID uuid.UUID) ([]PenaltyPoint, error)
	// CountLivePoints — действующие баллы роли.
	CountLivePoints(ctx context.Context, q Querier, userID uuid.UUID, role string) (LivePoints, error)
	// LockStatus заводит строку состояния роли, если её нет, и берёт на неё
	// блокировку до конца транзакции: пересчёты одной роли идут по очереди.
	LockStatus(ctx context.Context, q Querier, userID uuid.UUID, role string) (*PenaltyStatus, error)
	// SaveStatus записывает пересчитанное состояние роли.
	SaveStatus(ctx context.Context, q Querier, st *PenaltyStatus) error
	// ListStatuses — состояния всех ролей пользователя.
	ListStatuses(ctx context.Context, q Querier, userID uuid.UUID) ([]PenaltyStatus, error)

	// ApplySoftBan переводит пользователя в SOFT_BANNED и записывает, кто, когда
	// и почему, одним оператором: статус без причины или причина без статуса
	// невозможны. by == nil — бан поставила система. Для несуществующего
	// пользователя — sql.ErrNoRows.
	ApplySoftBan(ctx context.Context, q Querier, userID uuid.UUID, by *uuid.UUID, reason string) error
	// LiftSoftBan переводит пользователя из SOFT_BANNED в ACTIVE и стирает
	// причину бана. Флаг прошлой тихой блокировки не трогает: его снимает только
	// отдельное решение администратора. Возвращает ErrConflict, если
	// пользователь не в SOFT_BANNED.
	LiftSoftBan(ctx context.Context, q Querier, userID uuid.UUID) error
	// GetFlags отдаёт флаги пользователя; для пользователя без строки — пустые.
	GetFlags(ctx context.Context, q Querier, userID uuid.UUID) (*PenaltyFlags, error)
}

type penaltyRepo struct {
	db *sql.DB
}

// NewPenaltyRepository создаёт PenaltyRepository.
func NewPenaltyRepository(db *sql.DB) PenaltyRepository {
	return &penaltyRepo{db: db}
}

func (r *penaltyRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *penaltyRepo) ApplySoftBan(ctx context.Context, q Querier, userID uuid.UUID, by *uuid.UUID, reason string) error {
	var id uuid.UUID
	err := r.exec(q).QueryRowContext(ctx, `
        WITH banned AS (
            UPDATE users SET status = 'SOFT_BANNED'
            WHERE id = $1
            RETURNING id
        )
        INSERT INTO user_penalty_flags (user_id, soft_banned_at, soft_banned_by, soft_ban_reason, updated_at)
        SELECT id, now(), $2, $3, now() FROM banned
        ON CONFLICT (user_id) DO UPDATE SET
            soft_banned_at  = EXCLUDED.soft_banned_at,
            soft_banned_by  = EXCLUDED.soft_banned_by,
            soft_ban_reason = EXCLUDED.soft_ban_reason,
            updated_at      = now()
        RETURNING user_id
    `, userID, by, reason).Scan(&id)
	return err
}

func (r *penaltyRepo) LiftSoftBan(ctx context.Context, q Querier, userID uuid.UUID) error {
	// Строки флагов может и не быть, поэтому снят ли бан, считается по
	// обновлённой строке пользователя, а не по флагам.
	var lifted int
	if err := r.exec(q).QueryRowContext(ctx, `
        WITH lifted AS (
            UPDATE users SET status = 'ACTIVE'
            WHERE id = $1 AND status = 'SOFT_BANNED'
            RETURNING id
        ), cleared AS (
            UPDATE user_penalty_flags f
            SET soft_banned_at = NULL, soft_banned_by = NULL, soft_ban_reason = NULL, updated_at = now()
            FROM lifted
            WHERE f.user_id = lifted.id
        )
        SELECT count(*) FROM lifted
    `, userID).Scan(&lifted); err != nil {
		return err
	}
	if lifted == 0 {
		return ErrConflict
	}
	return nil
}

func (r *penaltyRepo) GetFlags(ctx context.Context, q Querier, userID uuid.UUID) (*PenaltyFlags, error) {
	flags := &PenaltyFlags{UserID: userID}
	var by uuid.NullUUID
	var reason sql.NullString
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT had_silent_block_at, soft_banned_at, soft_banned_by, soft_ban_reason
        FROM user_penalty_flags WHERE user_id = $1
    `, userID).Scan(&flags.HadSilentBlockAt, &flags.SoftBannedAt, &by, &reason)
	if err == sql.ErrNoRows {
		return flags, nil
	}
	if err != nil {
		return nil, err
	}
	if by.Valid {
		flags.SoftBannedBy = &by.UUID
	}
	flags.SoftBanReason = reason.String
	return flags, nil
}

const penaltyPointColumns = `id, user_id, role, order_id, dispute_id, assigned_by, reason,
    created_at, revoked_at, revoked_by, expired_at`

func scanPenaltyPoint(row interface{ Scan(...interface{}) error }) (*PenaltyPoint, error) {
	var p PenaltyPoint
	var orderID, disputeID, assignedBy, revokedBy uuid.NullUUID
	if err := row.Scan(&p.ID, &p.UserID, &p.Role, &orderID, &disputeID, &assignedBy, &p.Reason,
		&p.CreatedAt, &p.RevokedAt, &revokedBy, &p.ExpiredAt); err != nil {
		return nil, err
	}
	p.OrderID = nullableUUID(orderID)
	p.DisputeID = nullableUUID(disputeID)
	p.AssignedBy = nullableUUID(assignedBy)
	p.RevokedBy = nullableUUID(revokedBy)
	return &p, nil
}

func nullableUUID(v uuid.NullUUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := v.UUID
	return &id
}

func (r *penaltyRepo) AddPoint(ctx context.Context, q Querier, p *PenaltyPoint) error {
	err := r.exec(q).QueryRowContext(ctx, `
        INSERT INTO penalty_points (user_id, role, order_id, dispute_id, assigned_by, reason)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id, created_at
    `, p.UserID, p.Role, p.OrderID, p.DisputeID, p.AssignedBy, p.Reason).Scan(&p.ID, &p.CreatedAt)
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (r *penaltyRepo) RevokePoint(ctx context.Context, q Querier, pointID, revokedBy uuid.UUID) (*PenaltyPoint, error) {
	p, err := scanPenaltyPoint(r.exec(q).QueryRowContext(ctx, `
        UPDATE penalty_points SET revoked_at = now(), revoked_by = $2
        WHERE id = $1 AND revoked_at IS NULL AND expired_at IS NULL
        RETURNING `+penaltyPointColumns, pointID, revokedBy))
	if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	var exists bool
	if err := r.exec(q).QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM penalty_points WHERE id = $1)`, pointID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrConflict
	}
	return nil, sql.ErrNoRows
}

func (r *penaltyRepo) ListPoints(ctx context.Context, q Querier, userID uuid.UUID) ([]PenaltyPoint, error) {
	rows, err := r.exec(q).QueryContext(ctx,
		`SELECT `+penaltyPointColumns+` FROM penalty_points WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []PenaltyPoint{}
	for rows.Next() {
		p, err := scanPenaltyPoint(rows)
		if err != nil {
			return nil, err
		}
		points = append(points, *p)
	}
	return points, rows.Err()
}

func (r *penaltyRepo) CountLivePoints(ctx context.Context, q Querier, userID uuid.UUID, role string) (LivePoints, error) {
	var live LivePoints
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT count(*), max(created_at) FROM penalty_points
        WHERE user_id = $1 AND role = $2 AND revoked_at IS NULL AND expired_at IS NULL
    `, userID, role).Scan(&live.Count, &live.Last)
	return live, err
}

const penaltyStatusColumns = `user_id, role, active_points, photo_required_until,
    silent_block_started_at, silent_block_ends_at, updated_at`

func scanPenaltyStatus(row interface{ Scan(...interface{}) error }) (*PenaltyStatus, error) {
	var st PenaltyStatus
	if err := row.Scan(&st.UserID, &st.Role, &st.ActivePoints, &st.PhotoRequiredUntil,
		&st.SilentBlockStartedAt, &st.SilentBlockEndsAt, &st.UpdatedAt); err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *penaltyRepo) LockStatus(ctx context.Context, q Querier, userID uuid.UUID, role string) (*PenaltyStatus, error) {
	if _, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO user_penalty_status (user_id, role) VALUES ($1, $2)
        ON CONFLICT (user_id, role) DO NOTHING
    `, userID, role); err != nil {
		return nil, err
	}
	return scanPenaltyStatus(r.exec(q).QueryRowContext(ctx,
		`SELECT `+penaltyStatusColumns+` FROM user_penalty_status WHERE user_id = $1 AND role = $2 FOR UPDATE`,
		userID, role))
}

func (r *penaltyRepo) SaveStatus(ctx context.Context, q Querier, st *PenaltyStatus) error {
	return execExpectingOne(ctx, r.exec(q), `
        UPDATE user_penalty_status
        SET active_points = $3, photo_required_until = $4,
            silent_block_started_at = $5, silent_block_ends_at = $6, updated_at = now()
        WHERE user_id = $1 AND role = $2
    `, st.UserID, st.Role, st.ActivePoints, st.PhotoRequiredUntil, st.SilentBlockStartedAt, st.SilentBlockEndsAt)
}

func (r *penaltyRepo) ListStatuses(ctx context.Context, q Querier, userID uuid.UUID) ([]PenaltyStatus, error) {
	rows, err := r.exec(q).QueryContext(ctx,
		`SELECT `+penaltyStatusColumns+` FROM user_penalty_status WHERE user_id = $1 ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := []PenaltyStatus{}
	for rows.Next() {
		st, err := scanPenaltyStatus(rows)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, *st)
	}
	return statuses, rows.Err()
}
