package service

import (
	"database/sql"
	"errors"
	"log"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// BidService manages bidding business operations.
type BidService struct {
	bidRepo     repository.BidRepository
	orderRepo   repository.OrderRepository
	shiftRepo   repository.ShiftRepository
	ledger      *Ledger
	userRepo    repository.UserRepository
	catalogRepo repository.ServiceCatalogRepository
	chatRepo    repository.ChatRepository
}

// NewBidService creates a new BidService.
func NewBidService(
	bidRepo repository.BidRepository,
	orderRepo repository.OrderRepository,
	shiftRepo repository.ShiftRepository,
	ledger *Ledger,
	userRepo repository.UserRepository,
	catalogRepo repository.ServiceCatalogRepository,
	chatRepo repository.ChatRepository,
) *BidService {
	return &BidService{
		bidRepo:     bidRepo,
		orderRepo:   orderRepo,
		shiftRepo:   shiftRepo,
		ledger:      ledger,
		userRepo:    userRepo,
		catalogRepo: catalogRepo,
		chatRepo:    chatRepo,
	}
}

// maxBidPrice caps an offer so a typo (or an abusive client) cannot push a
// value the NUMERIC(18,2) column cannot hold.
// maxBidPrice caps an offer at ten million rubles.
const maxBidPrice = money.Amount(10_000_000 * 100)

// CreateBid submits a bid on a construction waste order.
func (s *BidService) CreateBid(orderID, executorID uuid.UUID, offeredPrice money.Amount) (*repository.Bid, error) {
	if !offeredPrice.IsPositive() {
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

	if s.ledger != nil {
		balance, err := s.ledger.GetBalance(executorID)
		if err != nil {
			return nil, err
		}
		if balance.IsNegative() {
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

// AcceptBid accepts an offer: it holds the customer's money, assigns the
// executor and closes the remaining offers, all in one transaction.
//
// This used to live in the repository, which meant it applied a different set
// of rules than Accept() for regular orders — an executor could win an auction
// while banned, off shift, or below the age the service variant requires.
func (s *BidService) AcceptBid(bidID, customerID uuid.UUID) error {
	var acceptedOrderID uuid.UUID

	err := s.ledger.RunInTx(func(tx *sql.Tx) error {
		bid, err := s.bidRepo.LockBidForUpdate(tx, bidID)
		if err != nil {
			return errors.New("bid not found")
		}
		if bid.Status != "PENDING" {
			return errors.New("bid is not pending")
		}

		order, err := s.orderRepo.LockForUpdate(tx, bid.OrderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.CustomerID != customerID {
			return errors.New("forbidden: you do not own this order")
		}
		if order.Status != repository.OrderStatusSearching {
			return errors.New("order is no longer in searching status")
		}

		variant, err := s.catalogRepo.GetNodeByID(order.ServiceVariantID)
		if err != nil {
			return err
		}
		if variant == nil || !variant.IsAuction {
			return errors.New("order is not an auction")
		}

		// The executor must still be allowed to take this order at the moment
		// the customer accepts, not only when the bid was placed.
		executor, err := s.userRepo.FindByID(bid.ExecutorID)
		if err != nil {
			return errors.New("executor not found")
		}
		if err := canExecutorTakeOrder(executor, variant); err != nil {
			return err
		}
		shift, err := s.shiftRepo.GetActiveShift(bid.ExecutorID)
		if err != nil || shift == nil {
			return errors.New("исполнитель сейчас не на смене, выберите другое предложение")
		}

		// The accepted price moves from the customer into escrow, exactly like a
		// regular order hold.
		if err := s.ledger.Reserve(tx, customerID, repository.AccountEscrow, bid.OfferedPrice, repository.TransactionTypeHold, &order.ID); err != nil {
			return err
		}
		if err := s.orderRepo.AssignWithHold(tx, order.ID, bid.ExecutorID, bid.OfferedPrice); err != nil {
			return err
		}
		if err := s.bidRepo.SetBidStatus(tx, bid.ID, "ACCEPTED"); err != nil {
			return err
		}
		if err := s.bidRepo.RejectOtherBids(tx, order.ID, bid.ID); err != nil {
			return err
		}
		acceptedOrderID = order.ID
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientFunds) {
			return errors.New("insufficient balance to accept this bid")
		}
		if errors.Is(err, repository.ErrConflict) {
			return errors.New("предложение уже неактуально, обновите список")
		}
		return err
	}

	// The chat room is not part of the money transaction: failing to create it
	// must not undo an accepted bid.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(acceptedOrderID); err != nil {
			log.Printf("[BidService] failed to create chat for order %s: %v", acceptedOrderID, err)
		}
	}
	return nil
}
