package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// MaxUserAddresses — сколько адресов может хранить пользователь (заказчик или исполнитель).
const MaxUserAddresses = 2

// ErrAddressLimitReached возвращается, когда у пользователя уже достигнут максимум.
var ErrAddressLimitReached = errors.New("address limit reached")

// ErrAddressNotFound возвращается для адреса, которого не существует или который принадлежит не вызывающему.
var ErrAddressNotFound = errors.New("address not found")

// Address — один сохранённый адрес вместе с его структурными частями.
type Address struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Address   string    `json:"address"`
	IsDefault bool      `json:"is_default"`

	Region string `json:"region,omitempty"`
	City   string `json:"city,omitempty"`
	Street string `json:"street,omitempty"`
	House  string `json:"house,omitempty"`
	Flat   string `json:"flat,omitempty"`
	FiasID string `json:"fias_id,omitempty"`

	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`

	Source string `json:"source,omitempty"`
}

// AddressRepository хранит сохранённые адреса заказчиков и исполнителей.
type AddressRepository interface {
	List(ctx context.Context, userID uuid.UUID) ([]Address, error)
	Add(ctx context.Context, userID uuid.UUID, address Address) ([]Address, error)
	Delete(ctx context.Context, userID, addressID uuid.UUID) ([]Address, error)
	SetDefault(ctx context.Context, userID, addressID uuid.UUID) ([]Address, error)
	// SetDefaultByValue сохраняет работоспособность старых клиентов: они опознают
	// адрес по тексту, а не по id.
	SetDefaultByValue(ctx context.Context, userID uuid.UUID, address string) ([]Address, error)
}

type addressRepo struct {
	db *sql.DB
}

// NewAddressRepository создаёт AddressRepository.
func NewAddressRepository(db *sql.DB) AddressRepository {
	return &addressRepo{db: db}
}

const addressSelectCols = `id, user_id, address, is_default,
	COALESCE(region, ''), COALESCE(city, ''), COALESCE(street, ''),
	COALESCE(house, ''), COALESCE(flat, ''), COALESCE(fias_id, ''),
	geo_lat, geo_lon, COALESCE(source, '')`

func scanAddresses(rows *sql.Rows) ([]Address, error) {
	addresses := make([]Address, 0, MaxUserAddresses)
	for rows.Next() {
		var a Address
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Address, &a.IsDefault,
			&a.Region, &a.City, &a.Street, &a.House, &a.Flat, &a.FiasID,
			&a.Lat, &a.Lon, &a.Source,
		); err != nil {
			return nil, err
		}
		addresses = append(addresses, a)
	}
	return addresses, rows.Err()
}

// List возвращает адреса: сначала адрес по умолчанию, затем от старых к новым.
func (r *addressRepo) List(ctx context.Context, userID uuid.UUID) ([]Address, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+addressSelectCols+` FROM addresses
		 WHERE user_id = $1 ORDER BY is_default DESC, created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAddresses(rows)
}

// Add сохраняет адрес, делая его адресом по умолчанию, если он у пользователя
// первый или если вызывающий об этом попросил. Повторное сохранение уже
// имеющегося адреса — это обновление, а не новый адрес: оно освежает части и координаты.
func (r *addressRepo) Add(ctx context.Context, userID uuid.UUID, address Address) ([]Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Предел считает адреса, поэтому отклонять он может только по-настоящему
	// новый. Взимание его за обновление и мешало пользователю на пределе
	// пересохранить уже имеющийся адрес — например, чтобы прикрепить
	// координаты, приходящие с подсказкой.
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM addresses WHERE user_id = $1 AND address = $2)`,
		userID, address.Address).Scan(&exists); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM addresses WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if !exists && count >= MaxUserAddresses {
		return nil, ErrAddressLimitReached
	}

	isDefault := count == 0 || address.IsDefault
	if isDefault && count > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE addresses SET is_default = FALSE WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}

	// is_default объединяется по ИЛИ, а не перезаписывается: обновление, которое
	// не просит стать умолчанием, не должно сбрасывать флаг, иначе пользователь
	// останется с адресами, но вовсе без адреса по умолчанию.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO addresses
		     (user_id, address, is_default, region, city, street, house, flat, fias_id, geo_lat, geo_lon, source)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (user_id, address) DO UPDATE SET
		     is_default = addresses.is_default OR EXCLUDED.is_default,
		     region = EXCLUDED.region, city = EXCLUDED.city, street = EXCLUDED.street,
		     house = EXCLUDED.house, flat = EXCLUDED.flat, fias_id = EXCLUDED.fias_id,
		     geo_lat = EXCLUDED.geo_lat, geo_lon = EXCLUDED.geo_lon, source = EXCLUDED.source,
		     updated_at = now()`,
		userID, address.Address, isDefault,
		address.Region, address.City, address.Street, address.House, address.Flat, address.FiasID,
		address.Lat, address.Lon, address.Source,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(ctx, userID)
}

// Delete удаляет адрес. Если он был по умолчанию, им становится другой оставшийся.
func (r *addressRepo) Delete(ctx context.Context, userID, addressID uuid.UUID) ([]Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var wasDefault bool
	err = tx.QueryRowContext(ctx,
		`DELETE FROM addresses WHERE id = $1 AND user_id = $2 RETURNING is_default`,
		addressID, userID).Scan(&wasDefault)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAddressNotFound
		}
		return nil, err
	}

	if wasDefault {
		if _, err := tx.ExecContext(ctx,
			`UPDATE addresses SET is_default = TRUE
			 WHERE id = (SELECT id FROM addresses WHERE user_id = $1 ORDER BY created_at ASC LIMIT 1)`,
			userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(ctx, userID)
}

func (r *addressRepo) SetDefault(ctx context.Context, userID, addressID uuid.UUID) ([]Address, error) {
	return r.setDefault(ctx, userID, `id = $2`, addressID)
}

func (r *addressRepo) SetDefaultByValue(ctx context.Context, userID uuid.UUID, address string) ([]Address, error) {
	return r.setDefault(ctx, userID, `address = $2`, address)
}

func (r *addressRepo) setDefault(ctx context.Context, userID uuid.UUID, match string, arg interface{}) ([]Address, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE addresses SET is_default = FALSE WHERE user_id = $1 AND is_default`, userID); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE addresses SET is_default = TRUE, updated_at = now() WHERE user_id = $1 AND `+match, userID, arg)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrAddressNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(ctx, userID)
}

