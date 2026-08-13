package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type GeoAlert struct {
	ID                 uuid.UUID `json:"id"`
	ExecutorID         uuid.UUID `json:"executor_id"`
	OldLat             *float64  `json:"old_lat,omitempty"`
	OldLon             *float64  `json:"old_lon,omitempty"`
	NewLat             float64   `json:"new_lat"`
	NewLon             float64   `json:"new_lon"`
	CalculatedSpeedKMH float64   `json:"calculated_speed_kmh"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
}

type MapOrder struct {
	Order
	CanAccept  bool    `json:"can_accept"`
	DistanceKM float64 `json:"distance_km"`
}

type ExecutorGeoRepository interface {
	UpdateExecutorLocation(executorID uuid.UUID, lat, lon float64, isManual bool) error
	GetExecutorLocation(executorID uuid.UUID) (lat *float64, lon *float64, lastManual *time.Time, err error)
	CreateGeoAlert(alert *GeoAlert) error
	GetGeoAlerts(status string, limit, offset int) ([]GeoAlert, error)
}

type executorGeoRepository struct {
	db *sql.DB
}

func NewExecutorGeoRepository(db *sql.DB) ExecutorGeoRepository {
	return &executorGeoRepository{db: db}
}

func (r *executorGeoRepository) UpdateExecutorLocation(executorID uuid.UUID, lat, lon float64, isManual bool) error {
	now := time.Now()
	if isManual {
		query := `
			INSERT INTO executor_profiles (user_id, full_name, current_lat, current_lon, last_manual_location_change_at)
			VALUES ($4, 'Исполнитель', $1, $2, $3)
			ON CONFLICT (user_id) DO UPDATE
			SET current_lat = EXCLUDED.current_lat,
			    current_lon = EXCLUDED.current_lon,
			    last_manual_location_change_at = EXCLUDED.last_manual_location_change_at
		`
		_, err := r.db.Exec(query, lat, lon, now, executorID)
		return err
	}
	query := `
		INSERT INTO executor_profiles (user_id, full_name, current_lat, current_lon)
		VALUES ($3, 'Исполнитель', $1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET current_lat = EXCLUDED.current_lat,
		    current_lon = EXCLUDED.current_lon
	`
	_, err := r.db.Exec(query, lat, lon, executorID)
	return err
}

func (r *executorGeoRepository) GetExecutorLocation(executorID uuid.UUID) (lat *float64, lon *float64, lastManual *time.Time, err error) {
	var cLat, cLon sql.NullFloat64
	var lm sql.NullTime
	query := `SELECT current_lat, current_lon, last_manual_location_change_at FROM executor_profiles WHERE user_id = $1`
	err = r.db.QueryRow(query, executorID).Scan(&cLat, &cLon, &lm)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, err
	}
	if cLat.Valid {
		l := cLat.Float64
		lat = &l
	}
	if cLon.Valid {
		l := cLon.Float64
		lon = &l
	}
	if lm.Valid {
		t := lm.Time
		lastManual = &t
	}
	return lat, lon, lastManual, nil
}

func (r *executorGeoRepository) CreateGeoAlert(alert *GeoAlert) error {
	if alert.ID == uuid.Nil {
		alert.ID = uuid.New()
	}
	alert.CreatedAt = time.Now()
	if alert.Status == "" {
		alert.Status = "PENDING"
	}
	query := `
		INSERT INTO geo_alerts (id, executor_id, old_lat, old_lon, new_lat, new_lon, calculated_speed_kmh, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(query, alert.ID, alert.ExecutorID, alert.OldLat, alert.OldLon, alert.NewLat, alert.NewLon, alert.CalculatedSpeedKMH, alert.Status, alert.CreatedAt)
	return err
}

func (r *executorGeoRepository) GetGeoAlerts(status string, limit, offset int) ([]GeoAlert, error) {
	query := `
		SELECT id, executor_id, old_lat, old_lon, new_lat, new_lon, calculated_speed_kmh, status, created_at
		FROM geo_alerts
		WHERE ($1 = '' OR status = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(query, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []GeoAlert
	for rows.Next() {
		var a GeoAlert
		var oldLat, oldLon sql.NullFloat64
		if err := rows.Scan(&a.ID, &a.ExecutorID, &oldLat, &oldLon, &a.NewLat, &a.NewLon, &a.CalculatedSpeedKMH, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		if oldLat.Valid {
			v := oldLat.Float64
			a.OldLat = &v
		}
		if oldLon.Valid {
			v := oldLon.Float64
			a.OldLon = &v
		}
		alerts = append(alerts, a)
	}
	return alerts, nil
}
