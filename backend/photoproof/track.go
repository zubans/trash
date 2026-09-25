package photoproof

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Источники точки трека.
const (
	// SourceLive — обычный отчёт приложения исполнителя.
	SourceLive = "LIVE"
	// SourcePhoto — точка, приехавшая вместе со снимком.
	SourcePhoto = "PHOTO"
)

// Position — одна точка трека исполнителя.
type Position struct {
	ID         uuid.UUID  `json:"id"`
	ExecutorID uuid.UUID  `json:"executor_id"`
	Lat        float64    `json:"lat"`
	Lon        float64    `json:"lon"`
	AccuracyM  *float64   `json:"accuracy_m,omitempty"`
	Source     string     `json:"source"`
	OrderID    *uuid.UUID `json:"order_id,omitempty"`
	// DeviceAt — время устройства, ReportedAt — время сервера. Расхождение
	// показывает, сколько точка пролежала в офлайн-очереди.
	DeviceAt   time.Time `json:"device_at"`
	ReportedAt time.Time `json:"reported_at"`
	ClientKey  string    `json:"client_key,omitempty"`
}

// ErrPositionInvalid — точка вне допустимых координат или без времени.
var ErrPositionInvalid = errors.New("точка трека: координаты вне диапазона или нет времени устройства")

// TrackRepository хранит трек исполнителя.
type TrackRepository interface {
	// Add дописывает точки пачкой. Повторно присланная точка (тот же
	// client_key) молча пропускается: офлайн-очередь повторяет то, что не
	// смогла подтвердить.
	Add(ctx context.Context, q Querier, positions []Position) (int, error)
	// Nearest отдаёт отчёт LIVE, ближайший по времени устройства к at, но не
	// дальше window. Нет такого — nil. Точки PHOTO не участвуют: это координаты,
	// которые телефон назвал вместе со снимком, и подтверждать ими снимок нельзя.
	Nearest(ctx context.Context, q Querier, executorID uuid.UUID, at time.Time, window time.Duration) (*Position, error)
	// DeleteOlderThan чистит трек по возрасту.
	DeleteOlderThan(ctx context.Context, before time.Time) (int, error)
}

type trackRepo struct {
	db *sql.DB
}

// NewTrackRepository создаёт TrackRepository.
func NewTrackRepository(db *sql.DB) TrackRepository {
	return &trackRepo{db: db}
}

func (r *trackRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (p *Position) validate() error {
	if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return ErrPositionInvalid
	}
	if p.DeviceAt.IsZero() {
		return ErrPositionInvalid
	}
	if p.Source != SourceLive && p.Source != SourcePhoto {
		p.Source = SourceLive
	}
	p.ClientKey = strings.TrimSpace(p.ClientKey)
	return nil
}

// Add пишет пачку одним оператором: офлайн-очередь присылает до пятисот точек
// за запрос, и по одному INSERT на точку это было бы пятьсот обращений к базе.
// Повтор (тот же client_key у того же исполнителя) отсекает ON CONFLICT, а не
// ошибка уникальности: внутри транзакции вызывающего ошибка обрывала бы её.
// Id выдаются здесь, чтобы по RETURNING узнать, какие именно точки записаны.
func (r *trackRepo) Add(ctx context.Context, q Querier, positions []Position) (int, error) {
	n := len(positions)
	if n == 0 {
		return 0, nil
	}
	var (
		ids       = make([]string, n)
		executors = make([]string, n)
		lats      = make([]float64, n)
		lons      = make([]float64, n)
		accuracy  = make([]sql.NullFloat64, n)
		sources   = make([]string, n)
		orders    = make([]sql.NullString, n)
		deviceAts = make([]string, n)
		keys      = make([]sql.NullString, n)
		byID      = make(map[uuid.UUID]int, n)
	)
	for i := range positions {
		p := &positions[i]
		if err := p.validate(); err != nil {
			return 0, err
		}
		id := uuid.New()
		byID[id] = i
		ids[i] = id.String()
		executors[i] = p.ExecutorID.String()
		lats[i], lons[i] = p.Lat, p.Lon
		if p.AccuracyM != nil {
			accuracy[i] = sql.NullFloat64{Float64: *p.AccuracyM, Valid: true}
		}
		sources[i] = p.Source
		if p.OrderID != nil {
			orders[i] = sql.NullString{String: p.OrderID.String(), Valid: true}
		}
		// Время уходит строкой: элемент массива с пробелом внутри должен быть
		// в кавычках, и строку драйвер кавычит сам.
		deviceAts[i] = p.DeviceAt.Format(time.RFC3339Nano)
		if p.ClientKey != "" {
			keys[i] = sql.NullString{String: p.ClientKey, Valid: true}
		}
	}

	rows, err := r.exec(q).QueryContext(ctx, `
        INSERT INTO executor_positions (id, executor_id, lat, lon, accuracy_m, source, order_id, device_at, client_key)
        SELECT * FROM unnest(
            $1::uuid[], $2::uuid[], $3::float8[], $4::float8[], $5::float8[],
            $6::text[], $7::uuid[], $8::timestamptz[], $9::text[]
        )
        ON CONFLICT (executor_id, client_key) WHERE client_key IS NOT NULL DO NOTHING
        RETURNING id, reported_at
    `, pq.Array(ids), pq.Array(executors), pq.Array(lats), pq.Array(lons), pq.Array(accuracy),
		pq.Array(sources), pq.Array(orders), pq.Array(deviceAts), pq.Array(keys))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	added := 0
	for rows.Next() {
		var id uuid.UUID
		var reportedAt time.Time
		if err := rows.Scan(&id, &reportedAt); err != nil {
			return added, err
		}
		i, ok := byID[id]
		if !ok {
			continue
		}
		positions[i].ID = id
		positions[i].ReportedAt = reportedAt
		added++
	}
	return added, rows.Err()
}

func (r *trackRepo) Nearest(ctx context.Context, q Querier, executorID uuid.UUID, at time.Time, window time.Duration) (*Position, error) {
	var p Position
	var clientKey sql.NullString
	var orderID uuid.NullUUID
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT id, executor_id, lat, lon, accuracy_m, source, order_id, device_at, reported_at, client_key
        FROM executor_positions
        WHERE executor_id = $1 AND source = 'LIVE' AND device_at BETWEEN $2 AND $3
        ORDER BY abs(EXTRACT(EPOCH FROM (device_at - $4)))
        LIMIT 1
    `, executorID, at.Add(-window), at.Add(window), at).
		Scan(&p.ID, &p.ExecutorID, &p.Lat, &p.Lon, &p.AccuracyM, &p.Source, &orderID, &p.DeviceAt, &p.ReportedAt, &clientKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if orderID.Valid {
		id := orderID.UUID
		p.OrderID = &id
	}
	p.ClientKey = clientKey.String
	return &p, nil
}

func (r *trackRepo) DeleteOlderThan(ctx context.Context, before time.Time) (int, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM executor_positions WHERE reported_at < $1`, before)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	return int(affected), err
}
