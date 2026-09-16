package photoproof

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Камеры снимка.
const (
	CameraFront = "FRONT"
	CameraRear  = "REAR"
)

// Результаты проверки снимка. Их устройство описано вне репозитория; здесь —
// только значения, которые хранит база.
const (
	SealValid   = "VALID"
	SealMissing = "MISSING"
	SealInvalid = "INVALID"

	MarkFound    = "FOUND"
	MarkNotFound = "NOT_FOUND"
	MarkMismatch = "MISMATCH"
)

// MaxPhotoBytes — потолок одного снимка. Снимок уходит без пересжатия, чтобы
// сохранить EXIF, поэтому потолок выше, чем у вложений чата.
const MaxPhotoBytes = 15 << 20

// Ошибки загрузки снимка.
var (
	ErrProofForbidden   = errors.New("заказ назначен другому исполнителю")
	ErrProofNotRequired = errors.New("заказ не требует фото-подтверждения")
	ErrProofClosed      = errors.New("заказ уже отмечен исполненным: снимки больше не принимаются")
	ErrProofKind        = errors.New("вид снимка: AREA или SELFIE")
	ErrProofCamera      = errors.New("камера: FRONT или REAR")
	ErrProofNotJPEG     = errors.New("снимок должен быть JPEG с камеры")
	ErrProofTooLarge    = errors.New("снимок слишком большой")
	ErrProofClientKey   = errors.New("у снимка нет ключа отправки")
	ErrProofDeviceTime  = errors.New("у снимка нет времени съёмки по часам телефона")
	ErrProofOrderAbsent = errors.New("заказ не найден")
)

// Proof — снимок фото-подтверждения.
type Proof struct {
	ID            uuid.UUID  `json:"id"`
	OrderID       uuid.UUID  `json:"order_id"`
	ExecutorID    uuid.UUID  `json:"executor_id"`
	Kind          string     `json:"kind"`
	Camera        string     `json:"camera"`
	SymbolID      uuid.UUID  `json:"symbol_id"`
	ClientKey     string     `json:"client_key"`
	FileURL       string     `json:"file_url"`
	FileSHA256    string     `json:"file_sha256"`
	FileSize      int64      `json:"file_size"`
	ExifTakenAt   *time.Time `json:"exif_taken_at,omitempty"`
	ExifLat       *float64   `json:"exif_lat,omitempty"`
	ExifLon       *float64   `json:"exif_lon,omitempty"`
	DeviceTakenAt time.Time  `json:"device_taken_at"`
	DeviceLat     *float64   `json:"device_lat,omitempty"`
	DeviceLon     *float64   `json:"device_lon,omitempty"`
	SealStatus    string     `json:"seal_status"`
	MarkStatus    string     `json:"mark_status"`
	UploadedAt    time.Time  `json:"uploaded_at"`
}

// UploadInput — снимок и то, что телефон сообщил о нём.
type UploadInput struct {
	Kind          string
	Camera        string
	ClientKey     string
	DeviceTakenAt time.Time
	DeviceLat     *float64
	DeviceLon     *float64
	DeviceAccM    *float64
	Data          []byte
}

// CheckInput — то, с чем сверяется снимок при проверке.
type CheckInput struct {
	OrderID       uuid.UUID
	SymbolCode    string
	SymbolNumber  int
	Key           []byte
	DeviceTakenAt time.Time
	DeviceLat     *float64
	DeviceLon     *float64
}

// Checker проверяет снимок. Устройство проверки описано вне репозитория.
type Checker interface {
	Check(data []byte, in CheckInput) (seal, mark string)
}

// noChecker — проверка не подключена: ничего не найдено.
type noChecker struct{}

func (noChecker) Check([]byte, CheckInput) (string, string) { return SealMissing, MarkNotFound }

// Storage сохраняет файл снимка и отдаёт его путь для ссылок.
type Storage interface {
	Save(orderID uuid.UUID, name string, data []byte) (string, error)
	// Open открывает сохранённый файл по пути, который вернул Save.
	Open(fileURL string) (io.ReadCloser, error)
}

// DiskStorage кладёт снимки в каталог загрузок: <root>/photo-proofs/<заказ>/.
type DiskStorage struct {
	Root string
}

// Save пишет файл и возвращает путь вида /uploads/photo-proofs/<заказ>/<имя>.
func (d DiskStorage) Save(orderID uuid.UUID, name string, data []byte) (string, error) {
	dir := filepath.Join(d.Root, "photo-proofs", orderID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o640); err != nil {
		return "", err
	}
	return "/uploads/photo-proofs/" + orderID.String() + "/" + name, nil
}

// Open открывает файл снимка. Путь принимается только того вида, какой отдаёт
// Save: никакого выхода за каталог снимков.
func (d DiskStorage) Open(fileURL string) (io.ReadCloser, error) {
	const prefix = "/uploads/photo-proofs/"
	rel := strings.TrimPrefix(fileURL, prefix)
	if rel == fileURL || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(d.Root, "photo-proofs", filepath.FromSlash(rel)))
}

// ProofByID отдаёт снимок по id.
func (s *Service) ProofByID(ctx context.Context, id uuid.UUID) (*Proof, error) {
	if s.db == nil {
		return nil, sql.ErrNoRows
	}
	return scanProof(s.db.QueryRowContext(ctx, `SELECT `+proofColumns+` FROM order_photo_proofs WHERE id = $1`, id))
}

// OpenProofFile открывает файл снимка.
func (s *Service) OpenProofFile(p *Proof) (io.ReadCloser, error) {
	if s.storage == nil {
		return nil, os.ErrNotExist
	}
	return s.storage.Open(p.FileURL)
}

// WithProofs подключает приём снимков: базу, хранилище файлов и проверку.
func (s *Service) WithProofs(db *sql.DB, storage Storage, checker Checker) *Service {
	s.db = db
	s.storage = storage
	if checker == nil {
		checker = noChecker{}
	}
	s.checker = checker
	return s
}

func (s *Service) runInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func isJPEG(data []byte) bool {
	return len(data) > 3 && bytes.Equal(data[:3], []byte{0xFF, 0xD8, 0xFF})
}

func (in *UploadInput) validate() error {
	in.Kind = strings.ToUpper(strings.TrimSpace(in.Kind))
	in.Camera = strings.ToUpper(strings.TrimSpace(in.Camera))
	in.ClientKey = strings.TrimSpace(in.ClientKey)
	switch {
	case in.Kind != KindArea && in.Kind != KindSelfie:
		return ErrProofKind
	case in.Camera != CameraFront && in.Camera != CameraRear:
		return ErrProofCamera
	case in.ClientKey == "" || len(in.ClientKey) > 64:
		return ErrProofClientKey
	case in.DeviceTakenAt.IsZero():
		return ErrProofDeviceTime
	case len(in.Data) > MaxPhotoBytes:
		return ErrProofTooLarge
	case !isJPEG(in.Data):
		return ErrProofNotJPEG
	}
	if in.DeviceLat != nil && in.DeviceLon != nil && !validCoordinates(*in.DeviceLat, *in.DeviceLon) {
		in.DeviceLat, in.DeviceLon = nil, nil
	}
	return nil
}

// UploadProof принимает снимок фото-подтверждения.
//
//   - Снимать может только исполнитель заказа, и только пока заказ не отмечен
//     исполненным: после отметки набор снимков закрыт.
//   - Повтор с тем же ключом отправки возвращает уже принятый снимок: очередь в
//     офлайне повторяет то, что не смогла подтвердить.
//   - Новый снимок того же вида до отметки заменяет прежний — исполнитель
//     переснял кадр.
//   - Файл сохраняется как есть, без пересжатия: EXIF — часть доказательства.
//   - Координаты телефона уходят и в трек, точкой со снимком.
func (s *Service) UploadProof(ctx context.Context, executorID, orderID uuid.UUID, in UploadInput) (*Proof, error) {
	if s.storage == nil || s.db == nil {
		return nil, errors.New("приём снимков не подключён")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}

	var proof *Proof
	err := s.runInTx(ctx, func(tx *sql.Tx) error {
		var orderExecutor uuid.NullUUID
		var status string
		var required bool
		var symbolID uuid.NullUUID
		var key []byte
		err := tx.QueryRowContext(ctx, `
            SELECT executor_id, status::text, photo_required, watermark_symbol_id, proof_key
            FROM orders WHERE id = $1 FOR UPDATE
        `, orderID).Scan(&orderExecutor, &status, &required, &symbolID, &key)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProofOrderAbsent
		}
		if err != nil {
			return err
		}
		if !orderExecutor.Valid || orderExecutor.UUID != executorID {
			return ErrProofForbidden
		}
		if !required || !symbolID.Valid {
			return ErrProofNotRequired
		}

		// Повтор уже принятой отправки отвечает тем же снимком — даже после
		// отметки «Исполнил»: очередь могла отправить снимок, отметку, а
		// подтверждение снимка потерять.
		if existing, err := s.proofByClientKey(ctx, tx, orderID, in.ClientKey); err != nil || existing != nil {
			proof = existing
			return err
		}
		if status != "ASSIGNED" {
			return ErrProofClosed
		}

		symbol, err := s.symbols.Get(ctx, symbolID.UUID)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(in.Data)
		url, err := s.storage.Save(orderID, uuid.NewString()+".jpg", in.Data)
		if err != nil {
			return err
		}

		facts := readExif(in.Data, in.DeviceTakenAt.Location())
		seal, mark := s.checker.Check(in.Data, CheckInput{
			OrderID: orderID, SymbolCode: symbol.Code, SymbolNumber: symbol.Number, Key: key,
			DeviceTakenAt: in.DeviceTakenAt, DeviceLat: in.DeviceLat, DeviceLon: in.DeviceLon,
		})

		if _, err := tx.ExecContext(ctx,
			`DELETE FROM order_photo_proofs WHERE order_id = $1 AND kind = $2`, orderID, in.Kind); err != nil {
			return err
		}
		proof = &Proof{
			OrderID: orderID, ExecutorID: executorID, Kind: in.Kind, Camera: in.Camera,
			SymbolID: symbol.ID, ClientKey: in.ClientKey, FileURL: url,
			FileSHA256: hex.EncodeToString(sum[:]), FileSize: int64(len(in.Data)),
			ExifTakenAt: facts.TakenAt, ExifLat: facts.Lat, ExifLon: facts.Lon,
			DeviceTakenAt: in.DeviceTakenAt, DeviceLat: in.DeviceLat, DeviceLon: in.DeviceLon,
			SealStatus: seal, MarkStatus: mark,
		}
		if err := tx.QueryRowContext(ctx, `
            INSERT INTO order_photo_proofs (order_id, executor_id, kind, camera, symbol_id, client_key,
                file_url, file_sha256, file_size, exif_taken_at, exif_lat, exif_lon,
                device_taken_at, device_lat, device_lon, seal_status, mark_status)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
            RETURNING id, uploaded_at
        `, proof.OrderID, proof.ExecutorID, proof.Kind, proof.Camera, proof.SymbolID, proof.ClientKey,
			proof.FileURL, proof.FileSHA256, proof.FileSize, proof.ExifTakenAt, proof.ExifLat, proof.ExifLon,
			proof.DeviceTakenAt, proof.DeviceLat, proof.DeviceLon, proof.SealStatus, proof.MarkStatus).
			Scan(&proof.ID, &proof.UploadedAt); err != nil {
			return err
		}

		if s.track != nil && in.DeviceLat != nil && in.DeviceLon != nil {
			if _, err := s.track.Add(ctx, tx, []Position{{
				ExecutorID: executorID, Lat: *in.DeviceLat, Lon: *in.DeviceLon, AccuracyM: in.DeviceAccM,
				Source: SourcePhoto, OrderID: &orderID, DeviceAt: in.DeviceTakenAt,
				ClientKey: "photo:" + in.ClientKey,
			}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proof, nil
}

const proofColumns = `id, order_id, executor_id, kind, camera, symbol_id, client_key, file_url, file_sha256,
    file_size, exif_taken_at, exif_lat, exif_lon, device_taken_at, device_lat, device_lon,
    seal_status, mark_status, uploaded_at`

func scanProof(row interface{ Scan(...interface{}) error }) (*Proof, error) {
	var p Proof
	if err := row.Scan(&p.ID, &p.OrderID, &p.ExecutorID, &p.Kind, &p.Camera, &p.SymbolID, &p.ClientKey,
		&p.FileURL, &p.FileSHA256, &p.FileSize, &p.ExifTakenAt, &p.ExifLat, &p.ExifLon,
		&p.DeviceTakenAt, &p.DeviceLat, &p.DeviceLon, &p.SealStatus, &p.MarkStatus, &p.UploadedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Service) proofByClientKey(ctx context.Context, q Querier, orderID uuid.UUID, clientKey string) (*Proof, error) {
	p, err := scanProof(q.QueryRowContext(ctx,
		`SELECT `+proofColumns+` FROM order_photo_proofs WHERE order_id = $1 AND client_key = $2`, orderID, clientKey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ProofsForOrder отдаёт снимки заказа, обязательный первым.
func (s *Service) ProofsForOrder(ctx context.Context, q Querier, orderID uuid.UUID) ([]Proof, error) {
	if q == nil {
		q = s.db
	}
	rows, err := q.QueryContext(ctx,
		`SELECT `+proofColumns+` FROM order_photo_proofs WHERE order_id = $1 ORDER BY kind`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	proofs := []Proof{}
	for rows.Next() {
		p, err := scanProof(rows)
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, *p)
	}
	return proofs, rows.Err()
}
