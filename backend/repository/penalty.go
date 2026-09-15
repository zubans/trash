package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

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
