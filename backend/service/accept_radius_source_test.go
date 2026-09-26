package service

import "testing"

// Радиус взятия берётся из настройки админки, без неё — из запасного значения,
// которое main.go читает из ACCEPT_RADIUS_KM, без него — из умолчания. Обзор
// карты не бывает уже зоны взятия, какой бы источник её ни задал.
func TestAcceptRadiusSourceOrder(t *testing.T) {
	cases := []struct {
		name       string
		settings   settingsMap
		fallbackKM float64
		want       float64
	}{
		{"setting wins", settingsMap{SettingAcceptRadiusKM: "2"}, 5, 2},
		{"fallback without setting", settingsMap{}, 5, 5},
		{"zero setting means unset", settingsMap{SettingAcceptRadiusKM: "0"}, 5, 5},
		{"default without both", settingsMap{}, 0, defaultAcceptRadiusKM},
		{"negative fallback ignored", settingsMap{}, -3, defaultAcceptRadiusKM},
	}
	for _, c := range cases {
		if got := acceptRadiusKM(c.settings, c.fallbackKM); got != c.want {
			t.Errorf("%s: acceptRadiusKM = %v, want %v", c.name, got, c.want)
		}
	}

	wide := defaultMapOverviewRadiusKM + 10
	if got := mapOverviewRadiusKM(settingsMap{}, wide); got != wide {
		t.Errorf("overview %v must not be narrower than accept radius %v", got, wide)
	}
}
