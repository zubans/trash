package repository_test

import (
	"context"
	"testing"

	"healthlogin/backend/repository"
)

// An executor who has never touched the marker should keep following their
// phone: that is what makes the map useful out of the box.
func TestExecutorGeo_DeviceReportMovesAnchorUntilManualChoice(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewExecutorGeoRepository(db)
	ctx := context.Background()
	executorID := createTestUser(t, db, "EXECUTOR")

	if err := repo.RecordDevicePosition(ctx, executorID, 55.7512, 37.6000); err != nil {
		t.Fatalf("unexpected error recording device position: %v", err)
	}

	lat, lon, _, err := repo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		t.Fatalf("unexpected error reading location: %v", err)
	}
	if lat == nil || lon == nil {
		t.Fatal("the anchor should follow the device before any manual choice")
	}
	if *lat != 55.7512 || *lon != 37.6 {
		t.Errorf("anchor (%f, %f) should match the reported fix (55.7512, 37.6)", *lat, *lon)
	}
}

// The bug this guards: a periodic GPS report used to overwrite the district the
// executor had chosen by hand, dragging their work area back on its own.
func TestExecutorGeo_DeviceReportLeavesManualAnchorAlone(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewExecutorGeoRepository(db)
	ctx := context.Background()
	executorID := createTestUser(t, db, "EXECUTOR")

	// The executor picks a district by hand ...
	const manualLat, manualLon = 55.8000, 37.7000
	if err := repo.UpdateExecutorLocation(ctx, executorID, manualLat, manualLon, true); err != nil {
		t.Fatalf("unexpected error setting manual location: %v", err)
	}

	// ... and the phone goes on reporting from somewhere else.
	if err := repo.RecordDevicePosition(ctx, executorID, 55.7512, 37.6000); err != nil {
		t.Fatalf("unexpected error recording device position: %v", err)
	}

	lat, lon, _, err := repo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		t.Fatalf("unexpected error reading location: %v", err)
	}
	if lat == nil || lon == nil {
		t.Fatal("the manual anchor disappeared")
	}
	if *lat != manualLat || *lon != manualLon {
		t.Errorf("anchor moved to (%f, %f); a device report must not disturb a manual choice at (%f, %f)",
			*lat, *lon, manualLat, manualLon)
	}

	// The report is still recorded — it is simply not in charge of the anchor.
	device, err := repo.GetDevicePosition(ctx, executorID)
	if err != nil {
		t.Fatalf("unexpected error reading device position: %v", err)
	}
	if device == nil {
		t.Fatal("the device position should still be stored")
	}
	if device.Lat != 55.7512 || device.Lon != 37.6 {
		t.Errorf("device position (%f, %f) does not match the reported fix", device.Lat, device.Lon)
	}
}

// Pressing "my location" is the way back: it moves the anchor onto the device
// and lets later reports move it again.
func TestExecutorGeo_FollowDeviceResumesAutomaticPositioning(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	repo := repository.NewExecutorGeoRepository(db)
	ctx := context.Background()
	executorID := createTestUser(t, db, "EXECUTOR")

	if err := repo.UpdateExecutorLocation(ctx, executorID, 55.8000, 37.7000, true); err != nil {
		t.Fatalf("unexpected error setting manual location: %v", err)
	}

	if err := repo.FollowDevicePosition(ctx, executorID, 55.7512, 37.6000); err != nil {
		t.Fatalf("unexpected error following device: %v", err)
	}

	lat, lon, lastManual, err := repo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		t.Fatalf("unexpected error reading location: %v", err)
	}
	if lat == nil || *lat != 55.7512 || lon == nil || *lon != 37.6 {
		t.Fatalf("anchor should have moved onto the device fix, got (%v, %v)", lat, lon)
	}
	if lastManual != nil {
		t.Error("following the device must clear the manual override, and with it the cooldown")
	}

	// And a later report moves the anchor again, because the override is gone.
	if err := repo.RecordDevicePosition(ctx, executorID, 55.7600, 37.6100); err != nil {
		t.Fatalf("unexpected error recording device position: %v", err)
	}
	lat, lon, _, err = repo.GetExecutorLocation(ctx, executorID)
	if err != nil {
		t.Fatalf("unexpected error reading location: %v", err)
	}
	if lat == nil || *lat != 55.76 || lon == nil || *lon != 37.61 {
		t.Errorf("anchor should follow the device again, got (%v, %v)", lat, lon)
	}
}
