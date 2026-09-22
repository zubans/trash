package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// TestLedgerRecordsCounterpartyIntegration выполняется на настоящем Postgres,
// потому что охраняет он именно запись в столбец. Ledger.record с самого начала
// заполнял Transaction.Counterparty, а INSERT его не перечислял, так что на
// моках проводка выглядела правильной, а в базе у каждой строки был NULL. Эту
// разницу видно только оттуда.
//
//	ORDER_TEST_DSN="postgres://postgres:x@localhost:55432/healthlogin?sslmode=disable" \
//	    go test ./service/ -run Integration
func TestLedgerRecordsCounterpartyIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	userID := seedLedgerUser(t, db, money.FromRubles(1000))
	ledger := NewLedger(repository.NewTransactionRepository(db), repository.NewSystemAccountRepository(db))

	// Три движения в разные стороны и на разные счета: с баланса на счёт, со
	// счёта на баланс и без охраны баланса. Один счёт на всех не показал бы, что
	// в строку попадает именно тот, против которого шло движение.
	err := ledger.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := ledger.Reserve(ctx, tx, userID, repository.AccountEscrow, money.FromRubles(300),
			repository.TransactionTypeHold, nil); err != nil {
			return err
		}
		if err := ledger.Charge(ctx, tx, userID, repository.AccountFines, money.FromRubles(50),
			repository.TransactionTypeFine, nil); err != nil {
			return err
		}
		return ledger.Deposit(ctx, tx, userID, money.FromRubles(100), nil)
	})
	if err != nil {
		t.Fatalf("ledger movements must not fail: %v", err)
	}

	want := map[string]string{
		string(repository.TransactionTypeHold):  repository.AccountEscrow,
		string(repository.TransactionTypeFine):  repository.AccountFines,
		string(repository.TransactionTypeTopUp): repository.AccountDeposits,
	}

	rows, err := db.Query(
		`SELECT type::text, COALESCE(counterparty, '') FROM transactions WHERE user_id = $1`, userID)
	if err != nil {
		t.Fatalf("read back the ledger: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var kind, counterparty string
		if err := rows.Scan(&kind, &counterparty); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[kind] = counterparty
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read back the ledger: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d: %v", len(want), len(got), got)
	}
	for kind, account := range want {
		switch stored, ok := got[kind]; {
		case !ok:
			t.Errorf("%s was not written at all", kind)
		case stored == "":
			t.Errorf("%s stored no counterparty, want %s", kind, account)
		case stored != account:
			t.Errorf("%s faced %s, want %s", kind, stored, account)
		}
	}
}

// seedLedgerUser создаёт пользователя с балансом и убирает за собой.
func seedLedgerUser(t *testing.T, db *sql.DB, balance money.Amount) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'CUSTOMER', $2, 'x', $3, 'ACTIVE')`,
		id, "+7997"+id.String()[:7], balance); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, id)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, id)
	})
	return id
}
