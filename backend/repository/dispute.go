package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"healthlogin/backend/money"
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

// AdminDispute — спор глазами арбитра: сам спор, заказ и обе стороны с их
// текущими штрафными баллами в ролях этого спора.
type AdminDispute struct {
	Dispute
	OrderStatus   string       `json:"order_status"`
	OrderAddress  string       `json:"order_address,omitempty"`
	OrderComment  string       `json:"order_comment,omitempty"`
	HoldAmount    money.Amount `json:"hold_amount"`
	FinalAmount   money.Amount `json:"final_amount"`
	ServiceName   string       `json:"service_name"`
	OrderCreated  time.Time    `json:"order_created_at"`
	OrderAssigned *time.Time   `json:"order_assigned_at,omitempty"`
	CustomerName  string       `json:"customer_name"`
	CustomerPhone string       `json:"customer_phone"`
	ExecutorName  string       `json:"executor_name"`
	ExecutorPhone string       `json:"executor_phone"`
	// Действующие баллы: исполнителя — в роли исполнителя, заказчика — в роли
	// заказчика. Арбитру важно, спорит ли сторона не впервые.
	CustomerActivePoints int `json:"customer_active_points"`
	ExecutorActivePoints int `json:"executor_active_points"`
	// Сколько всего споров было у исполнителя, включая этот.
	ExecutorDisputesTotal int `json:"executor_disputes_total"`
}

// DisputeRepository хранит споры.
type DisputeRepository interface {
	// FindByID отдаёт спор; sql.ErrNoRows, если его нет.
	FindByID(ctx context.Context, q Querier, disputeID uuid.UUID) (*Dispute, error)
	// ListForAdmin отдаёт очередь арбитража. status пустой — все споры,
	// открытые первыми; иначе только с этим статусом. Открытые — от старых к
	// новым (кто дольше ждёт), закрытые — от недавно закрытых.
	ListForAdmin(ctx context.Context, status string, limit, offset int) ([]AdminDispute, error)
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

func (r *disputeRepo) FindByID(ctx context.Context, q Querier, disputeID uuid.UUID) (*Dispute, error) {
	return scanDispute(r.exec(q).QueryRowContext(ctx,
		`SELECT `+disputeColumns+` FROM order_disputes WHERE id = $1`, disputeID))
}

func (r *disputeRepo) ListForAdmin(ctx context.Context, status string, limit, offset int) ([]AdminDispute, error) {
	limit, offset = clampPage(limit, offset)
	rows, err := r.db.QueryContext(ctx, `
        SELECT d.id, d.order_id, d.customer_id, d.executor_id, d.claim, d.status,
               COALESCE(d.closure, ''), COALESCE(d.decision, ''), COALESCE(d.resolution_note, ''),
               d.created_at, d.closed_at, d.closed_by,
               o.status::text, COALESCE(o.address, ''), COALESCE(o.comment, ''),
               o.hold_amount, o.final_amount, COALESCE(sn.name->>'ru', sn.code),
               o.created_at, o.assigned_at,
               btrim(COALESCE(cu.last_name, '') || ' ' || COALESCE(cu.first_name, '')), cu.phone,
               btrim(COALESCE(eu.last_name, '') || ' ' || COALESCE(eu.first_name, '')), eu.phone,
               COALESCE(cs.active_points, 0), COALESCE(es.active_points, 0),
               (SELECT count(*) FROM order_disputes d2 WHERE d2.executor_id = d.executor_id)
        FROM order_disputes d
        JOIN orders o ON o.id = d.order_id
        JOIN service_nodes sn ON sn.id = o.service_variant_id
        JOIN users cu ON cu.id = d.customer_id
        JOIN users eu ON eu.id = d.executor_id
        LEFT JOIN user_penalty_status cs ON cs.user_id = d.customer_id AND cs.role = 'CUSTOMER'
        LEFT JOIN user_penalty_status es ON es.user_id = d.executor_id AND es.role = 'EXECUTOR'
        WHERE ($1 = '' OR d.status = $1)
        ORDER BY (d.status = 'OPEN') DESC,
                 CASE WHEN d.status = 'OPEN' THEN d.created_at END ASC,
                 d.closed_at DESC
        LIMIT $2 OFFSET $3
    `, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AdminDispute{}
	for rows.Next() {
		var a AdminDispute
		var closedBy uuid.NullUUID
		if err := rows.Scan(&a.ID, &a.OrderID, &a.CustomerID, &a.ExecutorID, &a.Claim, &a.Status,
			&a.Closure, &a.Decision, &a.ResolutionNote, &a.CreatedAt, &a.ClosedAt, &closedBy,
			&a.OrderStatus, &a.OrderAddress, &a.OrderComment,
			&a.HoldAmount, &a.FinalAmount, &a.ServiceName,
			&a.OrderCreated, &a.OrderAssigned,
			&a.CustomerName, &a.CustomerPhone, &a.ExecutorName, &a.ExecutorPhone,
			&a.CustomerActivePoints, &a.ExecutorActivePoints, &a.ExecutorDisputesTotal); err != nil {
			return nil, err
		}
		a.ClosedBy = nullableUUID(closedBy)
		out = append(out, a)
	}
	return out, rows.Err()
}
