package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"healthlogin/backend/repository"
)

// Журнал проводок: поиск по идентификатору — точный, по разобранному uuid
// (частичный id ничему не соответствует, а строка без цифр и не uuid — пустой
// результат, не вся таблица); счётчик считается только по просьбе.
func TestTransactionJournalSearchAndCount(t *testing.T) {
	db := testDB(t)
	defer db.Close()
	ctx := context.Background()
	journal := repository.NewTransactionJournal(db)

	userID := createTestUser(t, db, "CUSTOMER")
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id = $1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	})
	var account string
	if err := db.QueryRow(`SELECT code FROM system_accounts ORDER BY code LIMIT 1`).Scan(&account); err != nil {
		t.Fatalf("system account: %v", err)
	}
	// Проводка с админом: по его uuid ищется так же, как по uuid заказа.
	adminID := createTestUser(t, db, "ADMIN")
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, adminID) })
	topUpID, fineID := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO transactions (id, user_id, admin_id, type, amount, counterparty) VALUES
		($1, $3, $4, 'TOP_UP', 100, $5),
		($2, $3, NULL, 'FINE', 40, $5)`, topUpID, fineID, userID, adminID, account); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var phone string
	if err := db.QueryRow(`SELECT phone FROM users WHERE id = $1`, userID).Scan(&phone); err != nil {
		t.Fatalf("phone: %v", err)
	}

	page := func(search string, withTotal bool) ([]*repository.Transaction, int) {
		t.Helper()
		txs, total, err := journal.GetTransactions(ctx, repository.TransactionsFilter{
			Search: search, Page: repository.PageRequest{Limit: 10, WithTotal: withTotal},
		})
		if err != nil {
			t.Fatalf("search %q: %v", search, err)
		}
		return txs, total
	}

	// Полный uuid проводки или админа находит ровно её; обрезанный — ничего.
	if txs, total := page(topUpID.String(), true); len(txs) != 1 || total != 1 || txs[0].ID != topUpID {
		t.Errorf("full transaction uuid: %d rows, total %d", len(txs), total)
	}
	if txs, total := page(adminID.String(), true); len(txs) != 1 || total != 1 || txs[0].AdminID == nil || *txs[0].AdminID != adminID {
		t.Errorf("full admin uuid: %d rows, total %d", len(txs), total)
	}
	if txs, _ := page(topUpID.String()[:8], true); len(txs) != 0 {
		t.Errorf("partial uuid matched %d rows, want 0", len(txs))
	}
	// Строка без цифр и не uuid — пусто, а не вся таблица.
	if txs, _ := page("nothing-here", true); len(txs) != 0 {
		t.Errorf("garbage search matched %d rows, want 0", len(txs))
	}
	// Телефон по цифрам находит обе проводки пользователя. База общая с
	// другими тестами, и чужие телефоны могут содержать те же цифры, поэтому
	// проверяется присутствие своих строк, а не точное число.
	has := func(txs []*repository.Transaction, id uuid.UUID) bool {
		for _, tx := range txs {
			if tx.ID == id {
				return true
			}
		}
		return false
	}
	txs, total := page(phone, true)
	if !has(txs, topUpID) || !has(txs, fineID) {
		t.Errorf("phone search: own transactions missing among %d rows", len(txs))
	}
	if total < len(txs) || total < 2 {
		t.Errorf("phone search: total %d with %d rows", total, len(txs))
	}
	// Без просьбы счётчик не считается: строки те же, total — ноль.
	if again, total := page(phone, false); len(again) != len(txs) || total != 0 {
		t.Errorf("without total: %d rows, total %d; want %d and 0", len(again), total, len(txs))
	}
}
