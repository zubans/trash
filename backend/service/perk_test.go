package service

import (
	"errors"
	"testing"
)

func f(v float64) *float64 { return &v }

// Формула применяется после уровня и зажата в [0, base] — по каждому виду и на
// границах.
func TestApplyPerk(t *testing.T) {
	cases := []struct {
		name         string
		levelPercent float64
		base         float64
		kind         string
		value        *float64
		want         float64
		wantOK       bool
	}{
		// Множитель: обещание «вдвое» держится при любой базе.
		{"multiplier halves the level rate", 7, 10, PerkKindCommissionMultiplier, f(0.5), 3.5, true},
		{"multiplier after base rate grew", 15, 15, PerkKindCommissionMultiplier, f(0.5), 7.5, true},
		{"multiplier of zero rate is zero", 0, 10, PerkKindCommissionMultiplier, f(0.5), 0, true},
		{"multiplier at zero base is zero", 0, 0, PerkKindCommissionMultiplier, f(0.5), 0, true},
		{"multiplier one keeps the rate", 7, 10, PerkKindCommissionMultiplier, f(1), 7, true},
		// Вычитание пунктов: предсказуемая цифра, но не ниже нуля.
		{"discount subtracts points", 7, 10, PerkKindCommissionDiscountPP, f(5), 2, true},
		{"discount larger than the rate clamps to zero", 3, 10, PerkKindCommissionDiscountPP, f(5), 0, true},
		{"discount at zero base clamps to zero", 0, 0, PerkKindCommissionDiscountPP, f(5), 0, true},
		// Беспроцентный период.
		{"free zeroes any rate", 7, 10, PerkKindCommissionFree, nil, 0, true},
		{"free zeroes the full base too", 10, 10, PerkKindCommissionFree, nil, 0, true},
		// То, что не применяется: неизвестный вид и чужие значения. Ставка
		// возвращается неизменной, а ok=false — повод для денежного инцидента.
		{"unknown kind is not applied", 7, 10, "COMISSION_MULTIPLIER", f(0.5), 7, false},
		{"zero multiplier is not applied", 7, 10, PerkKindCommissionMultiplier, f(0), 7, false},
		{"multiplier above one is not applied", 7, 10, PerkKindCommissionMultiplier, f(1.5), 7, false},
		{"missing multiplier is not applied", 7, 10, PerkKindCommissionMultiplier, nil, 7, false},
		{"negative points are not applied", 7, 10, PerkKindCommissionDiscountPP, f(-1), 7, false},
		{"zero points are not applied", 7, 10, PerkKindCommissionDiscountPP, f(0), 7, false},
		{"a value on the free kind is not applied", 7, 10, PerkKindCommissionFree, f(0.5), 7, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ApplyPerk(tc.levelPercent, tc.base, tc.kind, tc.value)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, expected %v", ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("percent = %v, expected %v", got, tc.want)
			}
		})
	}
}

// Один валидатор на админку, ручную выдачу и покупку: каждое запрещённое
// сочетание отклоняется, каждое разрешённое проходит.
func TestValidatePerk(t *testing.T) {
	valid := []struct {
		name  string
		kind  string
		value *float64
		days  int
	}{
		{"multiplier", PerkKindCommissionMultiplier, f(0.5), 30},
		{"discount points", PerkKindCommissionDiscountPP, f(5), 30},
		{"free day", PerkKindCommissionFree, nil, 1},
		{"free week", PerkKindCommissionFree, nil, 7},
	}
	for _, tc := range valid {
		t.Run("valid "+tc.name, func(t *testing.T) {
			if err := ValidatePerk(tc.kind, tc.value, tc.days); err != nil {
				t.Errorf("expected valid, got %v", err)
			}
		})
	}

	invalid := []struct {
		name  string
		kind  string
		value *float64
		days  int
	}{
		{"unknown kind", "HALF_OFF", f(0.5), 30},
		{"multiplier zero", PerkKindCommissionMultiplier, f(0), 30},
		{"multiplier above one", PerkKindCommissionMultiplier, f(1.5), 30},
		{"multiplier missing", PerkKindCommissionMultiplier, nil, 30},
		{"points negative", PerkKindCommissionDiscountPP, f(-1), 30},
		{"points zero", PerkKindCommissionDiscountPP, f(0), 30},
		{"points missing", PerkKindCommissionDiscountPP, nil, 30},
		{"free with a value", PerkKindCommissionFree, f(0.5), 1},
		// Бессрочной привилегии нет: разовая цена за вечную половину комиссии —
		// это продажа доли выручки платформы.
		{"no days", PerkKindCommissionMultiplier, f(0.5), 0},
		{"negative days", PerkKindCommissionFree, nil, -7},
	}
	for _, tc := range invalid {
		t.Run("invalid "+tc.name, func(t *testing.T) {
			if err := ValidatePerk(tc.kind, tc.value, tc.days); !errors.Is(err, ErrInvalidPerk) {
				t.Errorf("expected ErrInvalidPerk, got %v", err)
			}
		})
	}
}
