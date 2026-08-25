package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockBidRepo struct {
	bids []*repository.Bid
}

func (m *mockBidRepo) CreateBid(orderID, executorID uuid.UUID, offeredPrice float64) (*repository.Bid, error) {
	b := &repository.Bid{
		ID:           uuid.New(),
		OrderID:      orderID,
		ExecutorID:   executorID,
		OfferedPrice: offeredPrice,
		Status:       "PENDING",
		CreatedAt:    time.Now(),
	}
	m.bids = append(m.bids, b)
	return b, nil
}

func (m *mockBidRepo) GetBidsForOrder(orderID uuid.UUID) ([]*repository.Bid, error) {
	var list []*repository.Bid
	for _, b := range m.bids {
		if b.OrderID == orderID {
			list = append(list, b)
		}
	}
	return list, nil
}

func (m *mockBidRepo) LockBidForUpdate(q repository.Querier, bidID uuid.UUID) (*repository.Bid, error) {
	for _, b := range m.bids {
		if b.ID == bidID {
			return b, nil
		}
	}
	return nil, errors.New("bid not found")
}

func (m *mockBidRepo) SetBidStatus(q repository.Querier, bidID uuid.UUID, status string) error {
	for _, b := range m.bids {
		if b.ID == bidID {
			// Mirrors the guarded UPDATE: only a pending bid can be decided.
			if b.Status != "PENDING" {
				return repository.ErrConflict
			}
			b.Status = status
			return nil
		}
	}
	return repository.ErrConflict
}

func (m *mockBidRepo) RejectOtherBids(q repository.Querier, orderID, exceptBidID uuid.UUID) error {
	for _, b := range m.bids {
		if b.OrderID == orderID && b.ID != exceptBidID && b.Status == "PENDING" {
			b.Status = "REJECTED"
		}
	}
	return nil
}

func TestBidService_CreateBid(t *testing.T) {
	bidRepo := &mockBidRepo{}
	shiftRepo := &mockShiftRepo{}
	orderRepo := &mockOrderRepo{}
	catalogRepo := newMockCatalogRepo()
	userRepo := newMockUserRepo()
	srv := NewBidService(bidRepo, orderRepo, shiftRepo, &mockTransactionRepo{}, userRepo, catalogRepo, nil)

	executorID := uuid.New()

	// The order has to exist: bidding now re-checks the order and the executor.
	order := &repository.Order{
		ID:               uuid.New(),
		CustomerID:       uuid.New(),
		ServiceVariantID: constructionVariantID,
		Status:           repository.OrderStatusSearching,
	}
	orderRepo.orders = append(orderRepo.orders, order)
	orderID := order.ID

	// Case 1: No active shift (should fail)
	_, err := srv.CreateBid(orderID, executorID, 350.00)
	if err == nil {
		t.Error("expected error placing bid without active shift")
	}

	// Case 2: Active shift (should succeed)
	_, _ = shiftRepo.StartShift(executorID, 1) // Start active shift in mock
	bid, err := srv.CreateBid(orderID, executorID, 350.00)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bid.OfferedPrice != 350.00 {
		t.Errorf("expected price 350.00, got %f", bid.OfferedPrice)
	}

	// Case 3: Invalid price (should fail)
	_, err = srv.CreateBid(orderID, executorID, -10.0)
	if err == nil {
		t.Error("expected error placing bid with negative price")
	}

	// Case 4: bidding on your own order is refused.
	_, err = srv.CreateBid(orderID, order.CustomerID, 350.00)
	if err == nil {
		t.Error("expected error placing bid on own order")
	}
}
