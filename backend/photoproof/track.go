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
	// Nearest отдаёт точку, ближайшую по времени устройства к at, но не дальше
	// window. Нет такой — nil.
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

func (r *trackRepo) Add(ctx context.Context, q Querier, positions []Position) (int, error) {
	added := 0
	for i := range positions {
		p := &positions[i]
		if err := p.validate(); err != nil {
			return added, err
		}
		var clientKey interface{}
		if p.ClientKey != "" {
			clientKey = p.ClientKey
		}
		err := r.exec(q).QueryRowContext(ctx, `
            INSERT INTO executor_positions (executor_id, lat, lon, accuracy_m, source, order_id, device_at, client_key)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            RETURNING id, reported_at
        `, p.ExecutorID, p.Lat, p.Lon, p.AccuracyM, p.Source, p.OrderID, p.DeviceAt, clientKey).
			Scan(&p.ID, &p.ReportedAt)
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Повтор пачки: точка уже записана, и это норма, а не ошибка.
			continue
		}
		if err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

func (r *trackRepo) Nearest(ctx context.Context, q Querier, executorID uuid.UUID, at time.Time, window time.Duration) (*Position, error) {
	var p Position
	var clientKey sql.NullString
	var orderID uuid.NullUUID
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT id, executor_id, lat, lon, accuracy_m, source, order_id, device_at, reported_at, client_key
        FROM executor_positions
        WHERE executor_id = $1 AND device_at BETWEEN $2 AND $3
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
