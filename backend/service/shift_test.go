package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

type mockShiftRepo struct {
	shifts []*repository.Shift
	logs   []bool
}

func (m *mockShiftRepo) StartShift(ctx context.Context, executorID uuid.UUID, durationHours int) (*repository.Shift, error) {
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

func (m *mockShiftRepo) GetActiveShift(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	for _, s := range m.shifts {
		if s.ExecutorID == executorID && s.Status == "ACTIVE" {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockShiftRepo) UpdateShiftStatus(ctx context.Context, shiftID uuid.UUID, status string) error {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			s.Status = repository.ShiftStatus(status)
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockShiftRepo) GetActiveShifts(ctx context.Context) ([]*repository.Shift, error) {
	return m.shifts, nil
}

// Methods required by the ShiftRepository interface.
func (m *mockShiftRepo) Create(ctx context.Context, shift *repository.Shift) error {
	m.shifts = append(m.shifts, shift)
	return nil
}

func (m *mockShiftRepo) FindActiveByExecutor(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
	return m.GetActiveShift(context.Background(), executorID)
}

func (m *mockShiftRepo) End(ctx context.Context, shiftID uuid.UUID) error {
	return m.UpdateShiftStatus(context.Background(), shiftID, string(repository.ShiftStatusCompleted))
}

func (m *mockShiftRepo) Penalize(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			s.Status = repository.ShiftStatusPenalized
			s.FineAmount += fine
			return nil
		}
	}
	return errors.New("not found")
}

func (m *mockShiftRepo) EarlyEnd(ctx context.Context, shiftID uuid.UUID, fine money.Amount) error {
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

func (m *mockShiftRepo) GetShiftByID(ctx context.Context, shiftID uuid.UUID) (*repository.Shift, error) {
	for _, s := range m.shifts {
		if s.ID == shiftID {
			return s, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockShiftRepo) GetLastShiftByExecutor(ctx context.Context, executorID uuid.UUID) (*repository.Shift, error) {
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

func TestShiftService_StartShift(t *testing.T) {
	repo := &mockShiftRepo{}
	srv := NewShiftService(repo, nil, nil, nil, nil, nil)

	executorID := uuid.New()

	// Case 1: Valid shift (1 hour)
	s, err := srv.StartShift(context.Background(), executorID, 1)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}
	if s.DurationHours != 1 {
		t.Errorf("expected 1 hour duration, got %d", s.DurationHours)
	}

	// Case 2: Invalid duration (2 hours)
	_, err = srv.StartShift(context.Background(), executorID, 2)
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

type mockShiftTransactionRepo struct {
	// txs records the ledger entries written by the service under test.
	txs []*repository.Transaction
}

func (m *mockShiftTransactionRepo) GetBalance(ctx context.Context, userID uuid.UUID) (money.Amount, error) {
	return 0, nil
}

func (m *mockShiftTransactionRepo) Debit(ctx context.Context, tx *sql.Tx, userID uuid.UUID, amount money.Amount) error {
	return m.UpdateBalance(context.Background(), tx, userID, -amount)
}

func (m *mockShiftTransactionRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, userID uuid.UUID, delta money.Amount) error {
	return nil
}

func (m *mockShiftTransactionRepo) CreateTransaction(ctx context.Context, tx *sql.Tx, t *repository.Transaction) error {
	m.txs = append(m.txs, t)
	return nil
}

func (m *mockShiftTransactionRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return fn(nil)
}

func (m *mockShiftTransactionRepo) GetTransactionsByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.Transaction, error) {
	return nil, nil
}

func (m *mockShiftTransactionRepo) HasTip(ctx context.Context, q repository.Querier, orderID uuid.UUID) (bool, error) {
	return false, nil
}

// recordedLocation is what a fake recorder captured, so a test can assert that
// the coordinates reached the store rather than being accepted and dropped.
type recordedLocation struct {
	executorID uuid.UUID
	lat, lon   float64
}

type fakeLocationRecorder struct {
	calls  []recordedLocation
	stored bool
	err    error
}

func (f *fakeLocationRecorder) RecordLiveLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64) (bool, error) {
	f.calls = append(f.calls, recordedLocation{executorID: executorID, lat: lat, lon: lon})
	return f.stored, f.err
}

func newShiftServiceForLocation(recorder ExecutorLocationRecorder) (*ShiftService, *mockShiftRepo) {
	repo := &mockShiftRepo{}
	srv := NewShiftService(repo, NewLedger(&mockShiftTransactionRepo{}, newMockAccounts()), nil, nil, nil, nil)
	if recorder != nil {
		srv = srv.WithExecutorLocation(recorder)
	}
	return srv, repo
}

// A reported position is only useful if it is actually stored: automatic
// matching measures distance against it, so a report that is accepted and
// dropped leaves matching working from a stale fix.
func TestShiftService_RecordLocationPersistsCoordinates(t *testing.T) {
	recorder := &fakeLocationRecorder{stored: true}
	srv, _ := newShiftServiceForLocation(recorder)

	executorID := uuid.New()
	if _, err := srv.StartShift(context.Background(), executorID, 1); err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	stored, err := srv.RecordLocation(context.Background(), executorID, 55.80, 37.70)
	if err != nil {
		t.Fatalf("unexpected error recording location: %v", err)
	}
	if !stored {
		t.Errorf("expected the position to be reported as stored")
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected exactly one write to the location store, got %d", len(recorder.calls))
	}
	got := recorder.calls[0]
	if got.executorID != executorID || got.lat != 55.80 || got.lon != 37.70 {
		t.Errorf("location store received %+v, want executor %s at (55.80, 37.70)", got, executorID)
	}
}

// The location rules may decline a move that looks like a district change
// inside its cooldown. That is a legitimate outcome, not a failure.
func TestShiftService_RecordLocationReportsDeclinedMove(t *testing.T) {
	recorder := &fakeLocationRecorder{stored: false}
	srv, _ := newShiftServiceForLocation(recorder)

	executorID := uuid.New()
	if _, err := srv.StartShift(context.Background(), executorID, 1); err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	stored, err := srv.RecordLocation(context.Background(), executorID, 55.80, 37.70)
	if err != nil {
		t.Fatalf("a declined move must not be an error, got: %v", err)
	}
	if stored {
		t.Errorf("expected the declined move to be reported as not stored")
	}
}

// Without an active shift there is nothing to report a position against.
func TestShiftService_RecordLocationRequiresActiveShift(t *testing.T) {
	recorder := &fakeLocationRecorder{stored: true}
	srv, _ := newShiftServiceForLocation(recorder)

	if _, err := srv.RecordLocation(context.Background(), uuid.New(), 55.80, 37.70); err == nil {
		t.Fatal("expected an error when the executor has no active shift")
	}
	if len(recorder.calls) != 0 {
		t.Errorf("nothing should be written without an active shift, got %d writes", len(recorder.calls))
	}
}

// A service assembled without a location store must say so rather than accept
// positions and silently discard them.
func TestShiftService_RecordLocationWithoutStoreFails(t *testing.T) {
	srv, _ := newShiftServiceForLocation(nil)

	executorID := uuid.New()
	if _, err := srv.StartShift(context.Background(), executorID, 1); err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	if _, err := srv.RecordLocation(context.Background(), executorID, 55.80, 37.70); err == nil {
		t.Fatal("expected an error when no location store is configured")
	}
}

func TestShiftService_EarlyEnd(t *testing.T) {
	repo := &mockShiftRepo{}
	txRepo := &mockShiftTransactionRepo{}
	srv := NewShiftService(repo, NewLedger(txRepo, newMockAccounts()), nil, nil, nil, nil)

	executorID := uuid.New()
	shift, err := srv.StartShift(context.Background(), executorID, 3)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	ended, err := srv.EarlyEnd(context.Background(), executorID)
	if err != nil {
		t.Fatalf("unexpected error ending shift early: %v", err)
	}
	if ended.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected status PENALIZED, got %s", ended.Status)
	}
	if ended.ActualEndAt == nil {
		t.Error("expected actual_end_at to be set")
	}
	if ended.FineAmount != money.FromRubles(50) {
		t.Errorf("expected fine amount 50.0, got %s", ended.FineAmount)
	}
	if shift.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected original shift status PENALIZED, got %s", shift.Status)
	}
}

func TestShiftService_EarlyEnd_WithAssignedOrder(t *testing.T) {
	repo := &mockShiftRepo{}
	txRepo := &mockShiftTransactionRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewShiftService(repo, NewLedger(txRepo, newMockAccounts()), nil, orderRepo, nil, nil)

	executorID := uuid.New()
	customerID := uuid.New()
	orderID := uuid.New()

	shift, err := srv.StartShift(context.Background(), executorID, 3)
	if err != nil {
		t.Fatalf("unexpected error starting shift: %v", err)
	}

	orderRepo.orders = append(orderRepo.orders, &repository.Order{
		ID:         orderID,
		CustomerID: customerID,
		ExecutorID: &executorID,
		Status:     repository.OrderStatusAssigned,
		HoldAmount: money.FromRubles(300),
	})

	ended, err := srv.EarlyEnd(context.Background(), executorID)
	if err != nil {
		t.Fatalf("unexpected error ending shift early with order: %v", err)
	}

	// Double penalty (50 * 2) + order cost (300) = 400
	expectedFine := money.FromRubles(400)
	if ended.FineAmount != expectedFine {
		t.Errorf("expected fine amount %s, got %s", expectedFine, ended.FineAmount)
	}
	if ended.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected status PENALIZED, got %s", ended.Status)
	}

	// Assigned order should be unassigned (SEARCHING).
	updatedOrder, err := orderRepo.GetOrderByID(context.Background(), orderID)
	if err != nil {
		t.Fatalf("unexpected error fetching order: %v", err)
	}
	if updatedOrder.Status != repository.OrderStatusSearching {
		t.Errorf("expected order status SEARCHING, got %s", updatedOrder.Status)
	}
	if shift.Status != repository.ShiftStatusPenalized {
		t.Errorf("expected original shift status PENALIZED, got %s", shift.Status)
	}
}
