package money

import (
	"encoding/json"
	"testing"
)

func TestParseRubles(t *testing.T) {
	cases := map[string]Amount{
		"0":        0,
		"1500":     150000,
		"1500.25":  150025,
		"1500.2":   150020,
		"0.01":     1,
		"-42.50":   -4250,
		"+7.07":    707,
		" 12.34 ":  1234,
		".5":       50,
		"1500.256": 150026, // половина от нуля
		"1500.254": 150025,
		"9.999":    1000, // переносится в целую часть
	}
	for in, want := range cases {
		got, err := ParseRubles(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: expected %d kopecks, got %d", in, want, got)
		}
	}

	for _, bad := range []string{"", "abc", "1.2.3", "--5"} {
		if _, err := ParseRubles(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	for _, a := range []Amount{0, 1, 99, 100, 150025, -4250, -1} {
		parsed, err := ParseRubles(a.String())
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		if parsed != a {
			t.Errorf("%d kopecks formatted as %q parsed back as %d", a, a.String(), parsed)
		}
	}
}

// TestScaleIsExactWhereFloatWasNot фиксирует случай, найденный аудитом: базовая
// цена, умноженная на тарифный коэффициент, давала значение, которое колонка
// NUMERIC затем округляла, и ошибка накапливалась.
func TestScaleIsExactWhereFloatWasNot(t *testing.T) {
	base := Amount(10000) // 100.00

	if got := base.Scale(8); got != 80000 {
		t.Errorf("100.00 × 8 should be 800.00, got %s", got)
	}
	if got := base.Scale(0.5); got != 5000 {
		t.Errorf("100.00 × 0.5 should be 50.00, got %s", got)
	}
	// У трети рубля нет точного представления; округление определено и
	// происходит один раз.
	if got := Amount(100).Scale(1.0 / 3.0); got != 33 {
		t.Errorf("1.00 × 1/3 should round to 0.33, got %s", got)
	}

	// Путь через float дрейфует; целочисленный — нет.
	drifting := 0.0
	for i := 0; i < 10; i++ {
		drifting += 0.1
	}
	if drifting == 1.0 {
		t.Skip("float arithmetic is exact on this platform, nothing to contrast")
	}
	exact := Zero
	for i := 0; i < 10; i++ {
		exact = exact.Add(Amount(10))
	}
	if exact != 100 {
		t.Errorf("ten times 0.10 should be exactly 1.00, got %s", exact)
	}
}

func TestArithmetic(t *testing.T) {
	hold := Amount(80000)
	final := Amount(10000)

	if refund := hold.Sub(final); refund != 70000 {
		t.Errorf("expected a refund of 700.00, got %s", refund)
	}
	if got := final.Add(Amount(1)); got != 10001 {
		t.Errorf("expected 100.01, got %s", got)
	}
	if got := Amount(-4250).Abs(); got != 4250 {
		t.Errorf("expected 42.50, got %s", got)
	}
	if !Amount(-1).IsNegative() || !Amount(1).IsPositive() || !Zero.IsZero() {
		t.Error("sign helpers disagree with the values")
	}
}

// TestJSONStaysInRubles — причина, по которой формат обмена не изменился:
// установленный мобильный клиент должен и дальше видеть 1500.25, а не 150025.
func TestJSONStaysInRubles(t *testing.T) {
	payload, err := json.Marshal(map[string]Amount{"balance": 150025})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(payload) != `{"balance":1500.25}` {
		t.Errorf("unexpected payload: %s", payload)
	}

	var decoded struct {
		Amount Amount `json:"amount"`
	}
	for _, in := range []string{`{"amount":1500.25}`, `{"amount":"1500.25"}`, `{"amount":1500.256}`} {
		if err := json.Unmarshal([]byte(in), &decoded); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
	}
	if decoded.Amount != 150026 {
		t.Errorf("expected the last value to round to 150026 kopecks, got %d", decoded.Amount)
	}

	var zero struct {
		Amount Amount `json:"amount"`
	}
	if err := json.Unmarshal([]byte(`{"amount":null}`), &zero); err != nil {
		t.Fatalf("null: %v", err)
	}
	if !zero.Amount.IsZero() {
		t.Errorf("null should decode to zero, got %s", zero.Amount)
	}
}

// TestDatabaseRoundTrip покрывает пару Scan/Value, которая позволяет колонкам
// NUMERIC остаться как есть.
func TestDatabaseRoundTrip(t *testing.T) {
	for _, want := range []Amount{0, 1, 150025, -4250} {
		v, err := want.Value()
		if err != nil {
			t.Fatalf("value: %v", err)
		}

		var got Amount
		if err := got.Scan([]byte(v.(string))); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if got != want {
			t.Errorf("%d kopecks round-tripped as %d", want, got)
		}
	}

	var fromNil Amount
	if err := fromNil.Scan(nil); err != nil || !fromNil.IsZero() {
		t.Errorf("NULL should scan as zero, got %s (%v)", fromNil, err)
	}

	var bad Amount
	if err := bad.Scan(struct{}{}); err == nil {
		t.Error("expected an error scanning an unsupported type")
	}
}
