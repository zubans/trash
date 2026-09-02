package worker

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/metrics"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// SLAWorker автоматически понижает просроченные заказы ASAP/URGENT.
type SLAWorker struct {
	db           *sql.DB
	orderService *service.OrderService
	chatService  *service.ChatService
	ledger       *service.Ledger
	guard func(func() error) error
}

// NewSLAWorker создаёт новый SLAWorker. Реестр обязателен: понижение возвращает
// часть удержания, а возврат, который не выходит со счёта эскроу, — ровно то
// одностороннее движение, ради предотвращения которого реестр и существует.
func NewSLAWorker(db *sql.DB, orderService *service.OrderService, chatService *service.ChatService, ledger *service.Ledger) *SLAWorker {
	return &SLAWorker{
		db:           db,
		orderService: orderService,
		chatService:  chatService,
		ledger:       ledger,
	}
}

// Start периодически выполняет цикл воркера.
func (w *SLAWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := metrics.TrackWorker("sla", func() error { return w.runGuarded(w.CheckSLAOverdue) }); err != nil {
				log.Printf("[SLAWorker] Error checking overdue orders: %v", err)
			}
		}
	}()
	log.Printf("[SLAWorker] Background worker started every %v", interval)
}

type overdueOrder struct {
	ID               uuid.UUID
	CustomerID       uuid.UUID
	ServiceVariantID uuid.UUID
	HoldAmount       money.Amount
}

// CheckSLAOverdue ищет просроченные заказы и обновляет их.
func (w *SLAWorker) CheckSLAOverdue() error {
	query := `
		SELECT id, customer_id, service_variant_id, hold_amount 
		FROM orders 
		WHERE status = 'ASSIGNED' 
		  AND is_downgraded = FALSE 
		  AND deadline_at < now() 
		  AND (is_urgent = TRUE OR is_asap = TRUE)`

	rows, err := w.db.QueryContext(context.Background(), query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var list []overdueOrder
	for rows.Next() {
		var o overdueOrder
		err := rows.Scan(&o.ID, &o.CustomerID, &o.ServiceVariantID, &o.HoldAmount)
		if err != nil {
			return err
		}
		list = append(list, o)
	}

	for _, o := range list {
		err := w.downgradeOrder(o)
		if err != nil {
			log.Printf("[SLAWorker] Failed to downgrade order %s: %v", o.ID, err)
		} else {
			metrics.OrderEvent("downgraded")
			log.Printf("[SLAWorker] Downgraded order %s to REGULAR due to delay. Customer refunded.", o.ID)
		}
	}

	return nil
}

func (w *SLAWorker) downgradeOrder(o overdueOrder) error {
	tx, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Считаем базовую (несрочную) цену варианта.
	basePrice, err := w.orderService.CalculatePrice(context.Background(), o.ServiceVariantID, false, false, false)
	if err != nil {
		return err
	}
	if basePrice > o.HoldAmount {
		// Никогда не поднимаем удержание задним числом; заказчик авторизовал только
		// ту сумму, которая была взята в момент заказа.
		basePrice = o.HoldAmount
	}

	refund := o.HoldAmount.Sub(basePrice)
	if refund.IsNegative() {
		refund = money.Zero
	}

	// 2. Обновляем колонки заказа. hold_amount обязан следовать за возвратом:
	// выплата в момент подтверждения выводится из удержания, поэтому оставленное
	// исходное срочное удержание выплатило бы исполнителю полную срочную цену уже
	// после того, как заказчику вернули разницу.
	res, err := tx.Exec(`
		UPDATE orders
		SET is_urgent = FALSE, is_asap = FALSE, final_amount = $1, hold_amount = $1, is_downgraded = TRUE
		WHERE id = $2 AND status = 'ASSIGNED' AND is_downgraded = FALSE`, basePrice, o.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Другой воркер или подтверждение уже сдвинули этот заказ дальше.
		return nil
	}

	// 3. Выдаём возврат, если он положен.
	//
	// Из эскроу, через реестр. Удержание сокращается до basePrice чуть выше,
	// поэтому эскроу обязан отдать ровно разницу; заменённый этим сырой UPDATE
	// зачислял заказчику и оставлял эскроу держать деньги, на которые больше не
	// претендовал ни один заказ, — один из путей, которыми книги платформы
	// разошлись с суммой балансов пользователей.
	if refund.IsPositive() {
		if err := w.ledger.Release(context.Background(), tx, repository.AccountEscrow, o.CustomerID, refund, repository.TransactionTypeRefund, &o.ID, nil); err != nil {
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	// 4. Отправляем уведомление по websocket в активные комнаты
	w.chatService.BroadcastSystemMessage(context.Background(), o.ID, map[string]interface{}{
		"type":         "system",
		"action":       "downgrade",
		"is_urgent":    false,
		"is_asap":      false,
		"final_amount": basePrice,
	})

	return nil
}

// guard выполняет один тик под advisory-блокировкой задачи, когда подключён
// Leader, чтобы вторая реплика пропустила тик, а не продублировала его. Без
// него выполняется напрямую — так и делают однопроцессный деплой и тесты.
func (w *SLAWorker) runGuarded(job func() error) error {
	if w.guard == nil {
		return job()
	}
	return w.guard(job)
}

// WithLeader заставляет этот воркер выполняться не более одного раза среди всех процессов.
func (w *SLAWorker) WithLeader(leader *Leader, name string) *SLAWorker {
	w.guard = leader.Guard(name)
	return w
}
