package service

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/jpeg"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/photoproof"
	"healthlogin/backend/repository"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Карточка доказательств: снимок у адреса, вовремя и с треком рядом — без
// отметок, кроме непройденной проверки файла; снимок далеко, не вовремя и без
// трека — с отметками обо всём этом.
func TestDisputeEvidenceIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	ctx := context.Background()

	proofs := photoproof.NewService(photoproof.NewSymbolRepository(f.db)).
		WithTrack(photoproof.NewTrackRepository(f.db), f.srv.settingsRepo).
		WithProofs(f.db, photoproof.DiskStorage{Root: t.TempDir()}, photoproof.NewChecker())
	f.srv.WithEvidence(proofs).WithExecutorGeo(repository.NewExecutorGeoRepository(f.db))
	t.Cleanup(func() {
		_, _ = f.db.Exec(`DELETE FROM executor_positions WHERE executor_id = $1`, f.executorID)
		_, _ = f.db.Exec(`DELETE FROM geo_alerts WHERE executor_id = $1`, f.executorID)
	})

	// Заказ требует фото: жест и ключ, как при взятии.
	if _, err := f.db.Exec(`UPDATE orders SET status = 'ASSIGNED' WHERE id = $1`, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.ledger.RunInTx(ctx, func(tx *sql.Tx) error { return proofs.RequireForOrderTx(ctx, tx, f.order.ID) }); err != nil {
		t.Fatalf("require: %v", err)
	}

	shot := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	// Трек: исполнитель был у заказа за три минуты до снимка.
	nearLat, nearLon := 55.7560, 37.6175
	if _, err := proofs.RecordPositions(ctx, nil, f.executorID, []photoproof.Position{
		{Lat: nearLat, Lon: nearLon, Source: photoproof.SourceLive, DeviceAt: shot.Add(-3 * time.Minute)},
	}); err != nil {
		t.Fatalf("track: %v", err)
	}

	// Снимок места — у адреса (55.7558, 37.6173), вовремя.
	lat, lon := 55.7559, 37.6174
	if _, err := proofs.UploadProof(ctx, f.executorID, f.order.ID, photoproof.UploadInput{
		Kind: "AREA", Camera: "REAR", ClientKey: "area", DeviceTakenAt: shot, DeviceLat: &lat, DeviceLon: &lon, Data: testJPEG(t),
	}); err != nil {
		t.Fatalf("upload area: %v", err)
	}
	// Селфи — в двух часах и в Петербурге.
	farLat, farLon := 59.9386, 30.3141
	if _, err := proofs.UploadProof(ctx, f.executorID, f.order.ID, photoproof.UploadInput{
		Kind: "SELFIE", Camera: "FRONT", ClientKey: "selfie", DeviceTakenAt: shot.Add(-2 * time.Hour), DeviceLat: &farLat, DeviceLon: &farLon, Data: testJPEG(t),
	}); err != nil {
		t.Fatalf("upload selfie: %v", err)
	}
	// «Телепорт» за полчаса до селфи.
	if _, err := f.db.Exec(`INSERT INTO geo_alerts (executor_id, new_lat, new_lon, calculated_speed_kmh, created_at)
		VALUES ($1, 59.9, 30.3, 900, $2)`, f.executorID, shot.Add(-150*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Отметка «Исполнил» через пять минут после снимка места; спор.
	executedAt := shot.Add(5 * time.Minute)
	if _, err := f.db.Exec(`UPDATE orders SET status = 'EXECUTED', executed_at = now(), executed_at_device = $2 WHERE id = $1`,
		f.order.ID, executedAt); err != nil {
		t.Fatal(err)
	}
	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "не вывезли")
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}

	if _, err := f.srv.DisputeEvidence(ctx, uuid.New()); err != ErrDisputeNotFound {
		t.Fatalf("missing dispute: %v", err)
	}
	ev, err := f.srv.DisputeEvidence(ctx, dispute.ID)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if ev.Gesture == nil || ev.Gesture.Title == "" || !ev.Order.PhotoRequired || len(ev.Proofs) != 2 {
		t.Fatalf("evidence card: gesture %+v, required %v, %d proofs", ev.Gesture, ev.Order.PhotoRequired, len(ev.Proofs))
	}

	byKind := map[string]ProofEvidence{}
	for _, p := range ev.Proofs {
		byKind[p.Kind] = p
	}
	area, selfie := byKind["AREA"], byKind["SELFIE"]

	// Снимок места: только проверка файла (тестовый JPEG не из приложения).
	for _, want := range []string{EvidenceSeal, EvidenceMark} {
		if !slices.Contains(area.Flags, want) {
			t.Errorf("area photo misses flag %s: %v", want, area.Flags)
		}
	}
	for _, unwanted := range []string{EvidenceTime, EvidenceDistance, EvidenceNoTrack, EvidenceTrackDistance, EvidenceGeoAlert} {
		if slices.Contains(area.Flags, unwanted) {
			t.Errorf("area photo has flag %s: %v", unwanted, area.Flags)
		}
	}
	if area.Track == nil || area.Track.Source != photoproof.SourceLive || area.Track.AgeMin > -2.9 || area.Track.AgeMin < -3.1 {
		t.Errorf("area track: %+v", area.Track)
	}
	if area.TakenVsExecutedMin == nil || *area.TakenVsExecutedMin != 5 {
		t.Errorf("area time to execution: %v", area.TakenVsExecutedMin)
	}
	if area.DistanceToOrderM == nil || *area.DistanceToOrderM > 50 {
		t.Errorf("area distance to the order: %v", area.DistanceToOrderM)
	}
	if area.FileURL != "/api/admin/photo-proofs/"+area.ID.String()+"/file" {
		t.Errorf("file url: %s", area.FileURL)
	}

	// Селфи: далеко, не вовремя, трека нет, рядом «телепорт».
	for _, want := range []string{EvidenceTime, EvidenceDistance, EvidenceNoTrack, EvidenceGeoAlert} {
		if !slices.Contains(selfie.Flags, want) {
			t.Errorf("selfie misses flag %s: %v", want, selfie.Flags)
		}
	}
	if len(selfie.GeoAlerts) != 1 {
		t.Errorf("selfie geo alerts: %+v", selfie.GeoAlerts)
	}
}
