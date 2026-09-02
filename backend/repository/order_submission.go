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

// Статусы эскалации.
const (
	EscalationOpen     = "OPEN"
	EscalationResolved = "RESOLVED"
)

// OrderSubmission — один набор полей, отправленный исполнителем на проверку,
// вместе с результатом сравнения. Значения, с которыми сравнивали, сюда не
// копируются: они живут в записи заказчика, а их дублирование расползлось бы
// теми самыми данными, которые поток и существует держать вне рук исполнителя.
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

// BehaviorEscalation — заказ, переданный поведением администратору.
type BehaviorEscalation struct {
	ID           uuid.UUID  `json:"id"`
	OrderID      uuid.UUID  `json:"order_id"`
	BehaviorCode string     `json:"behavior_code"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy   *uuid.UUID `json:"resolved_by,omitempty"`

	// Заполняется админским списком, который обязан показать, о чём случай.
	CustomerID   uuid.UUID          `json:"customer_id"`
	CustomerName string             `json:"customer_name,omitempty"`
	OrderStatus  string             `json:"order_status"`
	ServiceCode  string             `json:"service_code,omitempty"`
	Submissions  []*OrderSubmission `json:"submissions,omitempty"`
}

// ErrEscalationNotFound возвращается для id, по которому нет открытой эскалации.
var ErrEscalationNotFound = errors.New("escalation not found")

// SubmissionRepository хранит отправки исполнителей и порождаемые ими
// эскалации.
type SubmissionRepository interface {
	// Record пишет одну отправку внутри транзакции вызывающего. Номер попытки
	// выводится в том же операторе, поэтому две гоняющиеся отправки не могут обе
	// быть «попыткой 2».
	Record(ctx context.Context, q Querier, submission *OrderSubmission) error
	CountForOrder(ctx context.Context, orderID uuid.UUID) (int, error)
	ListForOrder(ctx context.Context, orderID uuid.UUID) ([]*OrderSubmission, error)

	// Escalate открывает эскалацию по заказу или ничего не делает, когда одна уже
	// открыта: поведение, спрашивающее дважды, описывает тот же случай.
	Escalate(ctx context.Context, q Querier, escalation *BehaviorEscalation) error
	HasOpenEscalation(ctx context.Context, orderID uuid.UUID) (bool, error)
	ListEscalations(ctx context.Context, status string, limit int) ([]*BehaviorEscalation, error)
	ResolveEscalation(ctx context.Context, id, adminID uuid.UUID) error
	// ResolveByOrder закрывает всё открытое по заказу — для путей, где случай
	// заканчивается сам: заказчик верифицирован, заказ закрыт.
	ResolveByOrder(ctx context.Context, q Querier, orderID uuid.UUID, adminID *uuid.UUID) error
}

type submissionRepo struct {
	db *sql.DB
}

// NewSubmissionRepository создаёт SubmissionRepository.
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
	// Идемпотентным это делает частичный уникальный индекс; конфликт — нормальный
	// исход, а не ошибка.
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

	// Отправленные попытки — смысл этого экрана: администратор сравнивает
	// прочитанное модератором в документе с учётной записью.
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
