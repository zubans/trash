package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// Споры по исполненным заказам. План механики —
// doc/implementation_plan_disputes_penalties_photo_proof.md.
//
// Спор открывает заказчик на заказе в EXECUTED, и закрывается он первым из
// трёх событий: заказчик подтвердил выполнение, исполнитель признал, что не
// выполнил, или решил арбитр. Каждое закрытие берёт блокировку строки заказа,
// поэтому два закрытия одного спора не пройдут: второе увидит уже закрытый
// заказ.

// maxDisputeClaimRunes ограничивает претензию: это описание, что не так, а не
// переписка — для неё есть чат заказа.
const maxDisputeClaimRunes = 2000

var (
	// ErrDisputeClaimRequired — претензия пустая.
	ErrDisputeClaimRequired = errors.New("опишите, что не выполнено")
	// ErrDisputeClaimTooLong — претензия длиннее maxDisputeClaimRunes.
	ErrDisputeClaimTooLong = errors.New("описание претензии слишком длинное")
	// ErrDisputeNotAllowed — заказ нельзя оспорить в его нынешнем виде.
	ErrDisputeNotAllowed = errors.New("оспорить можно только заказ, отмеченный исполнителем как выполненный")
	// ErrDisputeAlreadyOpen — по заказу уже идёт спор.
	ErrDisputeAlreadyOpen = errors.New("по заказу уже открыт спор")
	// ErrDisputeNotOpen — по заказу нет открытого спора.
	ErrDisputeNotOpen = errors.New("по заказу нет открытого спора")
	// ErrOrderHasOpenDispute — заказ пытаются закрыть в обход его спора.
	ErrOrderHasOpenDispute = errors.New("по заказу открыт спор")
)

// WithDisputes подключает споры.
func (s *OrderService) WithDisputes(disputes repository.DisputeRepository) *OrderService {
	s.disputes = disputes
	return s
}

// OpenDispute — заказчик заявляет, что исполненный заказ не выполнен.
func (s *OrderService) OpenDispute(ctx context.Context, customerID, orderID uuid.UUID, claim string) (*repository.Dispute, error) {
	if s.disputes == nil {
		return nil, ErrDisputeNotAllowed
	}
	claim = strings.TrimSpace(claim)
	if claim == "" {
		return nil, ErrDisputeClaimRequired
	}
	if utf8.RuneCountInString(claim) > maxDisputeClaimRunes {
		return nil, ErrDisputeClaimTooLong
	}

	var dispute *repository.Dispute
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.CustomerID != customerID {
			return errors.New("forbidden")
		}
		if order.Status == repository.OrderStatusDisputed {
			return ErrDisputeAlreadyOpen
		}
		if order.Status != repository.OrderStatusExecuted || order.ExecutorID == nil {
			return ErrDisputeNotAllowed
		}
		// Скриптовая услуга (верификация) закрывается сама по своему событию, а не
		// подтверждением заказчика, — спорить в ней не о чем, и спор повис бы
		// против скрипта, который его не видит.
		if variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID); err == nil && variant.HasBehavior() {
			return ErrDisputeNotAllowed
		}

		if err := s.orderRepo.MarkDisputed(ctx, tx, orderID); err != nil {
			return err
		}
		dispute = &repository.Dispute{
			OrderID:    order.ID,
			CustomerID: order.CustomerID,
			ExecutorID: *order.ExecutorID,
			Claim:      claim,
		}
		if err := s.disputes.Open(ctx, tx, dispute); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return ErrDisputeAlreadyOpen
			}
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventDisputeOpened, order, &customerID)
	})
	if err != nil {
		return nil, err
	}
	metrics.OrderEvent("disputed")

	s.systemChatMessage(ctx, orderID, customerID, "⚠️ Заказчик оспорил выполнение заказа: «"+claim+"». "+
		"Спор передан на разбор. Заказчик может закрыть его, подтвердив выполнение, исполнитель — признав, что заказ не выполнен.")
	return dispute, nil
}

// ConcedeDispute — исполнитель признаёт, что оспоренный заказ не выполнен.
// Заказ отменяется с полным возвратом заказчику, штрафного балла нет: признание
// избавляет обе стороны от арбитража, и платформа его поощряет, а не наказывает.
func (s *OrderService) ConcedeDispute(ctx context.Context, executorID, orderID uuid.UUID) error {
	if s.disputes == nil {
		return ErrDisputeNotOpen
	}
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return errors.New("order not found")
		}
		if order.ExecutorID == nil || *order.ExecutorID != executorID {
			return errors.New("forbidden")
		}
		if order.Status != repository.OrderStatusDisputed {
			return ErrDisputeNotOpen
		}
		dispute, err := s.disputes.FindOpenByOrder(ctx, tx, orderID)
		if err != nil {
			return err
		}
		if dispute == nil {
			return ErrDisputeNotOpen
		}
		if err := s.disputes.Close(ctx, tx, dispute.ID, repository.DisputeClosing{
			Closure:  repository.DisputeClosureExecutorConceded,
			ClosedBy: &executorID,
		}); err != nil {
			return err
		}
		if err := s.cancelTx(ctx, tx, orderID, repository.OrderStatusDisputed); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventDisputeConceded, order, &executorID)
	})
	if err != nil {
		return err
	}
	metrics.OrderEvent("conceded")

	s.systemChatMessage(ctx, orderID, executorID, "Исполнитель признал, что заказ не выполнен. "+
		"Заказ отменён, деньги возвращены заказчику. Спор закрыт.")
	return nil
}

// closeOpenDisputeTx закрывает открытый спор заказа в транзакции вызывающего,
// который уже держит блокировку строки заказа. Заказ в DISPUTED без открытого
// спора закрывать нечем — это не ошибка вызывающего, и он продолжает.
func (s *OrderService) closeOpenDisputeTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, closing repository.DisputeClosing) error {
	if s.disputes == nil {
		return nil
	}
	dispute, err := s.disputes.FindOpenByOrder(ctx, tx, orderID)
	if err != nil || dispute == nil {
		return err
	}
	return s.disputes.Close(ctx, tx, dispute.ID, closing)
}

// requireNoOpenDisputeTx не даёт закрыть заказ, спор которого ещё открыт.
// Подтверждение и отмена — общие пути для заказчика, скриптов и воркеров, и
// только те из них, что знают про спор, закрывают его раньше.
func (s *OrderService) requireNoOpenDisputeTx(ctx context.Context, tx *sql.Tx, order *repository.Order) error {
	if s.disputes == nil || order.Status != repository.OrderStatusDisputed {
		return nil
	}
	dispute, err := s.disputes.FindOpenByOrder(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if dispute != nil {
		return ErrOrderHasOpenDispute
	}
	return nil
}

// systemChatMessage пишет служебное сообщение в чат заказа. Сбой не отменяет
// действия, которое уже закоммичено: сообщение — уведомление, а не его часть.
func (s *OrderService) systemChatMessage(ctx context.Context, orderID, senderID uuid.UUID, text string) {
	if s.chatRepo == nil {
		return
	}
	chat, err := s.chatRepo.GetChatByOrderID(ctx, orderID)
	if err == nil && chat != nil {
		_, _ = s.chatRepo.SaveMessage(ctx, chat.ID, senderID, text)
	}
}
