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
	// Photo — откуда взялось фото: вердикт скрытой проверки снимка.
	Photo PassportPhoto
}

// Результаты скрытой проверки снимка. Значения — те же, что у снимков
// фото-подтверждения (backend/photoproof): считает их один и тот же код.
type PassportPhoto struct {
	// Path — имя файла снимка; пусто, когда фото не меняется.
	Path string
	// Seal — подпись файла: VALID, MISSING, INVALID.
	Seal string
	// Mark — незаметная метка в изображении: FOUND, NOT_FOUND, MISMATCH.
	Mark string
	// TakenAt — время съёмки по часам телефона; nil, если приложение его не
	// прислало (старая версия или загрузка из админки).
	TakenAt *time.Time
}

// CheckRequest — заявка на статус «проверенный» в очереди модерации.
type CheckRequest struct {
	UserID      uuid.UUID `json:"user_id"`
	Phone       string    `json:"phone"`
	Name        string    `json:"name,omitempty"`
	Role        string    `json:"role"`
	Verified    bool      `json:"is_verified"`
	RequestedAt time.Time `json:"requested_at"`
	HasPhoto    bool      `json:"has_photo"`
	// PhotoSeal — вердикт скрытой проверки снимка, чтобы очередь сразу
	// показывала, на что смотреть внимательнее.
	PhotoSeal string `json:"photo_seal,omitempty"`
}

// PassportRepository хранит паспорта, журнал доступа к ним и два флага
// пользователя, которые от паспорта зависят: «проверенный» и согласие на
// обработку персональных данных.
type PassportRepository interface {
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
	Get(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error)
	// Save записывает данные паспорта; фото, если оно было, остаётся.
	Save(ctx context.Context, q Querier, rec *PassportRecord) error
	// SetPhoto ставит фото вместе с вердиктом о его происхождении и возвращает
	// прежнее — его файл надо удалить.
	SetPhoto(ctx context.Context, q Querier, userID uuid.UUID, photo PassportPhoto) (previous *string, err error)
	// Delete удаляет паспорт и возвращает удалённую строку — ради файла фото.
	Delete(ctx context.Context, q Querier, userID uuid.UUID) (*PassportRecord, error)
	LogAccess(ctx context.Context, q Querier, userID, viewerID uuid.UUID, action string) error
	SetChecked(ctx context.Context, q Querier, userID uuid.UUID, checked bool, by uuid.UUID) error
	// RequestCheck ставит заявку на подтверждение «проверенного», если её ещё
	// нет и человек не проверен. Повторная заявка не сдвигает дату: в очереди
	// модерации порядок определяет первая просьба.
	RequestCheck(ctx context.Context, q Querier, userID uuid.UUID) error
	// CheckRequestedAt — когда просили подтвердить; nil, если не просили.
	CheckRequestedAt(ctx context.Context, q Querier, userID uuid.UUID) (*time.Time, error)
	// CheckRequests — очередь заявок, самая давняя первой.
	CheckRequests(ctx context.Context, q Querier, limit int) ([]CheckRequest, error)
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
	var seal, mark sql.NullString
	err := r.exec(q).QueryRowContext(ctx, `
		SELECT user_id, data_enc, photo_path, source, entered_by, key_version, created_at, updated_at,
		       photo_seal, photo_mark, photo_taken_at
		FROM user_passports WHERE user_id = $1`, userID).
		Scan(&rec.UserID, &rec.DataEnc, &rec.PhotoPath, &rec.Source, &rec.EnteredBy, &rec.KeyVersion,
			&rec.CreatedAt, &rec.UpdatedAt, &seal, &mark, &rec.Photo.TakenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPassportNotFound
	}
	rec.Photo.Seal, rec.Photo.Mark = seal.String, mark.String
	if rec.PhotoPath != nil {
		rec.Photo.Path = *rec.PhotoPath
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

func (r *passportRepo) SetPhoto(ctx context.Context, q Querier, userID uuid.UUID, photo PassportPhoto) (*string, error) {
	var previous *string
	err := r.exec(q).QueryRowContext(ctx, `
		UPDATE user_passports p
		SET photo_path = $2, photo_seal = $3, photo_mark = $4, photo_taken_at = $5, updated_at = now()
		FROM (SELECT photo_path FROM user_passports WHERE user_id = $1 FOR UPDATE) old
		WHERE p.user_id = $1
		RETURNING old.photo_path`, userID, photo.Path, photo.Seal, photo.Mark, photo.TakenAt).Scan(&previous)
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
	// Решение закрывает заявку в обе стороны: и отметка, и отказ снимают её с
	// очереди модерации.
	query := `UPDATE users SET is_checked = TRUE, checked_at = now(), checked_by = $2, check_requested_at = NULL WHERE id = $1`
	args := []interface{}{userID, by}
	if !checked {
		query = `UPDATE users SET is_checked = FALSE, checked_at = NULL, checked_by = NULL, check_requested_at = NULL WHERE id = $1`
		args = args[:1]
	}
	return execExpectingOne(ctx, r.exec(q), query, args...)
}

func (r *passportRepo) RequestCheck(ctx context.Context, q Querier, userID uuid.UUID) error {
	_, err := r.exec(q).ExecContext(ctx,
		`UPDATE users SET check_requested_at = now()
		 WHERE id = $1 AND is_checked = FALSE AND check_requested_at IS NULL`, userID)
	return err
}

func (r *passportRepo) CheckRequestedAt(ctx context.Context, q Querier, userID uuid.UUID) (*time.Time, error) {
	var at *time.Time
	err := r.exec(q).QueryRowContext(ctx, `SELECT check_requested_at FROM users WHERE id = $1`, userID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return at, err
}

func (r *passportRepo) CheckRequests(ctx context.Context, q Querier, limit int) ([]CheckRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := r.exec(q).QueryContext(ctx, `
		SELECT u.id, u.phone, TRIM(CONCAT_WS(' ', u.last_name, u.first_name, u.patronymic)),
		       u.role::text, u.is_verified, u.check_requested_at,
		       p.photo_path IS NOT NULL, COALESCE(p.photo_seal, '')
		FROM users u
		LEFT JOIN user_passports p ON p.user_id = u.id
		WHERE u.check_requested_at IS NOT NULL AND u.is_checked = FALSE
		ORDER BY u.check_requested_at
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckRequest{}
	for rows.Next() {
		var item CheckRequest
		if err := rows.Scan(&item.UserID, &item.Phone, &item.Name, &item.Role, &item.Verified,
			&item.RequestedAt, &item.HasPhoto, &item.PhotoSeal); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *passportRepo) AcceptPDConsent(ctx context.Context, userID uuid.UUID, version int) error {
	return execExpectingOne(ctx, r.db,
		`UPDATE users SET pd_consent_version = $2, pd_consent_at = now() WHERE id = $1`, userID, version)
}
