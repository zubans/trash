package photoproof_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/photoproof"
)

func seedUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'EXECUTOR', $2, 'x', 0, 'ACTIVE')`,
		id, "+7997"+id.String()[:7]); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id) })
	return id
}

func trackService(t *testing.T, db *sql.DB) *photoproof.Service {
	t.Helper()
	return photoproof.NewService(photoproof.NewSymbolRepository(db)).
		WithTrack(photoproof.NewTrackRepository(db), nil)
}

// Пачка из офлайна: точки дописываются, повтор той же пачки не удваивает трек,
// чужой executor_id в теле игнорируется.
func TestTrackAcceptsOfflineBatch(t *testing.T) {
	db := testDB(t)
	svc := trackService(t, db)
	ctx := context.Background()
	executorID := seedUser(t, db)
	stranger := seedUser(t, db)

	base := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	batch := []photoproof.Position{
		{Lat: 55.75, Lon: 37.60, Source: photoproof.SourceLive, DeviceAt: base, ClientKey: "k1"},
		{Lat: 55.76, Lon: 37.61, Source: photoproof.SourceLive, DeviceAt: base.Add(time.Minute), ClientKey: "k2"},
		// Точка, приехавшая со снимком, и с чужим id в теле.
		{ExecutorID: stranger, Lat: 55.77, Lon: 37.62, Source: photoproof.SourcePhoto, DeviceAt: base.Add(2 * time.Minute), ClientKey: "k3"},
	}
	added, err := svc.RecordPositions(ctx, nil, executorID, batch)
	if err != nil || added != 3 {
		t.Fatalf("first batch: %d %v", added, err)
	}
	// Повтор той же пачки — очередь не смогла подтвердить отправку.
	repeat := []photoproof.Position{
		{Lat: 55.75, Lon: 37.60, Source: photoproof.SourceLive, DeviceAt: base, ClientKey: "k1"},
		{Lat: 55.78, Lon: 37.63, Source: photoproof.SourceLive, DeviceAt: base.Add(3 * time.Minute), ClientKey: "k4"},
	}
	added, err = svc.RecordPositions(ctx, nil, executorID, repeat)
	if err != nil || added != 1 {
		t.Fatalf("repeated batch: %d %v", added, err)
	}

	var total, foreign int
	if err := db.QueryRow(`SELECT count(*) FROM executor_positions WHERE executor_id = $1`, executorID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM executor_positions WHERE executor_id = $1`, stranger).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if total != 4 || foreign != 0 {
		t.Fatalf("track rows: own %d, stranger %d", total, foreign)
	}

	// Координаты вне диапазона не принимаются.
	if _, err := svc.RecordPositions(ctx, nil, executorID, []photoproof.Position{{Lat: 91, Lon: 0, DeviceAt: base}}); err == nil {
		t.Fatal("a point outside the coordinate range was accepted")
	}
}

// Ближайшая точка ищется по времени устройства и только внутри окна.
func TestTrackNearestPosition(t *testing.T) {
	db := testDB(t)
	svc := trackService(t, db)
	ctx := context.Background()
	executorID := seedUser(t, db)

	shot := time.Now().Add(-time.Hour).Truncate(time.Second)
	if _, err := svc.RecordPositions(ctx, nil, executorID, []photoproof.Position{
		{Lat: 55.10, Lon: 37.10, Source: photoproof.SourceLive, DeviceAt: shot.Add(-40 * time.Minute)},
		{Lat: 55.20, Lon: 37.20, Source: photoproof.SourceLive, DeviceAt: shot.Add(-3 * time.Minute)},
		{Lat: 55.30, Lon: 37.30, Source: photoproof.SourceLive, DeviceAt: shot.Add(9 * time.Minute)},
		// Точка со снимком ближе всех по времени, но трек ею не подтверждается.
		{Lat: 59.90, Lon: 30.30, Source: photoproof.SourcePhoto, DeviceAt: shot},
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}

	nearest, err := svc.NearestPosition(ctx, nil, executorID, shot)
	if err != nil {
		t.Fatalf("nearest: %v", err)
	}
	if nearest == nil || nearest.Lat != 55.20 {
		t.Fatalf("nearest = %+v, want the point three minutes before the shot", nearest)
	}

	// Момент, вокруг которого точек нет: окно по умолчанию 15 минут.
	far, err := svc.NearestPosition(ctx, nil, executorID, shot.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("nearest far: %v", err)
	}
	if far != nil {
		t.Fatalf("a point outside the window was returned: %+v", far)
	}
}

// Уборка трека по сроку хранения.
func TestTrackSweep(t *testing.T) {
	db := testDB(t)
	svc := trackService(t, db)
	ctx := context.Background()
	executorID := seedUser(t, db)

	if _, err := svc.RecordPositions(ctx, nil, executorID, []photoproof.Position{
		{Lat: 55.1, Lon: 37.1, Source: photoproof.SourceLive, DeviceAt: time.Now()},
		{Lat: 55.2, Lon: 37.2, Source: photoproof.SourceLive, DeviceAt: time.Now()},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Одна точка приехала сорок дней назад.
	if _, err := db.Exec(`UPDATE executor_positions SET reported_at = now() - interval '40 days'
		WHERE executor_id = $1 AND lat = 55.1`, executorID); err != nil {
		t.Fatal(err)
	}

	removed, err := svc.SweepTrack(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed < 1 {
		t.Fatalf("sweep removed %d rows, want at least the 40-day-old one", removed)
	}
	var left int
	if err := db.QueryRow(`SELECT count(*) FROM executor_positions WHERE executor_id = $1`, executorID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("%d rows left, want the fresh one only", left)
	}
}
