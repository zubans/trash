package photoproof_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/photoproof"
)

// plainJPEG — настоящий JPEG без EXIF.
func plainJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for x := 0; x < 32; x++ {
		for y := 0; y < 24; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 10), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// jpegWithExif собирает JPEG с сегментом APP1: время съёмки и координаты — то,
// что пишет камера телефона.
func jpegWithExif(t *testing.T, takenAt string, lat, lon float64) []byte {
	t.Helper()
	be := binary.BigEndian
	var tiff bytes.Buffer
	u16 := func(v uint16) { _ = binary.Write(&tiff, be, v) }
	u32 := func(v uint32) { _ = binary.Write(&tiff, be, v) }
	entry := func(tag, typ uint16, count, value uint32) { u16(tag); u16(typ); u32(count); u32(value) }

	const (
		ifd0At    = 8
		exifAt    = 38
		dateAt    = 56
		gpsAt     = 76
		latDataAt = 130
		lonDataAt = 154
	)
	tiff.WriteString("MM")
	u16(42)
	u32(ifd0At)
	// IFD0: ссылки на Exif IFD и GPS IFD.
	u16(2)
	entry(0x8769, 4, 1, exifAt)
	entry(0x8825, 4, 1, gpsAt)
	u32(0)
	// Exif IFD: DateTimeOriginal.
	u16(1)
	entry(0x9003, 2, 20, dateAt)
	u32(0)
	tiff.WriteString(takenAt + "\x00")
	// GPS IFD.
	ref := func(positive bool, pos, neg byte) uint32 {
		b := neg
		if positive {
			b = pos
		}
		return uint32(b) << 24
	}
	u16(4)
	entry(0x0001, 2, 2, ref(lat >= 0, 'N', 'S'))
	entry(0x0002, 5, 3, latDataAt)
	entry(0x0003, 2, 2, ref(lon >= 0, 'E', 'W'))
	entry(0x0004, 5, 3, lonDataAt)
	u32(0)
	rational := func(v float64) {
		v = math.Abs(v)
		deg := math.Floor(v)
		min := math.Floor((v - deg) * 60)
		sec := ((v-deg)*60 - min) * 60
		u32(uint32(deg))
		u32(1)
		u32(uint32(min))
		u32(1)
		u32(uint32(math.Round(sec * 1000)))
		u32(1000)
	}
	rational(lat)
	rational(lon)

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	plain := plainJPEG(t)
	var out bytes.Buffer
	out.Write(plain[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	_ = binary.Write(&out, be, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(plain[2:])
	return out.Bytes()
}

type proofFixture struct {
	db         *sql.DB
	svc        *photoproof.Service
	executorID uuid.UUID
	orderID    uuid.UUID
	storage    string
}

func newProofFixture(t *testing.T, required bool) *proofFixture {
	t.Helper()
	db := testDB(t)
	storage := t.TempDir()
	svc := photoproof.NewService(photoproof.NewSymbolRepository(db)).
		WithTrack(photoproof.NewTrackRepository(db), nil).
		WithProofs(db, photoproof.DiskStorage{Root: storage}, nil)

	customerID := seedUser(t, db)
	executorID := seedUser(t, db)
	var variantID, symbolID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM service_nodes WHERE node_type = 'VARIANT' LIMIT 1`).Scan(&variantID); err != nil {
		t.Fatalf("variant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM watermark_symbols WHERE deleted_at IS NULL LIMIT 1`).Scan(&symbolID); err != nil {
		t.Fatalf("symbol: %v", err)
	}
	orderID := uuid.New()
	var symbol interface{}
	var key interface{}
	if required {
		k := make([]byte, 32)
		_, _ = rand.Read(k)
		symbol, key = symbolID, k
	}
	if _, err := db.Exec(`INSERT INTO orders (id, customer_id, executor_id, service_variant_id, status, photo_required, watermark_symbol_id, proof_key)
		VALUES ($1, $2, $3, $4, 'ASSIGNED', $5, $6, $7)`, orderID, customerID, executorID, variantID, required, symbol, key); err != nil {
		t.Fatalf("order: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM executor_positions WHERE executor_id = $1`, executorID)
		_, _ = db.Exec(`DELETE FROM orders WHERE id = $1`, orderID)
	})
	return &proofFixture{db: db, svc: svc, executorID: executorID, orderID: orderID, storage: storage}
}

func (f *proofFixture) upload(kind, key string, data []byte, takenAt time.Time, lat, lon *float64) (*photoproof.Proof, error) {
	return f.svc.UploadProof(context.Background(), f.executorID, f.orderID, photoproof.UploadInput{
		Kind: kind, Camera: photoproof.CameraRear, ClientKey: key, DeviceTakenAt: takenAt,
		DeviceLat: lat, DeviceLon: lon, Data: data,
	})
}

func TestUploadProof(t *testing.T) {
	f := newProofFixture(t, true)
	ctx := context.Background()
	moscow := time.FixedZone("MSK", 3*3600)
	takenAt := time.Date(2026, 9, 16, 14, 30, 5, 0, moscow)
	lat, lon := 55.7558, 37.6173
	photo := jpegWithExif(t, "2026:09:16 14:30:00", lat, lon)

	// Чужой исполнитель и не JPEG.
	if _, err := f.svc.UploadProof(ctx, uuid.New(), f.orderID, photoproof.UploadInput{
		Kind: "AREA", Camera: "REAR", ClientKey: "x", DeviceTakenAt: takenAt, Data: photo,
	}); !errors.Is(err, photoproof.ErrProofForbidden) {
		t.Fatalf("stranger: %v", err)
	}
	if _, err := f.upload("AREA", "k0", []byte("not a jpeg at all"), takenAt, nil, nil); !errors.Is(err, photoproof.ErrProofNotJPEG) {
		t.Fatalf("not jpeg: %v", err)
	}

	proof, err := f.upload("area", "k1", photo, takenAt, &lat, &lon)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if proof.Kind != photoproof.KindArea || proof.FileSize != int64(len(photo)) {
		t.Fatalf("proof: %+v", proof)
	}
	sum := sha256.Sum256(photo)
	if proof.FileSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("sha256 mismatch")
	}
	// Файл лежит как есть, без пересжатия.
	stored, err := os.ReadFile(filepath.Join(f.storage, strings.TrimPrefix(proof.FileURL, "/uploads/")))
	if err != nil || !bytes.Equal(stored, photo) {
		t.Fatalf("stored file differs from the upload: %v", err)
	}
	// EXIF: время в поясе телефона, координаты.
	wantTaken := time.Date(2026, 9, 16, 14, 30, 0, 0, moscow)
	if proof.ExifTakenAt == nil || !proof.ExifTakenAt.Equal(wantTaken) {
		t.Fatalf("exif time %v, want %v", proof.ExifTakenAt, wantTaken)
	}
	if proof.ExifLat == nil || math.Abs(*proof.ExifLat-lat) > 1e-4 || proof.ExifLon == nil || math.Abs(*proof.ExifLon-lon) > 1e-4 {
		t.Fatalf("exif coordinates %v %v", proof.ExifLat, proof.ExifLon)
	}
	// Без подключённой проверки — ничего не найдено.
	if proof.SealStatus != photoproof.SealMissing || proof.MarkStatus != photoproof.MarkNotFound {
		t.Fatalf("check statuses: %s %s", proof.SealStatus, proof.MarkStatus)
	}
	// Координаты снимка ушли в трек точкой PHOTO.
	var photoPoints int
	if err := f.db.QueryRow(`SELECT count(*) FROM executor_positions WHERE executor_id = $1 AND source = 'PHOTO' AND order_id = $2`,
		f.executorID, f.orderID).Scan(&photoPoints); err != nil || photoPoints != 1 {
		t.Fatalf("photo track points: %d %v", photoPoints, err)
	}

	// Повтор отправки — тот же снимок.
	again, err := f.upload("AREA", "k1", photo, takenAt, &lat, &lon)
	if err != nil || again.ID != proof.ID {
		t.Fatalf("repeated upload: %+v %v", again, err)
	}

	// Пересъёмка заменяет снимок того же вида; селфи — отдельный снимок, и
	// без EXIF его поля пусты.
	retake, err := f.upload("AREA", "k2", photo, takenAt, nil, nil)
	if err != nil || retake.ID == proof.ID {
		t.Fatalf("retake: %+v %v", retake, err)
	}
	selfie, err := f.upload("SELFIE", "k3", plainJPEG(t), takenAt, nil, nil)
	if err != nil {
		t.Fatalf("selfie: %v", err)
	}
	if selfie.ExifTakenAt != nil || selfie.ExifLat != nil {
		t.Fatalf("exif read from a photo without it: %+v", selfie)
	}
	proofs, err := f.svc.ProofsForOrder(ctx, nil, f.orderID)
	if err != nil || len(proofs) != 2 {
		t.Fatalf("proofs of the order: %d %v", len(proofs), err)
	}

	// После отметки «Исполнил» новые снимки не принимаются, повтор старой
	// отправки — отвечает принятым.
	if _, err := f.db.Exec(`UPDATE orders SET status = 'EXECUTED' WHERE id = $1`, f.orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.upload("AREA", "k4", photo, takenAt, nil, nil); !errors.Is(err, photoproof.ErrProofClosed) {
		t.Fatalf("upload after execution: %v", err)
	}
	if late, err := f.upload("SELFIE", "k3", plainJPEG(t), takenAt, nil, nil); err != nil || late.ID != selfie.ID {
		t.Fatalf("late repeat of an accepted upload: %+v %v", late, err)
	}
}

func TestUploadProofNotRequired(t *testing.T) {
	f := newProofFixture(t, false)
	if _, err := f.upload("AREA", "k1", plainJPEG(t), time.Now(), nil, nil); !errors.Is(err, photoproof.ErrProofNotRequired) {
		t.Fatalf("order without a requirement: %v", err)
	}
}
