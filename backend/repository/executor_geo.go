package repository

import (
	"context"
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
	// CategoryName is the service variant's parent category, resolved so the map
	// can show "category · service" without the client walking the catalog tree.
	CategoryName string `json:"category_name,omitempty"`
}

type ExecutorGeoRepository interface {
	UpdateExecutorLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64, isManual bool) error
	GetExecutorLocation(ctx context.Context, executorID uuid.UUID) (lat *float64, lon *float64, lastManual *time.Time, err error)
	// RecordDevicePosition stores the fix the executor's phone reported.
	//
	// It always saves the device position, and moves the working anchor with it
	// only while the executor has not chosen a district by hand. Once they have,
	// the anchor stays where they put it until they ask to follow the device
	// again — a periodic report must not undo a deliberate choice.
	RecordDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error
	// GetDevicePosition returns the last position the executor's phone reported.
	GetDevicePosition(ctx context.Context, executorID uuid.UUID) (*ExecutorPosition, error)
	// FollowDevicePosition moves the working anchor onto a device fix and drops
	// the manual override, so automatic reports resume moving the anchor.
	FollowDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error
	// GetExecutorLocations resolves the stored positions of several executors
	// in one query, for the matching worker: it compares every candidate
	// against every waiting order, and asking per candidate made the cost of a
	// cycle the product of the two.
	//
	// Executors with no stored position are absent from the result, which
	// callers must read as "position unknown" — never as a free pass.
	GetExecutorLocations(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]ExecutorPosition, error)
	CreateGeoAlert(ctx context.Context, alert *GeoAlert) error
	GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]GeoAlert, error)
}

// ExecutorPosition is an executor's stored working position.
type ExecutorPosition struct {
	Lat float64
	Lon float64
}

type executorGeoRepository struct {
	db *sql.DB
}

func NewExecutorGeoRepository(db *sql.DB) ExecutorGeoRepository {
	return &executorGeoRepository{db: db}
}

func (r *executorGeoRepository) UpdateExecutorLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64, isManual bool) error {
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
		_, err := r.db.ExecContext(ctx, query, lat, lon, now, executorID)
		return err
	}
	query := `
		INSERT INTO executor_profiles (user_id, full_name, current_lat, current_lon)
		VALUES ($3, 'Исполнитель', $1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET current_lat = EXCLUDED.current_lat,
		    current_lon = EXCLUDED.current_lon
	`
	_, err := r.db.ExecContext(ctx, query, lat, lon, executorID)
	return err
}

func (r *executorGeoRepository) RecordDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	// One statement so the device fix and the conditional anchor move cannot
	// disagree: the anchor follows only while no manual choice is on record.
	query := `
		INSERT INTO executor_profiles (user_id, full_name, device_lat, device_lon, device_reported_at, current_lat, current_lon)
		VALUES ($3, 'Исполнитель', $1, $2, $4, $1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET device_lat = EXCLUDED.device_lat,
		    device_lon = EXCLUDED.device_lon,
		    device_reported_at = EXCLUDED.device_reported_at,
		    current_lat = CASE
		        WHEN executor_profiles.last_manual_location_change_at IS NULL
		        THEN EXCLUDED.current_lat ELSE executor_profiles.current_lat END,
		    current_lon = CASE
		        WHEN executor_profiles.last_manual_location_change_at IS NULL
		        THEN EXCLUDED.current_lon ELSE executor_profiles.current_lon END
	`
	_, err := r.db.ExecContext(ctx, query, lat, lon, executorID, time.Now())
	return err
}

func (r *executorGeoRepository) GetDevicePosition(ctx context.Context, executorID uuid.UUID) (*ExecutorPosition, error) {
	var lat, lon sql.NullFloat64
	err := r.db.QueryRowContext(ctx,
		`SELECT device_lat, device_lon FROM executor_profiles WHERE user_id = $1`, executorID).
		Scan(&lat, &lon)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !lat.Valid || !lon.Valid {
		return nil, nil
	}
	return &ExecutorPosition{Lat: lat.Float64, Lon: lon.Float64}, nil
}

func (r *executorGeoRepository) FollowDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	// Clearing last_manual_location_change_at is the point: it both releases the
	// anchor back to the device and ends the district-change cooldown, because
	// returning to where you actually are is not a district change.
	query := `
		INSERT INTO executor_profiles (user_id, full_name, current_lat, current_lon, device_lat, device_lon, device_reported_at)
		VALUES ($3, 'Исполнитель', $1, $2, $1, $2, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET current_lat = EXCLUDED.current_lat,
		    current_lon = EXCLUDED.current_lon,
		    device_lat = EXCLUDED.device_lat,
		    device_lon = EXCLUDED.device_lon,
		    device_reported_at = EXCLUDED.device_reported_at,
		    last_manual_location_change_at = NULL
	`
	_, err := r.db.ExecContext(ctx, query, lat, lon, executorID, time.Now())
	return err
}

func (r *executorGeoRepository) GetExecutorLocations(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]ExecutorPosition, error) {
	positions := make(map[uuid.UUID]ExecutorPosition, len(executorIDs))
	placeholders, args := idList(executorIDs)
	if len(args) == 0 {
		return positions, nil
	}

	// Rows with a missing coordinate are filtered out in SQL rather than
	// returned as a partial position: half a coordinate pair is not a location,
	// and the callers treat an absent entry as "unknown" already.
	rows, err := r.db.QueryContext(ctx,
		`SELECT user_id, current_lat, current_lon FROM executor_profiles
		 WHERE user_id IN (`+placeholders+`)
		   AND current_lat IS NOT NULL AND current_lon IS NOT NULL`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var pos ExecutorPosition
		if err := rows.Scan(&id, &pos.Lat, &pos.Lon); err != nil {
			return nil, err
		}
		positions[id] = pos
	}
	return positions, rows.Err()
}

func (r *executorGeoRepository) GetExecutorLocation(ctx context.Context, executorID uuid.UUID) (lat *float64, lon *float64, lastManual *time.Time, err error) {
	var cLat, cLon sql.NullFloat64
	var lm sql.NullTime
	query := `SELECT current_lat, current_lon, last_manual_location_change_at FROM executor_profiles WHERE user_id = $1`
	err = r.db.QueryRowContext(ctx, query, executorID).Scan(&cLat, &cLon, &lm)
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

func (r *executorGeoRepository) CreateGeoAlert(ctx context.Context, alert *GeoAlert) error {
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
	_, err := r.db.ExecContext(ctx, query, alert.ID, alert.ExecutorID, alert.OldLat, alert.OldLon, alert.NewLat, alert.NewLon, alert.CalculatedSpeedKMH, alert.Status, alert.CreatedAt)
	return err
}

func (r *executorGeoRepository) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]GeoAlert, error) {
	query := `
		SELECT id, executor_id, old_lat, old_lon, new_lat, new_lon, calculated_speed_kmh, status, created_at
		FROM geo_alerts
		WHERE ($1 = '' OR status = $1)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, status, limit, offset)
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
