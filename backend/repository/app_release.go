package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// AppRelease represents a mobile application release.
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

// AppReleaseRepository defines storage operations for mobile app releases.
type AppReleaseRepository interface {
	GetActiveRelease(platform string) (*AppRelease, error)
	GetReleaseByVersionCode(platform string, versionCode int) (*AppRelease, error)
	GetNextVersionCode(platform string) (int, error)
	CreateRelease(release *AppRelease) error
	DeactivateOldReleases(platform string, excludeID uuid.UUID) error
}

type appReleaseRepo struct {
	db *sql.DB
}

// NewAppReleaseRepository creates a new AppReleaseRepository.
func NewAppReleaseRepository(db *sql.DB) AppReleaseRepository {
	return &appReleaseRepo{db: db}
}

func (r *appReleaseRepo) GetActiveRelease(platform string) (*AppRelease, error) {
	var release AppRelease
	query := `
		SELECT id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at
		FROM mobile_app_releases
		WHERE platform = $1 AND is_active = TRUE
		ORDER BY version_code DESC
		LIMIT 1`
	err := r.db.QueryRow(query, platform).Scan(
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

func (r *appReleaseRepo) GetReleaseByVersionCode(platform string, versionCode int) (*AppRelease, error) {
	var release AppRelease
	query := `
		SELECT id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at
		FROM mobile_app_releases
		WHERE platform = $1 AND version_code = $2
		LIMIT 1`
	err := r.db.QueryRow(query, platform, versionCode).Scan(
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

func (r *appReleaseRepo) GetNextVersionCode(platform string) (int, error) {
	var maxCode sql.NullInt32
	err := r.db.QueryRow(
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

func (r *appReleaseRepo) CreateRelease(release *AppRelease) error {
	if release.ID == uuid.Nil {
		release.ID = uuid.New()
	}
	release.CreatedAt = time.Now()
	release.IsActive = true

	query := `
		INSERT INTO mobile_app_releases (id, platform, version_name, version_code, file_name, file_path, release_notes, is_active, force_update, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(query,
		release.ID, release.Platform, release.VersionName, release.VersionCode,
		release.FileName, release.FilePath, release.ReleaseNotes, release.IsActive,
		release.ForceUpdate, release.CreatedAt,
	)
	return err
}

func (r *appReleaseRepo) DeactivateOldReleases(platform string, excludeID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE mobile_app_releases SET is_active = FALSE WHERE platform = $1 AND id <> $2`,
		platform, excludeID,
	)
	return err
}
