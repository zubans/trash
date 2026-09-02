package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// AppRelease представляет релиз мобильного приложения.
type AppRelease struct {
	ID           uuid.UUID `json:"id"`
	Platform     string    `json:"platform"`
	VersionName  string    `json:"version_name"`
	VersionCode  int       `json:"version_code"`
	FileName     string    `json:"file_name"`
	FilePath     string    `json:"file_path"`
	ReleaseNotes string    `json:"release_notes,omitempty"`
	IsActive     bool      `json:"is_active"`
	ForceUpdate  bool      `json:"force_update"`
	CreatedAt    time.Time `json:"created_at"`
}

// AppReleaseRepository описывает операции хранения релизов мобильного приложения.
type AppReleaseRepository interface {
	GetActiveRelease(ctx context.Context, platform string) (*AppRelease, error)
	GetReleaseByVersionCode(ctx context.Context, platform string, versionCode int) (*AppRelease, error)
	GetNextVersionCode(ctx context.Context, platform string) (int, error)
	CreateRelease(ctx context.Context, release *AppRelease) error
	DeactivateOldReleases(ctx context.Context, platform string, excludeID uuid.UUID) error
}

type appReleaseRepo struct {
	db *sql.DB
}

// NewAppReleaseRepository создаёт новый AppReleaseRepository.
func NewAppReleaseRepository(db *sql.DB) AppReleaseRepository {
	return &appReleaseRepo{db: db}
}

func (r *appReleaseRepo) GetActiveRelease(ctx context.Context, platform string) (*AppRelease, error) {
	var release AppRelease
	query := `
		SELECT id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at
		FROM mobile_app_releases
		WHERE platform = $1 AND is_active = TRUE
		ORDER BY version_code DESC
		LIMIT 1`
	err := r.db.QueryRowContext(ctx, query, platform).Scan(
		&release.ID, &release.Platform, &release.VersionName, &release.VersionCode,
		&release.FileName, &release.FilePath, &release.ReleaseNotes, &release.IsActive,
		&release.ForceUpdate, &release.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &release, nil
}

func (r *appReleaseRepo) GetReleaseByVersionCode(ctx context.Context, platform string, versionCode int) (*AppRelease, error) {
	var release AppRelease
	query := `
		SELECT id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at
		FROM mobile_app_releases
		WHERE platform = $1 AND version_code = $2
		LIMIT 1`
	err := r.db.QueryRowContext(ctx, query, platform, versionCode).Scan(
		&release.ID, &release.Platform, &release.VersionName, &release.VersionCode,
		&release.FileName, &release.FilePath, &release.ReleaseNotes, &release.IsActive,
		&release.ForceUpdate, &release.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &release, nil
}

func (r *appReleaseRepo) GetNextVersionCode(ctx context.Context, platform string) (int, error) {
	var maxCode sql.NullInt32
	err := r.db.QueryRowContext(ctx,
		`SELECT MAX(version_code) FROM mobile_app_releases WHERE platform = $1`,
		platform,
	).Scan(&maxCode)
	if err != nil {
		return 0, err
	}
	if !maxCode.Valid {
		return 1, nil
	}
	return int(maxCode.Int32) + 1, nil
}

func (r *appReleaseRepo) CreateRelease(ctx context.Context, release *AppRelease) error {
	if release.ID == uuid.Nil {
		release.ID = uuid.New()
	}
	release.CreatedAt = time.Now()
	release.IsActive = true

	query := `
		INSERT INTO mobile_app_releases (id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.ExecContext(ctx, query,
		release.ID, release.Platform, release.VersionName, release.VersionCode,
		release.FileName, release.FilePath, release.ReleaseNotes, release.IsActive,
		release.ForceUpdate, release.CreatedAt,
	)
	return err
}

func (r *appReleaseRepo) DeactivateOldReleases(ctx context.Context, platform string, excludeID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mobile_app_releases SET is_active = FALSE WHERE platform = $1 AND id <> $2`,
		platform, excludeID,
	)
	return err
}
