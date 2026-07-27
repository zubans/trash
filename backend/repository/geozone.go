package repository

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// GeozoneType describes the shape of a working area.
type GeozoneType string

const (
	GeozoneTypeCircle  GeozoneType = "CIRCLE"
	GeozoneTypePolygon GeozoneType = "POLYGON"
)

// Point represents a geographic coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Geozone represents an executor working area.
type Geozone struct {
	ID              int
	Name            string
	Type            string
	CenterLatitude  *float64
	CenterLongitude *float64
	RadiusMeters    *float64
	Coordinates     *string
	Polygon         []Point
}

// GeozoneRepository defines storage operations for geozones.
type GeozoneRepository interface {
	FindByID(id int) (*Geozone, error)
	FindByExecutor(userID uuid.UUID) (*Geozone, error)
}

// geozoneRepo implements GeozoneRepository using *sql.DB.
type geozoneRepo struct {
	db *sql.DB
}

// NewGeozoneRepository creates a new GeozoneRepository.
func NewGeozoneRepository(db *sql.DB) GeozoneRepository {
	return &geozoneRepo{db: db}
}

func (r *geozoneRepo) FindByID(id int) (*Geozone, error) {
	var g Geozone
	var coordinatesRaw []byte
	err := r.db.QueryRow(
		`SELECT id, name, type, center_latitude, center_longitude, radius_meters, coordinates FROM geozones WHERE id = $1`, id,
	).Scan(&g.ID, &g.Name, &g.Type, &g.CenterLatitude, &g.CenterLongitude, &g.RadiusMeters, &coordinatesRaw)
	if err != nil {
		return nil, err
	}
	if len(coordinatesRaw) > 0 {
		s := string(coordinatesRaw)
		g.Coordinates = &s
		if err := json.Unmarshal(coordinatesRaw, &g.Polygon); err != nil {
			return nil, err
		}
	}
	return &g, nil
}

func (r *geozoneRepo) FindByExecutor(userID uuid.UUID) (*Geozone, error) {
	var geozoneID *int
	err := r.db.QueryRow(
		`SELECT work_area_id FROM executor_profiles WHERE user_id = $1`, userID,
	).Scan(&geozoneID)
	if err != nil {
		return nil, err
	}
	if geozoneID == nil {
		return nil, errors.New("executor has no geozone")
	}
	return r.FindByID(*geozoneID)
}
