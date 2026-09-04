package repository

import "testing"

// TestCompletedOrderSortsAreWhitelisted: ключ сортировки приходит из строки
// запроса и конкатенируется в ORDER BY, поэтому всё, чего нет в карте, обязано
// откатиться к умолчанию, а не дойти до SQL.
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

// TestDigitsOnly: телефон, набранный с маской отображения, обязан совпасть с
// сохранённым +79997454656, а запрос без цифр не должен превращаться в пустой
// шаблон, которому соответствует каждая строка.
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

// TestTransactionSortsAreWhitelisted: тот же контракт для журнала проводок —
// ключ сортировки приходит из query string и попадает в ORDER BY.
func TestTransactionSortsAreWhitelisted(t *testing.T) {
	for _, key := range []string{"t.created_at; DROP TABLE transactions", "(SELECT 1)", "user_phone", ""} {
		if _, ok := transactionSorts[key]; ok {
			t.Errorf("%q must not be an accepted sort key", key)
		}
	}
	for _, key := range []string{"created_at", "amount", "type", "user"} {
		if _, ok := transactionSorts[key]; !ok {
			t.Errorf("%q should be sortable", key)
		}
	}
}

// TestLedgerSignBacksTransactionDirection: направление в списке берётся из
// соглашения о знаках, а не выводится заново, поэтому каждый тип обязан там быть.
func TestLedgerSignBacksTransactionDirection(t *testing.T) {
	for _, tt := range []TransactionType{
		TransactionTypeTopUp, TransactionTypeHold, TransactionTypePayment,
		TransactionTypeReward, TransactionTypeFine, TransactionTypeRefund,
	} {
		if _, ok := LedgerSign(tt); !ok {
			t.Errorf("тип %q не объявлен в ledgerSigns", tt)
		}
	}
}
