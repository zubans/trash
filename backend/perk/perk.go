// Package perk — правила привилегий магазина: скрипт решает, какой станет
// ставка комиссии, ядро зажимает ответ и решает всё остальное.
//
// Правило — чистая функция rate(f) над четырьмя числами: базовая ставка,
// ставка по уровню, уровень и константы. Оно не видит ни заказа, ни
// пользователя, не двигает деньги и не выбирает, чья привилегия действует
// (implementation_plan_delivery_passport.md §1.2).
//
// Скрипт компилируется под кодом версии правила, а не под кодом правила:
// версия неизменяема, поэтому скомпилированное можно держать сколько угодно
// долго, а купленная привилегия считается ровно тем текстом, который был
// продан.
package perk

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"sort"
	"time"

	"go.starlark.net/starlark"

	"healthlogin/backend/script"
)

// HookRate — единственный хук правила.
const HookRate = "rate"

type (
	// Limits ограничивают один вызов rate.
	Limits = script.Limits
	// SourceFile — один файл правила.
	SourceFile = script.SourceFile
	// Manifest — объявление правила: название, описание, константы по умолчанию.
	Manifest = script.Manifest
)

// DefaultLimits строже, чем у услуг: rate выполняется при подтверждении заказа
// и считает одно число.
var DefaultLimits = Limits{MaxSteps: 10_000, Timeout: 20 * time.Millisecond}

// Facts — всё, что правило знает о ставке.
type Facts struct {
	// Base — базовая ставка платформы, в процентах.
	Base float64
	// LevelPercent — ставка по уровню исполнителя, до привилегии.
	LevelPercent float64
	Level        int
	// Config — константы правила, слитые с настройками товара.
	Config map[string]interface{}
}

// Engine хранит скомпилированные версии правил.
type Engine struct {
	runtime *script.Engine
}

// New создаёт пустой движок.
func New(limits Limits) *Engine {
	return &Engine{runtime: script.New(limits, script.Options{Hooks: []string{HookRate}})}
}

// ReadShipped читает файлы поставляемого правила из каталога fsys.
func ReadShipped(fsys fs.FS, code string) ([]SourceFile, error) {
	return script.ReadDir(fsys, code)
}

// ShippedCodes перечисляет поставляемые правила — каталоги в корне fsys.
func ShippedCodes(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, entry := range entries {
		if entry.IsDir() {
			codes = append(codes, entry.Name())
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// CompileFiles компилирует версию правила под ключом key. Правило без rate
// ничего не считает и не регистрируется.
func (e *Engine) CompileFiles(key string, files []SourceFile) error {
	if err := e.runtime.CompileFiles(key, files); err != nil {
		return err
	}
	m, _ := e.runtime.Manifest(key)
	if !hasRate(m) {
		e.runtime.Remove(key)
		return errors.New("правило обязано определить функцию rate(f)")
	}
	return nil
}

// Validate компилирует кандидата, не регистрируя его, и возвращает его
// объявление — форме админки.
func (e *Engine) Validate(files []SourceFile) (Manifest, error) {
	probe := New(e.runtime.Limits())
	if err := probe.CompileFiles("candidate", files); err != nil {
		return Manifest{}, err
	}
	m, _ := probe.runtime.Manifest("candidate")
	return m, nil
}

// Has сообщает, скомпилирована ли версия.
func (e *Engine) Has(key string) bool { return e.runtime.Has(key) }

// Manifest возвращает объявление скомпилированной версии.
func (e *Engine) Manifest(key string) (Manifest, bool) { return e.runtime.Manifest(key) }

func hasRate(m Manifest) bool {
	for _, h := range m.Hooks {
		if h == HookRate {
			return true
		}
	}
	return false
}

// Config сливает константы товара с константами правила по умолчанию. Ключ,
// которого правило не объявило, — ошибка: опечатка в имени константы иначе
// молча продала бы правило с умолчанием.
func Config(defaults, override map[string]interface{}) (map[string]interface{}, error) {
	merged := make(map[string]interface{}, len(defaults))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range override {
		if _, ok := defaults[k]; !ok {
			return nil, fmt.Errorf("у правила нет константы %s", k)
		}
		merged[k] = v
	}
	return merged, nil
}

// Rate вызывает rate скомпилированной версии и возвращает ответ как есть, без
// зажима: зажимает вызывающий, а проверка по сетке ловит ответы вне границ до
// того, как правило попадёт в продажу.
func (e *Engine) Rate(key string, f Facts) (float64, error) {
	facts := script.Struct("facts", starlark.StringDict{
		"base":          starlark.Float(f.Base),
		"level_percent": starlark.Float(f.LevelPercent),
		"level":         starlark.MakeInt(f.Level),
		"config":        script.Dict(f.Config),
	})
	v, err := e.runtime.CallHook(key, HookRate, facts)
	if err != nil {
		return 0, err
	}
	var out float64
	switch n := v.(type) {
	case starlark.Int:
		i, ok := n.Int64()
		if !ok {
			return 0, errors.New("rate вернул слишком большое число")
		}
		out = float64(i)
	case starlark.Float:
		out = float64(n)
	default:
		return 0, fmt.Errorf("rate обязан вернуть число, а вернул %s", typeName(v))
	}
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return 0, errors.New("rate вернул не число")
	}
	return out, nil
}

func typeName(v starlark.Value) string {
	if v == nil {
		return "ничего"
	}
	return v.Type()
}

// GridRow — один ответ правила на прогоне по сетке.
type GridRow struct {
	Base         float64 `json:"base"`
	Level        int     `json:"level"`
	LevelPercent float64 `json:"level_percent"`
	Percent      float64 `json:"percent"`
}

// Сетка проверки: базовые ставки и уровни, на которых правило обязано
// отвечать числом в [0, base] (implementation_plan_delivery_passport.md §1.3).
var (
	gridBases     = []float64{0, 5, 10, 20}
	gridMaxLevels = 20
)

// ErrGrid — правило ответило на сетке тем, что ядро применить не может.
var ErrGrid = errors.New("правило не прошло проверку по сетке")

// Check прогоняет версию правила по сетке с данными константами. discountPP —
// сколько пунктов снимает один уровень; при нуле уровни ставку не меняют, и
// проверяется только нулевой. Возвращает таблицу прогона — её показывает
// форма админки, — и ошибку, если хоть один ответ не годится.
func (e *Engine) Check(key string, config map[string]interface{}, discountPP float64) ([]GridRow, error) {
	var rows []GridRow
	for _, base := range gridBases {
		for level := 0; level <= gridMaxLevels; level++ {
			levelPercent := math.Max(0, base-float64(level)*discountPP)
			percent, err := e.Rate(key, Facts{Base: base, LevelPercent: levelPercent, Level: level, Config: config})
			if err != nil {
				return rows, fmt.Errorf("%w: база %g %%, уровень %d: %v", ErrGrid, base, level, err)
			}
			rows = append(rows, GridRow{Base: base, Level: level, LevelPercent: levelPercent, Percent: percent})
			// Маленький допуск на арифметику с плавающей точкой: 10 × 0.1 × 10
			// не обязано дать ровно 10.
			if percent < -1e-9 || percent > base+1e-9 {
				return rows, fmt.Errorf("%w: база %g %%, уровень %d: ставка %g %% вне [0, %g]", ErrGrid, base, level, percent, base)
			}
			if discountPP <= 0 || levelPercent == 0 {
				break
			}
		}
	}
	return rows, nil
}
