package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// Bid represents an executor price offer for construction waste orders.
type Bid struct {
	ID            uuid.UUID    `json:"id"`
	OrderID       uuid.UUID    `json:"order_id"`
	ExecutorID    uuid.UUID    `json:"executor_id"`
	OfferedPrice  money.Amount `json:"offered_price"`
	Status        string       `json:"status"` // PENDING, ACCEPTED, REJECTED
	CreatedAt     time.Time    `json:"created_at"`
	ExecutorPhone string       `json:"executor_phone,omitempty"`
}

// BidRepository defines database operations for bidding. Accepting a bid is a
// business transaction and lives in the service layer; the repository only
// provides the locked read and the individual writes it needs.
type BidRepository interface {
	CreateBid(orderID, executorID uuid.UUID, offeredPrice money.Amount) (*Bid, error)
	GetBidsForOrder(orderID uuid.UUID) ([]*Bid, error)
	LockBidForUpdate(q Querier, bidID uuid.UUID) (*Bid, error)
	SetBidStatus(q Querier, bidID uuid.UUID, status string) error
	RejectOtherBids(q Querier, orderID, exceptBidID uuid.UUID) error
}

type bidRepo struct {
	db *sql.DB
}

// NewBidRepository creates a new BidRepository.
func NewBidRepository(db *sql.DB) BidRepository {
	return &bidRepo{db: db}
}

func (r *bidRepo) CreateBid(orderID, executorID uuid.UUID, offeredPrice money.Amount) (*Bid, error) {
	// 1. Check if the order is an auction and in SEARCHING status
	var isAuction bool
	var status string
	err := r.db.QueryRow(`
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

	// 2. Insert the bid. One executor holds at most one bid per order, so a
	//    repeated submission updates the offer instead of stacking duplicates
	//    (the unique index is created in migration 024).
	query := `
		INSERT INTO bids (order_id, executor_id, offered_price, status, created_at)
		VALUES ($1, $2, $3, 'PENDING', now())
		ON CONFLICT (order_id, executor_id)
		DO UPDATE SET offered_price = EXCLUDED.offered_price, status = 'PENDING', created_at = now()
		RETURNING id, order_id, executor_id, offered_price, status, created_at`

	var b Bid
	err = r.db.QueryRow(query, orderID, executorID, offeredPrice).Scan(
		&b.ID, &b.OrderID, &b.ExecutorID, &b.OfferedPrice, &b.Status, &b.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &b, nil
}

func (r *bidRepo) GetBidsForOrder(orderID uuid.UUID) ([]*Bid, error) {
	query := `
		SELECT b.id, b.order_id, b.executor_id, b.offered_price, b.status, b.created_at, u.phone
		FROM bids b
		JOIN users u ON b.executor_id = u.id
		WHERE b.order_id = $1
		ORDER BY b.offered_price ASC, b.created_at DESC`

	rows, err := r.db.Query(query, orderID)
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

// LockBidForUpdate reads a bid taking a row lock, so two customers accepting
// concurrently serialise instead of both seeing it as PENDING.
func (r *bidRepo) LockBidForUpdate(q Querier, bidID uuid.UUID) (*Bid, error) {
	var b Bid
	err := r.exec(q).QueryRow(`
		SELECT id, order_id, executor_id, offered_price, status, created_at
		FROM bids WHERE id = $1 FOR UPDATE`, bidID).Scan(
		&b.ID, &b.OrderID, &b.ExecutorID, &b.OfferedPrice, &b.Status, &b.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// SetBidStatus moves a bid out of PENDING; the guard keeps a concurrent accept
// from overwriting an already decided bid.
func (r *bidRepo) SetBidStatus(q Querier, bidID uuid.UUID, status string) error {
	return execExpectingOne(r.exec(q),
		`UPDATE bids SET status = $1 WHERE id = $2 AND status = 'PENDING'`, status, bidID)
}

// RejectOtherBids closes every other open offer on an order. It may legitimately
// affect no rows, so it is not guarded.
func (r *bidRepo) RejectOtherBids(q Querier, orderID, exceptBidID uuid.UUID) error {
	_, err := r.exec(q).Exec(
		`UPDATE bids SET status = 'REJECTED' WHERE order_id = $1 AND id != $2 AND status = 'PENDING'`,
		orderID, exceptBidID)
	return err
}

func (r *bidRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}
