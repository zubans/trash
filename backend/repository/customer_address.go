package repository

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// MaxCustomerAddresses is the number of addresses a customer may keep. The
// profile page has always shown "x/2"; the limit now exists on the server too.
const MaxCustomerAddresses = 2

// ErrAddressLimitReached is returned when a customer already has the maximum.
var ErrAddressLimitReached = errors.New("address limit reached")

// ErrAddressNotFound is returned for an address that is not the caller's.
var ErrAddressNotFound = errors.New("address not found")

// CustomerAddress is one saved pickup address, kept as its parts.
//
// Address remains the display line so that clients reading only that keep
// working; the parts beside it are what everything new uses. Rows saved before
// the parts existed carry them empty, and the service layer recovers them from
// the line on read.
type CustomerAddress struct {
	ID        uuid.UUID `json:"id"`
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

// addressColumns lists the stored columns once, so the SELECTs and the scan
// cannot drift apart.
const addressColumns = `id, address, is_default,
	COALESCE(region, ''), COALESCE(city, ''), COALESCE(street, ''),
	COALESCE(house, ''), COALESCE(flat, ''), COALESCE(fias_id, ''),
	geo_lat, geo_lon, COALESCE(source, '')`

// scanAddresses reads a result set of addressColumns.
func scanAddresses(rows *sql.Rows) ([]CustomerAddress, error) {
	addresses := make([]CustomerAddress, 0, MaxCustomerAddresses)
	for rows.Next() {
		var a CustomerAddress
		if err := rows.Scan(
			&a.ID, &a.Address, &a.IsDefault,
			&a.Region, &a.City, &a.Street, &a.House, &a.Flat, &a.FiasID,
			&a.Lat, &a.Lon, &a.Source,
		); err != nil {
			return nil, err
		}
		addresses = append(addresses, a)
	}
	return addresses, rows.Err()
}

// CustomerAddressRepository stores the addresses a customer orders from.
type CustomerAddressRepository interface {
	List(userID uuid.UUID) ([]CustomerAddress, error)
	Add(userID uuid.UUID, address CustomerAddress) ([]CustomerAddress, error)
	Delete(userID, addressID uuid.UUID) ([]CustomerAddress, error)
	SetDefault(userID, addressID uuid.UUID) ([]CustomerAddress, error)
	// SetDefaultByValue keeps the older clients working: they identify an
	// address by its text rather than by id.
	SetDefaultByValue(userID uuid.UUID, address string) ([]CustomerAddress, error)
	Default(userID uuid.UUID) (string, error)
}

type customerAddressRepo struct {
	db *sql.DB
}

// NewCustomerAddressRepository creates a CustomerAddressRepository.
func NewCustomerAddressRepository(db *sql.DB) CustomerAddressRepository {
	return &customerAddressRepo{db: db}
}

// List returns the addresses with the default one first, then oldest to newest,
// so the order the client renders is stable.
func (r *customerAddressRepo) List(userID uuid.UUID) ([]CustomerAddress, error) {
	rows, err := r.db.Query(
		`SELECT `+addressColumns+` FROM customer_addresses
		 WHERE user_id = $1 ORDER BY is_default DESC, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAddresses(rows)
}

// Add saves an address, making it the default when it is the first one.
func (r *customerAddressRepo) Add(userID uuid.UUID, address CustomerAddress) ([]CustomerAddress, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM customer_addresses WHERE user_id = $1`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxCustomerAddresses {
		return nil, ErrAddressLimitReached
	}

	if _, err := tx.Exec(
		`INSERT INTO customer_addresses
		     (user_id, address, is_default, region, city, street, house, flat, fias_id, geo_lat, geo_lon, source)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (user_id, address) DO UPDATE SET
		     region = EXCLUDED.region, city = EXCLUDED.city, street = EXCLUDED.street,
		     house = EXCLUDED.house, flat = EXCLUDED.flat, fias_id = EXCLUDED.fias_id,
		     geo_lat = EXCLUDED.geo_lat, geo_lon = EXCLUDED.geo_lon, source = EXCLUDED.source`,
		userID, address.Address, count == 0,
		address.Region, address.City, address.Street, address.House, address.Flat, address.FiasID,
		address.Lat, address.Lon, address.Source,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(userID)
}

// Delete removes an address. If it was the default, the oldest remaining one
// takes over, so a customer is never left without a default while having
// addresses.
func (r *customerAddressRepo) Delete(userID, addressID uuid.UUID) ([]CustomerAddress, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var wasDefault bool
	err = tx.QueryRow(
		`DELETE FROM customer_addresses WHERE id = $1 AND user_id = $2 RETURNING is_default`,
		addressID, userID).Scan(&wasDefault)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAddressNotFound
		}
		return nil, err
	}

	if wasDefault {
		if _, err := tx.Exec(
			`UPDATE customer_addresses SET is_default = TRUE
			 WHERE id = (SELECT id FROM customer_addresses WHERE user_id = $1 ORDER BY created_at LIMIT 1)`,
			userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.List(userID)
}

func (r *customerAddressRepo) SetDefault(userID, addressID uuid.UUID) ([]CustomerAddress, error) {
	return r.setDefault(userID, `id = $2`, addressID)
}

func (r *customerAddressRepo) SetDefaultByValue(userID uuid.UUID, address string) ([]CustomerAddress, error) {
	return r.setDefault(userID, `address = $2`, address)
}

// setDefault clears the previous default and sets the new one in a single
// transaction; the partial unique index would reject an overlap otherwise.
func (r *customerAddressRepo) setDefault(userID uuid.UUID, match string, arg interface{}) ([]CustomerAddress, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE customer_addresses SET is_default = FALSE WHERE user_id = $1 AND is_default`, userID); err != nil {
		return nil, err
	}
	res, err := tx.Exec(
		`UPDATE customer_addresses SET is_default = TRUE WHERE user_id = $1 AND `+match, userID, arg)
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
	return r.List(userID)
}

// Default returns the customer's default address, or an empty string.
func (r *customerAddressRepo) Default(userID uuid.UUID) (string, error) {
	var address string
	err := r.db.QueryRow(
		`SELECT address FROM customer_addresses WHERE user_id = $1 AND is_default`, userID).Scan(&address)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return address, nil
}
