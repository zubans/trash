package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/behavior"
	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Привилегия магазина применяется в Levels.For — единственной точке, где
// определяется ставка исполнителя. Тесты ниже проверяют, что она идёт после
// уровня, что из нескольких берётся выгоднейшая, а не их композиция, и что
// подтверждение заказа записывает её в заказ.

// activePerks — хранилище привилегий, которому важен только список действующих.
type activePerks struct {
	repository.PerkRepository
	active []*repository.UserPerk
}

func (p *activePerks) Active(ctx context.Context, q repository.Querier, userID uuid.UUID, now time.Time) ([]*repository.UserPerk, error) {
	return p.active, nil
}

func userPerk(rule string, value *float64) *repository.UserPerk {
	now := time.Now()
	return &repository.UserPerk{
		ID: uuid.New(), UserID: uuid.New(), RuleCode: rule, Config: perkValue(value),
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(0, 0, 30),
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestPerkAppliesAfterTheLevel(t *testing.T) {
	ctx := context.Background()
	// База 10 %, уровень 3 снимает три пункта: ставка по уровню 7 %.
	settings := levelSettings("10", "500", "1")
	cases := []struct {
		name string
		perk *repository.UserPerk
		want float64
	}{
		{"multiplier halves the level rate", userPerk(ruleMultiplier, floatPtr(0.5)), 3.5},
		{"discount subtracts points from the level rate", userPerk(ruleDiscountPP, floatPtr(5)), 2},
		{"discount below zero clamps to zero", userPerk(ruleDiscountPP, floatPtr(9)), 0},
		{"free period zeroes the rate", userPerk(ruleFree, nil), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			levels := NewLevels(&pointsRepo{points: 1500}, settings).
				WithPerks(&activePerks{active: []*repository.UserPerk{tc.perk}}, newTestPerkRules(t, newFakePerkRules(), settings), nil)
			level := levels.For(ctx, nil, uuid.New())
			if level.LevelPercent != 7 {
				t.Fatalf("level rate = %v, want 7", level.LevelPercent)
			}
			if level.Percent != tc.want {
				t.Errorf("rate with perk = %v, want %v", level.Percent, tc.want)
			}
			if level.PerkID == nil || *level.PerkID != tc.perk.ID {
				t.Errorf("perk id = %v, want %s", level.PerkID, tc.perk.ID)
			}
		})
	}
}

// Две привилегии сразу бывают только по ошибке админа. Тогда берётся самая
// выгодная покупателю, а не «минус 5 пунктов от половины».
func TestTheBestOfSeveralPerksWinsInsteadOfComposing(t *testing.T) {
	multiplier := userPerk(ruleMultiplier, floatPtr(0.5))
	discount := userPerk(ruleDiscountPP, floatPtr(5))
	settings := levelSettings("10", "500", "1")
	levels := NewLevels(&pointsRepo{points: 1500}, settings).
		WithPerks(&activePerks{active: []*repository.UserPerk{multiplier, discount}}, newTestPerkRules(t, newFakePerkRules(), settings), nil)

	level := levels.For(context.Background(), nil, uuid.New())
	// 7 × 0.5 = 3.5, 7 − 5 = 2: выигрывают пункты. Композиция дала бы 0 или −1.5.
	if level.Percent != 2 {
		t.Errorf("rate = %v, want 2 (the discount alone)", level.Percent)
	}
	if level.PerkID == nil || *level.PerkID != discount.ID {
		t.Errorf("recorded perk %v, want the discount %s", level.PerkID, discount.ID)
	}
}

// Упавшее правило ставку не трогает — заказ закрывается по ставке уровня, — а
// инцидент его видит.
func TestAFailingPerkRuleIsIgnoredAndRecorded(t *testing.T) {
	incidents := &recordingIncidents{}
	// Собственное правило, чьей версии нет: так выглядит правка мимо приложения.
	broken := userPerk("vanished_rule", floatPtr(0.5))
	missing := uuid.New()
	broken.RuleVersionID = &missing
	settings := levelSettings("10", "500", "1")
	levels := NewLevels(&pointsRepo{points: 0}, settings).
		WithPerks(&activePerks{active: []*repository.UserPerk{broken}}, newTestPerkRules(t, newFakePerkRules(), settings), incidents)

	level := levels.For(context.Background(), nil, uuid.New())
	if level.Percent != 10 || level.PerkID != nil {
		t.Errorf("rate = %v with perk %v, want the plain 10%%", level.Percent, level.PerkID)
	}
	if len(incidents.recorded) != 1 || incidents.recorded[0].Kind != repository.IncidentPerkScriptFailed {
		t.Errorf("incidents = %+v, want one perk_script_failed", incidents.recorded)
	}
}

// Подтверждение заказа с действующей привилегией: платформа берёт меньше,
// исполнитель получает остаток, книги сходятся, а привилегия записана в заказ
// рядом со ставкой.
func TestConfirmOrderAppliesThePerkAndRecordsIt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		perk       *repository.UserPerk
		commission money.Amount
	}{
		// База 15 %, уровень 5: 10 % по уровню.
		{"half commission", userPerk(ruleMultiplier, floatPtr(0.5)), money.FromRubles(5)},
		{"minus five points", userPerk(ruleDiscountPP, floatPtr(5)), money.FromRubles(5)},
		// Нулевая комиссия: исполнитель получает всю сумму, COMMISSION не двигается.
		{"commission-free day", userPerk(ruleFree, nil), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			txRepo := &mockTransactionRepo{}
			accounts := newMockAccounts()
			orderRepo := &mockOrderRepo{}
			settings := levelSettings("15", "500", "1")
			ledger := NewLedger(txRepo, accounts)
			levels := NewLevels(&pointsRepo{points: 2500}, settings).
				WithPerks(&activePerks{active: []*repository.UserPerk{tc.perk}}, newTestPerkRules(t, newFakePerkRules(), settings), nil)
			srv := NewOrderService(orderRepo, ledger, settings,
				newMockUserRepo(), &orderMockShiftRepo{}, nil, newMockCatalogRepo(), nil).
				WithAchievements(levels, nil)

			ctx := context.Background()
			customerID, executorID := uuid.New(), uuid.New()
			_, _ = txRepo.GetBalance(ctx, customerID)
			_, _ = txRepo.GetBalance(ctx, executorID)
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

			price := money.FromRubles(100)
			if got := accounts.balances[repository.AccountCommission]; got != tc.commission {
				t.Errorf("commission = %s, want %s", got, tc.commission)
			}
			if got := txRepo.balances[executorID].Sub(mockDefaultBalance); got != price.Sub(tc.commission) {
				t.Errorf("executor got %s, want %s", got, price.Sub(tc.commission))
			}
			if got := booksTotal(txRepo, accounts); got != opening {
				t.Errorf("books moved from %s to %s", opening, got)
			}
			if got := orderRepo.commissionPerk[order.ID]; got == nil || *got != tc.perk.ID {
				t.Errorf("order recorded perk %v, want %s", got, tc.perk.ID)
			}
		})
	}
}

// Вознаграждения из BONUSES берут базовую ставку без уровня — и без
// привилегии, включая беспроцентный период: доля с них считается не через
// Levels, поэтому до неё привилегия не дотягивается вовсе.
func TestBonusCommissionIgnoresPerks(t *testing.T) {
	d := &BehaviorDispatcher{settings: levelSettings("10", "500", "1")}
	got := d.commissionOnBonus(context.Background(), behavior.Effect{Commission: true}, money.FromRubles(1000))
	if got != money.FromRubles(100) {
		t.Errorf("commission on a bonus = %s, want the base 10%% of 1000", got)
	}
}
