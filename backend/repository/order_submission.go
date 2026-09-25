package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
var ErrEscalationNotFound = fmt.Errorf("escalation not found: %w", ErrNotFound)

// SubmissionRepository хранит отправки исполнителей и порождаемые ими
// эскалации.
type SubmissionRepository interface {
	// Record пишет одну отправку внутри транзакции вызывающего. Номер попытки
	// выводится в том же операторе, поэтому две гоняющиеся отправки не могут обе
	// быть «попыткой 2».
	Record(ctx context.Context, q Querier, submission *OrderSubmission) error
	// AttemptsSinceEscalation — сколько попыток сделано после последней
	// закрытой администратором эскалации, считая текущую.
	//
	// Это номер попытки «в текущем круге», и именно его видит скрипт. Сама
	// строка хранит сквозной номер: он нужен администратору, который смотрит
	// всю историю ввода по заказу. Без разделения снятие с модерации не
	// возвращало заказ исполнителю по-настоящему — сквозной счётчик уже
	// перевалил за предел попыток, и первая же следующая отправка уходила на
	// модерацию снова.
	AttemptsSinceEscalation(ctx context.Context, q Querier, orderID uuid.UUID) (int, error)

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
	return exec(r.db, q).QueryRowContext(ctx, `
        INSERT INTO order_submissions (id, order_id, executor_id, attempt, matched, fields, mismatches)
        VALUES ($1, $2, $3,
                (SELECT COALESCE(MAX(attempt), 0) + 1 FROM order_submissions WHERE order_id = $2),
                $4, $5, $6)
        RETURNING attempt, created_at
    `, submission.ID, submission.OrderID, submission.ExecutorID,
		submission.Matched, fields, pq.Array(submission.Mismatches),
	).Scan(&submission.Attempt, &submission.CreatedAt)
}

// AttemptsSinceEscalation считает отправки, сделанные после того, как
// администратор в последний раз снял заказ с модерации. Пока модерации не было,
// это все отправки по заказу, поэтому первый круг ничем не отличается от
// прежнего поведения.
func (r *submissionRepo) AttemptsSinceEscalation(ctx context.Context, q Querier, orderID uuid.UUID) (int, error) {
	var count int
	err := exec(r.db, q).QueryRowContext(ctx, `
        SELECT COUNT(*) FROM order_submissions
        WHERE order_id = $1
          AND created_at > COALESCE(
              (SELECT MAX(resolved_at) FROM behavior_escalations
                WHERE order_id = $1 AND status = 'RESOLVED' AND resolved_at IS NOT NULL),
              '-infinity'::timestamptz)
    `, orderID).Scan(&count)
	return count, err
}

func (r *submissionRepo) Escalate(ctx context.Context, q Querier, escalation *BehaviorEscalation) error {
	if escalation.ID == uuid.Nil {
		escalation.ID = uuid.New()
	}
	// Идемпотентным это делает частичный уникальный индекс; конфликт — нормальный
	// исход, а не ошибка.
	_, err := exec(r.db, q).ExecContext(ctx, `
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
	// прочитанное модератором в документе с учётной записью. Читаются одним
	// запросом на всю страницу, а не по запросу на эскалацию.
	orderIDs := make([]uuid.UUID, 0, len(escalations))
	for _, e := range escalations {
		orderIDs = append(orderIDs, e.OrderID)
	}
	byOrder, err := r.listForOrders(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	for _, e := range escalations {
		e.Submissions = byOrder[e.OrderID]
		if e.Submissions == nil {
			e.Submissions = []*OrderSubmission{}
		}
	}
	return escalations, nil
}

// listForOrders — отправки по каждому из заказов, по номеру попытки.
func (r *submissionRepo) listForOrders(ctx context.Context, orderIDs []uuid.UUID) (map[uuid.UUID][]*OrderSubmission, error) {
	out := make(map[uuid.UUID][]*OrderSubmission, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT id, order_id, executor_id, attempt, matched, fields, mismatches, created_at
        FROM order_submissions WHERE order_id = ANY($1) ORDER BY order_id, attempt
    `, pq.Array(orderIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var s OrderSubmission
		var fields []byte
		if err := rows.Scan(&s.ID, &s.OrderID, &s.ExecutorID, &s.Attempt, &s.Matched,
			&fields, pq.Array(&s.Mismatches), &s.CreatedAt); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(fields, &s.Fields); err != nil {
			return nil, fmt.Errorf("submission %s: fields: %w", s.ID, err)
		}
		out[s.OrderID] = append(out[s.OrderID], &s)
	}
	return out, rows.Err()
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
	_, err := exec(r.db, q).ExecContext(ctx, `
        UPDATE behavior_escalations
        SET status = 'RESOLVED', resolved_at = now(), resolved_by = $2
        WHERE order_id = $1 AND status = 'OPEN'
    `, orderID, adminID)
	return err
}
