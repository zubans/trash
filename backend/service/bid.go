package service

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Ошибки торгов.
var (
	// ErrBidNotPending — решить можно только ожидающую ставку.
	ErrBidNotPending = stateError("bid is not pending")
	// ErrBidStale — ставка уже неактуальна: заказ ушёл или её решили раньше.
	ErrBidStale = conflictError("предложение уже неактуально, обновите список")
	// ErrOrderNotSearching — ставки принимаются только по заказу в поиске.
	ErrOrderNotSearching = stateError("order is no longer in searching status")
	// ErrOrderNotAuction — заказ не аукционный, ставок по нему нет.
	ErrOrderNotAuction = stateError("order is not an auction")
	// ErrBidderOffShift — исполнитель, чью ставку принимают, уже не на смене.
	ErrBidderOffShift = ruleError("исполнитель сейчас не на смене, выберите другое предложение")
)

// BidService управляет бизнес-операциями торгов.
type BidService struct {
	// penalties — тихая блокировка ролей; nil означает «механизм штрафов не подключён».
	penalties   *PenaltyService
	bidRepo     repository.BidRepository
	orderRepo   repository.OrderRepository
	shiftRepo   repository.ShiftRepository
	ledger      *Ledger
	userRepo    repository.UserRepository
	catalogRepo repository.ServiceCatalogRepository
	chatRepo    repository.ChatRepository
	// behaviors применяет скриптовые правила услуги, когда они у варианта есть;
	// events записывает то, на что реагирует поведение. Оба необязательны: nil
	// означает, что ни у одной услуги нет скриптовых правил.
	behaviors *Behaviors
	events    repository.EventRepository
}

// WithBehaviors подключает скрипты поведений к воротам аукциона, чтобы
// скриптовая услуга ограничивала ставки ровно так же, как ограничивает
// принятие, и публиковала событие назначения, на которое реагирует поведение.
func (s *BidService) WithBehaviors(behaviors *Behaviors, events repository.EventRepository) *BidService {
	s.behaviors = behaviors
	s.events = events
	return s
}

// NewBidService создаёт новый BidService.
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

// maxBidPrice ограничивает предложение десятью миллионами рублей, чтобы
// опечатка (или злонамеренный клиент) не протолкнула значение, которое не
// влезет в колонку NUMERIC(18,2).
const maxBidPrice = money.Amount(10_000_000 * 100)

// CreateBid подаёт ставку по заказу на вывоз строительного мусора.
func (s *BidService) CreateBid(ctx context.Context, orderID, executorID uuid.UUID, offeredPrice money.Amount) (*repository.Bid, error) {
	if !offeredPrice.IsPositive() {
		return nil, validationError("offered price must be greater than zero")
	}
	if offeredPrice > maxBidPrice {
		return nil, validationError("offered price is too large")
	}

	// Проверяем, что у исполнителя есть активная смена
	shift, err := s.shiftRepo.FindActiveByExecutor(ctx, executorID)
	if err != nil {
		return nil, err
	}
	if shift == nil {
		return nil, ruleError("cannot place a bid without an active work shift")
	}

	// Те же правила возраста и верификации, что применяются к принятию заказа,
	// применяются и к ставке по нему.
	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return nil, orderNotFound(err)
	}
	if order.CustomerID == executorID {
		return nil, ruleError("нельзя делать ставки на собственный заказ")
	}
	if s.userRepo != nil && s.catalogRepo != nil {
		if _, err := eligibilityFor(ctx, s.userRepo, s.catalogRepo, s.behaviors, s.penalties, executorID, order); err != nil {
			return nil, err
		}
	}

	if s.ledger != nil {
		balance, err := s.ledger.GetBalance(ctx, executorID)
		if err != nil {
			return nil, err
		}
		if balance.IsNegative() {
			return nil, ruleError("нельзя делать ставки при отрицательном балансе (уход в минус)")
		}
	}

	bid, err := s.bidRepo.CreateBid(ctx, orderID, executorID, offeredPrice)
	if err != nil {
		return nil, err
	}
	metrics.BidEvent("placed")
	return bid, nil
}

// GetBidsForOrder перечисляет ставки по заказу, но только для владеющего им
// заказчика — ставки несут контактные данные исполнителей.
func (s *BidService) GetBidsForOrder(ctx context.Context, orderID, customerID uuid.UUID) ([]*repository.Bid, error) {
	order, err := s.orderRepo.FindByID(ctx, orderID)
	if err != nil {
		return nil, orderNotFound(err)
	}
	if order.CustomerID != customerID {
		return nil, ErrForbidden
	}
	return s.bidRepo.GetBidsForOrder(ctx, orderID)
}

// AcceptBid принимает предложение: он удерживает деньги заказчика, назначает
// исполнителя и закрывает остальные предложения — всё в одной транзакции.
//
// Раньше это жило в репозитории, из-за чего применялся другой набор правил, чем
// в Accept() для обычных заказов: исполнитель мог выиграть аукцион забаненным,
// вне смены или моложе возраста, которого требует вариант услуги.
func (s *BidService) AcceptBid(ctx context.Context, bidID, customerID uuid.UUID) error {
	var acceptedOrderID uuid.UUID

	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		bid, err := s.bidRepo.LockBidForUpdate(ctx, tx, bidID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, repository.ErrNotFound) {
				return ErrBidNotFound
			}
			return err
		}
		if bid.Status != "PENDING" {
			return ErrBidNotPending
		}

		order, err := s.orderRepo.LockForUpdate(ctx, tx, bid.OrderID)
		if err != nil {
			return orderNotFound(err)
		}
		if order.CustomerID != customerID {
			return ErrForbidden
		}
		if order.Status != repository.OrderStatusSearching {
			return ErrOrderNotSearching
		}

		// Исполнителю должно быть по-прежнему позволено взять этот заказ в момент,
		// когда заказчик принимает, а не только когда ставка подавалась.
		variant, err := eligibilityFor(ctx, s.userRepo, s.catalogRepo, s.behaviors, s.penalties, bid.ExecutorID, order)
		if err != nil {
			return err
		}
		if variant == nil || !variant.IsAuction {
			return ErrOrderNotAuction
		}
		shift, err := s.shiftRepo.FindActiveByExecutor(ctx, bid.ExecutorID)
		if err != nil {
			return err
		}
		if shift == nil {
			return ErrBidderOffShift
		}

		// Принятая цена уходит от заказчика в эскроу, ровно как удержание по
		// обычному заказу.
		if err := s.ledger.Reserve(ctx, tx, customerID, repository.AccountEscrow, bid.OfferedPrice, repository.TransactionTypeHold, &order.ID); err != nil {
			if errors.Is(err, repository.ErrInsufficientFunds) {
				return ErrInsufficientBalance
			}
			return err
		}
		if err := s.orderRepo.AssignWithHold(ctx, tx, order.ID, bid.ExecutorID, bid.OfferedPrice); err != nil {
			return err
		}
		if err := s.bidRepo.SetBidStatus(ctx, tx, bid.ID, "ACCEPTED"); err != nil {
			return err
		}
		if err := s.bidRepo.RejectOtherBids(ctx, tx, order.ID, bid.ID); err != nil {
			return err
		}
		acceptedOrderID = order.ID
		// Победа в аукционе назначает заказ ровно так же, как его принятие, поэтому
		// публикуется то же событие, в той же транзакции.
		if s.events != nil {
			if err := s.events.Publish(ctx, tx, &repository.DomainEvent{
				Type:        repository.EventOrderAccepted,
				SubjectType: repository.EventSubjectOrder,
				SubjectID:   order.ID,
				ActorID:     &bid.ExecutorID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return ErrBidStale
		}
		return err
	}

	// Чат-комната не входит в денежную транзакцию: неудача при её создании не
	// должна отменять принятую ставку.
	if s.chatRepo != nil {
		if _, err := s.chatRepo.CreateChat(ctx, acceptedOrderID); err != nil {
			log.Printf("[BidService] failed to create chat for order %s: %v", acceptedOrderID, err)
		}
	}
	metrics.BidEvent("accepted")
	metrics.OrderEvent("assigned")
	return nil
}

// WithPenalties подключает тихую блокировку: заблокированный исполнитель не
// видит заказов и не может их брать.
func (s *BidService) WithPenalties(penalties *PenaltyService) *BidService {
	s.penalties = penalties
	return s
}
