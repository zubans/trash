package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Статусы спора (order_disputes.status).
const (
	DisputeStatusOpen   = "OPEN"
	DisputeStatusClosed = "CLOSED"
)

// Как закрылся спор (order_disputes.closure).
const (
	// DisputeClosureCustomerConfirmed — заказчик подтвердил выполнение.
	DisputeClosureCustomerConfirmed = "CUSTOMER_CONFIRMED"
	// DisputeClosureExecutorConceded — исполнитель признал, что не выполнил.
	DisputeClosureExecutorConceded = "EXECUTOR_CONCEDED"
	// DisputeClosureArbitration — решил арбитр; только у такого закрытия есть
	// решение.
	DisputeClosureArbitration = "ARBITRATION"
)

// Решение арбитра (order_disputes.decision).
const (
	DisputeDecisionExecutor = "EXECUTOR"
	DisputeDecisionCustomer = "CUSTOMER"
	DisputeDecisionUnknown  = "UNKNOWN"
)

// Dispute — спор заказчика о выполнении заказа.
type Dispute struct {
	ID         uuid.UUID `json:"id"`
	OrderID    uuid.UUID `json:"order_id"`
	CustomerID uuid.UUID `json:"customer_id"`
	ExecutorID uuid.UUID `json:"executor_id"`
	Claim      string    `json:"claim"`
	Status     string    `json:"status"`
	// Closure, Decision, ResolutionNote, ClosedAt и ClosedBy пусты, пока спор
	// открыт. Decision заполнен только при закрытии арбитром.
	Closure        string     `json:"closure,omitempty"`
	Decision       string     `json:"decision,omitempty"`
	ResolutionNote string     `json:"resolution_note,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	ClosedBy       *uuid.UUID `json:"closed_by,omitempty"`
}

// DisputeClosing — чем закрывается спор.
type DisputeClosing struct {
	Closure string
	// Decision — только для DisputeClosureArbitration.
	Decision string
	Note     string
	// ClosedBy — тот, чьё действие закрыло спор.
	ClosedBy *uuid.UUID
}

// DisputeRepository хранит споры.
type DisputeRepository interface {
	// Open заводит открытый спор и заполняет ID и CreatedAt. Возвращает
	// ErrConflict, если по заказу уже есть открытый спор.
	Open(ctx context.Context, q Querier, d *Dispute) error
	// FindOpenByOrder отдаёт открытый спор заказа; nil, если его нет.
	FindOpenByOrder(ctx context.Context, q Querier, orderID uuid.UUID) (*Dispute, error)
	// Close закрывает открытый спор. Возвращает ErrConflict, если спор уже
	// закрыт: закрытий у спора ровно одно.
	Close(ctx context.Context, q Querier, disputeID uuid.UUID, c DisputeClosing) error
}

type disputeRepo struct {
	db *sql.DB
}

// NewDisputeRepository создаёт DisputeRepository.
func NewDisputeRepository(db *sql.DB) DisputeRepository {
	return &disputeRepo{db: db}
}

func (r *disputeRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

const disputeColumns = `id, order_id, customer_id, executor_id, claim, status,
    COALESCE(closure, ''), COALESCE(decision, ''), COALESCE(resolution_note, ''),
    created_at, closed_at, closed_by`

func scanDispute(row interface{ Scan(...interface{}) error }) (*Dispute, error) {
	var d Dispute
	var closedBy uuid.NullUUID
	if err := row.Scan(&d.ID, &d.OrderID, &d.CustomerID, &d.ExecutorID, &d.Claim, &d.Status,
		&d.Closure, &d.Decision, &d.ResolutionNote, &d.CreatedAt, &d.ClosedAt, &closedBy); err != nil {
		return nil, err
	}
	if closedBy.Valid {
		d.ClosedBy = &closedBy.UUID
	}
	return &d, nil
}

func (r *disputeRepo) Open(ctx context.Context, q Querier, d *Dispute) error {
	err := r.exec(q).QueryRowContext(ctx, `
        INSERT INTO order_disputes (order_id, customer_id, executor_id, claim)
        VALUES ($1, $2, $3, $4)
        RETURNING id, status, created_at
    `, d.OrderID, d.CustomerID, d.ExecutorID, d.Claim).Scan(&d.ID, &d.Status, &d.CreatedAt)
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (r *disputeRepo) FindOpenByOrder(ctx context.Context, q Querier, orderID uuid.UUID) (*Dispute, error) {
	d, err := scanDispute(r.exec(q).QueryRowContext(ctx,
		`SELECT `+disputeColumns+` FROM order_disputes WHERE order_id = $1 AND status = 'OPEN'`, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (r *disputeRepo) Close(ctx context.Context, q Querier, disputeID uuid.UUID, c DisputeClosing) error {
	var decision, note interface{}
	if c.Decision != "" {
		decision = c.Decision
	}
	if c.Note != "" {
		note = c.Note
	}
	return execExpectingOne(ctx, r.exec(q), `
        UPDATE order_disputes
        SET status = 'CLOSED', closure = $2, decision = $3, resolution_note = $4,
            closed_at = now(), closed_by = $5
        WHERE id = $1 AND status = 'OPEN'
    `, disputeID, c.Closure, decision, note, c.ClosedBy)
}
