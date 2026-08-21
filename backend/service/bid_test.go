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

func (m *mockBidRepo) AcceptBid(bidID, customerID uuid.UUID) error {
	for _, b := range m.bids {
		if b.ID == bidID {
			b.Status = "ACCEPTED"
			return nil
		}
	}
	return errors.New("bid not found")
}

func TestBidService_CreateBid(t *testing.T) {
	bidRepo := &mockBidRepo{}
	shiftRepo := &mockShiftRepo{}
	srv := NewBidService(bidRepo, nil, shiftRepo, nil)

	orderID := uuid.New()
	executorID := uuid.New()

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
}
