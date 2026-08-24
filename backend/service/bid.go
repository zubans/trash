package service

import (
	"errors"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// BidService manages bidding business operations.
type BidService struct {
	bidRepo         repository.BidRepository
	orderRepo       repository.OrderRepository
	shiftRepo       repository.ShiftRepository
	transactionRepo repository.TransactionRepository
	userRepo        repository.UserRepository
	catalogRepo     repository.ServiceCatalogRepository
}

// NewBidService creates a new BidService.
func NewBidService(
	bidRepo repository.BidRepository,
	orderRepo repository.OrderRepository,
	shiftRepo repository.ShiftRepository,
	transactionRepo repository.TransactionRepository,
	userRepo repository.UserRepository,
	catalogRepo repository.ServiceCatalogRepository,
) *BidService {
	return &BidService{
		bidRepo:         bidRepo,
		orderRepo:       orderRepo,
		shiftRepo:       shiftRepo,
		transactionRepo: transactionRepo,
		userRepo:        userRepo,
		catalogRepo:     catalogRepo,
	}
}

// maxBidPrice caps an offer so a typo (or an abusive client) cannot push a
// value the NUMERIC(18,2) column cannot hold.
const maxBidPrice = 10_000_000.0

// CreateBid submits a bid on a construction waste order.
func (s *BidService) CreateBid(orderID, executorID uuid.UUID, offeredPrice float64) (*repository.Bid, error) {
	if offeredPrice <= 0 {
		return nil, errors.New("offered price must be greater than zero")
	}
	if offeredPrice > maxBidPrice {
		return nil, errors.New("offered price is too large")
	}

	// Verify executor has an active shift
	shift, err := s.shiftRepo.GetActiveShift(executorID)
	if err != nil {
		return nil, err
	}
	if shift == nil {
		return nil, errors.New("cannot place a bid without an active work shift")
	}

	// The same age / verification rules that apply to accepting an order apply
	// to bidding on one.
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, errors.New("order not found")
	}
	if order.CustomerID == executorID {
		return nil, errors.New("нельзя делать ставки на собственный заказ")
	}
	if s.userRepo != nil && s.catalogRepo != nil {
		executor, err := s.userRepo.FindByID(executorID)
		if err != nil {
			return nil, errors.New("executor not found")
		}
		variant, err := s.catalogRepo.GetNodeByID(order.ServiceVariantID)
		if err != nil {
			return nil, err
		}
		if err := canExecutorTakeOrder(executor, variant); err != nil {
			return nil, err
		}
	}

	if s.transactionRepo != nil {
		balance, err := s.transactionRepo.GetBalance(executorID)
		if err != nil {
			return nil, err
		}
		if balance < 0 {
			return nil, errors.New("нельзя делать ставки при отрицательном балансе (уход в минус)")
		}
	}

	return s.bidRepo.CreateBid(orderID, executorID, offeredPrice)
}

// GetBidsForOrder lists bids placed on an order, but only for the customer who
// owns it — bids carry executor contact details.
func (s *BidService) GetBidsForOrder(orderID, customerID uuid.UUID) ([]*repository.Bid, error) {
	order, err := s.orderRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, errors.New("order not found")
	}
	if order.CustomerID != customerID {
		return nil, errors.New("forbidden: you do not own this order")
	}
	return s.bidRepo.GetBidsForOrder(orderID)
}

// AcceptBid accepts a bid, holds customer balance, and assigns the executor.
func (s *BidService) AcceptBid(bidID, customerID uuid.UUID) error {
	return s.bidRepo.AcceptBid(bidID, customerID)
}
