package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// OrderLifecycle — переходы заказа, которыми пользуются соседи OrderService:
// применитель поведений закрывает и отменяет заказы по решению скрипта, заявка
// на верификацию размещает и отменяет заказ от имени исполнителя, споры
// закрывают заказ по решению арбитра. Все они обязаны платить и возвращать
// ровно теми же шагами, что и сам заказчик, поэтому ходят через эти методы, а
// не через репозиторий; и ни одному из них не нужен весь *OrderService.
//
// Методы неэкспортированы намеренно: интерфейсу удовлетворяет только
// OrderService, а снаружи пакета его никто не реализует.
type OrderLifecycle interface {
	// prepareOrder проверяет запрос и собирает заказ, ничего не записывая;
	// placeOrderTx записывает его вместе с удержанием в транзакции вызывающего;
	// orderPlaced — действия после коммита. Разделены ради вызывающих со своей
	// транзакцией.
	prepareOrder(ctx context.Context, customerID uuid.UUID, req CreateOrderRequest) (*preparedOrder, error)
	placeOrderTx(ctx context.Context, tx *sql.Tx, p *preparedOrder) error
	orderPlaced(ctx context.Context, p *preparedOrder) *OrderView
	// confirmTx закрывает заказ как подтверждённый и платит исполнителю.
	confirmTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error
	// cancelTx отменяет заказ из одного из allowed статусов с возвратом заказчику.
	cancelTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, allowed ...repository.OrderStatus) error
	// closeOrderPaidTx закрывает уже заблокированный заказ как оплаченный;
	// деньги двигает settle. См. описание у метода.
	closeOrderPaidTx(ctx context.Context, tx *sql.Tx, order *repository.Order, settle orderSettler) error
	// publishOrderEvent добавляет доменное событие о заказе в транзакцию вызывающего.
	publishOrderEvent(ctx context.Context, tx *sql.Tx, eventType string, order *repository.Order, actorID *uuid.UUID) error
	// Cancel отменяет заказ конкретного заказчика.
	Cancel(ctx context.Context, customerID, orderID uuid.UUID) error
}

var _ OrderLifecycle = (*OrderService)(nil)

// orderSettler двигает деньги закрываемого заказа: paid — сколько заказчик
// заплатил, commission — доля платформы с этого, level — уровень исполнителя,
// по которому она посчитана.
type orderSettler func(ctx context.Context, tx *sql.Tx, order *repository.Order, paid, commission money.Amount, level Level) error

// confirmTx — само подтверждение, внутри транзакции вызывающего. У него три
// вызывающих: заказчик, который подтверждает, применитель поведений,
// закрывающий заказ, который скрипт объявил завершённым (скажем, состоявшуюся
// верификацию), и арбитр, решивший спор в пользу исполнителя. Все обязаны
// выплачивать ровно теми же шагами, поэтому копия у них одна.
func (s *OrderService) confirmTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID) error {
	order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
	if err != nil {
		return orderNotFound(err)
	}
	// Заказчик может одобрить и после того, как исполнитель пометил заказ
	// EXECUTED, и раньше, пока он ещё ASSIGNED, — раннее одобрение просто
	// закрывает заказ и платит исполнителю удержанную сумму, так же как путь
	// EXECUTED ниже.
	if order.Status != repository.OrderStatusExecuted && order.Status != repository.OrderStatusAssigned &&
		order.Status != repository.OrderStatusDisputed {
		return ErrOrderNotConfirmable
	}
	// Оспоренный заказ закрывается только вместе со спором: вызывающий обязан
	// закрыть спор в этой же транзакции раньше, чем платить.
	if err := requireNoOpenDisputeTx(ctx, tx, s.disputes, order); err != nil {
		return err
	}
	if err := s.closeOrderPaidTx(ctx, tx, order, s.settleConfirmed); err != nil {
		return err
	}
	return s.publishOrderEvent(ctx, tx, repository.EventOrderConfirmed, order, nil)
}

// settleConfirmed — деньги подтверждённого заказа: эскроу держит по нему ровно
// order.HoldAmount, и здесь он опустошается полностью: возврат заказчику +
// комиссия + вознаграждение. Распределение целиком делает реестр — он
// единственный, кому видно все три части сразу и кто поэтому может проверить,
// что исполнителю не досталось больше уплаченного заказчиком.
func (s *OrderService) settleConfirmed(ctx context.Context, tx *sql.Tx, order *repository.Order, paid, commission money.Amount, _ Level) error {
	return s.ledger.SettleOrder(ctx, tx, OrderSettlement{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		ExecutorID: *order.ExecutorID,
		Hold:       order.HoldAmount,
		Paid:       paid,
		Commission: commission,
	})
}

// closeOrderPaidTx закрывает уже заблокированный вызывающим заказ как
// оплаченный: считает сумму к оплате и персональную комиссию, двигает деньги
// через settle, обнуляет удержание, переводит заказ в COMPLETED, сохраняет
// ставку и пополняет агрегаты исполнителя. Подтверждение заказчиком и решение
// арбитра «неизвестно» различаются только тем, откуда приходят деньги, — это и
// есть settle; всё остальное у них обязано совпадать, поэтому оно здесь одно.
func (s *OrderService) closeOrderPaidTx(ctx context.Context, tx *sql.Tx, order *repository.Order, settle orderSettler) error {
	if order.ExecutorID == nil {
		return ErrOrderHasNoExecutor
	}
	paid, isDowngraded, err := s.payableAmount(ctx, order)
	if err != nil {
		return err
	}
	// Ставка платформы теперь персональная: уровень исполнителя снимает с неё
	// по проценту за уровень, до нуля. Уровень читается внутри этой же
	// транзакции, поэтому баллы, начисленные параллельно, не могут применить
	// себя к заказу задним числом.
	level := s.commissionLevel(ctx, tx, *order.ExecutorID)
	commission := commissionAt(paid, level.Percent)

	if err := settle(ctx, tx, order, paid, commission, level); err != nil {
		return err
	}
	if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
		return err
	}
	if err := s.orderRepo.Confirm(ctx, tx, order.ID, paid, isDowngraded); err != nil {
		return err
	}
	// Ставка и уровень сохраняются в заказе: без них через месяц никто не
	// объяснит, почему по двум одинаковым заказам разная комиссия.
	if err := s.orderRepo.SetCommission(ctx, tx, order.ID, level.Percent, level.Level, level.PerkID); err != nil {
		return err
	}
	return s.recordCompletion(ctx, tx, order, paid)
}

// payableAmount — сколько стоит заказ на момент закрытия: удержанное, а для
// ASAP, закрываемого после срока, — цена со сниженным тарифом, если она ниже.
func (s *OrderService) payableAmount(ctx context.Context, order *repository.Order) (money.Amount, bool, error) {
	finalAmount := order.HoldAmount
	isDowngraded := order.IsDowngraded
	if order.IsAsap && order.DeadlineAt != nil && time.Now().After(*order.DeadlineAt) {
		downgraded, err := s.CalculatePrice(ctx, order.ServiceVariantID, false, false, true)
		if err != nil {
			return 0, false, err
		}
		if downgraded < finalAmount {
			isDowngraded = true
			finalAmount = downgraded
		}
	}
	return finalAmount, isDowngraded, nil
}

// commissionLevel читает уровень исполнителя внутри транзакции подтверждения.
// Без подключённых уровней это нулевой уровень, то есть базовая ставка.
func (s *OrderService) commissionLevel(ctx context.Context, tx *sql.Tx, executorID uuid.UUID) Level {
	if s.levels == nil {
		base := commissionPercent(s.loadSettings(ctx))
		return Level{BasePercent: base, Percent: base, LevelPercent: base}
	}
	return s.levels.For(ctx, tx, executorID)
}

// recordCompletion пополняет агрегаты исполнителя в той же транзакции, что и
// подтверждение. Отдельным проходом их считать нельзя: агрегат, посчитанный
// позже, расходится с заказами ровно в тот момент, когда проход упал, — а по
// нему решают, выдать ли ачивку.
func (s *OrderService) recordCompletion(ctx context.Context, tx *sql.Tx, order *repository.Order, finalAmount money.Amount) error {
	if s.stats == nil || order.ExecutorID == nil {
		return nil
	}
	minutes := 0
	if !order.CreatedAt.IsZero() {
		minutes = int(time.Since(order.CreatedAt).Minutes())
	}
	return s.stats.RecordCompletion(ctx, tx, repository.CompletedOrder{
		ExecutorID: *order.ExecutorID,
		CustomerID: order.CustomerID,
		Minutes:    minutes,
		Earned:     finalAmount,
	})
}

// cancelTx — сама отмена, внутри транзакции вызывающего: тот же возврат,
// освобождение claim'а и событие, отменил ли заказчик, вымел ли воркер
// невостребованный аукцион или попросил скрипт поведения.
func (s *OrderService) cancelTx(ctx context.Context, tx *sql.Tx, orderID uuid.UUID, allowed ...repository.OrderStatus) error {
	order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
	if err != nil {
		return orderNotFound(err)
	}
	return s.cancelLockedTx(ctx, tx, order, allowed...)
}

// cancelLockedTx — отмена заказа, который вызывающий уже заблокировал.
func (s *OrderService) cancelLockedTx(ctx context.Context, tx *sql.Tx, order *repository.Order, allowed ...repository.OrderStatus) error {
	permitted := false
	for _, status := range allowed {
		if order.Status == status {
			permitted = true
			break
		}
	}
	if !permitted {
		return ErrOrderNotCancelable
	}
	if err := requireNoOpenDisputeTx(ctx, tx, s.disputes, order); err != nil {
		return err
	}

	if order.HoldAmount.IsPositive() {
		if err := s.ledger.Release(ctx, tx, repository.AccountEscrow, order.CustomerID, order.HoldAmount, repository.TransactionTypeRefund, &order.ID, nil); err != nil {
			return err
		}
		if err := s.orderRepo.SetHoldAmount(ctx, tx, order.ID, money.Zero); err != nil {
			return err
		}
	}
	if err := s.orderRepo.Cancel(ctx, tx, order.ID); err != nil {
		return err
	}
	// Отменённый заказ возвращает пользователю его единственную попытку. Без
	// этого заказчик, отменивший заказ верификации, никогда не смог бы заказать
	// другой, а значит, и никогда не верифицировался бы.
	if s.claimRepo != nil {
		variant, err := s.catalogRepo.GetNodeByID(ctx, order.ServiceVariantID)
		if err == nil && s.behaviors.ReleasesClaimOnCancel(variant) {
			if err := s.claimRepo.ReleaseByOrder(ctx, tx, order.ID); err != nil {
				return err
			}
		}
	}
	// Отмена засчитывается исполнителю, если он у заказа был: ачивки смотрят на
	// неё так же, как на выполнение, и агрегат должен меняться там же, где
	// меняется сам заказ.
	if s.stats != nil && order.ExecutorID != nil {
		if err := s.stats.RecordCancel(ctx, tx, *order.ExecutorID); err != nil {
			return err
		}
	}
	return s.publishOrderEvent(ctx, tx, repository.EventOrderCanceled, order, nil)
}

// requireNoOpenDisputeTx не даёт закрыть заказ, спор которого ещё открыт.
// Подтверждение и отмена — общие пути для заказчика, скриптов и воркеров, и
// только те из них, что знают про спор, закрывают его раньше.
func requireNoOpenDisputeTx(ctx context.Context, tx *sql.Tx, disputes repository.DisputeRepository, order *repository.Order) error {
	if disputes == nil || order.Status != repository.OrderStatusDisputed {
		return nil
	}
	dispute, err := disputes.FindOpenByOrder(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if dispute != nil {
		return ErrOrderHasOpenDispute
	}
	return nil
}

// closeOpenDisputeTx закрывает открытый спор заказа в транзакции вызывающего,
// который уже держит блокировку строки заказа, и возвращает его закрытым. Заказ
// в DISPUTED без открытого спора закрывать нечем — это не ошибка вызывающего:
// nil, nil.
func closeOpenDisputeTx(ctx context.Context, tx *sql.Tx, disputes repository.DisputeRepository, orderID uuid.UUID, closing repository.DisputeClosing) (*repository.Dispute, error) {
	if disputes == nil {
		return nil, nil
	}
	dispute, err := disputes.FindOpenByOrder(ctx, tx, orderID)
	if err != nil || dispute == nil {
		return nil, err
	}
	if err := disputes.Close(ctx, tx, dispute.ID, closing); err != nil {
		return nil, err
	}
	return disputes.FindByID(ctx, tx, dispute.ID)
}
