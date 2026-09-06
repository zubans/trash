package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// ExecutorStats — агрегаты, которые ачивке нужны и которые она не имеет права
// считать сама.
//
// Скрипт — чистая функция от переданных фактов, и это ограничение существует не
// ради чистоты: хук выполняется на пути обработки события, а скрипт, которому
// позволено спросить произвольный агрегат, стоит непредсказуемо дорого. Поэтому
// набор закрыт, строка одна на исполнителя, а обновляется она там же, где
// меняются сами числа.
type ExecutorStats struct {
	UserID               uuid.UUID `json:"user_id"`
	OrdersCompleted      int       `json:"orders_completed"`
	OrdersCompletedMonth int       `json:"orders_completed_month"`
	MonthKey             string    `json:"month_key"`
	DistinctCustomers    int       `json:"distinct_customers"`
	// FastestCompletionMin — лучшее время «создан → подтверждён», в минутах.
	// Ноль означает «ещё ни одного».
	FastestCompletionMin int          `json:"fastest_completion_min"`
	FiveStarStreak       int          `json:"five_star_streak"`
	RatingCount          int          `json:"rating_count"`
	Cancels              int          `json:"cancels"`
	EarnedTotal          money.Amount `json:"earned_total"`
	UpdatedAt            time.Time    `json:"updated_at"`
}

// CompletedOrder описывает один подтверждённый заказ так, как его видит счётчик.
type CompletedOrder struct {
	ExecutorID uuid.UUID
	CustomerID uuid.UUID
	// Minutes — сколько прошло от создания заказа до подтверждения.
	Minutes int
	Earned  money.Amount
}

// ExecutorStatsRepository ведёт агрегаты исполнителей.
type ExecutorStatsRepository interface {
	// Get возвращает строку; отсутствующая — это нули, а не ошибка.
	Get(ctx context.Context, q Querier, userID uuid.UUID) (*ExecutorStats, error)
	// RecordCompletion учитывает подтверждённый заказ. Вызывается в той же
	// транзакции, что и само подтверждение: агрегат, посчитанный отдельным
	// проходом, разъезжается с заказами ровно в тот момент, когда проход упал.
	RecordCompletion(ctx context.Context, q Querier, order CompletedOrder) error
	// RecordCancel учитывает отмену.
	RecordCancel(ctx context.Context, q Querier, executorID uuid.UUID) error
	// RecordRating учитывает оценку: серия пятёрок либо продолжается, либо
	// обрывается, и третьего не дано.
	RecordRating(ctx context.Context, q Querier, executorID uuid.UUID, rating int) error
	// Recalculate пересчитывает строку с нуля по заказам — админская команда на
	// случай расхождения, как сверка балансов.
	Recalculate(ctx context.Context, userID uuid.UUID) error
}

type executorStatsRepo struct {
	db *sql.DB
}

// NewExecutorStatsRepository создаёт ExecutorStatsRepository.
func NewExecutorStatsRepository(db *sql.DB) ExecutorStatsRepository {
	return &executorStatsRepo{db: db}
}

func (r *executorStatsRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *executorStatsRepo) Get(ctx context.Context, q Querier, userID uuid.UUID) (*ExecutorStats, error) {
	stats := &ExecutorStats{UserID: userID}
	var fastest sql.NullInt64
	err := r.exec(q).QueryRowContext(ctx, `
        SELECT orders_completed, orders_completed_month, month_key, distinct_customers,
               fastest_completion_min, five_star_streak, rating_count, cancels, earned_total, updated_at
        FROM executor_stats WHERE user_id = $1
    `, userID).Scan(&stats.OrdersCompleted, &stats.OrdersCompletedMonth, &stats.MonthKey,
		&stats.DistinctCustomers, &fastest, &stats.FiveStarStreak, &stats.RatingCount,
		&stats.Cancels, &stats.EarnedTotal, &stats.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		// Исполнитель без единого заказа — это нули, а не отсутствие ответа.
		return stats, nil
	}
	if err != nil {
		return nil, err
	}
	if fastest.Valid {
		stats.FastestCompletionMin = int(fastest.Int64)
	}
	// Счётчик месяца обнуляется сравнением, а не ночным проходом: строка,
	// помеченная прошлым месяцем, для текущего значит ноль.
	if stats.MonthKey != time.Now().Format("2006-01") {
		stats.OrdersCompletedMonth = 0
	}
	return stats, nil
}

func (r *executorStatsRepo) RecordCompletion(ctx context.Context, q Querier, order CompletedOrder) error {
	exec := r.exec(q)
	month := time.Now().Format("2006-01")

	// Заказчик засчитывается до агрегата, потому что «новый ли он» решает,
	// увеличивать ли счётчик разных заказчиков. Первая вставка вернёт строку,
	// повторная — ничего.
	var isNewCustomer bool
	err := exec.QueryRowContext(ctx, `
        INSERT INTO executor_customers (executor_id, customer_id, orders, last_at)
        VALUES ($1, $2, 1, now())
        ON CONFLICT (executor_id, customer_id)
        DO UPDATE SET orders = executor_customers.orders + 1, last_at = now()
        RETURNING executor_customers.orders = 1
    `, order.ExecutorID, order.CustomerID).Scan(&isNewCustomer)
	if err != nil {
		return err
	}

	newCustomers := 0
	if isNewCustomer {
		newCustomers = 1
	}
	var minutes interface{}
	if order.Minutes > 0 {
		minutes = order.Minutes
	}

	_, err = exec.ExecContext(ctx, `
        INSERT INTO executor_stats (user_id, orders_completed, orders_completed_month, month_key,
                                    distinct_customers, fastest_completion_min, earned_total, updated_at)
        VALUES ($1, 1, 1, $2, $3, $4::int, $5, now())
        ON CONFLICT (user_id) DO UPDATE SET
            orders_completed = executor_stats.orders_completed + 1,
            -- Смена месяца сбрасывает счётчик здесь же: отдельного прохода,
            -- который мог бы не состояться, для этого не нужно.
            orders_completed_month = CASE WHEN executor_stats.month_key = EXCLUDED.month_key
                                          THEN executor_stats.orders_completed_month + 1
                                          ELSE 1 END,
            month_key = EXCLUDED.month_key,
            distinct_customers = executor_stats.distinct_customers + $3,
            -- Лучшее время: NULL с любой стороны не должен побеждать, поэтому
            -- каждая сторона подстраховывает другую. Тип указан явно —
            -- нетипизированный параметр здесь Postgres вывести не может.
            fastest_completion_min = LEAST(
                COALESCE(executor_stats.fastest_completion_min, $4::int),
                COALESCE($4::int, executor_stats.fastest_completion_min)),
            earned_total = executor_stats.earned_total + EXCLUDED.earned_total,
            updated_at = now()
    `, order.ExecutorID, month, newCustomers, minutes, int64(order.Earned))
	return err
}

func (r *executorStatsRepo) RecordCancel(ctx context.Context, q Querier, executorID uuid.UUID) error {
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO executor_stats (user_id, cancels, updated_at) VALUES ($1, 1, now())
        ON CONFLICT (user_id) DO UPDATE SET
            cancels = executor_stats.cancels + 1, updated_at = now()
    `, executorID)
	return err
}

func (r *executorStatsRepo) RecordRating(ctx context.Context, q Querier, executorID uuid.UUID, rating int) error {
	streak := 0
	if rating >= 5 {
		streak = 1
	}
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO executor_stats (user_id, rating_count, five_star_streak, updated_at)
        VALUES ($1, 1, $2, now())
        ON CONFLICT (user_id) DO UPDATE SET
            rating_count = executor_stats.rating_count + 1,
            -- Серия либо растёт, либо обнуляется. Оценка ниже пяти её обрывает,
            -- и это единственное, что её обрывает.
            five_star_streak = CASE WHEN $2 = 1 THEN executor_stats.five_star_streak + 1 ELSE 0 END,
            updated_at = now()
    `, executorID, streak)
	return err
}

func (r *executorStatsRepo) Recalculate(ctx context.Context, userID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Пересчёт делает то же, что накопление, но по заказам: если они разошлись,
	// прав журнал заказов, а не счётчик.
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO executor_customers (executor_id, customer_id, orders, last_at)
        SELECT executor_id, customer_id, COUNT(*), MAX(COALESCE(completed_at, created_at))
        FROM orders
        WHERE executor_id = $1 AND status = 'COMPLETED'
        GROUP BY executor_id, customer_id
        ON CONFLICT (executor_id, customer_id)
        DO UPDATE SET orders = EXCLUDED.orders, last_at = EXCLUDED.last_at
    `, userID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
        INSERT INTO executor_stats (user_id, orders_completed, orders_completed_month, month_key,
                                    distinct_customers, fastest_completion_min, cancels, earned_total, updated_at)
        SELECT $1,
               COUNT(*) FILTER (WHERE status = 'COMPLETED'),
               COUNT(*) FILTER (WHERE status = 'COMPLETED'
                                  AND to_char(COALESCE(completed_at, created_at), 'YYYY-MM') = to_char(now(), 'YYYY-MM')),
               to_char(now(), 'YYYY-MM'),
               COUNT(DISTINCT customer_id) FILTER (WHERE status = 'COMPLETED'),
               MIN(EXTRACT(EPOCH FROM (completed_at - created_at)) / 60)
                   FILTER (WHERE status = 'COMPLETED' AND completed_at IS NOT NULL),
               COUNT(*) FILTER (WHERE status = 'CANCELED'),
               COALESCE(SUM(final_amount) FILTER (WHERE status = 'COMPLETED'), 0),
               now()
        FROM orders WHERE executor_id = $1
        ON CONFLICT (user_id) DO UPDATE SET
            orders_completed = EXCLUDED.orders_completed,
            orders_completed_month = EXCLUDED.orders_completed_month,
            month_key = EXCLUDED.month_key,
            distinct_customers = EXCLUDED.distinct_customers,
            fastest_completion_min = EXCLUDED.fastest_completion_min,
            cancels = EXCLUDED.cancels,
            earned_total = EXCLUDED.earned_total,
            updated_at = now()
    `, userID); err != nil {
		return err
	}
	return tx.Commit()
}
