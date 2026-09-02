package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// ShiftStatus представляет статус смены исполнителя.
type ShiftStatus string

const (
	ShiftStatusActive    ShiftStatus = "ACTIVE"
	ShiftStatusCompleted ShiftStatus = "COMPLETED"
	ShiftStatusPenalized ShiftStatus = "PENALIZED"
)

// Shift представляет рабочую смену исполнителя.
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

// ShiftRepository описывает операции хранения смен.
type ShiftRepository interface {
	Create(ctx context.Context, shift *Shift) error
	GetActiveShift(ctx context.Context, executorID uuid.UUID) (*Shift, error)
	GetShiftByID(ctx context.Context, shiftID uuid.UUID) (*Shift, error)
	GetActiveShifts(ctx context.Context) ([]*Shift, error)
	End(ctx context.Context, shiftID uuid.UUID) error
	Penalize(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error

	// EarlyEnd завершает смену раньше запланированного конца, записывает сумму
	// штрафа и помечает смену как PENALIZED.
	EarlyEnd(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error

	// GetLastShiftByExecutor возвращает самую свежую смену исполнителя,
	// независимо от статуса (активная, завершённая или со штрафом).
	GetLastShiftByExecutor(ctx context.Context, executorID uuid.UUID) (*Shift, error)

	StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*Shift, error)
}

// shiftRepo реализует ShiftRepository поверх *sql.DB.
type shiftRepo struct {
	db *sql.DB
}

// NewShiftRepository создаёт новый ShiftRepository.
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

func (r *shiftRepo) Create(ctx context.Context, shift *Shift) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO shifts (id, executor_id, duration_hours, started_at, planned_end_at, status, fine_amount)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		shift.ID, shift.ExecutorID, shift.DurationHours, shift.StartedAt, shift.PlannedEndAt, shift.Status, shift.FineAmount,
	)
	return err
}

// findActiveByExecutor — реализация, стоящая за GetActiveShift.
func (r *shiftRepo) findActiveByExecutor(ctx context.Context, executorID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRowContext(ctx,
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

func (r *shiftRepo) GetActiveShift(ctx context.Context, executorID uuid.UUID) (*Shift, error) {
	return r.findActiveByExecutor(ctx, executorID)
}

func (r *shiftRepo) GetShiftByID(ctx context.Context, shiftID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRowContext(ctx,
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

func (r *shiftRepo) GetActiveShifts(ctx context.Context) ([]*Shift, error) {
	rows, err := r.db.QueryContext(ctx,
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

func (r *shiftRepo) End(ctx context.Context, shiftID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE shifts SET status = $1, actual_end_at = now() WHERE id = $2`,
		ShiftStatusCompleted, shiftID,
	)
	return err
}

func (r *shiftRepo) Penalize(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE shifts SET status = $1, fine_amount = fine_amount + $2 WHERE id = $3`,
		ShiftStatusPenalized, fine, shiftID,
	)
	return err
}

// EarlyEnd завершает смену раньше запланированного конца и записывает штраф.
func (r *shiftRepo) EarlyEnd(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE shifts SET status = $1, actual_end_at = now(), fine_amount = fine_amount + $2 WHERE id = $3`,
		ShiftStatusPenalized, fine, shiftID,
	)
	return err
}

// GetLastShiftByExecutor возвращает самую свежую смену исполнителя,
// независимо от статуса.
func (r *shiftRepo) GetLastShiftByExecutor(ctx context.Context, executorID uuid.UUID) (*Shift, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, executor_id, duration_hours, started_at, planned_end_at, actual_end_at, status, fine_amount
		 FROM shifts
		 WHERE executor_id = $1
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

// StartShift создаёт и сохраняет новую активную смену.
func (r *shiftRepo) StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*Shift, error) {
	now := time.Now()
	shift := &Shift{
		ID:            uuid.New(),
		ExecutorID:    executorID,
		DurationHours: durationHours,
		StartedAt:     now,
		PlannedEndAt:  now.Add(time.Duration(durationHours) * time.Hour),
		Status:        ShiftStatusActive,
	}
	if err := r.Create(ctx, shift); err != nil {
		return nil, err
	}
	return shift, nil
}
