package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// Виды денежных инцидентов. Вид называет, какая именно проверка сработала, и
// потому определяет, что смотреть первым: расчёт ставки, склад подарков или
// потолок начисления.
const (
	// IncidentRewardExceedsPayment — вознаграждение исполнителю оказалось больше
	// того, что заплатил заказчик. Записанное движение зажато уплаченным.
	IncidentRewardExceedsPayment = "reward_exceeds_payment"
	// IncidentCommissionOutOfRange — доля платформы вышла за [0, уплаченное].
	IncidentCommissionOutOfRange = "commission_out_of_range"
	// IncidentSettlementMismatch — части распределения не сошлись с удержанием.
	IncidentSettlementMismatch = "settlement_mismatch"
	// IncidentGiftOutOfStock — подарок кончился, ачивка выдана без него.
	IncidentGiftOutOfStock = "gift_out_of_stock"
	// IncidentPointsCapHit — суточный потолок баллов сработал. Не авария, но
	// след, по которому видно накрутку.
	IncidentPointsCapHit = "points_cap_hit"
)

// Уровни. CRITICAL будит человека, WARNING ждёт рабочего дня.
const (
	IncidentSeverityCritical = "CRITICAL"
	IncidentSeverityWarning  = "WARNING"
)

// MoneyIncident — расхождение, которое код заметил и обошёл зажимом, вместо
// того чтобы упасть или записать неверное движение.
//
// Существует потому, что у ошибки в расчёте нет хорошего исхода. Откат оставил
// бы заказ подтверждённым в одной половине системы и неоплаченным в другой, а
// повтор упёрся бы в ту же ошибку. Зажим оставляет книги сведёнными — и обязан
// оставить след, иначе он превращается в тихую потерю денег.
type MoneyIncident struct {
	ID         uuid.UUID              `json:"id"`
	Kind       string                 `json:"kind"`
	Severity   string                 `json:"severity"`
	OrderID    *uuid.UUID             `json:"order_id,omitempty"`
	UserID     *uuid.UUID             `json:"user_id,omitempty"`
	Expected   *money.Amount          `json:"expected,omitempty"`
	Actual     *money.Amount          `json:"actual,omitempty"`
	Applied    *money.Amount          `json:"applied,omitempty"`
	Details    map[string]interface{} `json:"details,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	ResolvedAt *time.Time             `json:"resolved_at,omitempty"`
	ResolvedBy *uuid.UUID             `json:"resolved_by,omitempty"`
	Resolution string                 `json:"resolution,omitempty"`
}

// MoneyIncidentRepository хранит инциденты.
type MoneyIncidentRepository interface {
	// Record записывает инцидент. Принимает Querier, потому что почти всегда
	// вызывается внутри той же транзакции, что и зажатое движение: инцидент не
	// должен закоммититься без него, а движение — без инцидента.
	Record(ctx context.Context, q Querier, incident *MoneyIncident) error
	// ListOpen возвращает неразобранные инциденты, свежие первыми.
	ListOpen(ctx context.Context, limit int) ([]*MoneyIncident, error)
	// List возвращает все инциденты, включая разобранные.
	List(ctx context.Context, limit int) ([]*MoneyIncident, error)
	// Resolve закрывает инцидент разбором администратора.
	Resolve(ctx context.Context, id, adminID uuid.UUID, resolution string) error
	// CountOpen — число неразобранных. Его публикует датчик, на который повешен
	// алерт: алерт про деньги обязан говорить только о закоммиченном, а счётчик
	// в процессе переоценивает себя при откате транзакции.
	CountOpen(ctx context.Context) (int, error)
}

type moneyIncidentRepo struct {
	db *sql.DB
}

// NewMoneyIncidentRepository создаёт MoneyIncidentRepository.
func NewMoneyIncidentRepository(db *sql.DB) MoneyIncidentRepository {
	return &moneyIncidentRepo{db: db}
}

func (r *moneyIncidentRepo) exec(q Querier) Querier {
	if q == nil {
		return r.db
	}
	return q
}

func (r *moneyIncidentRepo) Record(ctx context.Context, q Querier, incident *MoneyIncident) error {
	if incident.ID == uuid.Nil {
		incident.ID = uuid.New()
	}
	if incident.Severity == "" {
		incident.Severity = IncidentSeverityCritical
	}
	details := []byte("{}")
	if len(incident.Details) > 0 {
		encoded, err := json.Marshal(incident.Details)
		if err != nil {
			return err
		}
		details = encoded
	}
	_, err := r.exec(q).ExecContext(ctx, `
        INSERT INTO money_incidents (id, kind, severity, order_id, user_id, expected, actual, applied, details)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, incident.ID, incident.Kind, incident.Severity, incident.OrderID, incident.UserID,
		amountPtr(incident.Expected), amountPtr(incident.Actual), amountPtr(incident.Applied), details)
	return err
}

// amountPtr разворачивает сумму в копейки для базы, сохраняя NULL там, где
// инцидент этой стороны не касается.
func amountPtr(a *money.Amount) interface{} {
	if a == nil {
		return nil
	}
	return int64(*a)
}

func (r *moneyIncidentRepo) ListOpen(ctx context.Context, limit int) ([]*MoneyIncident, error) {
	return r.list(ctx, limit, true)
}

func (r *moneyIncidentRepo) List(ctx context.Context, limit int) ([]*MoneyIncident, error) {
	return r.list(ctx, limit, false)
}

func (r *moneyIncidentRepo) list(ctx context.Context, limit int, openOnly bool) ([]*MoneyIncident, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `
        SELECT id, kind, severity, order_id, user_id, expected, actual, applied,
               details, created_at, resolved_at, resolved_by, COALESCE(resolution, '')
        FROM money_incidents`
	if openOnly {
		query += ` WHERE resolved_at IS NULL`
	}
	query += ` ORDER BY created_at DESC LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	incidents := make([]*MoneyIncident, 0)
	for rows.Next() {
		var incident MoneyIncident
		var expected, actual, applied sql.NullInt64
		var details []byte
		if err := rows.Scan(&incident.ID, &incident.Kind, &incident.Severity,
			&incident.OrderID, &incident.UserID, &expected, &actual, &applied,
			&details, &incident.CreatedAt, &incident.ResolvedAt, &incident.ResolvedBy, &incident.Resolution); err != nil {
			return nil, err
		}
		incident.Expected = nullAmount(expected)
		incident.Actual = nullAmount(actual)
		incident.Applied = nullAmount(applied)
		if len(details) > 0 {
			_ = json.Unmarshal(details, &incident.Details)
		}
		incidents = append(incidents, &incident)
	}
	return incidents, rows.Err()
}

func nullAmount(v sql.NullInt64) *money.Amount {
	if !v.Valid {
		return nil
	}
	amount := money.Amount(v.Int64)
	return &amount
}

func (r *moneyIncidentRepo) Resolve(ctx context.Context, id, adminID uuid.UUID, resolution string) error {
	return execExpectingOne(ctx, r.db, `
        UPDATE money_incidents
           SET resolved_at = now(), resolved_by = $2, resolution = $3
         WHERE id = $1 AND resolved_at IS NULL
    `, id, adminID, resolution)
}

func (r *moneyIncidentRepo) CountOpen(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM money_incidents WHERE resolved_at IS NULL`).Scan(&count)
	return count, err
}
