package service

import (
	"context"
	"errors"
	"testing"
)

// TestComposeKeepsBuildingsTheOldRegexRejected — смысл этого изменения: прежняя
// проверка формата принимала только чисто числовой номер дома, поэтому корпус
// или строение — обычные российские адреса — можно было выбрать из списка
// подсказок и получить отказ формы.
func TestComposeKeepsBuildingsTheOldRegexRejected(t *testing.T) {
	cases := []struct {
		name string
		addr Address
		want string
	}{
		{
			name: "корпус",
			addr: Address{City: "г Москва", Street: "ул Тверская", House: "12 к. 1"},
			want: "г Москва, ул Тверская, д. 12 к. 1",
		},
		{
			name: "строение",
			addr: Address{City: "г Москва", Street: "ул Тверская", House: "10 стр. 2"},
			want: "г Москва, ул Тверская, д. 10 стр. 2",
		},
		{
			name: "буква в номере дома",
			addr: Address{City: "г Курск", Street: "ул Ленина", House: "5А"},
			want: "г Курск, ул Ленина, д. 5А",
		},
		{
			name: "с квартирой",
			addr: Address{City: "г Москва", Street: "ул Тверская", House: "7", Flat: "35"},
			want: "г Москва, ул Тверская, д. 7, кв. 35",
		},
		{
			name: "посёлок без улицы",
			addr: Address{City: "п Заречный", House: "3"},
			want: "п Заречный, д. 3",
		},
	}

	for _, c := range cases {
		if got := c.addr.Compose(); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, got)
		}
	}
}

// TestWithFlatRebuildsTheLine покрывает присоединение квартиры к адресу,
// который провайдер вернул для здания, — то есть то, что происходит всякий раз,
// когда человек выбирает дом из списка, а потом печатает свою квартиру.
func TestWithFlatRebuildsTheLine(t *testing.T) {
	building := Address{
		Value: "г Москва, ул Тверская, д 7", City: "г Москва", Street: "ул Тверская", House: "7",
	}

	withFlat := building.WithFlat(" 35 ")
	if withFlat.Flat != "35" {
		t.Errorf("the flat should be trimmed to \"35\", got %q", withFlat.Flat)
	}
	if want := "г Москва, ул Тверская, д. 7, кв. 35"; withFlat.Value != want {
		t.Errorf("expected %q, got %q", want, withFlat.Value)
	}
	if building.Flat != "" {
		t.Error("WithFlat must not modify the original address")
	}

	// Очистка квартиры снова убирает её из строки.
	if cleared := withFlat.WithFlat(""); cleared.Flat != "" ||
		cleared.Value != "г Москва, ул Тверская, д. 7" {
		t.Errorf("clearing the flat left %+v", cleared)
	}
}

// TestOnlyAddressesWithAHouseAreDeliverable: подсказка, заканчивающаяся улицей,
// полезна при наборе и бесполезна как адрес подачи.
func TestOnlyAddressesWithAHouseAreDeliverable(t *testing.T) {
	cases := map[string]struct {
		addr Address
		want bool
	}{
		"полный адрес":   {Address{City: "г Москва", Street: "ул Тверская", House: "7"}, true},
		"без дома":       {Address{City: "г Москва", Street: "ул Тверская"}, false},
		"только город":   {Address{City: "г Москва"}, false},
		"пустой":         {Address{}, false},
		"дом с корпусом": {Address{City: "г Москва", Street: "ул Тверская", House: "12 к. 1"}, true},
	}

	for name, c := range cases {
		if got := c.addr.IsDeliverable(); got != c.want {
			t.Errorf("%s: expected deliverable=%v, got %v", name, c.want, got)
		}
		if err := c.addr.Validate(); (err == nil) != c.want {
			t.Errorf("%s: Validate disagrees with IsDeliverable: %v", name, err)
		}
	}
}

// TestLegacyAddressesKeepTheirParts покрывает судьбу адресов, уже сохранённых
// одной строкой: они обязаны пережить переезд в структурные поля, включая
// квартиру, которую регистрация раньше дописывала без запятой.
func TestLegacyAddressesKeepTheirParts(t *testing.T) {
	cases := []struct {
		line         string
		city, street string
		house, flat  string
	}{
		{"Россия, Москва, Тверская улица, д. 7", "Москва", "Тверская улица", "7", ""},
		{"Россия, Москва, Тверская улица, д. 7 кв. 35", "Москва", "Тверская улица", "7", "35"},
		{"Россия, Курск, улица Ленина, д. 12, кв. 4", "Курск", "улица Ленина", "12", "4"},
	}

	for _, c := range cases {
		got := parseLegacyCanonical(c.line)
		if got.City != c.city || got.Street != c.street || got.House != c.house || got.Flat != c.flat {
			t.Errorf("%q parsed as city=%q street=%q house=%q flat=%q",
				c.line, got.City, got.Street, got.House, got.Flat)
		}
	}
}

// TestComposeFallsBackToStoredText: адрес, у которого не осталось ничего
// структурного, всё равно обязан отображаться, а не превращаться в пустую строку.
func TestComposeFallsBackToStoredText(t *testing.T) {
	addr := Address{Value: "Свободный текст, введённый вручную"}
	if got := addr.Compose(); got != "Свободный текст, введённый вручную" {
		t.Errorf("expected the stored text back, got %q", got)
	}
}

// TestComposeHouseKeepsCorpusAndBuilding покрывает сборку идентификатора здания
// из отдельных полей на стороне провайдера.
func TestComposeHouseKeepsCorpusAndBuilding(t *testing.T) {
	cases := []struct {
		house, houseType, block, blockType string
		want                               string
	}{
		{"12", "д", "1", "к", "12 к. 1"},
		{"7", "д", "", "", "7"},
		{"5", "влд", "", "", "влд. 5"},
		{"10", "д", "2", "стр", "10 стр. 2"},
		{"3", "д", "1", "", "3 к. 1"},
		{"", "д", "1", "к", ""},
	}

	for _, c := range cases {
		if got := composeHouse(c.house, c.houseType, c.block, c.blockType); got != c.want {
			t.Errorf("composeHouse(%q,%q,%q,%q) = %q, want %q",
				c.house, c.houseType, c.block, c.blockType, got, c.want)
		}
	}
}

// TestSuggestionsFailLoudlyWithoutAProvider: запасного варианта по замыслу нет,
// поэтому установка без ключа обязана сказать, что не умеет подсказывать
// адреса, а не вернуть пустой список — пользователь прочтёт это как «вашей улицы нет».
func TestSuggestionsFailLoudlyWithoutAProvider(t *testing.T) {
	suggester := NewAddressSuggester(nil, nil)

	if suggester.Configured() {
		t.Error("a suggester with no provider must not report itself configured")
	}
	if _, err := suggester.Suggest(context.Background(), "Тверская", 5); !errors.Is(err, ErrNoAddressProvider) {
		t.Errorf("expected ErrNoAddressProvider, got %v", err)
	}
	if _, err := suggester.LegacySuggest(context.Background(), "Тверская"); !errors.Is(err, ErrNoAddressProvider) {
		t.Errorf("expected ErrNoAddressProvider from the legacy path too, got %v", err)
	}
}

// TestNewDaDataNeedsAKey покрывает переключатель, решающий, доступны ли
// подсказки вообще.
func TestNewDaDataNeedsAKey(t *testing.T) {
	t.Setenv("DADATA_API_KEY", "")
	if NewDaData() != nil {
		t.Error("no key must yield no provider")
	}

	t.Setenv("DADATA_API_KEY", "  ")
	if NewDaData() != nil {
		t.Error("a blank key must yield no provider")
	}

	t.Setenv("DADATA_API_KEY", "test-key")
	if NewDaData() == nil {
		t.Error("a key must yield a provider")
	}
}
