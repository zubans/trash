package repository

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// ShiftStatus represents the status of an executor shift.
type ShiftStatus string

const (
	ShiftStatusActive    ShiftStatus = "ACTIVE"
	ShiftStatusCompleted ShiftStatus = "COMPLETED"
	ShiftStatusPenalized ShiftStatus = "PENALIZED"
)

// Shift represents an executor work shift.
type Shift struct {
	ID            uuid.UUID    `json:"id"`
	ExecutorID    uuid.UUID    `json:"executor_id"`
	DurationHours int          `json:"duration_hours"`
	StartedAt     time.Time    `json:"started_at"`
	PlannedEndAt  time.Time    `json:"planned_end_at"`
	ActualEndAt   *time.Time   `json:"actual_end_at"`
	Status        ShiftStatus  `json:"status"`
	FineAmount    money.Amount `json:"fine_amount"`
}

// GPSLog represents a single recorded coordinate.
type GPSLog struct {
	ID         uuid.UUID
	ShiftID    uuid.UUID
	Latitude   float64
	Longitude  float64
	IsInside   bool
	RecordedAt time.Time
}

// ShiftRepository defines storage operations for shifts and GPS logs.
type ShiftRepository interface {
	Create(shift *Shift) error
	GetActiveShift(executorID uuid.UUID) (*Shift, error)
	GetShiftByID(shiftID uuid.UUID) (*Shift, error)
	GetActiveShifts() ([]*Shift, error)
	End(shiftID uuid.UUID) error
	Penalize(shiftID uuid.UUID, fine money.Amount) error

	// EarlyEnd terminates a shift before its planned end time, records the
	// penalty amount and marks the shift as PENALIZED.
	EarlyEnd(shiftID uuid.UUID, fine money.Amount) error

	// GetLastShiftByExecutor returns the most recent shift for an executor,
	// regardless of status (active, completed or penalized).
	GetLastShiftByExecutor(executorID uuid.UUID) (*Shift, error)

	StartShift(executorID uuid.UUID, durationHours int) (*Shift, error)
	AddGPSLog(shiftID uuid.UUID, lat, lon float64, isInside bool) error
	GetLastGPSLogs(shiftID uuid.UUID, count int) ([]bool, error)
}

// shiftRepo implements ShiftRepository using *sql.DB.
type shiftRepo struct {
	db *sql.DB
}

// NewShiftRepository creates a new ShiftRepository.
func NewShiftRepository(db *sql.DB) ShiftRepository {
	return &shiftRepo{db: db}
}

func scanShiftRow(row *sql.Row) (Shift, error) {
	var s Shift
	err := row.Scan(
		&s.ID, &s.ExecutorID, &s.DurationHours, &s.StartedAt, &s.PlannedEndAt, &s.ActualEndAt, &s.Status, &s.FineAmount,
	)
	return s, err
}

func scanShiftRows(rows *sql.Rows) (Shift, error) {
	var s Shift
	err := rows.Scan(
		&s.ID, &s.ExecutorID, &s.DurationHours, &s.StartedAt, &s.PlannedEndAt, &s.ActualEndAt, &s.Status, &s.FineAmount,
	)
	return s, err
}

func (r *shiftRepo) Create(shift *Shift) error {
	_, err := r.db.Exec(
		`INSERT INTO shifts (id, executor_id, duration_hours, started_at, planned_end_at, status, fine_amount)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		shift.ID, shift.ExecutorID, shift.DurationHours, shift.StartedAt, shift.PlannedEndAt, shift.Status, shift.FineAmount,
	)
	return err
}

// findActiveByExecutor is the implementation behind GetActiveShift.
func (r *shiftRepo) findActiveByExecutor(executorID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRow(
		`SELECT id, executor_id, duration_hours, started_at, planned_end_at, actual_end_at, status, fine_amount
		 FROM shifts WHERE executor_id = $1 AND status = $2`,
		executorID, ShiftStatusActive,
	)
	s, err := scanShiftRow(row)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *shiftRepo) GetActiveShift(executorID uuid.UUID) (*Shift, error) {
	return r.findActiveByExecutor(executorID)
}

func (r *shiftRepo) GetShiftByID(shiftID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRow(
		`SELECT id, executor_id, duration_hours, started_at, planned_end_at, actual_end_at, status, fine_amount
		 FROM shifts WHERE id = $1`,
		shiftID,
	)
	s, err := scanShiftRow(row)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *shiftRepo) GetActiveShifts() ([]*Shift, error) {
	rows, err := r.db.Query(
		`SELECT id, executor_id, duration_hours, started_at, planned_end_at, actual_end_at, status, fine_amount
		 FROM shifts WHERE status = $1`,
		ShiftStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shifts []*Shift
	for rows.Next() {
		s, err := scanShiftRows(rows)
		if err != nil {
			return nil, err
		}
		shifts = append(shifts, &s)
	}
	return shifts, rows.Err()
}

func (r *shiftRepo) End(shiftID uuid.UUID) error {
	_, err := r.db.Exec(
		`UPDATE shifts SET status = $1, actual_end_at = now() WHERE id = $2`,
		ShiftStatusCompleted, shiftID,
	)
	return err
}

func (r *shiftRepo) Penalize(shiftID uuid.UUID, fine money.Amount) error {
	_, err := r.db.Exec(
		`UPDATE shifts SET status = $1, fine_amount = fine_amount + $2 WHERE id = $3`,
		ShiftStatusPenalized, fine, shiftID,
	)
	return err
}

// EarlyEnd terminates a shift before its planned end time and records the fine.
func (r *shiftRepo) EarlyEnd(shiftID uuid.UUID, fine money.Amount) error {
	_, err := r.db.Exec(
		`UPDATE shifts SET status = $1, actual_end_at = now(), fine_amount = fine_amount + $2 WHERE id = $3`,
		ShiftStatusPenalized, fine, shiftID,
	)
	return err
}

// GetLastShiftByExecutor returns the most recent shift for an executor,
// regardless of status.
func (r *shiftRepo) GetLastShiftByExecutor(executorID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRow(
		`SELECT id, executor_id, duration_hours, started_at, planned_end_at, actual_end_at, status, fine_amount
		 FROM shifts WHERE executor_id = $1
		 ORDER BY started_at DESC
		 LIMIT 1`,
		executorID,
	)
	s, err := scanShiftRow(row)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *shiftRepo) saveGPSLog(log *GPSLog) error {
	_, err := r.db.Exec(
		`INSERT INTO shift_gps_logs (id, shift_id, latitude, longitude, is_inside, recorded_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		log.ID, log.ShiftID, log.Latitude, log.Longitude, log.IsInside, log.RecordedAt,
	)
	return err
}

// StartShift creates and persists a new active shift.
func (r *shiftRepo) StartShift(executorID uuid.UUID, durationHours int) (*Shift, error) {
	now := time.Now()
	shift := &Shift{
		ID:            uuid.New(),
		ExecutorID:    executorID,
		DurationHours: durationHours,
		StartedAt:     now,
		PlannedEndAt:  now.Add(time.Duration(durationHours) * time.Hour),
		Status:        ShiftStatusActive,
	}
	if err := r.Create(shift); err != nil {
		return nil, err
	}
	return shift, nil
}

// AddGPSLog records a coordinate and whether it was inside the geozone.
func (r *shiftRepo) AddGPSLog(shiftID uuid.UUID, lat, lon float64, isInside bool) error {
	log := &GPSLog{
		ID:         uuid.New(),
		ShiftID:    shiftID,
		Latitude:   lat,
		Longitude:  lon,
		IsInside:   isInside,
		RecordedAt: time.Now(),
	}
	return r.saveGPSLog(log)
}

// GetLastGPSLogs returns recent inside/outside flags for a shift.
func (r *shiftRepo) GetLastGPSLogs(shiftID uuid.UUID, count int) ([]bool, error) {
	rows, err := r.db.Query(
		`SELECT is_inside FROM shift_gps_logs WHERE shift_id = $1 ORDER BY recorded_at DESC LIMIT $2`,
		shiftID, count,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []bool
	for rows.Next() {
		var isInside bool
		if err := rows.Scan(&isInside); err != nil {
			return nil, err
		}
		logs = append(logs, isInside)
	}
	return logs, rows.Err()
}
