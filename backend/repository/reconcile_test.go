package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
)

// allTransactionTypes — полный набор значений, которые может принимать enum
// transaction_type. Добавление константы без добавления её сюда роняет тест
// ниже, и в этом смысл: непокрытый тип молча выпадает из каждой суммы баланса
// и делает сверку тихо неверной.
var allTransactionTypes = []TransactionType{
	TransactionTypeTopUp,
	TransactionTypeHold,
	TransactionTypePayment,
	TransactionTypeReward,
	TransactionTypeFine,
	TransactionTypeRefund,
	TransactionTypeWithdrawal,
	TransactionTypeWithdrawalHold,
	TransactionTypeWithdrawalPaid,
	TransactionTypeTip,
	TransactionTypeTipReward,
	TransactionTypeCommission,
	TransactionTypeCommissionPayout,
	TransactionTypeBonus,
}

func TestEveryTransactionTypeHasALedgerSign(t *testing.T) {
	for _, tt := range allTransactionTypes {
		if _, known := LedgerSign(tt); !known {
			t.Errorf("transaction type %q has no ledger sign: add it to ledgerSigns", tt)
		}
	}

	if got, want := len(KnownTransactionTypes()), len(allTransactionTypes); got != want {
		t.Errorf("the sign convention covers %d types but %d exist", got, want)
	}
}

// TestLedgerSignsMatchTheServiceBehaviour фиксирует направление каждого типа
// против того, что сервисы на самом деле делают с балансом.
func TestLedgerSignsMatchTheServiceBehaviour(t *testing.T) {
	cases := map[TransactionType]int{
		// Деньги приходят.
		TransactionTypeTopUp:     +1,
		TransactionTypeReward:    +1,
		TransactionTypeRefund:    +1,
		TransactionTypeTipReward: +1,
		TransactionTypeBonus:     +1,
		// Деньги уходят.
		TransactionTypeHold:           -1,
		TransactionTypeFine:           -1,
		TransactionTypeWithdrawal:     -1,
		TransactionTypeWithdrawalHold: -1,
		TransactionTypeTip:            -1,
		// Записывается, но ничего не двигает: деньги заказчика ушли с баланса ещё
		// когда бралось удержание, а PAYMENT помечает это удержание израсходованным.
		TransactionTypePayment:        0,
		TransactionTypeWithdrawalPaid: 0,
		// Комиссия перемещается между двумя системными счетами, поэтому ни одна её
		// сторона не трогает баланс пользователя, против которого она записана.
		TransactionTypeCommission:       0,
		TransactionTypeCommissionPayout: 0,
	}

	for tt, want := range cases {
		got, known := LedgerSign(tt)
		if !known {
			t.Errorf("%q is not covered by the convention", tt)
			continue
		}
		if got != want {
			t.Errorf("%q: expected sign %+d, got %+d", tt, want, got)
		}
	}
}

// TestLedgerSumExprCoversEveryType охраняет сгенерированный SQL: выражение
// строится из той же карты, поэтому тип не может быть в соглашении и при этом
// отсутствовать в запросе.
func TestLedgerSumExprCoversEveryType(t *testing.T) {
	expr := ledgerSumExpr("t")

	for _, tt := range allTransactionTypes {
		if !strings.Contains(expr, "'"+string(tt)+"'") {
			t.Errorf("generated SQL does not mention %q:\n%s", tt, expr)
		}
	}
	if !strings.Contains(expr, "THEN -t.amount") {
		t.Error("generated SQL has no debit branch")
	}
	if !strings.Contains(expr, "THEN +t.amount") {
		t.Error("generated SQL has no credit branch")
	}
}

func TestReportOKAndSummary(t *testing.T) {
	clean := &ReconciliationReport{UsersChecked: 12}
	if !clean.OK() {
		t.Error("a report with no findings must be OK")
	}
	if !strings.Contains(clean.Summary(), "clean") {
		t.Errorf("unexpected summary: %s", clean.Summary())
	}

	for name, report := range map[string]*ReconciliationReport{
		"balance mismatch": {Discrepancies: []BalanceDiscrepancy{{UserID: uuid.New()}}},
		"hold anomaly":     {HoldAnomalies: []OrderHoldAnomaly{{OrderID: uuid.New()}}},
		"unknown type":     {UnknownTypes: []string{"BONUS"}},
	} {
		if report.OK() {
			t.Errorf("%s: report must not be OK", name)
		}
		if !strings.Contains(report.Summary(), "problems") {
			t.Errorf("%s: unexpected summary: %s", name, report.Summary())
		}
	}
}

// TestReconcileAgainstDatabase выполняет настоящий запрос. Он пропускается,
// если RECONCILE_TEST_DSN не указывает на базу с применённой схемой, например
//
//	RECONCILE_TEST_DSN="postgres://postgres:x@localhost:55432/healthlogin?sslmode=disable" go test ./repository/ -run Database
func TestReconcileAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("RECONCILE_TEST_DSN")
	if dsn == "" {
		t.Skip("set RECONCILE_TEST_DSN to run the reconciliation against a real database")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("connect: %v", err)
	}

	matching := uuid.New()
	drifted := uuid.New()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = ANY($1)`, "{"+matching.String()+","+drifted.String()+"}")
		_, _ = db.Exec(`DELETE FROM users WHERE id = ANY($1)`, "{"+matching.String()+","+drifted.String()+"}")
	})

	seed := func(id uuid.UUID, phone string, balance money.Amount) {
		if _, err := db.Exec(
			`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'CUSTOMER', $2, 'x', $3, 'ACTIVE')`,
			id, phone, balance); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	entry := func(id uuid.UUID, kind TransactionType, amount money.Amount) {
		if _, err := db.Exec(
			`INSERT INTO transactions (user_id, type, amount) VALUES ($1, $2, $3)`, id, kind, amount); err != nil {
			t.Fatalf("seed transaction: %v", err)
		}
	}

	// 1000 пришло, 300 удержано, 300 из этого удержания израсходовано: баланс 700,
	// и PAYMENT не должен считаться второй раз.
	seed(matching, "+7999"+matching.String()[:7], money.FromRubles(700))
	entry(matching, TransactionTypeTopUp, money.FromRubles(1000))
	entry(matching, TransactionTypeHold, money.FromRubles(300))
	entry(matching, TransactionTypePayment, money.FromRubles(300))

	// Тот же реестр, но баланс говорит 1000 — возврат, выполнившийся дважды.
	seed(drifted, "+7998"+drifted.String()[:7], money.FromRubles(1000))
	entry(drifted, TransactionTypeTopUp, money.FromRubles(1000))
	entry(drifted, TransactionTypeHold, money.FromRubles(300))
	entry(drifted, TransactionTypePayment, money.FromRubles(300))

	report, err := NewReconciliationRepository(db).Reconcile(context.Background(), money.FromRubles(0.01))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var found *BalanceDiscrepancy
	for i := range report.Discrepancies {
		if report.Discrepancies[i].UserID == matching {
			t.Errorf("a user whose balance matches the ledger was reported: %+v", report.Discrepancies[i])
		}
		if report.Discrepancies[i].UserID == drifted {
			found = &report.Discrepancies[i]
		}
	}
	if found == nil {
		t.Fatal("the drifted balance was not reported")
	}
	if found.Difference != money.FromRubles(300) {
		t.Errorf("expected a difference of +300.00, got %s", found.Difference)
	}
	if found.Ledger != money.FromRubles(700) {
		t.Errorf("expected a ledger of 700.00 (PAYMENT excluded), got %s", found.Ledger)
	}
}
