package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Domain event types. They name something that has happened, never something
// that should happen: what to do about it is a behaviour's decision, and two
// behaviours may well want different things from the same event.
const (
	EventUserVerified   = "user.verified"
	EventOrderCreated   = "order.created"
	EventOrderAccepted  = "order.accepted"
	EventOrderExecuted  = "order.executed"
	EventOrderConfirmed = "order.confirmed"
	EventOrderCanceled  = "order.canceled"
	// EventOrderSubmission carries data an executor submitted for checking and
	// how it compared — never the values it was compared against.
	EventOrderSubmission = "order.submission"
)

// Event subject types. The subject decides who the event is delivered to: an
// order event goes to that order's behaviour, a user event to the behaviours of
// that user's open orders.
const (
	EventSubjectUser  = "user"
	EventSubjectOrder = "order"
)

// DomainEvent is one row of the transactional outbox.
//
// The outbox exists because the two halves of a scripted service must not come
// apart. "The moderator marked the visit done" is a state change; "the verifier
// is paid" is what a behaviour decides in response. If the second were done in
// the same request, a failure there would roll back the first as well; if it
// were done afterwards, a crash in between would lose it. Writing the event
// with the state change, and acting on it later, is the only arrangement where
// neither is possible.
type DomainEvent struct {
	ID          uuid.UUID              `json:"id"`
	Type        string                 `json:"type"`
	SubjectType string                 `json:"subject_type"`
	SubjectID   uuid.UUID              `json:"subject_id"`
	ActorID     *uuid.UUID             `json:"actor_id,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	Attempts    int                    `json:"attempts"`
}

// ErrEffectAlreadyApplied reports that an effect with this idempotency key has
// already been applied. It is a normal outcome — a redelivered event asking for
// a payment that was made — and callers skip the effect rather than fail.
var ErrEffectAlreadyApplied = errors.New("behavior effect already applied")

// EventRepository stores domain events and the effects applied in response.
type EventRepository interface {
	// RunInTx runs fn in a transaction, for callers that have to publish an
	// event together with the change it describes.
	RunInTx(ctx context.Context, fn func(*sql.Tx) error) error
	// Publish appends an event. It takes a Querier because it is nearly always
	// called inside somebody else's transaction — that is the point of it.
	Publish(ctx context.Context, q Querier, event *DomainEvent) error
	// ClaimPending returns the oldest unprocessed events and counts an attempt
	// against each, so an event that kills the dispatcher every time cannot be
	// retried forever.
	ClaimPending(ctx context.Context, limit, maxAttempts int) ([]*DomainEvent, error)
	MarkProcessed(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string) error
	// RecordEffect claims an idempotency key inside the applier's transaction.
	// Returns ErrEffectAlreadyApplied when the key is taken, which is how a
	// reward is paid once however many events describe it.
	RecordEffect(ctx context.Context, q Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error
	// CountPending reports the backlog, for the admin panel and metrics.
	CountPending(ctx context.Context) (int, error)
	// PurgeProcessed drops events that were handled longer ago than the given
	// window. Processed rows are history, and history that nothing reads still
	// has to stop growing.
	PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error)
}

type eventRepo struct {
	db *sql.DB
}

// NewEventRepository creates an EventRepository.
func NewEventRepository(db *sql.DB) EventRepository {
	return &eventRepo{db: db}
}

func (r *eventRepo) RunInTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *eventRepo) Publish(ctx context.Context, q Querier, event *DomainEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	payload := []byte("{}")
	if len(event.Payload) > 0 {
		encoded, err := json.Marshal(event.Payload)
		if err != nil {
			return err
		}
		payload = encoded
	}
	exec := Querier(r.db)
	if q != nil {
		exec = q
	}
	_, err := exec.ExecContext(ctx, `
        INSERT INTO domain_events (id, type, subject_type, subject_id, actor_id, payload)
        VALUES ($1, $2, $3, $4, $5, $6)
    `, event.ID, event.Type, event.SubjectType, event.SubjectID, event.ActorID, payload)
	return err
}

func (r *eventRepo) ClaimPending(ctx context.Context, limit, maxAttempts int) ([]*DomainEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	// SKIP LOCKED on top of the leader guard: the guard makes the dispatcher
	// single, this makes a second one harmless rather than duplicating work.
	rows, err := r.db.QueryContext(ctx, `
        UPDATE domain_events SET attempts = attempts + 1
        WHERE id IN (
            SELECT id FROM domain_events
            WHERE processed_at IS NULL AND attempts < $2
            ORDER BY created_at
            LIMIT $1
            FOR UPDATE SKIP LOCKED
        )
        RETURNING id, type, subject_type, subject_id, actor_id, payload, created_at, attempts
    `, limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*DomainEvent
	for rows.Next() {
		var e DomainEvent
		var payload []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.SubjectType, &e.SubjectID, &e.ActorID, &payload, &e.CreatedAt, &e.Attempts); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &e.Payload)
		}
		events = append(events, &e)
	}
	return events, rows.Err()
}

func (r *eventRepo) MarkProcessed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE domain_events SET processed_at = now(), last_error = NULL WHERE id = $1`, id)
	return err
}

func (r *eventRepo) MarkFailed(ctx context.Context, id uuid.UUID, reason string) error {
	// The row stays unprocessed on purpose: it is retried until it succeeds or
	// runs out of attempts, and the last error is kept so the reason is visible
	// without reading the logs.
	_, err := r.db.ExecContext(ctx,
		`UPDATE domain_events SET last_error = $2 WHERE id = $1`, id, reason)
	return err
}

func (r *eventRepo) RecordEffect(ctx context.Context, q Querier, key string, eventID uuid.UUID, behaviorCode, kind string, payload map[string]interface{}) error {
	encoded := []byte("{}")
	if len(payload) > 0 {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		encoded = data
	}
	exec := Querier(r.db)
	if q != nil {
		exec = q
	}
	err := execExpectingOne(ctx, exec, `
        INSERT INTO behavior_effects (idempotency_key, event_id, behavior_code, kind, payload)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (idempotency_key) DO NOTHING
    `, key, eventID, behaviorCode, kind, encoded)
	if errors.Is(err, ErrConflict) {
		return ErrEffectAlreadyApplied
	}
	return err
}

func (r *eventRepo) PurgeProcessed(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	// The effects are removed with their event (ON DELETE CASCADE). That is safe
	// only because the window is far longer than any redelivery: an idempotency
	// key has to outlive every retry of the event that claimed it.
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM domain_events WHERE processed_at IS NOT NULL AND processed_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int64(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *eventRepo) CountPending(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM domain_events WHERE processed_at IS NULL`).Scan(&count)
	return count, err
}
