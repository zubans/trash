package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Escalation statuses.
const (
	EscalationOpen     = "OPEN"
	EscalationResolved = "RESOLVED"
)

// OrderSubmission is one set of fields an executor sent for checking, together
// with how it compared. The values it was compared against are not copied here:
// they live on the customer's record, and duplicating them would spread the very
// data the flow exists to keep out of the executor's hands.
type OrderSubmission struct {
	ID         uuid.UUID         `json:"id"`
	OrderID    uuid.UUID         `json:"order_id"`
	ExecutorID uuid.UUID         `json:"executor_id"`
	Attempt    int               `json:"attempt"`
	Matched    bool              `json:"matched"`
	Fields     map[string]string `json:"fields"`
	Mismatches []string          `json:"mismatches"`
	CreatedAt  time.Time         `json:"created_at"`
}

// BehaviorEscalation is an order a behaviour handed to an administrator.
type BehaviorEscalation struct {
	ID           uuid.UUID  `json:"id"`
	OrderID      uuid.UUID  `json:"order_id"`
	BehaviorCode string     `json:"behavior_code"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy   *uuid.UUID `json:"resolved_by,omitempty"`

	// Filled in by the admin listing, which has to show what the case is about.
	CustomerID   uuid.UUID          `json:"customer_id"`
	CustomerName string             `json:"customer_name,omitempty"`
	OrderStatus  string             `json:"order_status"`
	ServiceCode  string             `json:"service_code,omitempty"`
	Submissions  []*OrderSubmission `json:"submissions,omitempty"`
}

// ErrEscalationNotFound is returned for an id with no open escalation.
var ErrEscalationNotFound = errors.New("escalation not found")

// SubmissionRepository stores executor submissions and the escalations they
// produce.
type SubmissionRepository interface {
	// Record writes one submission inside the caller's transaction. The attempt
	// number is derived in the same statement, so two submissions racing cannot
	// both be "attempt 2".
	Record(ctx context.Context, q Querier, submission *OrderSubmission) error
	CountForOrder(ctx context.Context, orderID uuid.UUID) (int, error)
	ListForOrder(ctx context.Context, orderID uuid.UUID) ([]*OrderSubmission, error)

	// Escalate opens an escalation for the order, or does nothing when one is
	// already open — a behaviour asking twice is describing the same case.
	Escalate(ctx context.Context, q Querier, escalation *BehaviorEscalation) error
	HasOpenEscalation(ctx context.Context, orderID uuid.UUID) (bool, error)
	ListEscalations(ctx context.Context, status string, limit int) ([]*BehaviorEscalation, error)
	ResolveEscalation(ctx context.Context, id, adminID uuid.UUID) error
	// ResolveByOrder closes whatever was open on an order, for the paths where
	// the case ends by itself: the customer gets verified, the order is closed.
	ResolveByOrder(ctx context.Context, q Querier, orderID uuid.UUID, adminID *uuid.UUID) error
}

type submissionRepo struct {
	db *sql.DB
}

// NewSubmissionRepository creates a SubmissionRepository.
func NewSubmissionRepository(db *sql.DB) SubmissionRepository {
	return &submissionRepo{db: db}
}

func (r *submissionRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *submissionRepo) Record(ctx context.Context, q Querier, submission *OrderSubmission) error {
	if submission.ID == uuid.Nil {
		submission.ID = uuid.New()
	}
	fields, err := json.Marshal(submission.Fields)
	if err != nil {
		return err
	}
	if submission.Mismatches == nil {
		submission.Mismatches = []string{}
	}
	return r.exec(q).QueryRowContext(ctx, `
        INSERT INTO order_submissions (id, order_id, executor_id, attempt, matched, fields, mismatches)
        VALUES ($1, $2, $3,
                (SELECT COALESCE(MAX(attempt), 0) + 1 FROM order_submissions WHERE order_id = $2),
                $4, $5, $6)
        RETURNING attempt, created_at
    `, submission.ID, submission.OrderID, submission.ExecutorID,
		submission.Matched, fields, pq.Array(submission.Mismatches),
	).Scan(&submission.Attempt, &submission.CreatedAt)
}

func (r *submissionRepo) CountForOrder(ctx context.Context, orderID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM order_submissions WHERE order_id = $1`, orderID).Scan(&count)
	return count, err
}

func (r *submissionRepo) ListForOrder(ctx context.Context, orderID uuid.UUID) ([]*OrderSubmission, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, order_id, executor_id, attempt, matched, fields, mismatches, created_at
        FROM order_submissions WHERE order_id = $1 ORDER BY attempt
    `, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	submissions := []*OrderSubmission{}
	for rows.Next() {
		var s OrderSubmission
		var fields []byte
		if err := rows.Scan(&s.ID, &s.OrderID, &s.ExecutorID, &s.Attempt, &s.Matched,
			&fields, pq.Array(&s.Mismatches), &s.CreatedAt); err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			_ = json.Unmarshal(fields, &s.Fields)
		}
		submissions = append(submissions, &s)
	}
	return submissions, rows.Err()
}

func (r *submissionRepo) Escalate(ctx context.Context, q Querier, escalation *BehaviorEscalation) error {
	if escalation.ID == uuid.Nil {
		escalation.ID = uuid.New()
	}
	// The partial unique index is what makes this idempotent; the conflict is a
	// normal outcome, not an error.
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO behavior_escalations (id, order_id, behavior_code, reason)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT DO NOTHING
    `, escalation.ID, escalation.OrderID, escalation.BehaviorCode, escalation.Reason)
	return err
}

func (r *submissionRepo) HasOpenEscalation(ctx context.Context, orderID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
        SELECT EXISTS(SELECT 1 FROM behavior_escalations WHERE order_id = $1 AND status = 'OPEN')
    `, orderID).Scan(&exists)
	return exists, err
}

func (r *submissionRepo) ListEscalations(ctx context.Context, status string, limit int) ([]*BehaviorEscalation, error) {
	if status == "" {
		status = EscalationOpen
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT e.id, e.order_id, e.behavior_code, e.reason, e.status, e.created_at, e.resolved_at, e.resolved_by,
               o.customer_id, o.status,
               COALESCE(sn.code, ''),
               TRIM(CONCAT_WS(' ', u.last_name, u.first_name, u.patronymic))
        FROM behavior_escalations e
        JOIN orders o ON o.id = e.order_id
        JOIN users u ON u.id = o.customer_id
        LEFT JOIN service_nodes sn ON sn.id = o.service_variant_id
        WHERE e.status = $1
        ORDER BY e.created_at DESC
        LIMIT $2
    `, status, historyLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	escalations := []*BehaviorEscalation{}
	for rows.Next() {
		var e BehaviorEscalation
		if err := rows.Scan(&e.ID, &e.OrderID, &e.BehaviorCode, &e.Reason, &e.Status, &e.CreatedAt,
			&e.ResolvedAt, &e.ResolvedBy, &e.CustomerID, &e.OrderStatus, &e.ServiceCode, &e.CustomerName); err != nil {
			return nil, err
		}
		escalations = append(escalations, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The submitted attempts are the point of the screen: the administrator
	// compares what the moderator read off the document with the account.
	for _, e := range escalations {
		submissions, err := r.ListForOrder(ctx, e.OrderID)
		if err != nil {
			return nil, err
		}
		e.Submissions = submissions
	}
	return escalations, nil
}

func (r *submissionRepo) ResolveEscalation(ctx context.Context, id, adminID uuid.UUID) error {
	err := execExpectingOne(ctx, r.db, `
        UPDATE behavior_escalations
        SET status = 'RESOLVED', resolved_at = now(), resolved_by = $2
        WHERE id = $1 AND status = 'OPEN'
    `, id, adminID)
	if errors.Is(err, ErrConflict) {
		return ErrEscalationNotFound
	}
	return err
}

func (r *submissionRepo) ResolveByOrder(ctx context.Context, q Querier, orderID uuid.UUID, adminID *uuid.UUID) error {
	_, err := r.exec(q).ExecContext(ctx, `
        UPDATE behavior_escalations
        SET status = 'RESOLVED', resolved_at = now(), resolved_by = $2
        WHERE order_id = $1 AND status = 'OPEN'
    `, orderID, adminID)
	return err
}
