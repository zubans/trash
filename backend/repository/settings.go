package repository

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// SettingsRepository описывает операции с базой для системных настроек.
type SettingsRepository interface {
	GetSettings(ctx context.Context) (map[string]string, error)
	UpdateSettings(ctx context.Context, settings map[string]string) error
}

type settingsRepo struct {
	db *sql.DB
}

// NewSettingsRepository создаёт репозиторий для операций с настройками.
func NewSettingsRepository(db *sql.DB) SettingsRepository {
	return &settingsRepo{db: db}
}

func (r *settingsRepo) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key string
		var value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value.String
	}
	return settings, rows.Err()
}

// UpdateSettings пишет все пары одним оператором: он атомарен сам по себе,
// поэтому транзакция вокруг построчных INSERT'ов больше не нужна.
func (r *settingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}
	keys := make([]string, 0, len(settings))
	values := make([]string, 0, len(settings))
	for k, v := range settings {
		keys = append(keys, k)
		values = append(values, v)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO system_settings (key, value)
		SELECT * FROM unnest($1::text[], $2::text[])
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value`, pq.Array(keys), pq.Array(values))
	return err
}
