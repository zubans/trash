package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Откуда паспорт в системе.
const (
	PassportSourceOwner        = "OWNER"
	PassportSourceVerification = "VERIFICATION"
	PassportSourceAdmin        = "ADMIN"
)

// Что сделали с паспортом — для журнала доступа.
const (
	PassportActionView      = "VIEW"
	PassportActionViewPhoto = "VIEW_PHOTO"
	PassportActionWrite     = "WRITE"
	PassportActionDelete    = "DELETE"
)

// ErrPassportNotFound — у пользователя нет паспорта.
var ErrPassportNotFound = errors.New("passport not found")

// PassportRecord — строка user_passports. Данные зашифрованы: расшифровывает
// сервис, у которого есть ключ.
type PassportRecord struct {
	UserID     uuid.UUID
	DataEnc    []byte
	PhotoPath  *string
	Source     string
	EnteredBy  *uuid.UUID
	KeyVersion int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PassportRepository хранит паспорта, журнал доступа к ним и два флага
// пользователя, которые от паспорта зависят: «проверенный» и согласие на
// обработку персональных данных.
type PassportRepository interface {
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
	Get(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error)
	// Save записывает данные паспорта; фото, если оно было, остаётся.
	Save(ctx context.Context, q Querier, rec *PassportRecord) error
	// SetPhoto ставит фото и возвращает прежнее — его файл надо удалить.
	SetPhoto(ctx context.Context, q Querier, userID uuid.UUID, path string) (previous *string, err error)
	// Delete удаляет паспорт и возвращает удалённую строку — ради файла фото.
	Delete(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error)
	LogAccess(ctx context.Context, q Querier, userID, viewerID uuid.UUID, action string) error
	SetChecked(ctx context.Context, q Querier, userID uuid.UUID, checked bool, by uuid.UUID) error
	AcceptPDConsent(ctx context.Context, userID uuid.UUID, version int) error
}

type passportRepo struct {
	db *sql.DB
}

// NewPassportRepository создаёт PassportRepository.
func NewPassportRepository(db *sql.DB) PassportRepository {
	return &passportRepo{db: db}
}

func (r *passportRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *passportRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *passportRepo) Get(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error) {
	var rec PassportRecord
	err := r.exec(q).QueryRowContext(ctx, `
		SELECT user_id, data_enc, photo_path, source, entered_by, key_version, created_at, updated_at
		FROM user_passports WHERE user_id = $1`, userID).
		Scan(&rec.UserID, &rec.DataEnc, &rec.PhotoPath, &rec.Source, &rec.EnteredBy, &rec.KeyVersion, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPassportNotFound
	}
	return &rec, err
}

func (r *passportRepo) Save(ctx context.Context, q Querier, rec *PassportRecord) error {
	_, err := r.exec(q).ExecContext(ctx, `
		INSERT INTO user_passports (user_id, data_enc, source, entered_by, key_version)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			data_enc = EXCLUDED.data_enc, source = EXCLUDED.source, entered_by = EXCLUDED.entered_by,
			key_version = EXCLUDED.key_version, updated_at = now()`,
		rec.UserID, rec.DataEnc, rec.Source, rec.EnteredBy, rec.KeyVersion)
	return err
}

func (r *passportRepo) SetPhoto(ctx context.Context, q Querier, userID uuid.UUID, path string) (*string, error) {
	var previous *string
	err := r.exec(q).QueryRowContext(ctx, `
		UPDATE user_passports p SET photo_path = $2, updated_at = now()
		FROM (SELECT photo_path FROM user_passports WHERE user_id = $1 FOR UPDATE) old
		WHERE p.user_id = $1
		RETURNING old.photo_path`, userID, path).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPassportNotFound
	}
	return previous, err
}

func (r *passportRepo) Delete(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error) {
	var rec PassportRecord
	err := r.exec(q).QueryRowContext(ctx, `
		DELETE FROM user_passports WHERE user_id = $1
		RETURNING user_id, data_enc, photo_path, source, entered_by, key_version, created_at, updated_at`, userID).
		Scan(&rec.UserID, &rec.DataEnc, &rec.PhotoPath, &rec.Source, &rec.EnteredBy, &rec.KeyVersion, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPassportNotFound
	}
	return &rec, err
}

func (r *passportRepo) LogAccess(ctx context.Context, q Querier, userID, viewerID uuid.UUID, action string) error {
	_, err := r.exec(q).ExecContext(ctx,
		`INSERT INTO passport_access_log (user_id, viewer_id, action) VALUES ($1, $2, $3)`, userID, viewerID, action)
	return err
}

func (r *passportRepo) SetChecked(ctx context.Context, q Querier, userID uuid.UUID, checked bool, by uuid.UUID) error {
	query := `UPDATE users SET is_checked = TRUE, checked_at = now(), checked_by = $2 WHERE id = $1`
	args := []interface{}{userID, by}
	if !checked {
		query = `UPDATE users SET is_checked = FALSE, checked_at = NULL, checked_by = NULL WHERE id = $1`
		args = args[:1]
	}
	return execExpectingOne(ctx, r.exec(q), query, args...)
}

func (r *passportRepo) AcceptPDConsent(ctx context.Context, userID uuid.UUID, version int) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE users SET pd_consent_version = $2, pd_consent_at = now() WHERE id = $1`, userID, version)
}
