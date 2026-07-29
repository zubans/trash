package repository

import (
	"database/sql"
)

// SettingsRepository defines database operations for system settings.
type SettingsRepository interface {
	GetSettings() (map[string]string, error)
	UpdateSettings(settings map[string]string) error
}

type settingsRepo struct {
	db *sql.DB
}

// NewSettingsRepository creates a repository for settings operations.
func NewSettingsRepository(db *sql.DB) SettingsRepository {
	return &settingsRepo{db: db}
}

func (r *settingsRepo) GetSettings() (map[string]string, error) {
	rows, err := r.db.Query(`SELECT key, value FROM system_settings`)
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

func (r *settingsRepo) UpdateSettings(settings map[string]string) error {
	tx, err := r.db.Begin()
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
		_, err := tx.Exec(query, k, v)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
