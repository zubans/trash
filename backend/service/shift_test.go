package service

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockShiftRepo struct {
	shifts []*repository.Shift
	logs   []bool
}

func (m *mockShiftRepo) StartShift(executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
	s := &repository.Shift{
		ID:            uuid.New(),
		ExecutorID:    executorID,
		DurationHours: durationHours,
		StartedAt:     time.Now(),
		PlannedEndAt:  time.Now().Add(time.Duration(durationHours) * time.Hour),
		Status:        repository.ShiftStatusActive,
	}
	m.shifts = append(m.shifts, s)
	return s, nil
}

func (m *mockShiftRepo) GetActiveShift(executorID uuid.UUID) (*repository.Shift, error) {
	for _, s := range m.shifts {
		if s.ExecutorID == executorID && s.Status == "ACTIVE" {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockShiftRepo) UpdateShiftStatus(shiftID uuid.UUID, status string) error {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			s.Status = repository.ShiftStatus(status)
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockShiftRepo) AddGPSLog(shiftID uuid.UUID, lat, lon float64, isInside bool) error {
	m.logs = append(m.logs, isInside)
	return nil
}

func (m *mockShiftRepo) GetLastGPSLogs(shiftID uuid.UUID, count int) ([]bool, error) {
	if len(m.logs) == 0 {
		return []bool{}, nil
	}
	if count > len(m.logs) {
		count = len(m.logs)
	}
	result := make([]bool, 0, count)
	for i := len(m.logs) - 1; i >= len(m.logs)-count; i-- {
		result = append(result, m.logs[i])
	}
	return result, nil
}

func (m *mockShiftRepo) GetGeozoneByID(id int) (*repository.Geozone, error) {
	return nil, nil
}

func (m *mockShiftRepo) GetActiveShifts() ([]*repository.Shift, error) {
	return m.shifts, nil
}

// Methods required by the ShiftRepository interface.
func (m *mockShiftRepo) Create(shift *repository.Shift) error {
	m.shifts = append(m.shifts, shift)
	return nil
}

func (m *mockShiftRepo) FindActiveByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	return m.GetActiveShift(executorID)
}

func (m *mockShiftRepo) End(shiftID uuid.UUID) error {
	return m.UpdateShiftStatus(shiftID, string(repository.ShiftStatusCompleted))
}

func (m *mockShiftRepo) Penalize(shiftID uuid.UUID, fine float64) error {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			s.Status = repository.ShiftStatusPenalized
			s.FineAmount += fine
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockShiftRepo) EarlyEnd(shiftID uuid.UUID, fine float64) error {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			now := time.Now()
			s.Status = repository.ShiftStatusPenalized
			s.ActualEndAt = &now
			s.FineAmount += fine
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockShiftRepo) GetShiftByID(shiftID uuid.UUID) (*repository.Shift, error) {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			return s, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockShiftRepo) GetLastShiftByExecutor(executorID uuid.UUID) (*repository.Shift, error) {
	var last *repository.Shift
	for _, s := range m.shifts {
		if s.ExecutorID == executorID {
			if last == nil || s.StartedAt.After(last.StartedAt) {
				last = s
			}
		}
	}
	if last == nil {
		return nil, errors.New("not found")
	}
	return last, nil
}

func (m *mockShiftRepo) SaveGPSLog(log *repository.GPSLog) error {
	return nil
}

func TestShiftService_StartShift(t *testing.T) {
	repo := &mockShiftRepo{}
	srv := NewShiftService(repo, nil, nil, nil, nil)

	executorID := uuid.New()

	// Case 1: Valid shift (1 hour)
	s, err := srv.StartShift(executorID, 1)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}
	if s.DurationHours != 1 {
		t.Errorf("expected 1 hour duration, got %d", s.DurationHours)
	}

	// Case 2: Invalid duration (2 hours)
	_, err = srv.StartShift(executorID, 2)
	if err == nil {
		t.Error("expected error starting shift with duration 2")
	}
}

func TestShiftService_IsWithinRadius(t *testing.T) {
	// Center: Moscow (55.7558, 37.6173)
	// Point 1: Red Square (approx 55.7539, 37.6208) - should be within 1000m
	if !IsWithinRadius(55.7558, 37.6173, 55.7539, 37.6208, 1000.0) {
		t.Error("expected Red Square to be within 1km of Moscow Center")
	}

	// Point 2: Domodedovo Airport (approx 55.4087, 37.9063) - should be outside 5000m
	if IsWithinRadius(55.7558, 37.6173, 55.4087, 37.9063, 5000.0) {
		t.Error("expected Domodedovo Airport to be outside 5km of Moscow Center")
	}
}

func TestShiftService_IsPointInPolygon(t *testing.T) {
	// A simple 4x4 square polygon
	poly := []Point{
		{Lat: 0.0, Lon: 0.0},
		{Lat: 0.0, Lon: 4.0},
		{Lat: 4.0, Lon: 4.0},
		{Lat: 4.0, Lon: 0.0},
	}

	// Case 1: Inside
	if !IsPointInPolygon(Point{Lat: 2.0, Lon: 2.0}, poly) {
		t.Error("expected point (2,2) to be inside the square polygon")
	}

	// Case 2: Outside
	if IsPointInPolygon(Point{Lat: 5.0, Lon: 5.0}, poly) {
		t.Error("expected point (5,5) to be outside the square polygon")
	}
}

type mockGeozoneRepo struct {
	geozone *repository.Geozone
}

func (m *mockGeozoneRepo) FindByID(id int) (*repository.Geozone, error) {
	return m.geozone, nil
}

func (m *mockGeozoneRepo) FindByExecutor(userID uuid.UUID) (*repository.Geozone, error) {
	return m.geozone, nil
}

type mockShiftTransactionRepo struct{}

func (m *mockShiftTransactionRepo) GetBalance(userID uuid.UUID) (float64, error) {
	return 0, nil
}

func (m *mockShiftTransactionRepo) UpdateBalance(tx *sql.Tx, userID uuid.UUID, delta float64) error {
	return nil
}

func (m *mockShiftTransactionRepo) CreateTransaction(tx *sql.Tx, t *repository.Transaction) error {
	return nil
}

func (m *mockShiftTransactionRepo) RunInTx(fn func(*sql.Tx) error) error {
	return fn(nil)
}

func TestShiftService_RecordLocation_Penalty(t *testing.T) {
	repo := &mockShiftRepo{}
	centerLat := 55.7558
	centerLon := 37.6173
	radius := 100.0
	geoRepo := &mockGeozoneRepo{
		geozone: &repository.Geozone{
			ID:              1,
			Type:            string(repository.GeozoneTypeCircle),
			CenterLatitude:  &centerLat,
			CenterLongitude: &centerLon,
			RadiusMeters:    &radius,
		},
	}
	srv := NewShiftService(repo, geoRepo, &mockShiftTransactionRepo{}, nil, nil)

	executorID := uuid.New()
	shift, err := srv.StartShift(executorID, 1)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	// Three consecutive coordinates far outside the 100m geozone.
	for i := 0; i < 3; i++ {
		if err := srv.RecordLocation(executorID, 55.80, 37.70); err != nil {
			t.Fatalf("unexpected error recording location: %v", err)
		}
	}

	if len(repo.logs) != 3 {
		t.Fatalf("expected 3 gps logs, got %d", len(repo.logs))
	}
	for i, v := range repo.logs {
		if v {
			t.Errorf("expected log %d to be outside", i)
		}
	}
	if shift.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected shift status PENALIZED, got %s", shift.Status)
	}
}

func TestShiftService_EarlyEnd(t *testing.T) {
	repo := &mockShiftRepo{}
	txRepo := &mockShiftTransactionRepo{}
	srv := NewShiftService(repo, nil, txRepo, nil, nil)

	executorID := uuid.New()
	shift, err := srv.StartShift(executorID, 3)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	ended, err := srv.EarlyEnd(executorID)
	if err != nil {
		t.Fatalf("unexpected error ending shift early: %v", err)
	}
	if ended.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected status PENALIZED, got %s", ended.Status)
	}
	if ended.ActualEndAt == nil {
		t.Error("expected actual_end_at to be set")
	}
	if ended.FineAmount != 50.0 {
		t.Errorf("expected fine amount 50.0, got %f", ended.FineAmount)
	}
	if shift.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected original shift status PENALIZED, got %s", shift.Status)
	}
}
