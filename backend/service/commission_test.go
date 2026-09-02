package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// confirmWithCommission прогоняет один заказ от начала до конца по заданной
// ставке и отдаёт книги, чтобы тест мог посмотреть на обе стороны разделения,
// вместе с итогом книг до заказа: моковые балансы стартуют не с нуля, поэтому
// важно, что подтверждение оставляет итог ровно там, где он был.
func confirmWithCommission(t *testing.T, percent string) (*mockTransactionRepo, *mockAccounts, uuid.UUID, money.Amount) {
	t.Helper()

	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	orderRepo := &mockOrderRepo{}
	settings := &orderMockSettingsRepo{settings: map[string]string{
		SettingOrderCommissionPercent: percent,
	}}
	srv := NewOrderService(orderRepo, NewLedger(txRepo, accounts), settings,
		newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil)

	ctx := context.Background()
	customerID, executorID := uuid.New(), uuid.New()
	// Трогаем оба баланса, чтобы стартовый итог покрыл всех участников.
	if _, err := txRepo.GetBalance(ctx, customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if _, err := txRepo.GetBalance(ctx, executorID); err != nil {
		t.Fatalf("balance: %v", err)
	}

	opening := booksTotal(txRepo, accounts)

	order, err := srv.CreateOrder(ctx, customerID, standardVariantID, false, false, "", nil, nil)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if err := orderRepo.AssignOrder(ctx, order.ID, executorID); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := orderRepo.Execute(ctx, nil, order.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := srv.ConfirmOrder(ctx, order.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	return txRepo, accounts, executorID, opening
}

// Завершённый заказ — единственный источник комиссии для платформы, и её взятие
// не должно ни выдумывать, ни уничтожать деньги: эскроу держит ровно то, что
// заплатил заказчик, и обнуляется через вознаграждение исполнителя и счёт комиссии.
func TestConfirmOrderSplitsPaymentBetweenExecutorAndCommission(t *testing.T) {
	txRepo, accounts, executorID, opening := confirmWithCommission(t, "15")

	price := money.FromRubles(100)
	commission := money.FromRubles(15)
	reward := price.Sub(commission)

	if got := accounts.balances[repository.AccountCommission]; got != commission {
		t.Errorf("commission account = %s, expected %s", got, commission)
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != reward {
		t.Errorf("executor was rewarded %s, expected %s", got, reward)
	}
	// Заказчик в любом случае заплатил полную цену: комиссия выходит из
	// вознаграждения исполнителя, а не из второго списания.
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("confirming changed the books total: %s, expected %s", got, opening)
	}
}

// Когда ставка не настроена, ничего не берётся, поэтому выкатывание фичи не
// начинает тихо ужимать выплаты.
func TestConfirmOrderTakesNoCommissionWhenRateIsUnset(t *testing.T) {
	txRepo, accounts, executorID, _ := confirmWithCommission(t, "")

	if got := accounts.balances[repository.AccountCommission]; !got.IsZero() {
		t.Errorf("commission account = %s, expected nothing collected", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != money.FromRubles(100) {
		t.Errorf("executor was rewarded %s, expected the whole payment", got)
	}
}

// Ставка выше 100% иначе выплачивала бы отрицательное вознаграждение и брала
// деньги, которых эскроу не держит. Она ужимается, поэтому худший случай — ноль.
func TestCommissionNeverExceedsThePayment(t *testing.T) {
	txRepo, accounts, executorID, opening := confirmWithCommission(t, "150")

	if got := accounts.balances[repository.AccountCommission]; got != money.FromRubles(100) {
		t.Errorf("commission account = %s, expected the whole payment", got)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); !got.IsZero() {
		t.Errorf("executor was rewarded %s, expected nothing", got)
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("confirming changed the books total: %s, expected %s", got, opening)
	}
}

// Дробная доля всё равно ложится на целые копейки, а остаток округления
// остаётся у исполнителя, а не исчезает.
func TestCommissionRoundingLeavesNothingBehind(t *testing.T) {
	txRepo, accounts, executorID, _ := confirmWithCommission(t, "3.333")

	price := money.FromRubles(100)
	commission := accounts.balances[repository.AccountCommission]
	reward := txRepo.balances[executorID].Sub(mockDefaultBalance)

	if commission.Add(reward) != price {
		t.Errorf("commission %s + reward %s does not add up to %s", commission, reward, price)
	}
	if commission != money.FromKopecks(333) {
		t.Errorf("commission = %s, expected 3.33", commission)
	}
}

// newCommissionAdmin собирает админский сервис поверх книг, где уже лежит
// какая-то собранная комиссия.
func newCommissionAdmin(collected money.Amount) (*AdminService, *mockTransactionRepo, *mockAccounts) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	// Деньги, собранные как комиссия, когда-то пришли извне — именно это держит
	// стартовые книги сошедшимися.
	accounts.balances[repository.AccountCommission] = collected
	accounts.balances[repository.AccountDeposits] = collected.Neg()

	srv := NewAdminService(newMockUserRepo(), &mockAdminRepo{requests: make(map[uuid.UUID]*repository.TopUpRequest)},
		&mockSettingsRepo{settings: map[string]string{SettingOrderCommissionPercent: "15"}}, "secret", nil).
		WithLedger(NewLedger(txRepo, accounts))
	return srv, txRepo, accounts
}

func TestPayoutCommissionDrainsTheAccountAndKeepsBooksClosed(t *testing.T) {
	collected := money.FromRubles(500)
	srv, txRepo, accounts := newCommissionAdmin(collected)
	opening := booksTotal(txRepo, accounts)

	result, err := srv.PayoutCommission(context.Background(), uuid.New(), money.FromRubles(200))
	if err != nil {
		t.Fatalf("payout: %v", err)
	}
	if want := money.FromRubles(300); result.Balance != want {
		t.Errorf("commission balance after payout = %s, expected %s", result.Balance, want)
	}
	if result.Percent != 15 {
		t.Errorf("reported rate = %v, expected 15", result.Percent)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("paying out changed the books total: %s, expected %s", got, opening)
	}
}

// Счёт нельзя овердрафтить: выплата ограничена тем, что реально собрано, и
// проверяется тем же охраняемым оператором, который двигает деньги.
func TestPayoutCommissionRefusesMoreThanCollected(t *testing.T) {
	collected := money.FromRubles(500)
	srv, _, accounts := newCommissionAdmin(collected)

	if _, err := srv.PayoutCommission(context.Background(), uuid.New(), money.FromRubles(501)); err == nil {
		t.Fatal("expected a payout larger than the balance to be refused")
	}
	if got := accounts.balances[repository.AccountCommission]; got != collected {
		t.Errorf("refused payout still moved money: account = %s, expected %s", got, collected)
	}

	if _, err := srv.PayoutCommission(context.Background(), uuid.New(), money.Zero); err == nil {
		t.Error("expected a zero payout to be refused")
	}
}

// Экран настроек — единственное место, где задаётся ставка, поэтому границы
// обязаны держаться там: доля свыше 100% выплачивала бы отрицательное вознаграждение.
func TestUpdateSettingsBoundsTheCommissionRate(t *testing.T) {
	settings := &mockSettingsRepo{settings: map[string]string{}}
	srv := NewAdminService(newMockUserRepo(), &mockAdminRepo{}, settings, "secret", nil)
	ctx := context.Background()

	for _, bad := range []string{"-1", "101", "abc"} {
		if err := srv.UpdateSettings(ctx, map[string]string{SettingOrderCommissionPercent: bad}); err == nil {
			t.Errorf("expected rate %q to be rejected", bad)
		}
	}
	if err := srv.UpdateSettings(ctx, map[string]string{SettingOrderCommissionPercent: "12.5"}); err != nil {
		t.Errorf("expected rate 12.5 to be accepted, got %v", err)
	}
	if got := settings.settings[SettingOrderCommissionPercent]; got != "12.5" {
		t.Errorf("stored rate = %q, expected 12.5", got)
	}
}

// Страховка от того, что путь выплаты по ошибке подключат к неохраняемому
// Settle: ошибка, которую видит вызывающий при овердрафте, обязана быть о
// нехватке средств, потому что именно её обработчик превращает в 400, а не в 500.
func TestPayoutCommissionOverdrawReportsInsufficientFunds(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	ledger := NewLedger(txRepo, accounts)

	err := ledger.RunInTx(context.Background(), func(tx *sql.Tx) error {
		return ledger.Payout(context.Background(), tx, repository.AccountCommission, uuid.New(),
			money.FromRubles(1), repository.TransactionTypeCommissionPayout, nil)
	})
	if !errors.Is(err, repository.ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds, got %v", err)
	}
}
