package perk_test

import (
	"errors"
	"math"
	"testing"

	"healthlogin/backend/perk"
	"healthlogin/backend/perks"
)

func shipped(t *testing.T) *perk.Engine {
	t.Helper()
	e := perk.New(perk.DefaultLimits)
	codes, err := perk.ShippedCodes(perks.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range codes {
		files, err := perk.ReadShipped(perks.FS, code)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.CompileFiles(code, files); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
	}
	return e
}

// Поставляемые правила считают ровно то, что считала формула в Go до них:
// уже купленные привилегии после перехода на скрипты ведут себя как раньше.
func TestShippedRulesMatchTheOldFormula(t *testing.T) {
	e := shipped(t)
	old := map[string]func(level, value float64) float64{
		"commission_multiplier":  func(level, value float64) float64 { return level * value },
		"commission_discount_pp": func(level, value float64) float64 { return math.Max(0, level-value) },
		"commission_free":        func(level, value float64) float64 { return 0 },
	}
	values := map[string][]float64{
		"commission_multiplier":  {0.1, 0.5, 1},
		"commission_discount_pp": {0.5, 5, 30},
		"commission_free":        {0},
	}
	for code, formula := range old {
		m, _ := e.Manifest(code)
		for _, value := range values[code] {
			cfg := map[string]interface{}{}
			if _, ok := m.Defaults["VALUE"]; ok {
				cfg["VALUE"] = value
			}
			merged, err := perk.Config(m.Defaults, cfg)
			if err != nil {
				t.Fatalf("%s: %v", code, err)
			}
			for _, level := range []float64{0, 3.5, 7, 10, 20} {
				got, err := e.Rate(code, perk.Facts{Base: 20, LevelPercent: level, Config: merged})
				if err != nil {
					t.Fatalf("%s: %v", code, err)
				}
				if want := formula(level, value); math.Abs(got-want) > 1e-9 {
					t.Errorf("%s value=%g level=%g: got %g, want %g", code, value, level, got, want)
				}
			}
		}
	}
}

func compile(t *testing.T, src string) *perk.Engine {
	t.Helper()
	e := perk.New(perk.DefaultLimits)
	if err := e.CompileFiles("rule", []perk.SourceFile{{Name: "rule.star", Src: []byte(src)}}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return e
}

// Проверка по сетке не пускает в продажу правило, ответ которого ядро не
// сможет применить.
func TestGridRejectsBadRules(t *testing.T) {
	cases := map[string]string{
		"string":   `MANIFEST = {"defaults": {}}` + "\ndef rate(f):\n    return \"half\"\n",
		"negative": `MANIFEST = {"defaults": {}}` + "\ndef rate(f):\n    return f.level_percent - 100\n",
		"above":    `MANIFEST = {"defaults": {}}` + "\ndef rate(f):\n    return f.base + 1\n",
		"loop":     `MANIFEST = {"defaults": {}}` + "\ndef rate(f):\n    n = 0\n    for i in range(1000000):\n        n += i\n    return 0\n",
		"fails":    `MANIFEST = {"defaults": {}}` + "\ndef rate(f):\n    return 1 // 0\n",
	}
	for name, src := range cases {
		e := compile(t, src)
		if _, err := e.Check("rule", map[string]interface{}{}, 1); !errors.Is(err, perk.ErrGrid) {
			t.Errorf("%s: expected ErrGrid, got %v", name, err)
		}
	}

	// Константы товара проверяются той же сеткой: «вдвое больше» не пройдёт.
	e := shipped(t)
	m, _ := e.Manifest("commission_multiplier")
	cfg, _ := perk.Config(m.Defaults, map[string]interface{}{"VALUE": 1.5})
	if _, err := e.Check("commission_multiplier", cfg, 1); !errors.Is(err, perk.ErrGrid) {
		t.Errorf("multiplier 1.5: expected ErrGrid, got %v", err)
	}
	cfg, _ = perk.Config(m.Defaults, map[string]interface{}{"VALUE": 0.5})
	rows, err := e.Check("commission_multiplier", cfg, 1)
	if err != nil || len(rows) == 0 {
		t.Fatalf("multiplier 0.5: rows=%d err=%v", len(rows), err)
	}
}

func TestRuleWithoutRateIsRejected(t *testing.T) {
	e := perk.New(perk.DefaultLimits)
	err := e.CompileFiles("rule", []perk.SourceFile{{Name: "rule.star", Src: []byte(`MANIFEST = {}`)}})
	if err == nil || e.Has("rule") {
		t.Fatalf("a rule without rate compiled: %v", err)
	}
}

func TestConfigRejectsUnknownConstant(t *testing.T) {
	if _, err := perk.Config(map[string]interface{}{"VALUE": 0.5}, map[string]interface{}{"VALEU": 0.4}); err == nil {
		t.Fatal("a misspelt constant was accepted")
	}
}
