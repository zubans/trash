package photoproof

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// SymbolRepository хранит справочник жестов.
type SymbolRepository interface {
	// List отдаёт жесты в порядке показа. Удалённые — только по явной просьбе.
	List(ctx context.Context, includeDeleted bool) ([]Symbol, error)
	// Get отдаёт жест по id, включая удалённый: на него ссылаются старые заказы.
	Get(ctx context.Context, id uuid.UUID) (*Symbol, error)
	// Create заводит жест и присваивает ему постоянный номер.
	Create(ctx context.Context, s *Symbol) error
	// Update правит жест; номер не меняется никогда.
	Update(ctx context.Context, s *Symbol) error
	// Delete помечает жест удалённым, Restore возвращает.
	Delete(ctx context.Context, id uuid.UUID) error
	Restore(ctx context.Context, id uuid.UUID) error
	// PickRandomLive выбирает случайный действующий жест — для нового заказа.
	PickRandomLive(ctx context.Context, q Querier) (*Symbol, error)
	// ByIDs дополняет into жестами с перечисленными id, включая удалённые.
	ByIDs(ctx context.Context, ids []uuid.UUID, into map[uuid.UUID]Symbol) error
}

// Querier — то, чем модуль ходит в базу: пул или транзакция вызывающего.
// Повторяет repository.Querier, но не зависит от него: модуль держит свою
// границу.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type symbolRepo struct {
	db *sql.DB
}

// NewSymbolRepository создаёт SymbolRepository.
func NewSymbolRepository(db *sql.DB) SymbolRepository {
	return &symbolRepo{db: db}
}

func (r *symbolRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const symbolColumns = `id, code, number, title, description, COALESCE(hint_image_url, ''),
    fits_in_selfie, sort_order, deleted_at, created_at, updated_at`

func scanSymbol(row interface{ Scan(...interface{}) error }) (*Symbol, error) {
	var s Symbol
	if err := row.Scan(&s.ID, &s.Code, &s.Number, &s.Title, &s.Description, &s.HintImageURL,
		&s.FitsInSelfie, &s.SortOrder, &s.DeletedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *symbolRepo) List(ctx context.Context, includeDeleted bool) ([]Symbol, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+symbolColumns+` FROM watermark_symbols
		 WHERE ($1 OR deleted_at IS NULL)
		 ORDER BY sort_order, number`, includeDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	symbols := []Symbol{}
	for rows.Next() {
		s, err := scanSymbol(rows)
		if err != nil {
			return nil, err
		}
		symbols = append(symbols, *s)
	}
	return symbols, rows.Err()
}

func (r *symbolRepo) Get(ctx context.Context, id uuid.UUID) (*Symbol, error) {
	s, err := scanSymbol(r.db.QueryRowContext(ctx, `SELECT `+symbolColumns+` FROM watermark_symbols WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSymbolNotFound
	}
	return s, err
}

func (r *symbolRepo) Create(ctx context.Context, s *Symbol) error {
	// Номер выдаётся следующим за наибольшим когда-либо выданным, включая
	// номера удалённых жестов: старый снимок ссылается на номер, и повторно
	// выданный номер сделал бы его снимком другого жеста.
	err := r.db.QueryRowContext(ctx, `
        INSERT INTO watermark_symbols (code, number, title, description, hint_image_url, fits_in_selfie, sort_order)
        VALUES ($1, (SELECT COALESCE(MAX(number), 0) + 1 FROM watermark_symbols), $2, $3, NULLIF($4, ''), $5, $6)
        RETURNING `+symbolColumns,
		s.Code, s.Title, s.Description, s.HintImageURL, s.FitsInSelfie, s.SortOrder).
		Scan(&s.ID, &s.Code, &s.Number, &s.Title, &s.Description, &s.HintImageURL,
			&s.FitsInSelfie, &s.SortOrder, &s.DeletedAt, &s.CreatedAt, &s.UpdatedAt)
	return mapSymbolError(err)
}

func (r *symbolRepo) Update(ctx context.Context, s *Symbol) error {
	err := r.db.QueryRowContext(ctx, `
        UPDATE watermark_symbols
        SET code = $2, title = $3, description = $4, hint_image_url = NULLIF($5, ''),
            fits_in_selfie = $6, sort_order = $7, updated_at = now()
        WHERE id = $1
        RETURNING `+symbolColumns,
		s.ID, s.Code, s.Title, s.Description, s.HintImageURL, s.FitsInSelfie, s.SortOrder).
		Scan(&s.ID, &s.Code, &s.Number, &s.Title, &s.Description, &s.HintImageURL,
			&s.FitsInSelfie, &s.SortOrder, &s.DeletedAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSymbolNotFound
	}
	return mapSymbolError(err)
}

func (r *symbolRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.setDeleted(ctx, id, true)
}

func (r *symbolRepo) Restore(ctx context.Context, id uuid.UUID) error {
	return r.setDeleted(ctx, id, false)
}

func (r *symbolRepo) setDeleted(ctx context.Context, id uuid.UUID, deleted bool) error {
	query := `UPDATE watermark_symbols SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`
	if !deleted {
		query = `UPDATE watermark_symbols SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`
	}
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return mapSymbolError(err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSymbolNotFound
	}
	return nil
}

func (r *symbolRepo) PickRandomLive(ctx context.Context, q Querier) (*Symbol, error) {
	s, err := scanSymbol(r.exec(q).QueryRowContext(ctx,
		`SELECT `+symbolColumns+` FROM watermark_symbols WHERE deleted_at IS NULL ORDER BY random() LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSymbolsDefined
	}
	return s, err
}

// mapSymbolError переводит нарушение уникальности кода в понятную ошибку:
// частичный индекс сторожит только действующие жесты, поэтому код удалённого
// можно занять заново.
func mapSymbolError(err error) error {
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSymbolCodeTaken
	}
	return err
}
