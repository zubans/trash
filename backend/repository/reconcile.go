package repository

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// BalanceDiscrepancy is one user whose stored balance does not match the sum of
// their ledger entries.
type BalanceDiscrepancy struct {
	UserID     uuid.UUID `json:"user_id"`
	Phone      string    `json:"phone"`
	Role       string    `json:"role"`
	Balance    float64   `json:"balance"`
	Ledger     float64   `json:"ledger"`
	Difference float64   `json:"difference"`
	Entries    int       `json:"entries"`
}

// OrderHoldAnomaly is an order whose held amount contradicts its status: money
// still held on a finished order, or nothing held on a live one.
type OrderHoldAnomaly struct {
	OrderID    uuid.UUID `json:"order_id"`
	CustomerID uuid.UUID `json:"customer_id"`
	Status     string    `json:"status"`
	HoldAmount float64   `json:"hold_amount"`
	Reason     string    `json:"reason"`
}

// ReconciliationReport is the outcome of a full pass.
type ReconciliationReport struct {
	UsersChecked  int                  `json:"users_checked"`
	Discrepancies []BalanceDiscrepancy `json:"discrepancies"`
	HoldAnomalies []OrderHoldAnomaly   `json:"hold_anomalies"`
	UnknownTypes  []string             `json:"unknown_transaction_types"`
}

// OK reports whether the books balance.
func (r *ReconciliationReport) OK() bool {
	return len(r.Discrepancies) == 0 && len(r.HoldAnomalies) == 0 && len(r.UnknownTypes) == 0
}

// Summary renders a one-line result for logs.
func (r *ReconciliationReport) Summary() string {
	if r.OK() {
		return fmt.Sprintf("reconciliation clean: %d users match the ledger", r.UsersChecked)
	}
	return fmt.Sprintf(
		"reconciliation found problems: %d balance mismatches, %d hold anomalies, %d unknown transaction types (of %d users)",
		len(r.Discrepancies), len(r.HoldAnomalies), len(r.UnknownTypes), r.UsersChecked,
	)
}

// ReconciliationRepository verifies that the stored balances agree with the
// transaction log.
type ReconciliationRepository interface {
	Reconcile(tolerance float64) (*ReconciliationReport, error)
}

type reconcileRepo struct {
	db *sql.DB
}

// NewReconciliationRepository creates a ReconciliationRepository.
func NewReconciliationRepository(db *sql.DB) ReconciliationRepository {
	return &reconcileRepo{db: db}
}

// ledgerSumExpr builds the SUM(CASE ...) expression from the sign convention, so
// the SQL cannot drift away from LedgerSign.
func ledgerSumExpr(column string) string {
	types := KnownTransactionTypes()
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	var b strings.Builder
	b.WriteString("COALESCE(SUM(CASE ")
	for _, t := range types {
		sign, _ := LedgerSign(t)
		if sign == 0 {
			// Recorded but does not move the balance.
			fmt.Fprintf(&b, "WHEN %s.type = '%s' THEN 0 ", column, t)
			continue
		}
		op := "+"
		if sign < 0 {
			op = "-"
		}
		fmt.Fprintf(&b, "WHEN %s.type = '%s' THEN %s%s.amount ", column, t, op, column)
	}
	// Anything not covered by the convention is surfaced separately; treating it
	// as zero here would hide the very thing that makes the sum wrong.
	b.WriteString("ELSE 0 END), 0)")
	return b.String()
}

// Reconcile compares every user's balance with the sum of their ledger entries.
// tolerance absorbs float rounding; pass 0.01 to allow a kopeck of drift.
func (r *reconcileRepo) Reconcile(tolerance float64) (*ReconciliationReport, error) {
	report := &ReconciliationReport{}

	if err := r.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&report.UsersChecked); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}

	// 1. Transaction types the sign convention does not cover. Checked first:
	//    if any exist, the sums below cannot be trusted.
	unknown, err := r.unknownTransactionTypes()
	if err != nil {
		return nil, err
	}
	report.UnknownTypes = unknown

	// 2. Balance against the ledger.
	query := fmt.Sprintf(`
		SELECT u.id, u.phone, u.role, u.balance, %s AS ledger, COUNT(t.id) AS entries
		FROM users u
		LEFT JOIN transactions t ON t.user_id = u.id
		GROUP BY u.id, u.phone, u.role, u.balance
		HAVING ABS(u.balance - %s) > $1
		ORDER BY ABS(u.balance - %s) DESC`,
		ledgerSumExpr("t"), ledgerSumExpr("t"), ledgerSumExpr("t"))

	rows, err := r.db.Query(query, tolerance)
	if err != nil {
		return nil, fmt.Errorf("reconcile balances: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var d BalanceDiscrepancy
		if err := rows.Scan(&d.UserID, &d.Phone, &d.Role, &d.Balance, &d.Ledger, &d.Entries); err != nil {
			return nil, err
		}
		d.Difference = d.Balance - d.Ledger
		report.Discrepancies = append(report.Discrepancies, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. Holds that contradict the order status.
	anomalies, err := r.holdAnomalies(tolerance)
	if err != nil {
		return nil, err
	}
	report.HoldAnomalies = anomalies

	return report, nil
}

func (r *reconcileRepo) unknownTransactionTypes() ([]string, error) {
	known := KnownTransactionTypes()
	placeholders := make([]string, 0, len(known))
	args := make([]interface{}, 0, len(known))
	for i, t := range known {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, string(t))
	}

	rows, err := r.db.Query(fmt.Sprintf(
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

// holdAnomalies finds orders whose held amount contradicts their status. A
// finished order still holding money, or a live paid order holding none, is the
// signature of a refund or payout that ran without its state transition.
func (r *reconcileRepo) holdAnomalies(tolerance float64) ([]OrderHoldAnomaly, error) {
	rows, err := r.db.Query(`
		SELECT o.id, o.customer_id, o.status::text, o.hold_amount,
		       CASE
		           WHEN o.status IN ('COMPLETED', 'CANCELED') THEN 'finished order still holds money'
		           ELSE 'live non-auction order holds nothing'
		       END AS reason
		FROM orders o
		JOIN service_nodes sn ON sn.id = o.service_variant_id
		WHERE (o.status IN ('COMPLETED', 'CANCELED') AND o.hold_amount > $1)
		   OR (o.status IN ('ASSIGNED', 'EXECUTED') AND sn.is_auction = FALSE AND o.hold_amount <= $1)
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
