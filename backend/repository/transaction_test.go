package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// TestCreateTransactionWritesCounterparty охраняет обе ветки записи — внутри
// транзакции и без неё. Ветки раньше были двумя отдельными вызовами с
// одинаковым списком аргументов, и столбец, добавленный в одну, легко
// пропустить в другой; тест смотрит на них одинаково, поэтому такой пропуск
// теперь падает.
//
// Пустой счёт проверяется отдельно: counterparty — внешний ключ на
// system_accounts, поэтому «счёт не назван» обязан лечь в NULL, а не в пустую
// строку, которой там нет.
func TestCreateTransactionWritesCounterparty(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx := context.Background()
	repo := repository.NewTransactionRepository(db)
	userID := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})

	counterpartyOf := func(id uuid.UUID) (string, bool) {
		t.Helper()
		var stored sql.NullString
		if err := db.QueryRow(`SELECT counterparty FROM transactions WHERE id = $1`, id).Scan(&stored); err != nil {
			t.Fatalf("read back the transaction: %v", err)
		}
		return stored.String, stored.Valid
	}

	create := func(name, counterparty string, inTx bool) uuid.UUID {
		t.Helper()
		entry := &repository.Transaction{
			UserID:       userID,
			Type:         string(repository.TransactionTypeFine),
			Amount:       money.FromRubles(10),
			Counterparty: counterparty,
		}
		var err error
		if inTx {
			err = repo.RunInTx(ctx, func(tx *sql.Tx) error {
				return repo.CreateTransaction(ctx, tx, entry)
			})
		} else {
			err = repo.CreateTransaction(ctx, nil, entry)
		}
		if err != nil {
			t.Fatalf("%s: creating the transaction must not fail: %v", name, err)
		}
		return entry.ID
	}

	for _, tc := range []struct {
		name string
		inTx bool
	}{
		{"in a transaction", true},
		{"without a transaction", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored, ok := counterpartyOf(create(tc.name, repository.AccountFines, tc.inTx))
			if !ok {
				t.Fatalf("counterparty was not written at all")
			}
			if stored != repository.AccountFines {
				t.Errorf("counterparty = %q, want %q", stored, repository.AccountFines)
			}

			if _, ok := counterpartyOf(create(tc.name+" without an account", "", tc.inTx)); ok {
				t.Error("an unnamed account must be stored as NULL")
			}
		})
	}
}
