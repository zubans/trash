package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// alertingGeoRepo — хранилище позиций, которое запоминает записанные GeoAlert.
type alertingGeoRepo struct {
	mockExecutorGeoRepo
	lastManual *time.Time
	alerts     []*repository.GeoAlert
}

func (r *alertingGeoRepo) GetExecutorLocation(ctx context.Context, executorID uuid.UUID) (*float64, *float64, *time.Time, error) {
	lat, lon := 55.7558, 37.6173
	return &lat, &lon, r.lastManual, nil
}

func (r *alertingGeoRepo) CreateGeoAlert(ctx context.Context, alert *repository.GeoAlert) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.alerts = append(r.alerts, alert)
	return nil
}

// GeoAlert — след подделки GPS для арбитража, и он обязан быть записан к
// моменту ответа. Раньше его писала горутина с контекстом запроса, который
// net/http отменяет сразу после ответа: INSERT падал с «context canceled», и
// след терялся молча.
func TestSetLocationPersistsSpeedAlertBeforeReturning(t *testing.T) {
	recent := time.Now().Add(-11 * time.Minute)
	repo := &alertingGeoRepo{lastManual: &recent}
	srv := NewExecutorGeoService(repo, nil)

	// Контекст обрывается сразу после ответа, как у HTTP-запроса.
	ctx, cancel := context.WithCancel(context.Background())
	executorID := uuid.New()
	// ~100 км за 11 минут: скорость далеко за порогом.
	res, err := srv.SetLocation(ctx, executorID, SetLocationRequest{Lat: 56.65, Lon: 37.6173, IsManual: true})
	cancel()
	if err != nil || !res.Success {
		t.Fatalf("set location: %v %+v", err, res)
	}

	if len(repo.alerts) != 1 {
		t.Fatalf("geo alerts recorded: %d, want 1", len(repo.alerts))
	}
	alert := repo.alerts[0]
	if alert.ExecutorID != executorID || alert.Status != "PENDING" || alert.CalculatedSpeedKMH <= speedAlertThresholdKMH {
		t.Errorf("alert = %+v", alert)
	}
	if alert.OldLat == nil || *alert.OldLat != 55.7558 || alert.NewLat != 56.65 {
		t.Errorf("alert coordinates = %+v", alert)
	}
}

// Перемещение с обычной скоростью алерта не оставляет.
func TestSetLocationDoesNotAlertOnPlausibleSpeed(t *testing.T) {
	long := time.Now().Add(-10 * time.Hour)
	repo := &alertingGeoRepo{lastManual: &long}
	srv := NewExecutorGeoService(repo, nil)

	if _, err := srv.SetLocation(context.Background(), uuid.New(), SetLocationRequest{Lat: 56.65, Lon: 37.6173, IsManual: true}); err != nil {
		t.Fatalf("set location: %v", err)
	}
	if len(repo.alerts) != 0 {
		t.Errorf("unexpected alerts: %+v", repo.alerts)
	}
}
