package service

import (
	"context"
	"testing"
	"time"

	"healthlogin/backend/money"
	"healthlogin/backend/repository"
)

// Возврат за идущую привилегию — пропорционально неиспользованным дням
// (оферта, п. 8.6). Начатый день считается использованным, поэтому границы —
// первый и последний день — проверяются отдельно.
func TestProportionalRefundAtTheBoundaries(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	perk := &repository.UserPerk{StartsAt: start, ExpiresAt: start.AddDate(0, 0, 30)}
	price := money.FromRubles(1000)
	for _, tc := range []struct {
		name string
		now  time.Time
		want money.Amount
	}{
		{"not started", start.Add(-time.Hour), price},
		{"the very start", start, price},
		{"first day", start.Add(time.Hour), money.FromKopecks(96666)},                        // 29/30
		{"day ten begun", start.Add(9*24*time.Hour + time.Minute), money.FromKopecks(66666)}, // 20/30
		{"last day", start.Add(29*24*time.Hour + time.Hour), 0},
		{"expired", start.AddDate(0, 0, 31), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := proportionalRefund(price, perk, tc.now); got != tc.want {
				t.Errorf("refund = %s, want %s", got, tc.want)
			}
		})
	}

	revoked := *perk
	at := start.Add(time.Hour)
	revoked.RevokedAt = &at
	if got := proportionalRefund(price, &revoked, start.Add(2*time.Hour)); got != 0 {
		t.Errorf("revoked perk refunds %s, want nothing", got)
	}
}

// Выключатель магазина и редакция оферты правятся на экране настроек, и
// опечатка там не должна ни открыть витрину, ни сделать каждую покупку
// «offer_changed».
func TestUpdateSettingsGuardsTheShopSettings(t *testing.T) {
	settings := &mockSettingsRepo{settings: map[string]string{}}
	srv := NewAdminService(newMockUserRepo(), &mockAdminRepo{}, settings, "secret", nil)
	ctx := context.Background()

	for key, bad := range map[string][]string{
		SettingShopEnabled:      {"2", "yes", ""},
		SettingShopOfferVersion: {"0", "-1", "1.5", "abc"},
	} {
		for _, value := range bad {
			if err := srv.UpdateSettings(ctx, map[string]string{key: value}); err == nil {
				t.Errorf("%s = %q accepted", key, value)
			}
		}
	}
	if err := srv.UpdateSettings(ctx, map[string]string{SettingShopEnabled: "1", SettingShopOfferVersion: "2"}); err != nil {
		t.Errorf("valid shop settings refused: %v", err)
	}
}
