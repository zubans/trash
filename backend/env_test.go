package main

import "testing"

// Запасной радиус читается один раз при старте: мусор, пусто и
// неположительное значение дают ноль, то есть умолчание сервиса.
func TestAcceptRadiusFromEnv(t *testing.T) {
	for in, want := range map[string]float64{"": 0, "abc": 0, "-1": 0, "0": 0, " 1.5 ": 1.5, "3": 3} {
		t.Setenv("ACCEPT_RADIUS_KM", in)
		if got := acceptRadiusFromEnv(); got != want {
			t.Errorf("ACCEPT_RADIUS_KM=%q: got %v, want %v", in, got, want)
		}
	}
}
