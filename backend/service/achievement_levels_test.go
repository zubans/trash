package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Уровни превращают баллы в комиссию, и это единственное место, где
// геймификация трогает деньги. Тесты ниже проверяют не «правильно ли считает
// формула», а границы: нельзя уйти ниже нуля, нельзя обойти зажим настройкой и
// нельзя выплатить исполнителю больше, чем заплатил заказчик.

// levelSettings — настройки, при которых уровни включены.
func levelSettings(base, perLevel, discountPP string) *orderMockSettingsRepo {
	return &orderMockSettingsRepo{settings: map[string]string{
		SettingOrderCommissionPercent:     base,
		SettingAchievementLevelPoints:     perLevel,
		SettingAchievementLevelDiscountPP: discountPP,
	}}
}

// pointsRepo — реестр баллов, которому важна только сумма.
type pointsRepo struct {
	repository.AchievementRepository
	points int
}

func (p *pointsRepo) ActivePoints(ctx context.Context, q repository.Querier, userID uuid.UUID) (int, error) {
	return p.points, nil
}

func TestLevelLowersCommissionByOnePointPerLevel(t *testing.T) {
	ctx := context.Background()
	// База 10%, 500 баллов на уровень, 1 п.п. за уровень.
	levels := NewLevels(&pointsRepo{points: 1500}, levelSettings("10", "500", "1"))

	level := levels.For(ctx, nil, uuid.New())
	if level.Level != 3 {
		t.Errorf("level = %d, want 3", level.Level)
	}
	if level.Percent != 7 {
		t.Errorf("percent = %v, want 7", level.Percent)
	}
	// Следующий уровень наступает на 2000 баллах — именно это и рисует полоса.
	if level.NextLevelPoints != 2000 {
		t.Errorf("next level at %d points, want 2000", level.NextLevelPoints)
	}
}

// Дойдя до нуля, комиссия там и остаётся: дальнейшие баллы дают значки, но не
// деньги. Отрицательная комиссия означала бы выплату исполнителю больше того,
// что заплатил заказчик, а таких денег эскроу по заказу не держит.
func TestCommissionStopsAtZeroHoweverManyPoints(t *testing.T) {
	ctx := context.Background()
	levels := NewLevels(&pointsRepo{points: 1_000_000}, levelSettings("10", "500", "1"))

	level := levels.For(ctx, nil, uuid.New())
	if level.Percent != 0 {
		t.Errorf("percent = %v, want 0", level.Percent)
	}
	if level.DiscountPP != 10 {
		t.Errorf("discount = %v points, want it clamped to the 10 point base", level.DiscountPP)
	}
	if level.NextLevelPoints != 0 {
		t.Errorf("next level = %d, want none once the commission is zero", level.NextLevelPoints)
	}
}

// Ставка берётся текущая, а не запомненная: подняв базу, платформа снова
// начинает что-то получать с исполнителей, доросших до нуля.
func TestRaisingTheBaseRateRevivesCommissionForMaxedOutExecutors(t *testing.T) {
	ctx := context.Background()
	maxed := &pointsRepo{points: 10_000}

	if percent := NewLevels(maxed, levelSettings("10", "500", "1")).For(ctx, nil, uuid.New()).Percent; percent != 0 {
		t.Fatalf("percent at base 10 = %v, want 0", percent)
	}
	if percent := NewLevels(maxed, levelSettings("25", "500", "1")).For(ctx, nil, uuid.New()).Percent; percent != 5 {
		t.Errorf("percent at base 25 = %v, want 5 (20 levels off a 25 point base)", percent)
	}
}

// Уровни, выключенные настройкой, оставляют всех на базовой ставке — то же
// поведение, что было до геймификации.
func TestZeroDiscountKeepsEverybodyOnTheBaseRate(t *testing.T) {
	ctx := context.Background()
	levels := NewLevels(&pointsRepo{points: 10_000}, levelSettings("12", "500", "0"))

	level := levels.For(ctx, nil, uuid.New())
	if level.Percent != 12 || level.Level != 0 {
		t.Errorf("level = %d at %v percent, want 0 at 12", level.Level, level.Percent)
	}
}

// Ошибка чтения баллов не должна давать скидку: исполнитель считается нулевого
// уровня, то есть платит полную комиссию. Ошибаться здесь можно только в
// сторону платформы.
func TestUnreadablePointsMeanNoDiscount(t *testing.T) {
	levels := NewLevels(&failingPoints{}, levelSettings("10", "500", "1"))
	if percent := levels.For(context.Background(), nil, uuid.New()).Percent; percent != 10 {
		t.Errorf("percent = %v, want the full 10 when points cannot be read", percent)
	}
}

type failingPoints struct {
	repository.AchievementRepository
}

func (f *failingPoints) ActivePoints(ctx context.Context, q repository.Querier, userID uuid.UUID) (int, error) {
	return 0, sql.ErrConnDone
}

// Сквозная проверка: заказ, закрытый исполнителем третьего уровня, оставляет
// платформе меньшую долю, отдаёт остаток исполнителю — и книги сходятся ровно
// так же, как без всяких уровней.
func TestConfirmOrderUsesTheExecutorLevelAndKeepsBooksSquare(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	orderRepo := &mockOrderRepo{}
	settings := levelSettings("15", "500", "1")

	ledger := NewLedger(txRepo, accounts)
	srv := NewOrderService(orderRepo, ledger, settings,
		newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil).
		WithAchievements(NewLevels(&pointsRepo{points: 2500}, settings), nil)

	ctx := context.Background()
	customerID, executorID := uuid.New(), uuid.New()
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

	// Пять уровней с базы 15% дают 10%: заказ в 100 рублей оставляет платформе
	// десять, а не пятнадцать.
	price := money.FromRubles(100)
	commission := money.FromRubles(10)
	if got := accounts.balances[repository.AccountCommission]; got != commission {
		t.Errorf("commission = %s, want %s at level 5", got, commission)
	}
	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != price.Sub(commission) {
		t.Errorf("executor got %s, want %s", got, price.Sub(commission))
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("books moved from %s to %s", opening, got)
	}
	// Ставка и уровень сохранены в заказе: иначе разницу между двумя
	// одинаковыми заказами объяснить нечем.
	if orderRepo.commissionPercent[order.ID] != 10 || orderRepo.commissionLevel[order.ID] != 5 {
		t.Errorf("order recorded %v%% at level %d, want 10%% at level 5",
			orderRepo.commissionPercent[order.ID], orderRepo.commissionLevel[order.ID])
	}
}

// Зажим в реестре: комиссия, посчитанная неверно, не может увести
// вознаграждение выше уплаченного заказчиком. Движение всё равно происходит —
// по зажатым числам, — а расхождение фиксируется инцидентом.
func TestSettleOrderClampsAndRecordsAnIncident(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	incidents := &recordingIncidents{}
	ledger := NewLedger(txRepo, accounts).WithIncidents(incidents)

	ctx := context.Background()
	customerID, executorID := uuid.New(), uuid.New()
	if _, err := txRepo.GetBalance(ctx, customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if _, err := txRepo.GetBalance(ctx, executorID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	hold := money.FromRubles(100)
	if err := accounts.Credit(ctx, nil, repository.AccountEscrow, hold); err != nil {
		t.Fatalf("fund escrow: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	orderID := uuid.New()
	// Отрицательная комиссия — это то, как выглядела бы ошибка в расчёте ставки:
	// вознаграждение стало бы больше того, что заплатил заказчик.
	err := ledger.SettleOrder(ctx, nil, OrderSettlement{
		OrderID: orderID, CustomerID: customerID, ExecutorID: executorID,
		Hold: hold, Paid: hold, Commission: money.FromRubles(-40),
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}

	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != hold {
		t.Errorf("executor got %s, want the %s the customer paid and no more", got, hold)
	}
	if got := accounts.balances[repository.AccountEscrow]; !got.IsZero() {
		t.Errorf("escrow should drain to zero, holds %s", got)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("books moved from %s to %s", opening, got)
	}
	if len(incidents.recorded) != 1 {
		t.Fatalf("recorded %d incidents, want exactly one", len(incidents.recorded))
	}
	incident := incidents.recorded[0]
	if incident.Kind != repository.IncidentCommissionOutOfRange {
		t.Errorf("incident kind = %s, want %s", incident.Kind, repository.IncidentCommissionOutOfRange)
	}
	if incident.Applied == nil || !incident.Applied.IsZero() {
		t.Errorf("incident applied = %v, want the clamped zero", incident.Applied)
	}
}

// Заплатить из эскроу больше, чем он держит по заказу, — значит взять чужие
// удержания: эскроу общий на всю платформу.
func TestSettleOrderNeverPaysMoreThanEscrowHolds(t *testing.T) {
	txRepo := &mockTransactionRepo{}
	accounts := newMockAccounts()
	incidents := &recordingIncidents{}
	ledger := NewLedger(txRepo, accounts).WithIncidents(incidents)

	ctx := context.Background()
	customerID, executorID := uuid.New(), uuid.New()
	if _, err := txRepo.GetBalance(ctx, customerID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	if _, err := txRepo.GetBalance(ctx, executorID); err != nil {
		t.Fatalf("balance: %v", err)
	}
	hold := money.FromRubles(50)
	if err := accounts.Credit(ctx, nil, repository.AccountEscrow, hold); err != nil {
		t.Fatalf("fund escrow: %v", err)
	}
	opening := booksTotal(txRepo, accounts)

	if err := ledger.SettleOrder(ctx, nil, OrderSettlement{
		OrderID: uuid.New(), CustomerID: customerID, ExecutorID: executorID,
		Hold: hold, Paid: money.FromRubles(500), Commission: money.Zero,
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != hold {
		t.Errorf("executor got %s, want at most the %s in escrow", got, hold)
	}
	if got := booksTotal(txRepo, accounts); got != opening {
		t.Errorf("books moved from %s to %s", opening, got)
	}
	if len(incidents.recorded) == 0 {
		t.Fatal("the clamp fired without recording an incident")
	}
	if incidents.recorded[0].Kind != repository.IncidentRewardExceedsPayment {
		t.Errorf("incident kind = %s, want %s", incidents.recorded[0].Kind, repository.IncidentRewardExceedsPayment)
	}
}

// recordingIncidents запоминает то, что записал зажим.
type recordingIncidents struct {
	recorded []repository.MoneyIncident
}

func (r *recordingIncidents) Record(ctx context.Context, q repository.Querier, incident *repository.MoneyIncident) error {
	r.recorded = append(r.recorded, *incident)
	return nil
}

func (r *recordingIncidents) ListOpen(ctx context.Context, limit int) ([]*repository.MoneyIncident, error) {
	return nil, nil
}
func (r *recordingIncidents) List(ctx context.Context, limit int) ([]*repository.MoneyIncident, error) {
	return nil, nil
}
func (r *recordingIncidents) Resolve(ctx context.Context, id, adminID uuid.UUID, resolution string) error {
	return nil
}
func (r *recordingIncidents) CountOpen(ctx context.Context) (int, error) { return len(r.recorded), nil }

func TestExpiryIsNilForEternalGrants(t *testing.T) {
	if Expiry(time.Now(), 0) != nil {
		t.Error("a grant without a lifetime should never expire")
	}
	if got := Expiry(time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), 30); got == nil || got.Month() != time.October {
		t.Errorf("expiry = %v, want 30 days later", got)
	}
}
