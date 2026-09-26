package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// AuctionTTL — сколько аукцион ждёт, пока заказчик примет ставку, прежде чем
// его отменят как невостребованный.
const AuctionTTL = 7 * 24 * time.Hour

// DowngradeResult — итог попытки понизить просроченный заказ.
type DowngradeResult struct {
	// Downgraded — заказ понижен этим вызовом. false, если к моменту блокировки
	// он уже понижен, ушёл дальше или ещё не просрочен.
	Downgraded bool
	// FinalAmount — новая сумма заказа и удержания.
	FinalAmount money.Amount
	// Refund — сколько вернулось заказчику из эскроу.
	Refund money.Amount
}

// OverdueUrgentOrders — id просроченных срочных и ASAP-заказов, ждущих
// понижения, не больше limit за раз.
func (s *OrderService) OverdueUrgentOrders(ctx context.Context, limit int) ([]uuid.UUID, error) {
	orders, err := s.orderRepo.ListOverdueUrgent(ctx, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	return ids, nil
}

// ExpiredAuctionOrders — id аукционов, которые никто не забрал за AuctionTTL.
func (s *OrderService) ExpiredAuctionOrders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	return s.orderRepo.ListExpiredAuctions(ctx, now.Add(-AuctionTTL), limit)
}

// DowngradeOverdue понижает назначенный срочный или ASAP-заказ, у которого
// вышел срок, до обычного тарифа и возвращает заказчику разницу. Всё — в одной
// транзакции под блокировкой строки заказа, поэтому понижение и подтверждение
// не могут пройти одновременно, а повторный вызов ничего не вернёт дважды.
//
// Правило раньше жило в SLA-воркере сырым SQL; теперь воркер только выбирает
// заказы и зовёт этот метод.
func (s *OrderService) DowngradeOverdue(ctx context.Context, orderID uuid.UUID) (DowngradeResult, error) {
	if s.ledger == nil {
		return DowngradeResult{}, ErrNotConfigured
	}
	var res DowngradeResult
	err := s.ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		order, err := s.orderRepo.LockForUpdate(ctx, tx, orderID)
		if err != nil {
			return orderNotFound(err)
		}
		if !overdueForDowngrade(order, time.Now()) {
			return nil
		}

		// Удержание не поднимается задним числом: заказчик авторизовал только
		// ту сумму, что была взята при заказе.
		base, err := s.CalculatePrice(ctx, order.ServiceVariantID, false, false, false)
		if err != nil {
			return err
		}
		if base > order.HoldAmount {
			base = order.HoldAmount
		}
		refund := order.HoldAmount.Sub(base)
		if refund.IsNegative() {
			refund = money.Zero
		}

		if err := s.orderRepo.Downgrade(ctx, tx, order.ID, base); err != nil {
			if errors.Is(err, repository.ErrConflict) {
				return nil
			}
			return err
		}
		// Возврат — из эскроу, через реестр: удержание сократилось до base, и
		// эскроу обязан отдать ровно разницу.
		if refund.IsPositive() {
			if err := s.ledger.Release(ctx, tx, repository.AccountEscrow, order.CustomerID, refund,
				repository.TransactionTypeRefund, &order.ID, nil); err != nil {
				return err
			}
		}
		res = DowngradeResult{Downgraded: true, FinalAmount: base, Refund: refund}
		return nil
	})
	if err != nil {
		return DowngradeResult{}, err
	}
	if res.Downgraded {
		metrics.OrderEvent("downgraded")
	}
	return res, nil
}

// overdueForDowngrade — заказ ещё можно понизить: назначен, не понижен,
// срочный или ASAP, и срок вышел.
func overdueForDowngrade(o *repository.Order, now time.Time) bool {
	return o.Status == repository.OrderStatusAssigned && !o.IsDowngraded &&
		(o.IsUrgent || o.IsAsap) && o.DeadlineAt != nil && now.After(*o.DeadlineAt)
}
