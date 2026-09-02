package repository

import (
	"context"
	"database/sql"
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
	return settings, nil
}

func (r *settingsRepo) UpdateSettings(ctx context.Context, settings map[string]string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO system_settings (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value`

	for k, v := range settings {
		_, err := tx.ExecContext(ctx, query, k, v)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
