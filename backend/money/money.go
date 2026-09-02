// Package money представляет суммы целым числом копеек.
//
// Раньше деньги были float64 на всём протяжении Go-части, тогда как база
// хранила NUMERIC(18,2). Проблемой был не Postgres — расхождение приходило из
// арифметики, выполненной до того, как значение до него доберётся. Умножение
// базовой цены на тарифный коэффициент или деление удержания пополам ради
// штрафа давали значения вроде 800.0000000000001, которые колонка затем молча
// округляла, и ошибка накапливалась по возвратам и частичным выплатам.
//
// Amount — точное целое число копеек. Границу базы он пересекает как
// NUMERIC через Scan/Value, поэтому запросы менять не пришлось, а границу
// API — как число в рублях, поэтому и клиентов менять не пришлось
// тоже.
package money

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Amount — знаковое число копеек. Баланс законно может быть отрицательным:
// исполнители уходят в минус из-за штрафов.
type Amount int64

// Zero — пустая сумма.
const Zero Amount = 0

// kopecksPerRuble — масштаб колонок NUMERIC(18,2), стоящих за этими значениями.
const kopecksPerRuble = 100

// FromKopecks строит Amount из целого числа копеек.
func FromKopecks(k int64) Amount { return Amount(k) }

// FromRubles переводит значение в рублях в копейки, округляя половину от нуля.
// Используйте только на границах, куда пришёл float; никогда — для арифметики.
func FromRubles(v float64) Amount {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return Amount(math.Round(v * kopecksPerRuble))
}

// ParseRubles читает десятичную строку вроде "1500.25". Он точен: цифры
// читаются напрямую, а не через float.
func ParseRubles(s string) (Amount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty amount")
	}

	negative := false
	switch s[0] {
	case '-':
		negative, s = true, s[1:]
	case '+':
		s = s[1:]
	}

	whole, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		whole, frac = s[:i], s[i+1:]
	}
	if whole == "" {
		whole = "0"
	}
	// Дальше только цифры: без этого второй знак ("--5") проглотил бы
	// ParseInt и вернул результату положительный знак.
	if !isDigits(whole) || (frac != "" && !isDigits(frac)) {
		return 0, fmt.Errorf("invalid amount %q", s)
	}

	// Дополняем или округляем дробную часть ровно до двух знаков.
	switch {
	case len(frac) < 2:
		frac += strings.Repeat("0", 2-len(frac))
	case len(frac) > 2:
		roundUp := frac[2] >= '5'
		frac = frac[:2]
		if roundUp {
			extra, err := strconv.ParseInt(frac, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid amount %q", s)
			}
			extra++
			if extra == kopecksPerRuble {
				w, err := strconv.ParseInt(whole, 10, 64)
				if err != nil {
					return 0, fmt.Errorf("invalid amount %q", s)
				}
				whole, frac = strconv.FormatInt(w+1, 10), "00"
			} else {
				frac = fmt.Sprintf("%02d", extra)
			}
		}
	}

	wholeVal, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	fracVal, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}

	total := wholeVal*kopecksPerRuble + fracVal
	if negative {
		total = -total
	}
	return Amount(total), nil
}

// isDigits сообщает, все ли байты — ASCII-цифры.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Kopecks возвращает сырое количество.
func (a Amount) Kopecks() int64 { return int64(a) }

// Rubles отдаёт сумму как float. Для отображения и для вызывающих, которые всё
// ещё говорят на float; никогда не подавайте результат обратно в арифметику.
func (a Amount) Rubles() float64 { return float64(a) / kopecksPerRuble }

// String форматирует сумму с двумя знаками после запятой, без знака валюты.
func (a Amount) String() string {
	sign := ""
	v := int64(a)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/kopecksPerRuble, v%kopecksPerRuble)
}

// IsZero, IsPositive и IsNegative читаются в местах вызова лучше сравнений.
func (a Amount) IsZero() bool     { return a == 0 }
func (a Amount) IsPositive() bool { return a > 0 }
func (a Amount) IsNegative() bool { return a < 0 }

// Add и Sub точны.
func (a Amount) Add(b Amount) Amount { return a + b }
func (a Amount) Sub(b Amount) Amount { return a - b }

// Neg возвращает сумму с противоположным знаком.
func (a Amount) Neg() Amount { return -a }

// Abs возвращает модуль.
func (a Amount) Abs() Amount {
	if a < 0 {
		return -a
	}
	return a
}

// Scale умножает на коэффициент — тарифный, долю штрафа — и округляет половину
// от нуля. Округление происходит один раз, здесь, а не размазывается по местам
// вызова.
func (a Amount) Scale(ratio float64) Amount {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0
	}
	return Amount(math.Round(float64(a) * ratio))
}

// MarshalJSON выдаёт рубли числом JSON — в том виде, какого уже ждёт любой
// клиент: 150025 копеек сериализуются как 1500.25.
func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(a.String()), nil
}

// UnmarshalJSON принимает число JSON или десятичную строку в кавычках, и то и
// другое в рублях. Строки разбираются точно; числа проходят через float,
// который прислал клиент, — лучшее, что можно сделать с тем, что пришло.
func (a *Amount) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" {
		*a = 0
		return nil
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		parsed, err := ParseRubles(s)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	}

	parsed, err := ParseRubles(text)
	if err != nil {
		return fmt.Errorf("invalid amount %s", text)
	}
	*a = parsed
	return nil
}

// Value пишет сумму десятичной строкой, которую Postgres точно читает в
// NUMERIC. Именно поэтому сам SQL менять не пришлось.
func (a Amount) Value() (driver.Value, error) {
	return a.String(), nil
}

// Scan читает колонку NUMERIC. lib/pq отдаёт numeric как []byte, поэтому цифры
// разбираются напрямую и никогда не проходят через float.
func (a *Amount) Scan(src interface{}) error {
	switch v := src.(type) {
	case nil:
		*a = 0
		return nil
	case []byte:
		parsed, err := ParseRubles(string(v))
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case string:
		parsed, err := ParseRubles(v)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case int64:
		*a = Amount(v * kopecksPerRuble)
		return nil
	case float64:
		// Достижимо только через драйверы, конвертирующие заранее; округляется сразу.
		*a = FromRubles(v)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into money.Amount", src)
	}
}
