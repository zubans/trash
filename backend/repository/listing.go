package repository

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Общие кусочки постраничных админских списков: период, поиск, границы страницы.
// Ими пользуются журнал проводок (transaction.go) и список заказов
// (admin_order.go); держать их в одном из этих файлов значило бы, что второй
// зависит от первого без причины.

// PageRequest — то, что клиент просит у списка: страницу и нужен ли ему общий
// счётчик. COUNT(*) по таблице проводок или заказов стоит столько же, сколько
// сама страница, а нужен он только раз — при первом показе списка. Поэтому
// счётчик считается по просьбе, а не на каждой странице.
type PageRequest struct {
	Limit  int
	Offset int
	// WithTotal — считать ли общее число подходящих строк. Без него total в
	// ответе равен нулю и не значит ничего.
	WithTotal bool
}

// periodPattern — единственная форма периода, которую принимают фильтры: YYYY-MM.
var periodPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// periodArgs дописывает условие «column попадает в месяц period» как диапазон
// column >= начало месяца AND column < начало следующего, а не как
// to_char(column, 'YYYY-MM') = period: диапазон использует индекс по колонке,
// а to_char вычислялся для каждой строки таблицы. Границы считает база через
// to_timestamp в часовом поясе сессии — в том же, в котором фасеты собирают
// список периодов через to_char, поэтому оба видят месяц одинаково.
//
// Период не по форме раньше просто не совпадал ни с одной строкой; здесь он
// делает то же самое, а не роняет запрос ошибкой разбора даты.
func periodArgs(where string, args []interface{}, column, period string) (string, []interface{}) {
	period = strings.TrimSpace(period)
	if period == "" {
		return where, args
	}
	if !periodPattern.MatchString(period) {
		return where + " AND FALSE", args
	}
	args = append(args, period)
	n := len(args)
	return where + fmt.Sprintf(
		" AND %s >= to_timestamp($%d, 'YYYY-MM') AND %s < to_timestamp($%d, 'YYYY-MM') + interval '1 month'",
		column, n, column, n), args
}

// digitsOnly оставляет от поискового запроса цифры, чтобы набранный телефон
// совпадал с сохранённым при любой пунктуации с обеих сторон.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// searchUUID разбирает поисковую строку как uuid. Идентификаторы ищутся
// точным совпадением: ILIKE по id::text не умел пользоваться индексом и читал
// таблицу целиком, а частичный uuid никто не набирает руками — его копируют.
func searchUUID(search string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(search))
	return id, err == nil
}

// clampPage приводит страницу к разумным границам: без верхнего предела один
// запрос мог бы попросить всю историю пользователя целиком.
func clampPage(limit, offset int) (int, int) {
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
