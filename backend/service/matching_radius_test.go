package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// fakeGeoRepo serves fixed executor positions. Only the location lookup is
// exercised here; the alert side of the interface is inert.
type fakeGeoRepo struct {
	positions map[uuid.UUID][2]float64
	err       error
}

func newFakeGeoRepo() *fakeGeoRepo {
	return &fakeGeoRepo{positions: map[uuid.UUID][2]float64{}}
}

func (f *fakeGeoRepo) set(executorID uuid.UUID, lat, lon float64) {
	f.positions[executorID] = [2]float64{lat, lon}
}

func (f *fakeGeoRepo) UpdateExecutorLocation(ctx context.Context, executorID uuid.UUID, lat, lon float64, isManual bool) error {
	f.set(executorID, lat, lon)
	return nil
}

func (f *fakeGeoRepo) GetExecutorLocation(ctx context.Context, executorID uuid.UUID) (*float64, *float64, *time.Time, error) {
	if f.err != nil {
		return nil, nil, nil, f.err
	}
	pos, ok := f.positions[executorID]
	if !ok {
		return nil, nil, nil, nil
	}
	lat, lon := pos[0], pos[1]
	return &lat, &lon, nil, nil
}

func (f *fakeGeoRepo) GetExecutorLocations(ctx context.Context, executorIDs []uuid.UUID) (map[uuid.UUID]repository.ExecutorPosition, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]repository.ExecutorPosition, len(executorIDs))
	for _, id := range executorIDs {
		if pos, ok := f.positions[id]; ok {
			out[id] = repository.ExecutorPosition{Lat: pos[0], Lon: pos[1]}
		}
	}
	return out, nil
}

func (f *fakeGeoRepo) RecordDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	f.set(executorID, lat, lon)
	return nil
}

func (f *fakeGeoRepo) GetDevicePosition(ctx context.Context, executorID uuid.UUID) (*repository.ExecutorPosition, error) {
	pos, ok := f.positions[executorID]
	if !ok {
		return nil, nil
	}
	return &repository.ExecutorPosition{Lat: pos[0], Lon: pos[1]}, nil
}

func (f *fakeGeoRepo) FollowDevicePosition(ctx context.Context, executorID uuid.UUID, lat, lon float64) error {
	f.set(executorID, lat, lon)
	return nil
}

func (f *fakeGeoRepo) CreateGeoAlert(ctx context.Context, alert *repository.GeoAlert) error { return nil }

func (f *fakeGeoRepo) GetGeoAlerts(ctx context.Context, status string, limit, offset int) ([]repository.GeoAlert, error) {
	return nil, nil
}

// matchingFixture wires a matching service around one searching order and one
// executor on shift, so each test only has to vary the geography.
type matchingFixture struct {
	srv        *MatchingService
	orderRepo  *mockOrderRepo
	geoRepo    *fakeGeoRepo
	orderID    uuid.UUID
	executorID uuid.UUID
}

// Moscow city centre, and the pickup point every case measures against.
const (
	pickupLat = 55.7512
	pickupLon = 37.6000
)

func newMatchingFixture(t *testing.T, settings map[string]string) *matchingFixture {
	t.Helper()

	customerID := uuid.New()
	executorID := uuid.New()
	orderID := uuid.New()

	lat, lon := pickupLat, pickupLon
	// A real service variant, because eligibility resolves the order's variant
	// and an order whose variant cannot be resolved is not assignable — the same
	// answer the production repository gives for an unknown id.
	orderRepo := &mockOrderRepo{orders: []*repository.Order{{
		ID:               orderID,
		CustomerID:       customerID,
		ServiceVariantID: standardVariantID,
		Status:           "SEARCHING",
		PickupLat:        &lat,
		PickupLon:        &lon,
	}}}

	shiftRepo := &mockShiftRepo{shifts: []*repository.Shift{{
		ID:         uuid.New(),
		ExecutorID: executorID,
		Status:     repository.ShiftStatusActive,
	}}}

	// Automatic matching is off by default in production; these tests exercise
	// the matching logic itself, so enable it unless a case overrides the flag.
	if settings == nil {
		settings = map[string]string{}
	}
	if _, ok := settings["auto_matching_enabled"]; !ok {
		settings["auto_matching_enabled"] = "1"
	}

	geoRepo := newFakeGeoRepo()
	srv := NewMatchingService(orderRepo, shiftRepo, newMockUserRepo(), newMockCatalogRepo()).
		WithGeo(geoRepo, &mockSettingsRepo{settings: settings})

	return &matchingFixture{
		srv:        srv,
		orderRepo:  orderRepo,
		geoRepo:    geoRepo,
		orderID:    orderID,
		executorID: executorID,
	}
}

// assigned reports whether the order ended up with the executor.
func (f *matchingFixture) assigned(t *testing.T) bool {
	t.Helper()
	for _, o := range f.orderRepo.orders {
		if o.ID == f.orderID {
			return o.ExecutorID != nil && *o.ExecutorID == f.executorID
		}
	}
	t.Fatalf("order %s vanished from the repository", f.orderID)
	return false
}

// An executor standing next to the pickup point is the case matching exists for.
func TestMatching_AssignsExecutorInsideRadius(t *testing.T) {
	f := newMatchingFixture(t, nil)
	// ~150 m away.
	f.geoRepo.set(f.executorID, 55.7520, 37.6010)

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if !f.assigned(t) {
		t.Error("expected the nearby executor to be assigned the order")
	}
}

// With automatic matching turned off (the production default), even an executor
// standing next to the pickup is left alone — orders are taken manually.
func TestMatching_DisabledLeavesOrderUnassigned(t *testing.T) {
	f := newMatchingFixture(t, map[string]string{"auto_matching_enabled": "0"})
	f.geoRepo.set(f.executorID, 55.7520, 37.6010) // right next to the pickup

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if f.assigned(t) {
		t.Error("auto-matching is disabled; the order must stay unassigned")
	}
}

// Beyond the radius the order must stay in the queue rather than be handed to
// someone who would only cancel it.
func TestMatching_SkipsExecutorOutsideRadius(t *testing.T) {
	f := newMatchingFixture(t, nil)
	// Saint Petersburg: ~630 km from the pickup point.
	f.geoRepo.set(f.executorID, 59.9311, 30.3609)

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if f.assigned(t) {
		t.Error("an executor 630 km away must not be assigned the order")
	}
}

// An unknown position is a "no", not a free pass: this is the case that used to
// slip through and assign orders across the country.
func TestMatching_SkipsExecutorWithoutLocation(t *testing.T) {
	f := newMatchingFixture(t, nil)
	// Deliberately no position stored for the executor.

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if f.assigned(t) {
		t.Error("an executor with no known position must not be assigned the order")
	}
}

// An order with no pickup coordinates cannot be measured against anything, so
// it stays searching instead of being assigned blindly.
func TestMatching_SkipsOrderWithoutCoordinates(t *testing.T) {
	f := newMatchingFixture(t, nil)
	f.orderRepo.orders[0].PickupLat = nil
	f.orderRepo.orders[0].PickupLon = nil
	f.geoRepo.set(f.executorID, pickupLat, pickupLon)

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if f.assigned(t) {
		t.Error("an order without coordinates must not be assigned")
	}
}

// The bound is operational, not hard-coded: widening it lets a distant
// executor through, which is what makes the setting worth having.
func TestMatching_RadiusIsConfigurable(t *testing.T) {
	// ~57 km north of the pickup point: outside the 10 km default.
	const farLat, farLon = 56.2600, 37.6000

	tight := newMatchingFixture(t, nil)
	tight.geoRepo.set(tight.executorID, farLat, farLon)
	if err := tight.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if tight.assigned(t) {
		t.Fatal("the default radius should not reach an executor ~57 km away")
	}

	wide := newMatchingFixture(t, map[string]string{"auto_match_radius_km": "100"})
	wide.geoRepo.set(wide.executorID, farLat, farLon)
	if err := wide.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if !wide.assigned(t) {
		t.Error("a 100 km radius should reach an executor ~57 km away")
	}
}

// Without the geo dependencies the worker cannot judge distance at all, and
// must decline rather than fall back to assigning everyone.
func TestMatching_WithoutGeoAssignsNothing(t *testing.T) {
	f := newMatchingFixture(t, nil)
	f.srv.geoRepo = nil
	f.geoRepo.set(f.executorID, pickupLat, pickupLon)

	if err := f.srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("unexpected error matching orders: %v", err)
	}
	if f.assigned(t) {
		t.Error("matching without a location store must not assign orders")
	}
}

// One executor must not be handed two orders by the same cycle.
//
// The worker used to re-count an executor's assigned orders from the database
// before every pairing, which made this true as a side effect. The count is now
// loaded once per cycle, so the cycle has to remember what it has already
// handed out — this test is what holds that in place.
func TestMatching_AssignsAtMostOneOrderPerExecutorPerCycle(t *testing.T) {
	customerID := uuid.New()
	executorID := uuid.New()

	lat, lon := pickupLat, pickupLon
	orders := []*repository.Order{}
	for i := 0; i < 3; i++ {
		orders = append(orders, &repository.Order{
			ID:               uuid.New(),
			CustomerID:       customerID,
			ServiceVariantID: standardVariantID,
			Status:           "SEARCHING",
			PickupLat:        &lat,
			PickupLon:        &lon,
		})
	}
	orderRepo := &mockOrderRepo{orders: orders}

	shiftRepo := &mockShiftRepo{shifts: []*repository.Shift{{
		ID:         uuid.New(),
		ExecutorID: executorID,
		Status:     repository.ShiftStatusActive,
	}}}

	geoRepo := newFakeGeoRepo()
	geoRepo.positions[executorID] = [2]float64{pickupLat, pickupLon}

	srv := NewMatchingService(orderRepo, shiftRepo, newMockUserRepo(), newMockCatalogRepo()).
		WithGeo(geoRepo, &mockSettingsRepo{settings: map[string]string{"auto_matching_enabled": "1"}})

	if err := srv.MatchOrders(context.Background()); err != nil {
		t.Fatalf("MatchOrders: %v", err)
	}

	assigned := 0
	for _, o := range orderRepo.orders {
		if o.ExecutorID != nil && *o.ExecutorID == executorID {
			assigned++
		}
	}
	if assigned != 1 {
		t.Errorf("executor was assigned %d orders in one cycle, want exactly 1", assigned)
	}
}
