package service

import (
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
