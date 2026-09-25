package photoproof

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log"
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
	// Remove удаляет сохранённый файл по пути, который вернул Save. Файла
	// уже нет — не ошибка.
	Remove(fileURL string) error
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

// Open открывает файл снимка.
func (d DiskStorage) Open(fileURL string) (io.ReadCloser, error) {
	path, err := d.path(fileURL)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Remove удаляет файл снимка.
func (d DiskStorage) Remove(fileURL string) error {
	path, err := d.path(fileURL)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// path переводит путь, который отдал Save, в путь на диске. Принимается только
// путь того вида: никакого выхода за каталог снимков.
func (d DiskStorage) path(fileURL string) (string, error) {
	const prefix = "/uploads/photo-proofs/"
	rel := strings.TrimPrefix(fileURL, prefix)
	if rel == fileURL || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return "", os.ErrNotExist
	}
	return filepath.Join(d.Root, "photo-proofs", filepath.FromSlash(rel)), nil
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
//     переснял кадр; файл прежнего удаляется.
//   - Файл сохраняется как есть, без пересжатия: EXIF — часть доказательства.
//   - Координаты телефона уходят и в трек, точкой со снимком.
//
// Проверка снимка — декодирование JPEG до 15 МБ и проход по всем его блокам —
// стоит секунды процессора, и держать на это время строку заказа под
// блокировкой нельзя: каждый UPDATE этого заказа ждал бы её. Поэтому приём
// идёт в три шага: короткое чтение заказа без блокировки, вся тяжёлая работа
// вне транзакции, и короткая транзакция, которая под блокировкой строки
// перепроверяет то же самое и пишет снимок. Файл, записанный под снимок,
// который в итоге не принят, удаляется.
func (s *Service) UploadProof(ctx context.Context, executorID, orderID uuid.UUID, in UploadInput) (*Proof, error) {
	if s.storage == nil || s.db == nil {
		return nil, errors.New("приём снимков не подключён")
	}
	if err := in.validate(); err != nil {
		return nil, err
	}

	// 1. Чей заказ, в каком он статусе и не принят ли уже этот снимок.
	order, err := s.readProofOrder(ctx, s.db, orderID, false)
	if err != nil {
		return nil, err
	}
	if err := order.admits(executorID); err != nil {
		return nil, err
	}
	if existing, err := s.proofByClientKey(ctx, s.db, orderID, in.ClientKey); err != nil || existing != nil {
		return existing, err
	}
	if order.status != orderStatusAssigned {
		return nil, ErrProofClosed
	}
	symbol, err := s.symbols.Get(ctx, order.symbolID.UUID)
	if err != nil {
		return nil, err
	}

	// 2. Тяжёлая часть — вне какой-либо транзакции.
	sum := sha256.Sum256(in.Data)
	facts := readExif(in.Data, in.DeviceTakenAt.Location())
	seal, mark := s.checker.Check(in.Data, CheckInput{
		OrderID: orderID, SymbolCode: symbol.Code, SymbolNumber: symbol.Number, Key: order.key,
		DeviceTakenAt: in.DeviceTakenAt, DeviceLat: in.DeviceLat, DeviceLon: in.DeviceLon,
	})
	url, err := s.storage.Save(orderID, uuid.NewString()+".jpg", in.Data)
	if err != nil {
		return nil, err
	}
	proof := &Proof{
		OrderID: orderID, ExecutorID: executorID, Kind: in.Kind, Camera: in.Camera,
		SymbolID: symbol.ID, ClientKey: in.ClientKey, FileURL: url,
		FileSHA256: hex.EncodeToString(sum[:]), FileSize: int64(len(in.Data)),
		ExifTakenAt: facts.TakenAt, ExifLat: facts.Lat, ExifLon: facts.Lon,
		DeviceTakenAt: in.DeviceTakenAt, DeviceLat: in.DeviceLat, DeviceLon: in.DeviceLon,
		SealStatus: seal, MarkStatus: mark,
	}

	// 3. Под блокировкой строки заказа: те же проверки ещё раз — пока шла
	// проверка, заказ мог закрыться, перейти к другому или принять этот же
	// снимок из параллельной отправки, — и запись.
	var accepted *Proof
	var replaced []string
	err = s.runInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.readProofOrder(ctx, tx, orderID, true)
		if err != nil {
			return err
		}
		if err := order.admits(executorID); err != nil {
			return err
		}
		if existing, err := s.proofByClientKey(ctx, tx, orderID, in.ClientKey); err != nil || existing != nil {
			accepted = existing
			return err
		}
		if order.status != orderStatusAssigned {
			return ErrProofClosed
		}

		replaced, err = s.deleteProofsOfKind(ctx, tx, orderID, in.Kind)
		if err != nil {
			return err
		}
		if err := s.insertProof(ctx, tx, proof); err != nil {
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
		accepted = proof
		return nil
	})
	if err != nil || accepted != proof {
		// Снимка с этим файлом не будет: транзакция не прошла или тот же ключ
		// уже принят параллельной отправкой.
		s.removeFile(url)
		return accepted, err
	}
	for _, old := range replaced {
		s.removeFile(old)
	}
	return proof, nil
}

// orderStatusAssigned — единственный статус, в котором принимаются снимки.
const orderStatusAssigned = "ASSIGNED"

// proofOrder — то, что о заказе нужно знать приёму снимка.
type proofOrder struct {
	executor uuid.NullUUID
	status   string
	required bool
	symbolID uuid.NullUUID
	key      []byte
}

// readProofOrder читает заказ; forUpdate блокирует строку на время транзакции.
func (s *Service) readProofOrder(ctx context.Context, q Querier, orderID uuid.UUID, forUpdate bool) (proofOrder, error) {
	query := `SELECT executor_id, status::text, photo_required, watermark_symbol_id, proof_key FROM orders WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var o proofOrder
	err := q.QueryRowContext(ctx, query, orderID).Scan(&o.executor, &o.status, &o.required, &o.symbolID, &o.key)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrProofOrderAbsent
	}
	return o, err
}

// admits проверяет, может ли исполнитель вообще присылать снимки к заказу.
func (o proofOrder) admits(executorID uuid.UUID) error {
	if !o.executor.Valid || o.executor.UUID != executorID {
		return ErrProofForbidden
	}
	if !o.required || !o.symbolID.Valid {
		return ErrProofNotRequired
	}
	return nil
}

// deleteProofsOfKind снимает прежние снимки этого вида и отдаёт пути их
// файлов — удалить их можно только после фиксации транзакции.
func (s *Service) deleteProofsOfKind(ctx context.Context, q Querier, orderID uuid.UUID, kind string) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`DELETE FROM order_photo_proofs WHERE order_id = $1 AND kind = $2 RETURNING file_url`, orderID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, rows.Err()
}

func (s *Service) insertProof(ctx context.Context, q Querier, proof *Proof) error {
	return q.QueryRowContext(ctx, `
        INSERT INTO order_photo_proofs (order_id, executor_id, kind, camera, symbol_id, client_key,
            file_url, file_sha256, file_size, exif_taken_at, exif_lat, exif_lon,
            device_taken_at, device_lat, device_lon, seal_status, mark_status)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
        RETURNING id, uploaded_at
    `, proof.OrderID, proof.ExecutorID, proof.Kind, proof.Camera, proof.SymbolID, proof.ClientKey,
		proof.FileURL, proof.FileSHA256, proof.FileSize, proof.ExifTakenAt, proof.ExifLat, proof.ExifLon,
		proof.DeviceTakenAt, proof.DeviceLat, proof.DeviceLon, proof.SealStatus, proof.MarkStatus).
		Scan(&proof.ID, &proof.UploadedAt)
}

// removeFile удаляет файл снимка, которого больше нет в базе. Неудача — только
// в журнал: снимок уже принят или отклонён, и лишний файл на диске этого не меняет.
func (s *Service) removeFile(fileURL string) {
	if err := s.storage.Remove(fileURL); err != nil {
		log.Printf("[photoproof] cannot remove file %s: %v", fileURL, err)
	}
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
