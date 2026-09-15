package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
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
	// ErrDisputeDecision — неизвестное решение арбитра.
	ErrDisputeDecision = errors.New("решение арбитра: executor, customer или unknown")
	// ErrDisputeClosed — спор уже закрыт: заказчиком, исполнителем или другим
	// арбитром раньше.
	ErrDisputeClosed = errors.New("спор уже закрыт")
	// ErrDisputeNotFound — спора нет.
	ErrDisputeNotFound = errors.New("спор не найден")
	// ErrOrderHasOpenDispute — заказ пытаются закрыть в обход его спора.
	ErrOrderHasOpenDispute = errors.New("по заказу открыт спор")
)

// maxResolutionNoteRunes ограничивает комментарий арбитра.
const maxResolutionNoteRunes = 2000

// WithPenalties подключает штрафные баллы, которые начисляет решение арбитра.
func (s *OrderService) WithPenalties(penalties *PenaltyService) *OrderService {
	s.penalties = penalties
	return s
}

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
	s.disputeNotifier.DisputeOpened(ctx, dispute)
	return dispute, nil
}

// ConcedeDispute — исполнитель признаёт, что оспоренный заказ не выполнен.
// Заказ отменяется с полным возвратом заказчику, штрафного балла нет: признание
// избавляет обе стороны от арбитража, и платформа его поощряет, а не наказывает.
func (s *OrderService) ConcedeDispute(ctx context.Context, executorID, orderID uuid.UUID) error {
	if s.disputes == nil {
		return ErrDisputeNotOpen
	}
	var closed *repository.Dispute
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
		closed, err = s.closeOpenDisputeTx(ctx, tx, orderID, repository.DisputeClosing{
			Closure:  repository.DisputeClosureExecutorConceded,
			ClosedBy: &executorID,
		})
		if err != nil {
			return err
		}
		if closed == nil {
			return ErrDisputeNotOpen
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
	s.disputeNotifier.DisputeClosed(ctx, closed)
	return nil
}

// ParseDisputeDecision переводит решение из запроса (executor, customer,
// unknown) в значение базы.
func ParseDisputeDecision(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "executor":
		return repository.DisputeDecisionExecutor, nil
	case "customer":
		return repository.DisputeDecisionCustomer, nil
	case "unknown":
		return repository.DisputeDecisionUnknown, nil
	}
	return "", ErrDisputeDecision
}

var disputeDecisionText = map[string]string{
	repository.DisputeDecisionExecutor: "прав исполнитель",
	repository.DisputeDecisionCustomer: "прав заказчик",
	repository.DisputeDecisionUnknown:  "установить, кто прав, не удалось",
}

// ResolveDispute — решение арбитра по открытому спору.
//
//   - executor: заказ оплачивается исполнителю как подтверждённый, балл —
//     заказчику;
//   - customer: заказ отменяется с полным возвратом заказчику, балл —
//     исполнителю;
//   - unknown: заказчику полный возврат, исполнителю оплата со счёта DISPUTES,
//     баллы — обоим.
//
// Деньги, закрытие спора и баллы — одна транзакция. Строка заказа блокируется
// первой, как и в подтверждении заказчиком, поэтому решение, опоздавшее за
// подтверждением, увидит закрытый спор и получит ErrDisputeClosed.
func (s *OrderService) ResolveDispute(ctx context.Context, disputeID, arbiterID uuid.UUID, decision, note string) (*repository.Dispute, error) {
	if s.disputes == nil || s.penalties == nil {
		return nil, ErrDisputeNotFound
	}
	if _, ok := disputeDecisionText[decision]; !ok {
		return nil, ErrDisputeDecision
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxResolutionNoteRunes {
		return nil, errors.New("комментарий арбитра слишком длинный")
	}

	found, err := s.disputes.FindByID(ctx, nil, disputeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDisputeNotFound
	}
	if err != nil {
		return nil, err
	}

	var order *repository.Order
	err = s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		locked, lockErr := s.orderRepo.LockForUpdate(ctx, tx, found.OrderID)
		if lockErr != nil {
			return errors.New("order not found")
		}
		order = locked
		if err := s.disputes.Close(ctx, tx, disputeID, repository.DisputeClosing{
			Closure:  repository.DisputeClosureArbitration,
			Decision: decision,
			Note:     note,
			ClosedBy: &arbiterID,
		}); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return ErrDisputeClosed
			}
			return err
		}

		reason := "Спор по заказу: " + disputeDecisionText[decision]
		executorPoint := PenaltyAward{UserID: found.ExecutorID, Role: repository.RoleExecutor,
			OrderID: &found.OrderID, DisputeID: &disputeID, AssignedBy: &arbiterID, Reason: reason}
		customerPoint := PenaltyAward{UserID: found.CustomerID, Role: repository.RoleCustomer,
			OrderID: &found.OrderID, DisputeID: &disputeID, AssignedBy: &arbiterID, Reason: reason}

		var awards []PenaltyAward
		switch decision {
		case repository.DisputeDecisionExecutor:
			if err := s.confirmTx(ctx, tx, found.OrderID); err != nil {
				return err
			}
			awards = []PenaltyAward{customerPoint}
		case repository.DisputeDecisionCustomer:
			if err := s.cancelTx(ctx, tx, found.OrderID, repository.OrderStatusDisputed); err != nil {
				return err
			}
			awards = []PenaltyAward{executorPoint}
		case repository.DisputeDecisionUnknown:
			if err := s.settleUnknownDisputeTx(ctx, tx, found.OrderID); err != nil {
				return err
			}
			awards = []PenaltyAward{executorPoint, customerPoint}
		}
		if _, err := s.penalties.AwardTx(ctx, tx, awards...); err != nil {
			return err
		}
		return s.publishOrderEvent(ctx, tx, repository.EventDisputeResolved, order, &arbiterID)
	})
	if err != nil {
		return nil, err
	}
	metrics.OrderEvent("dispute_resolved")

	text := "⚖️ Спор разобран: " + disputeDecisionText[decision] + "."
	if note != "" {
		text += " Комментарий арбитра: «" + note + "»."
	}
	s.systemChatMessage(ctx, found.OrderID, arbiterID, text)

	resolved, err := s.disputes.FindByID(ctx, nil, disputeID)
	if err != nil {
		return nil, err
	}
	s.disputeNotifier.DisputeClosed(ctx, resolved)
	return resolved, nil
}

// ListDisputes — очередь арбитража.
func (s *OrderService) ListDisputes(ctx context.Context, status string, limit, offset int) ([]repository.AdminDispute, error) {
	if s.disputes == nil {
		return []repository.AdminDispute{}, nil
	}
	switch status {
	case "", repository.DisputeStatusOpen, repository.DisputeStatusClosed:
	default:
		return nil, errors.New("invalid status filter")
	}
	return s.disputes.ListForAdmin(ctx, status, limit, offset)
}

// settleUnknownDisputeTx закрывает оспоренный заказ по решению «неизвестно»:
// заказчику возвращается всё удержанное, исполнитель получает оплату как за
// подтверждённый заказ, по своей обычной ставке комиссии, со счёта DISPUTES.
// Заказ становится COMPLETED — работа оплачена.
//
// Спор вызывающий обязан закрыть в этой же транзакции раньше. Событие
// order.confirmed не публикуется: заказчик выполнение не подтверждал, и ачивки
// за подтверждённый заказ здесь не выдаются. Агрегаты исполнителя пополняются —
// заказ завершён и оплачен.
func (s *OrderService) settleUnknownDisputeTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
	if err != nil {
		return errors.New("order not found")
	}
	if order.Status != repository.OrderStatusDisputed || order.ExecutorID == nil {
		return ErrDisputeNotOpen
	}
	if err := s.requireNoOpenDisputeTx(ctx, tx, order); err != nil {
		return err
	}

	payout, isDowngraded, err := s.payableAmount(ctx, order)
	if err != nil {
		return err
	}
	level := s.commissionLevel(ctx, tx, *order.ExecutorID)
	commission := commissionAt(payout, level.Percent)

	if err := s.ledger.SettleUnknownDispute(ctx, tx, UnknownDisputeSettlement{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		ExecutorID: *order.ExecutorID,
		Hold:       order.HoldAmount,
		Payout:     payout,
		Commission: commission,
	}); err != nil {
		return err
	}
	if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
		return err
	}
	if err := s.orderRepo.Confirm(ctx, tx, orderID, payout, isDowngraded); err != nil {
		return err
	}
	if err := s.orderRepo.SetCommission(ctx, tx, order.ID, level.Percent, level.Level); err != nil {
		return err
	}
	return s.recordCompletion(ctx, tx, order, payout)
}

// closeOpenDisputeTx закрывает открытый спор заказа в транзакции вызывающего,
// который уже держит блокировку строки заказа, и возвращает его закрытым. Заказ
// в DISPUTED без открытого спора закрывать нечем — это не ошибка вызывающего:
// nil, nil.
func (s *OrderService) closeOpenDisputeTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, closing repository.DisputeClosing) (*repository.Dispute, error) {
	if s.disputes == nil {
		return nil, nil
	}
	dispute, err := s.disputes.FindOpenByOrder(ctx, tx, orderID)
	if err != nil || dispute == nil {
		return nil, err
	}
	if err := s.disputes.Close(ctx, tx, dispute.ID, closing); err != nil {
		return nil, err
	}
	return s.disputes.FindByID(ctx, tx, dispute.ID)
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
