package service

import (
	"context"
	"errors"
	"testing"
)

type failingSettings struct{}

func (failingSettings) GetSettings(ctx context.Context) (map[string]string, error) {
	return nil, errors.New("db is down")
}

// Правило чтения настроек одно: отсутствующий, пустой или негодный ключ даёт
// умолчание; ноль и минус — значения. Здесь оно зафиксировано для всех типов.
func TestSettingsMapReadsOneRuleForAllTypes(t *testing.T) {
	m := settingsMap{
		"empty":    "",
		"garbage":  "abc",
		"zero":     "0",
		"negative": "-3",
		"float":    "2.5",
		"intish":   "3.0",
		"spaced":   " 7 ",
		"on":       "true",
		"off":      "0",
		"yes":      "YES",
		"maybe":    "perhaps",
	}

	t.Run("float", func(t *testing.T) {
		cases := []struct {
			key  string
			want float64
		}{
			{"missing", 1.5}, {"empty", 1.5}, {"garbage", 1.5},
			{"zero", 0}, {"negative", -3}, {"float", 2.5}, {"spaced", 7},
		}
		for _, c := range cases {
			if got := m.float(c.key, 1.5); got != c.want {
				t.Errorf("float(%q) = %v, want %v", c.key, got, c.want)
			}
		}
	})
	t.Run("int", func(t *testing.T) {
		cases := []struct {
			key  string
			want int
		}{
			{"missing", 4}, {"empty", 4}, {"garbage", 4},
			{"zero", 0}, {"negative", -3}, {"intish", 3}, {"float", 2}, {"spaced", 7},
		}
		for _, c := range cases {
			if got := m.int(c.key, 4); got != c.want {
				t.Errorf("int(%q) = %v, want %v", c.key, got, c.want)
			}
		}
	})
	t.Run("bool", func(t *testing.T) {
		cases := []struct {
			key  string
			def  bool
			want bool
		}{
			{"missing", true, true}, {"missing", false, false},
			{"empty", true, true}, {"maybe", false, false}, {"garbage", true, true},
			{"on", false, true}, {"yes", false, true}, {"off", true, false}, {"zero", true, false},
		}
		for _, c := range cases {
			if got := m.bool(c.key, c.def); got != c.want {
				t.Errorf("bool(%q, %v) = %v, want %v", c.key, c.def, got, c.want)
			}
		}
	})
	t.Run("positive treats zero and negative as unset", func(t *testing.T) {
		if got := m.positiveFloat("zero", 9); got != 9 {
			t.Errorf("positiveFloat(zero) = %v, want default 9", got)
		}
		if got := m.positiveFloat("negative", 9); got != 9 {
			t.Errorf("positiveFloat(negative) = %v, want default 9", got)
		}
		if got := m.positiveFloat("float", 9); got != 2.5 {
			t.Errorf("positiveFloat(float) = %v, want 2.5", got)
		}
		if got := m.positiveInt("negative", 9); got != 9 {
			t.Errorf("positiveInt(negative) = %v, want default 9", got)
		}
		if got := m.positiveInt("spaced", 9); got != 7 {
			t.Errorf("positiveInt(spaced) = %v, want 7", got)
		}
	})
}

// Без хранилища и при сбое чтения каждое чтение даёт умолчание, как если бы
// строки не было; сбой базы не превращается ни в ноль, ни в панику.
func TestSettingsReadersFallBackWithoutStore(t *testing.T) {
	ctx := context.Background()
	if got := settingFloat(ctx, nil, "k", 1.25); got != 1.25 {
		t.Errorf("nil repo: %v", got)
	}
	if got := settingInt(ctx, failingSettings{}, "k", 8); got != 8 {
		t.Errorf("failing repo: %v", got)
	}
	if got := settingBool(ctx, failingSettings{}, "k", true); !got {
		t.Error("failing repo: bool default lost")
	}
	if got := settingFloat(ctx, &orderMockSettingsRepo{settings: map[string]string{"k": "0.5"}}, "k", 1); got != 0.5 {
		t.Errorf("stored value: %v", got)
	}
}
