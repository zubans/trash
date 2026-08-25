// Package money represents amounts as whole kopecks.
//
// Money used to be float64 all the way through the Go side while the database
// stored NUMERIC(18,2). Postgres was never the problem — the drift came from
// arithmetic done before the value reached it. Multiplying a base price by a
// tariff coefficient, or halving a hold to compute a penalty, produced values
// like 800.0000000000001 that the column then silently rounded, and the error
// accumulated across refunds and partial payments.
//
// An Amount is an exact integer count of kopecks. It crosses the database
// boundary as NUMERIC through Scan/Value, so queries did not have to change,
// and it crosses the API boundary as a number in rubles, so clients did not
// either.
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

// Amount is a signed number of kopecks. A balance may legitimately be negative:
// executors go into the red through fines.
type Amount int64

// Zero is the empty amount.
const Zero Amount = 0

// kopecksPerRuble is the scale of the NUMERIC(18,2) columns behind these values.
const kopecksPerRuble = 100

// FromKopecks builds an Amount from a whole number of kopecks.
func FromKopecks(k int64) Amount { return Amount(k) }

// FromRubles converts a rubles value to kopecks, rounding half away from zero.
// Use it only at the edges, where a float is what arrived; never for arithmetic.
func FromRubles(v float64) Amount {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return Amount(math.Round(v * kopecksPerRuble))
}

// ParseRubles reads a decimal string such as "1500.25". It is exact: the digits
// are read directly rather than going through a float.
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
	// Only digits from here on: without this a second sign character ("--5")
	// would be swallowed by ParseInt and flip the result back to positive.
	if !isDigits(whole) || (frac != "" && !isDigits(frac)) {
		return 0, fmt.Errorf("invalid amount %q", s)
	}

	// Pad or round the fractional part to exactly two digits.
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

// isDigits reports whether every byte is an ASCII digit.
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

// Kopecks returns the raw count.
func (a Amount) Kopecks() int64 { return int64(a) }

// Rubles renders the amount as a float. For display and for callers that still
// speak float; never feed the result back into arithmetic.
func (a Amount) Rubles() float64 { return float64(a) / kopecksPerRuble }

// String formats the amount with two decimals, without a currency sign.
func (a Amount) String() string {
	sign := ""
	v := int64(a)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/kopecksPerRuble, v%kopecksPerRuble)
}

// IsZero, IsPositive and IsNegative read better than comparisons at call sites.
func (a Amount) IsZero() bool     { return a == 0 }
func (a Amount) IsPositive() bool { return a > 0 }
func (a Amount) IsNegative() bool { return a < 0 }

// Add and Sub are exact.
func (a Amount) Add(b Amount) Amount { return a + b }
func (a Amount) Sub(b Amount) Amount { return a - b }

// Neg returns the amount with the opposite sign.
func (a Amount) Neg() Amount { return -a }

// Abs returns the magnitude.
func (a Amount) Abs() Amount {
	if a < 0 {
		return -a
	}
	return a
}

// Scale multiplies by a ratio — a tariff coefficient, a penalty share — and
// rounds half away from zero. Rounding happens once, here, instead of being
// spread across the call sites.
func (a Amount) Scale(ratio float64) Amount {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0
	}
	return Amount(math.Round(float64(a) * ratio))
}

// MarshalJSON emits rubles as a JSON number, which is the shape every client
// already expects: 150025 kopecks marshals as 1500.25.
func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(a.String()), nil
}

// UnmarshalJSON accepts a JSON number or a quoted decimal string, both in
// rubles. Strings are parsed exactly; numbers go through the float the client
// sent us, which is the best that can be done with what arrived.
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

// Value writes the amount as a decimal string, which Postgres reads into
// NUMERIC exactly. Because of this the SQL itself did not have to change.
func (a Amount) Value() (driver.Value, error) {
	return a.String(), nil
}

// Scan reads a NUMERIC column. lib/pq hands numerics over as []byte, so the
// digits are parsed directly and never pass through a float.
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
		// Only reachable through drivers that pre-convert; rounded immediately.
		*a = FromRubles(v)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into money.Amount", src)
	}
}
