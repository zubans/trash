package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	_ "github.com/lib/pq"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Две одновременные заявки на вывод одного пользователя: пройти должна ровно
// одна. Проверка «уже есть открытая» стоит внутри транзакции под
// advisory-блокировкой по пользователю, поэтому вторая транзакция ждёт первую
// и видит её заявку. На моках этого не показать — гонка живёт в базе.
//
//	ORDER_TEST_DSN="postgres://postgres:x@localhost:55432/healthlogin?sslmode=disable" \
//	    go test ./service/ -run Integration
func TestWithdrawalPendingCheckSerializesIntegration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	userID := seedLedgerUser(t, db, money.FromRubles(1000))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM balance_withdrawal_requests WHERE user_id = $1`, userID)
	})
	ledger := NewLedger(repository.NewTransactionRepository(db), repository.NewSystemAccountRepository(db))
	wallet := NewWalletService(repository.New(db), repository.NewPayoutRepository(db), ledger)

	const attempts = 6
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		refused int
		others  []error
	)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := wallet.CreateWithdrawalRequest(ctx, userID, money.FromRubles(100))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrWithdrawalPending):
				refused++
			default:
				others = append(others, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(others) > 0 {
		t.Fatalf("unexpected errors: %v", others)
	}
	if created != 1 || refused != attempts-1 {
		t.Fatalf("created %d, refused %d; want exactly one created and %d refused", created, refused, attempts-1)
	}

	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM balance_withdrawal_requests WHERE user_id = $1 AND status = 'PENDING'`, userID).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 1 {
		t.Errorf("pending requests in the database: %d, want 1", pending)
	}
	var balance money.Amount
	if err := db.QueryRow(`SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if want := money.FromRubles(900); balance != want {
		t.Errorf("balance %s, want %s: only one request may reserve money", balance, want)
	}
}
