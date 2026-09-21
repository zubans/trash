package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Оплата покупки — это дебет с проверкой баланса на счёт SHOP: деньги уходят
// с баланса покупателя и появляются на счёте выручки, итог книг не меняется.
func TestShopChargeMovesPaymentToShopAccount(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	userID := uuid.New()
	if _, err := txRepo.GetBalance(context.Background(), userID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	price := money.FromRubles(1500)
	if err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.ShopCharge(context.Background(), tx, userID, price)
	}); err != nil {
		t.Fatalf("charge: %v", err)
	}

	if got := accounts.balances[repository.AccountShop]; got != price {
		t.Errorf("shop account = %s, expected %s", got, price)
	}
	if got := txRepo.balances[userID].Sub(mockDefaultBalance); got != price.Neg() {
		t.Errorf("user was charged %s, expected %s", got.Neg(), price)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("charging changed the books total: %s, expected %s", got, opening)
	}
	var entries int
	for _, entry := range txRepo.txs {
		if entry.Type == string(repository.TransactionTypeShopPurchase) {
			entries++
		}
	}
	if entries != 1 {
		t.Errorf("expected one SHOP_PURCHASE entry, got %d", entries)
	}
}

// Купить в долг нельзя: при нехватке не двигается ни баланс, ни счёт, и в
// журнал ничего не пишется.
func TestShopChargeRefusesWhenBalanceIsShort(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	userID := uuid.New()
	if _, err := txRepo.GetBalance(context.Background(), userID); err != nil {
		t.Fatalf("balance: %v", err)
	}

	err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.ShopCharge(context.Background(), tx, userID, mockDefaultBalance.Add(money.FromRubles(1)))
	})
	if !errors.Is(err, repository.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if got := accounts.balances[repository.AccountShop]; !got.IsZero() {
		t.Errorf("refused charge still moved money: shop account = %s", got)
	}
	if got := txRepo.balances[userID]; got != mockDefaultBalance {
		t.Errorf("refused charge changed the balance: %s", got)
	}
	if len(txRepo.txs) != 0 {
		t.Errorf("refused charge wrote %d entries", len(txRepo.txs))
	}
}

// Возврат по уже выведенной выручке уводит SHOP в минус, и это нормально:
// вернуть деньги покупателю — обязанность платформы, а минус на счёте — сигнал
// сверки, а не повод отказать.
func TestShopRefundDrivesShopNegativeWhenRevenuePaidOut(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	userID, adminID := uuid.New(), uuid.New()
	if _, err := txRepo.GetBalance(context.Background(), userID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	price := money.FromRubles(1000)
	steps := []func(tx *sql.Tx) error{
		func(tx *sql.Tx) error { return ledger.ShopCharge(context.Background(), tx, userID, price) },
		func(tx *sql.Tx) error { return ledger.ShopPayout(context.Background(), tx, adminID, price) },
		func(tx *sql.Tx) error { return ledger.ShopRefund(context.Background(), tx, userID, price, &adminID) },
	}
	for i, step := range steps {
		if err := ledger.RunInTx(context.Background(), step); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	if got := accounts.balances[repository.AccountShop]; got != price.Neg() {
		t.Errorf("shop account after refund of paid-out revenue = %s, expected %s", got, price.Neg())
	}
	if got := txRepo.balances[userID]; got != mockDefaultBalance {
		t.Errorf("buyer balance = %s, expected the refund to restore %s", got, mockDefaultBalance)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("charge → payout → refund changed the books total: %s, expected %s", got, opening)
	}
}

// Вывод ограничен собранным: больше, чем лежит на SHOP, забрать нельзя, и
// отказанный вывод не двигает счёт.
func TestShopPayoutRefusesMoreThanCollected(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)
	accounts.balances[repository.AccountShop] = money.FromRubles(500)
	accounts.balances[repository.AccountDeposits] = money.FromRubles(500).Neg()

	err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.ShopPayout(context.Background(), tx, uuid.New(), money.FromRubles(501))
	})
	if !errors.Is(err, repository.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if got := accounts.balances[repository.AccountShop]; got != money.FromRubles(500) {
		t.Errorf("refused payout still moved money: shop account = %s", got)
	}
}

// Два параллельных вывода не заберут больше, чем собрано: списание со счёта
// охраняется одним оператором, поэтому второй вывод упирается в остаток, уже
// изменённый первым. На моках это не воспроизводится — там нет настоящей
// блокировки строки, поэтому тест ходит в реальную базу.
func TestShopPayoutRaceNeverOverdraws(t *testing.T) {
	db := openTestDB(t)
	ledger := NewLedger(repository.NewTransactionRepository(db), repository.NewSystemAccountRepository(db))

	buyerID, adminID := uuid.New(), uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'CUSTOMER', $2, 'x', $3, 'ACTIVE')`,
		buyerID, "+7999"+buyerID.String()[:7], money.FromRubles(100)); err != nil {
		t.Fatalf("seed buyer: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (id, role, phone, password, balance, status) VALUES ($1, 'ADMIN', $2, 'x', 0, 'ACTIVE')`,
		adminID, "+7999"+adminID.String()[:7]); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM transactions WHERE user_id IN ($1, $2)`, buyerID, adminID)
		_, _ = db.Exec(`DELETE FROM users WHERE id IN ($1, $2)`, buyerID, adminID)
		_, _ = db.Exec(`UPDATE system_accounts SET balance = 0 WHERE code = $1`, repository.AccountShop)
	})

	if err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.ShopCharge(context.Background(), tx, buyerID, money.FromRubles(100))
	}); err != nil {
		t.Fatalf("charge: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
				return ledger.ShopPayout(context.Background(), tx, adminID, money.FromRubles(60))
			})
		}(i)
	}
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, repository.ErrInsufficientFunds) {
			t.Errorf("unexpected payout error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Errorf("expected exactly one of the two payouts to succeed, got %d", succeeded)
	}
	var balance money.Amount
	if err := db.QueryRow(`SELECT balance FROM system_accounts WHERE code = $1`, repository.AccountShop).Scan(&balance); err != nil {
		t.Fatalf("shop balance: %v", err)
	}
	if balance != money.FromRubles(40) {
		t.Errorf("shop account = %s, expected 40.00 after one payout of 60 out of 100", balance)
	}
}
