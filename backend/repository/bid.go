package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// Bid представляет ценовое предложение исполнителя по заказам на строительный мусор.
type Bid struct {
	ID            uuid.UUID    `json:"id"`
	OrderID       uuid.UUID    `json:"order_id"`
	ExecutorID    uuid.UUID    `json:"executor_id"`
	OfferedPrice  money.Amount `json:"offered_price"`
	Status        string       `json:"status"` // PENDING, ACCEPTED, REJECTED
	CreatedAt     time.Time    `json:"created_at"`
	ExecutorPhone string       `json:"executor_phone,omitempty"`
}

// BidRepository описывает операции с базой для торгов. Принятие ставки — это
// бизнес-транзакция, и она живёт в слое сервисов; репозиторий предоставляет
// лишь заблокированное чтение и нужные ему отдельные записи.
type BidRepository interface {
	CreateBid(ctx context.Context, orderID, executorID uuid.UUID, offeredPrice money.Amount) (*Bid, error)
	GetBidsForOrder(ctx context.Context, orderID uuid.UUID) ([]*Bid, error)
	LockBidForUpdate(ctx context.Context, q Querier, bidID uuid.UUID) (*Bid, error)
	SetBidStatus(ctx context.Context, q Querier, bidID uuid.UUID, status string) error
	RejectOtherBids(ctx context.Context, q Querier, orderID, exceptBidID uuid.UUID) error
}

type bidRepo struct {
	db *sql.DB
}

// NewBidRepository создаёт новый BidRepository.
func NewBidRepository(db *sql.DB) BidRepository {
	return &bidRepo{db: db}
}

func (r *bidRepo) CreateBid(ctx context.Context, orderID, executorID uuid.UUID, offeredPrice money.Amount) (*Bid, error) {
	// 1. Проверяем, что заказ — аукцион и в статусе SEARCHING
	var isAuction bool
	var status string
	err := r.db.QueryRowContext(ctx, `
		SELECT sn.is_auction, o.status
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.id = $1`, orderID).Scan(&isAuction, &status)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("order not found")
		}
		return nil, err
	}
	if !isAuction {
		return nil, errors.New("cannot bid on non-auction orders")
	}
	if status != "SEARCHING" {
		return nil, errors.New("order is not open for bidding")
	}

	// 2. Вставляем ставку. У одного исполнителя не больше одной ставки на заказ,
	//    поэтому повторная отправка обновляет предложение, а не громоздит дубли
	//    (уникальный индекс создан в миграции 024).
	query := `
		INSERT INTO bids (order_id, executor_id, offered_price, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		ON CONFLICT (order_id, executor_id)
		DO UPDATE SET offered_price = EXCLUDED.offered_price, status = 'PENDING', created_at = now()
		RETURNING id, order_id, executor_id, offered_price, status, created_at`

	var b Bid
	err = r.db.QueryRowContext(ctx, query, orderID, executorID, offeredPrice).Scan(
		&b.ID, &b.OrderID, &b.ExecutorID, &b.OfferedPrice, &b.Status, &b.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (r *bidRepo) GetBidsForOrder(ctx context.Context, orderID uuid.UUID) ([]*Bid, error) {
	query := `
		SELECT b.id, b.order_id, b.executor_id, b.offered_price, b.status, b.created_at, u.phone
		FROM bids b
		JOIN users u ON b.executor_id = u.id
		WHERE b.order_id = $1
		ORDER BY b.offered_price ASC, b.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bids []*Bid
	for rows.Next() {
		var b Bid
		err := rows.Scan(&b.ID, &b.OrderID, &b.ExecutorID, &b.OfferedPrice, &b.Status, &b.CreatedAt, &b.ExecutorPhone)
		if err != nil {
			return nil, err
		}
		bids = append(bids, &b)
	}
	return bids, rows.Err()
}

// LockBidForUpdate читает ставку, беря блокировку строки, чтобы два заказчика,
// принимающих одновременно, сериализовались, а не увидели её оба как PENDING.
func (r *bidRepo) LockBidForUpdate(ctx context.Context, q Querier, bidID uuid.UUID) (*Bid, error) {
	var b Bid
	err := r.exec(ctx, q).QueryRowContext(ctx, `
		SELECT id, order_id, executor_id, offered_price, status, created_at
		FROM bids WHERE id = $1 FOR UPDATE`, bidID).Scan(
		&b.ID, &b.OrderID, &b.ExecutorID, &b.OfferedPrice, &b.Status, &b.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SetBidStatus выводит ставку из PENDING; охрана не даёт параллельному принятию
// переписать уже решённую ставку.
func (r *bidRepo) SetBidStatus(ctx context.Context, q Querier, bidID uuid.UUID, status string) error {
	return execExpectingOne(ctx, r.exec(ctx, q),
		`UPDATE bids SET status = $1 WHERE id = $2 AND status = 'PENDING'`, status, bidID)
}

// RejectOtherBids закрывает все прочие открытые предложения по заказу. Он
// законно может не затронуть ни одной строки, поэтому не охраняется.
func (r *bidRepo) RejectOtherBids(ctx context.Context, q Querier, orderID, exceptBidID uuid.UUID) error {
	_, err := r.exec(ctx, q).ExecContext(ctx,
		`UPDATE bids SET status = 'REJECTED' WHERE order_id = $1 AND id != $2 AND status = 'PENDING'`,
		orderID, exceptBidID)
	return err
}

func (r *bidRepo) exec(ctx context.Context, q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}
