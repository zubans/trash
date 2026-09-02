package repository

import (
	"context"
	"database/sql"
)

// CachedGeocode — разрешённый адрес, прочитанный из таблицы geocoding_cache.
type CachedGeocode struct {
	Address string
	Lat     float64
	Lon     float64
}

// GeocodeCacheRepository хранит пары «разрешённый адрес → координаты», чтобы
// повторное разрешение того же адреса не обращалось снова к платному
// провайдеру. Сюда пишет только запасной путь разрешения; адреса, выбранные из
// списка подсказок, уже несут свои координаты и в разрешении не нуждаются.
type GeocodeCacheRepository interface {
	// Lookup возвращает закэшированный результат для точного запроса или (nil, nil) при промахе.
	Lookup(ctx context.Context, query string) (*CachedGeocode, error)
	// Save вставляет или обновляет результат для запроса.
	Save(ctx context.Context, query, address string, lat, lon float64) error
}

type geocodeCacheRepo struct{ db *sql.DB }

// NewGeocodeCacheRepository собирает GeocodeCacheRepository поверх Postgres.
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
