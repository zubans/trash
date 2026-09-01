package repository

import "testing"

// TestCompletedOrderSortsAreWhitelisted: the sort key arrives from the query
// string and is concatenated into ORDER BY, so anything outside the map has to
// fall back rather than reach SQL.
func TestCompletedOrderSortsAreWhitelisted(t *testing.T) {
	rejected := []string{
		"o.completed_at; DROP TABLE orders",
		"(SELECT password FROM users LIMIT 1)",
		"customer_phone",
		"",
	}
	for _, key := range rejected {
		if _, ok := completedOrderSorts[key]; ok {
			t.Errorf("%q must not be an accepted sort key", key)
		}
	}

	for _, key := range []string{"completed_at", "final_amount", "service", "customer", "executor"} {
		if _, ok := completedOrderSorts[key]; !ok {
			t.Errorf("%q should be sortable", key)
		}
	}
}

// TestDigitsOnly: a phone typed with the display mask has to match the stored
// +79997454656, and a term with no digits must not turn into an empty pattern
// that matches every row.
func TestDigitsOnly(t *testing.T) {
	cases := map[string]string{
		"+7 (999) 745-46-56": "79997454656",
		"9997":               "9997",
		"уборка":             "",
		"":                   "",
	}
	for in, want := range cases {
		if got := digitsOnly(in); got != want {
			t.Errorf("digitsOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
