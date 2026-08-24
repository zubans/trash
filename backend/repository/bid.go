package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Bid represents an executor price offer for construction waste orders.
type Bid struct {
	ID            uuid.UUID `json:"id"`
	OrderID       uuid.UUID `json:"order_id"`
	ExecutorID    uuid.UUID `json:"executor_id"`
	OfferedPrice  float64   `json:"offered_price"`
	Status        string    `json:"status"` // PENDING, ACCEPTED, REJECTED
	CreatedAt     time.Time `json:"created_at"`
	ExecutorPhone string    `json:"executor_phone,omitempty"`
}

// BidRepository defines database operations for bidding.
type BidRepository interface {
	CreateBid(orderID, executorID uuid.UUID, offeredPrice float64) (*Bid, error)
	GetBidsForOrder(orderID uuid.UUID) ([]*Bid, error)
	AcceptBid(bidID, customerID uuid.UUID) error
}

type bidRepo struct {
	db *sql.DB
}

// NewBidRepository creates a new BidRepository.
func NewBidRepository(db *sql.DB) BidRepository {
	return &bidRepo{db: db}
}

func (r *bidRepo) CreateBid(orderID, executorID uuid.UUID, offeredPrice float64) (*Bid, error) {
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

func (r *bidRepo) AcceptBid(bidID, customerID uuid.UUID) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Get bid details
	var orderID, executorID uuid.UUID
	var offeredPrice float64
	var bidStatus string
	err = tx.QueryRow(`
		SELECT order_id, executor_id, offered_price, status 
		FROM bids 
		WHERE id = $1 FOR UPDATE`, bidID).Scan(&orderID, &executorID, &offeredPrice, &bidStatus)
	if err != nil {
		return err
	}
	if bidStatus != "PENDING" {
		return errors.New("bid is not pending")
	}

	// 2. Lock and verify order ownership & status
	var ordCustomerID uuid.UUID
	var ordStatus string
	var isAuction bool
	err = tx.QueryRow(`
		SELECT o.customer_id, o.status, sn.is_auction
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE o.id = $1 FOR UPDATE`, orderID).Scan(&ordCustomerID, &ordStatus, &isAuction)
	if err != nil {
		return err
	}
	if ordCustomerID != customerID {
		return errors.New("forbidden: you do not own this order")
	}
	if ordStatus != "SEARCHING" {
		return errors.New("order is no longer in searching status")
	}
	if !isAuction {
		return errors.New("order is not an auction")
	}

	// 3. Lock and check customer balance
	var balance float64
	err = tx.QueryRow(`SELECT balance FROM users WHERE id = $1 FOR UPDATE`, customerID).Scan(&balance)
	if err != nil {
		return err
	}
	if balance < offeredPrice {
		return errors.New("insufficient balance to accept this bid")
	}

	// 4. Deduct customer balance
	_, err = tx.Exec(`UPDATE users SET balance = balance - $1 WHERE id = $2`, offeredPrice, customerID)
	if err != nil {
		return err
	}

	// 5. Update order: status to ASSIGNED, executor assigned, hold_amount set to offered price
	_, err = tx.Exec(`
		UPDATE orders 
		SET executor_id = $1, status = 'ASSIGNED', hold_amount = $2, final_amount = $2 
		WHERE id = $3`,
		executorID, offeredPrice, orderID)
	if err != nil {
		return err
	}

	// 6. Update bid status to ACCEPTED
	_, err = tx.Exec(`UPDATE bids SET status = 'ACCEPTED' WHERE id = $1`, bidID)
	if err != nil {
		return err
	}

	// 7. Update other bids for this order to REJECTED
	_, err = tx.Exec(`UPDATE bids SET status = 'REJECTED' WHERE order_id = $1 AND id != $2`, orderID, bidID)
	if err != nil {
		return err
	}

	// 8. Log HOLD transaction
	_, err = tx.Exec(`
		INSERT INTO transactions (user_id, order_id, type, amount, created_at)
		VALUES ($1, $2, 'HOLD', $3, now())`,
		customerID, orderID, offeredPrice)
	if err != nil {
		return err
	}

	// 9. Create Chat Room
	_, err = tx.Exec(`
		INSERT INTO chats (order_id, is_active) 
		VALUES ($1, TRUE) 
		ON CONFLICT (order_id) DO UPDATE SET is_active = TRUE`, orderID)
	if err != nil {
		return err
	}

	return tx.Commit()
}
