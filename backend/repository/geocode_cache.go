package repository

import (
	"context"
	"database/sql"
)

// CachedGeocode is a resolved address read back from the geocoding_cache table.
type CachedGeocode struct {
	Address string
	Lat     float64
	Lon     float64
}

// GeocodeCacheRepository stores resolved address→coordinate pairs so that
// resolving the same address twice does not call the paid address provider
// again. Only the fallback resolve path writes here; addresses picked from the
// suggestion box already carry their coordinates and never need resolving.
type GeocodeCacheRepository interface {
	// Lookup returns the cached result for an exact query, or (nil, nil) on miss.
	Lookup(ctx context.Context, query string) (*CachedGeocode, error)
	// Save upserts the result for a query.
	Save(ctx context.Context, query, address string, lat, lon float64) error
}

type geocodeCacheRepo struct{ db *sql.DB }

// NewGeocodeCacheRepository builds a GeocodeCacheRepository backed by Postgres.
func NewGeocodeCacheRepository(db *sql.DB) GeocodeCacheRepository {
	return &geocodeCacheRepo{db: db}
}

func (r *geocodeCacheRepo) Lookup(ctx context.Context, query string) (*CachedGeocode, error) {
	if r.db == nil {
		return nil, nil
	}
	var c CachedGeocode
	err := r.db.QueryRowContext(ctx,
		`SELECT address, lat, lon FROM geocoding_cache WHERE query = $1`, query,
	).Scan(&c.Address, &c.Lat, &c.Lon)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *geocodeCacheRepo) Save(ctx context.Context, query, address string, lat, lon float64) error {
	if r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO geocoding_cache (query, address, lat, lon)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (query) DO UPDATE
		     SET address = EXCLUDED.address, lat = EXCLUDED.lat, lon = EXCLUDED.lon`,
		query, address, lat, lon,
	)
	return err
}
