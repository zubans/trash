package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/money"
)

// BalanceDiscrepancy — один пользователь, чей сохранённый баланс не совпадает с
// суммой его проводок.
type BalanceDiscrepancy struct {
	UserID     uuid.UUID    `json:"user_id"`
	Phone      string       `json:"phone"`
	Role       string       `json:"role"`
	Balance    money.Amount `json:"balance"`
	Ledger     money.Amount `json:"ledger"`
	Difference money.Amount `json:"difference"`
	Entries    int          `json:"entries"`
}

// OrderHoldAnomaly — заказ, чья удержанная сумма противоречит его статусу:
// деньги ещё удерживаются на завершённом заказе или ничего не удерживается на живом.
type OrderHoldAnomaly struct {
	OrderID    uuid.UUID    `json:"order_id"`
	CustomerID uuid.UUID    `json:"customer_id"`
	Status     string       `json:"status"`
	HoldAmount money.Amount `json:"hold_amount"`
	Reason     string       `json:"reason"`
}

// BooksSummary — итоговая позиция всей системы.
type BooksSummary struct {
	UserTotal    money.Amount    `json:"user_total"`
	AccountTotal money.Amount    `json:"account_total"`
	Difference   money.Amount    `json:"difference"`
	Accounts     []SystemAccount `json:"accounts"`
	EscrowHeld   money.Amount    `json:"escrow_held"`
	LiveOrderSum money.Amount    `json:"live_order_sum"`
	EscrowDrift  money.Amount    `json:"escrow_drift"`
}

// ReconciliationReport — исход полного прохода.
type ReconciliationReport struct {
	UsersChecked  int                  `json:"users_checked"`
	Discrepancies []BalanceDiscrepancy `json:"discrepancies"`
	HoldAnomalies []OrderHoldAnomaly   `json:"hold_anomalies"`
	UnknownTypes  []string             `json:"unknown_transaction_types"`
	Books         BooksSummary         `json:"books"`
	// BooksOpen выставляется, когда две стороны системы не взаимозачитываются.
	BooksOpen bool `json:"books_open"`
	// EscrowMismatch выставляется, когда счёт эскроу и удержания по живым заказам
	// разошлись.
	EscrowMismatch bool `json:"escrow_mismatch"`
}

// OK сообщает, сходятся ли книги.
func (r *ReconciliationReport) OK() bool {
	return len(r.Discrepancies) == 0 && len(r.HoldAnomalies) == 0 &&
		len(r.UnknownTypes) == 0 && !r.BooksOpen && !r.EscrowMismatch
}

// Summary отдаёт однострочный результат для логов.
func (r *ReconciliationReport) Summary() string {
	if r.OK() {
		return fmt.Sprintf("reconciliation clean: %d users match the ledger", r.UsersChecked)
	}
	parts := fmt.Sprintf(
		"reconciliation found problems: %d balance mismatches, %d hold anomalies, %d unknown transaction types (of %d users)",
		len(r.Discrepancies), len(r.HoldAnomalies), len(r.UnknownTypes), r.UsersChecked,
	)
	if r.BooksOpen {
		parts += fmt.Sprintf("; books do not close by %s", r.Books.Difference)
	}
	if r.EscrowMismatch {
		parts += fmt.Sprintf("; escrow off by %s against live order holds", r.Books.EscrowDrift)
	}
	return parts
}

// ReconciliationRepository проверяет, что сохранённые балансы согласуются с
// журналом транзакций.
type ReconciliationRepository interface {
	Reconcile(ctx context.Context, tolerance money.Amount) (*ReconciliationReport, error)
}

type reconcileRepo struct {
	db *sql.DB
}

// NewReconciliationRepository создаёт ReconciliationRepository.
func NewReconciliationRepository(db *sql.DB) ReconciliationRepository {
	return &reconcileRepo{db: db}
}

// ledgerSumExpr строит выражение SUM(CASE ...) из соглашения о знаках, чтобы
// SQL не мог разойтись с LedgerSign.
func ledgerSumExpr(column string) string {
	types := KnownTransactionTypes()
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	var b strings.Builder
	b.WriteString("COALESCE(SUM(CASE ")
	for _, t := range types {
		sign, _ := LedgerSign(t)
		if sign == 0 {
			// Записывается, но баланс не двигает.
			fmt.Fprintf(&b, "WHEN %s.type = '%s' THEN 0 ", column, t)
			continue
		}
		op := "+"
		if sign < 0 {
			op = "-"
		}
		fmt.Fprintf(&b, "WHEN %s.type = '%s' THEN %s%s.amount ", column, t, op, column)
	}
	// Всё, что соглашением не покрыто, выносится отдельно; трактовка этого здесь
	// как нуля прятала бы ровно то, из-за чего сумма и неверна.
	b.WriteString("ELSE 0 END), 0)")
	return b.String()
}

// Reconcile сравнивает баланс каждого пользователя с суммой его проводок.
// tolerance поглощает округление float; передайте 0.01, чтобы допустить копейку расхождения.
func (r *reconcileRepo) Reconcile(ctx context.Context, tolerance money.Amount) (*ReconciliationReport, error) {
	report := &ReconciliationReport{}

	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&report.UsersChecked); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}

	// 1. Типы транзакций, не покрытые соглашением о знаках. Проверяются первыми:
	//    если такие есть, суммам ниже доверять нельзя.
	unknown, err := r.unknownTransactionTypes(ctx)
	if err != nil {
		return nil, err
	}
	report.UnknownTypes = unknown

	// 2. Баланс против реестра.
	query := fmt.Sprintf(`
		SELECT u.id, u.phone, u.role, u.balance, %s AS ledger, COUNT(t.id) AS entries
		FROM users u
		LEFT JOIN transactions t ON t.user_id = u.id
		GROUP BY u.id, u.phone, u.role, u.balance
		HAVING ABS(u.balance - %s) > $1
		ORDER BY ABS(u.balance - %s) DESC`,
		ledgerSumExpr("t"), ledgerSumExpr("t"), ledgerSumExpr("t"))

	rows, err := r.db.QueryContext(ctx, query, tolerance)
	if err != nil {
		return nil, fmt.Errorf("reconcile balances: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var d BalanceDiscrepancy
		if err := rows.Scan(&d.UserID, &d.Phone, &d.Role, &d.Balance, &d.Ledger, &d.Entries); err != nil {
			return nil, err
		}
		d.Difference = d.Balance.Sub(d.Ledger)
		report.Discrepancies = append(report.Discrepancies, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. Удержания, противоречащие статусу заказа.
	anomalies, err := r.holdAnomalies(ctx, tolerance)
	if err != nil {
		return nil, err
	}
	report.HoldAnomalies = anomalies

	// 4. Сходятся ли книги? Каждое движение затрагивает баланс пользователя и
	//    системный счёт, поэтому две стороны обязаны взаимозачесться точно.
	books, err := r.books(ctx)
	if err != nil {
		return nil, err
	}
	report.Books = *books
	report.BooksOpen = books.Difference.Abs() > tolerance
	report.EscrowMismatch = books.EscrowDrift.Abs() > tolerance

	return report, nil
}

// books складывает обе стороны системы и сравнивает эскроу с заказами, ради
// которых он должен держать деньги.
func (r *reconcileRepo) books(ctx context.Context) (*BooksSummary, error) {
	var b BooksSummary

	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(balance), 0) FROM users`).Scan(&b.UserTotal); err != nil {
		return nil, fmt.Errorf("sum user balances: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(balance), 0) FROM system_accounts`).Scan(&b.AccountTotal); err != nil {
		return nil, fmt.Errorf("sum system accounts: %w", err)
	}
	b.Difference = b.UserTotal.Add(b.AccountTotal)

	rows, err := r.db.QueryContext(ctx, `SELECT code, name, balance FROM system_accounts ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a SystemAccount
		if err := rows.Scan(&a.Code, &a.Name, &a.Balance); err != nil {
			return nil, err
		}
		if a.Code == AccountEscrow {
			b.EscrowHeld = a.Balance
		}
		b.Accounts = append(b.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(hold_amount), 0) FROM orders
		WHERE status IN ('SEARCHING', 'ASSIGNED', 'EXECUTED')`).Scan(&b.LiveOrderSum); err != nil {
		return nil, fmt.Errorf("sum live order holds: %w", err)
	}
	b.EscrowDrift = b.EscrowHeld.Sub(b.LiveOrderSum)

	return &b, nil
}

func (r *reconcileRepo) unknownTransactionTypes(ctx context.Context) ([]string, error) {
	known := KnownTransactionTypes()
	placeholders := make([]string, 0, len(known))
	args := make([]interface{}, 0, len(known))
	for i, t := range known {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, string(t))
	}

	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT DISTINCT type::text FROM transactions WHERE type::text NOT IN (%s)`,
		strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return nil, fmt.Errorf("check transaction types: %w", err)
	}
	defer rows.Close()

	var unknown []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		unknown = append(unknown, t)
	}
	return unknown, rows.Err()
}

// holdAnomalies находит заказы, чья удержанная сумма противоречит их статусу:
// завершённый заказ, всё ещё держащий деньги, или живой заказ, который деньги
// держал и больше не держит, — подпись возврата или выплаты, прошедших без
// своего перехода состояния.
//
// «Ничего не удерживает» само по себе ничего не доказывает. Два вида живых
// заказов законно не удерживают ничего: аукцион, который ничего не держит до
// принятия ставки, и обычный заказ бесплатной услуги, где держать было нечего.
// Их попадание в отчёт означало, что проверка срабатывает на корректных данных,
// а проверку, срабатывающую на корректных данных, люди учатся пролистывать.
//
// Различитель — журнал, а не текущая цена. Ledger.Reserve возвращается, ничего
// не записав, когда сумма нулевая, поэтому у бесплатного заказа записи HOLD нет
// вовсе, а у оплаченного она есть независимо от того, правили ли цену после.
// Это оставляет ответ верным даже тогда, когда услугу переоценили задним
// числом.
func (r *reconcileRepo) holdAnomalies(ctx context.Context, tolerance money.Amount) ([]OrderHoldAnomaly, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.id, o.customer_id, o.status::text, o.hold_amount,
		       CASE
		           WHEN o.status IN ('COMPLETED', 'CANCELED') THEN 'finished order still holds money'
		           ELSE 'live order no longer holds the money it took'
		       END AS reason
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE (o.status IN ('COMPLETED', 'CANCELED') AND o.hold_amount > $1)
		   OR (
		        o.status IN ('ASSIGNED', 'EXECUTED')
		        AND sn.is_auction = FALSE
		        AND o.hold_amount <= $1
		        -- Money was taken for this order once. Without this the check
		        -- reports every free order, which is a supported case, not a
		        -- fault.
		        AND EXISTS (
		            SELECT 1 FROM transactions t
		            WHERE t.order_id = o.id AND t.type = 'HOLD'
		        )
		      )
		ORDER BY o.created_at DESC
		LIMIT 500`, tolerance)
	if err != nil {
		return nil, fmt.Errorf("check order holds: %w", err)
	}
	defer rows.Close()

	var anomalies []OrderHoldAnomaly
	for rows.Next() {
		var a OrderHoldAnomaly
		if err := rows.Scan(&a.OrderID, &a.CustomerID, &a.Status, &a.HoldAmount, &a.Reason); err != nil {
			return nil, err
		}
		anomalies = append(anomalies, a)
	}
	return anomalies, rows.Err()
}
