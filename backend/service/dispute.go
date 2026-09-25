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
//
// Подтверждение заказчиком живёт в OrderService.Confirm: это его обычное
// действие, которое заодно закрывает спор. Всё остальное — здесь.

// maxDisputeClaimRunes ограничивает претензию: это описание, что не так, а не
// переписка — для неё есть чат заказа.
const maxDisputeClaimRunes = 2000

// maxResolutionNoteRunes ограничивает комментарий арбитра.
const maxResolutionNoteRunes = 2000

var (
	// ErrDisputeClaimRequired — претензия пустая.
	ErrDisputeClaimRequired = validationError("опишите, что не выполнено")
	// ErrDisputeClaimTooLong — претензия длиннее maxDisputeClaimRunes.
	ErrDisputeClaimTooLong = validationError("описание претензии слишком длинное")
	// ErrDisputeNoteTooLong — комментарий арбитра длиннее maxResolutionNoteRunes.
	ErrDisputeNoteTooLong = validationError("комментарий арбитра слишком длинный")
	// ErrDisputeNotAllowed — заказ нельзя оспорить в его нынешнем виде.
	ErrDisputeNotAllowed = stateError("оспорить можно только заказ, отмеченный исполнителем как выполненный")
	// ErrDisputeAlreadyOpen — по заказу уже идёт спор.
	ErrDisputeAlreadyOpen = stateError("по заказу уже открыт спор")
	// ErrDisputeNotOpen — по заказу нет открытого спора.
	ErrDisputeNotOpen = stateError("по заказу нет открытого спора")
	// ErrDisputeDecision — неизвестное решение арбитра.
	ErrDisputeDecision = validationError("решение арбитра: executor, customer или unknown")
	// ErrDisputeClosed — спор уже закрыт: заказчиком, исполнителем или другим
	// арбитром раньше.
	ErrDisputeClosed = stateError("спор уже закрыт")
	// ErrOrderHasOpenDispute — заказ пытаются закрыть в обход его спора.
	ErrOrderHasOpenDispute = stateError("по заказу открыт спор")
	// ErrDisputeStatusFilter — фильтр очереди арбитража не OPEN и не CLOSED.
	ErrDisputeStatusFilter = validationError("invalid status filter")
)

// DisputeService ведёт споры: открытие, признание исполнителя, решение арбитра,
// очередь и карточку доказательств. Заказ он закрывает через OrderLifecycle —
// теми же шагами, что и обычное подтверждение или отмена.
type DisputeService struct {
	orders    OrderLifecycle
	orderRepo repository.OrderRepository
	ledger    *Ledger
	disputes  repository.DisputeRepository
	catalog   repository.ServiceCatalogRepository
	chatRepo  repository.ChatRepository
	// penalties начисляет штрафные баллы по решению арбитра. Без них арбитраж недоступен.
	penalties *PenaltyService
	// notifier сообщает сторонам об открытии и закрытии спора.
	notifier *DisputeNotifier
	// evidence, geoRepo и settings — карточка доказательств; см. dispute_evidence.go.
	evidence ProofEvidenceSource
	geoRepo  repository.ExecutorGeoRepository
	settings repository.SettingsRepository
}

// NewDisputeService создаёт DisputeService.
func NewDisputeService(orders OrderLifecycle, orderRepo repository.OrderRepository, ledger *Ledger, disputes repository.DisputeRepository, catalog repository.ServiceCatalogRepository, chatRepo repository.ChatRepository) *DisputeService {
	return &DisputeService{orders: orders, orderRepo: orderRepo, ledger: ledger, disputes: disputes, catalog: catalog, chatRepo: chatRepo}
}

// WithPenalties подключает штрафные баллы, которые начисляет решение арбитра.
func (s *DisputeService) WithPenalties(penalties *PenaltyService) *DisputeService {
	s.penalties = penalties
	return s
}

// WithNotifier подключает уведомления сторонам.
func (s *DisputeService) WithNotifier(n *DisputeNotifier) *DisputeService {
	s.notifier = n
	return s
}

// OpenDispute — заказчик заявляет, что исполненный заказ не выполнен.
func (s *DisputeService) OpenDispute(ctx context.Context, customerID, orderID uuid.UUID, claim string) (*repository.Dispute, error) {
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
			return orderNotFound(err)
		}
		if order.CustomerID != customerID {
			return ErrForbidden
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
		if variant, err := s.catalog.GetNodeByID(ctx, order.ServiceVariantID); err == nil && variant.HasBehavior() {
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
		return s.orders.publishOrderEvent(ctx, tx, repository.EventDisputeOpened, order, &customerID)
	})
	if err != nil {
		return nil, err
	}
	metrics.OrderEvent("disputed")

	systemChatMessage(ctx, s.chatRepo, orderID, customerID, "⚠️ Заказчик оспорил выполнение заказа: «"+claim+"». "+
		"Спор передан на разбор. Заказчик может закрыть его, подтвердив выполнение, исполнитель — признав, что заказ не выполнен.")
	s.notifier.DisputeOpened(ctx, dispute)
	return dispute, nil
}

// ConcedeDispute — исполнитель признаёт, что оспоренный заказ не выполнен.
// Заказ отменяется с полным возвратом заказчику, штрафного балла нет: признание
// избавляет обе стороны от арбитража, и платформа его поощряет, а не наказывает.
func (s *DisputeService) ConcedeDispute(ctx context.Context, executorID, orderID uuid.UUID) error {
	if s.disputes == nil {
		return ErrDisputeNotOpen
	}
	var closed *repository.Dispute
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if order.ExecutorID == nil || *order.ExecutorID != executorID {
			return ErrForbidden
		}
		if order.Status != repository.OrderStatusDisputed {
			return ErrDisputeNotOpen
		}
		closed, err = closeOpenDisputeTx(ctx, tx, s.disputes, orderID, repository.DisputeClosing{
			Closure:  repository.DisputeClosureExecutorConceded,
			ClosedBy: &executorID,
		})
		if err != nil {
			return err
		}
		if closed == nil {
			return ErrDisputeNotOpen
		}
		if err := s.orders.cancelTx(ctx, tx, orderID, repository.OrderStatusDisputed); err != nil {
			return err
		}
		return s.orders.publishOrderEvent(ctx, tx, repository.EventDisputeConceded, order, &executorID)
	})
	if err != nil {
		return err
	}
	metrics.OrderEvent("conceded")

	systemChatMessage(ctx, s.chatRepo, orderID, executorID, "Исполнитель признал, что заказ не выполнен. "+
		"Заказ отменён, деньги возвращены заказчику. Спор закрыт.")
	s.notifier.DisputeClosed(ctx, closed)
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
func (s *DisputeService) ResolveDispute(ctx context.Context, disputeID, arbiterID uuid.UUID, decision, note string) (*repository.Dispute, error) {
	if s.disputes == nil || s.penalties == nil {
		return nil, ErrDisputeNotFound
	}
	if _, ok := disputeDecisionText[decision]; !ok {
		return nil, ErrDisputeDecision
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxResolutionNoteRunes {
		return nil, ErrDisputeNoteTooLong
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
			return orderNotFound(lockErr)
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
			if err := s.orders.confirmTx(ctx, tx, found.OrderID); err != nil {
				return err
			}
			awards = []PenaltyAward{customerPoint}
		case repository.DisputeDecisionCustomer:
			if err := s.orders.cancelTx(ctx, tx, found.OrderID, repository.OrderStatusDisputed); err != nil {
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
		return s.orders.publishOrderEvent(ctx, tx, repository.EventDisputeResolved, order, &arbiterID)
	})
	if err != nil {
		return nil, err
	}
	metrics.OrderEvent("dispute_resolved")

	text := "⚖️ Спор разобран: " + disputeDecisionText[decision] + "."
	if note != "" {
		text += " Комментарий арбитра: «" + note + "»."
	}
	systemChatMessage(ctx, s.chatRepo, found.OrderID, arbiterID, text)

	resolved, err := s.disputes.FindByID(ctx, nil, disputeID)
	if err != nil {
		return nil, err
	}
	s.notifier.DisputeClosed(ctx, resolved)
	return resolved, nil
}

// ListDisputes — очередь арбитража.
func (s *DisputeService) ListDisputes(ctx context.Context, status string, limit, offset int) ([]repository.AdminDispute, error) {
	if s.disputes == nil {
		return []repository.AdminDispute{}, nil
	}
	switch status {
	case "", repository.DisputeStatusOpen, repository.DisputeStatusClosed:
	default:
		return nil, ErrDisputeStatusFilter
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
func (s *DisputeService) settleUnknownDisputeTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
	if err != nil {
		return orderNotFound(err)
	}
	if order.Status != repository.OrderStatusDisputed || order.ExecutorID == nil {
		return ErrDisputeNotOpen
	}
	if err := requireNoOpenDisputeTx(ctx, tx, s.disputes, order); err != nil {
		return err
	}
	return s.orders.closeOrderPaidTx(ctx, tx, order, s.settleUnknown)
}

// settleUnknown — деньги решения «неизвестно»: заказчику возврат из эскроу,
// исполнителю выплата с DISPUTES.
func (s *DisputeService) settleUnknown(ctx context.Context, tx *sql.Tx, order *repository.Order, payout, commission money.Amount, _ Level) error {
	return s.ledger.SettleUnknownDispute(ctx, tx, UnknownDisputeSettlement{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		ExecutorID: *order.ExecutorID,
		Hold:       order.HoldAmount,
		Payout:     payout,
		Commission: commission,
	})
}
