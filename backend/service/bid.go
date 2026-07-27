package service

import (
	"errors"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// BidService manages bidding business operations.
type BidService struct {
	bidRepo   repository.BidRepository
	orderRepo repository.OrderRepository
	shiftRepo repository.ShiftRepository
}

// NewBidService creates a new BidService.
func NewBidService(
	bidRepo repository.BidRepository,
	orderRepo repository.OrderRepository,
	shiftRepo repository.ShiftRepository,
) *BidService {
	return &BidService{
		bidRepo:   bidRepo,
		orderRepo: orderRepo,
		shiftRepo: shiftRepo,
	}
}

// CreateBid submits a bid on a construction waste order.
func (s *BidService) CreateBid(orderID, executorID uuid.UUID, offeredPrice float64) (*repository.Bid, error) {
	if offeredPrice <= 0 {
		return nil, errors.New("offered price must be greater than zero")
	}

	// Verify executor has an active shift
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return nil, err
	}
	if shift == nil {
		return nil, errors.New("cannot place a bid without an active work shift")
	}

	return s.bidRepo.CreateBid(orderID, executorID, offeredPrice)
}

// GetBidsForOrder lists bids placed on a customer's order.
func (s *BidService) GetBidsForOrder(orderID uuid.UUID) ([]*repository.Bid, error) {
	return s.bidRepo.GetBidsForOrder(orderID)
}

// AcceptBid accepts a bid, holds customer balance, and assigns the executor.
func (s *BidService) AcceptBid(bidID, customerID uuid.UUID) error {
	return s.bidRepo.AcceptBid(bidID, customerID)
}
