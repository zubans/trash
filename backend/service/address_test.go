package service

import "testing"

// TestComposeKeepsBuildingsTheOldRegexRejected is the point of the change: the
// previous format check accepted only a purely numeric house number, so a
// корпус or a строение — ordinary Russian addresses — could be chosen from the
// suggestion list and then refused by the form.
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

// TestWithFlatRebuildsTheLine covers joining the apartment onto an address the
// provider returned for a building — which is what happens whenever a person
// picks a house from the list and then types their flat.
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

	// Clearing the flat removes it from the line again.
	if cleared := withFlat.WithFlat(""); cleared.Flat != "" ||
		cleared.Value != "г Москва, ул Тверская, д. 7" {
		t.Errorf("clearing the flat left %+v", cleared)
	}
}

// TestOnlyAddressesWithAHouseAreDeliverable: a suggestion that stops at the
// street is useful while typing and useless as a pickup address.
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

// TestLegacyAddressesKeepTheirParts covers what happens to the addresses already
// stored as one line: they have to survive the move to structured fields,
// including the flat that registration used to append without a comma.
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

// TestComposeFallsBackToStoredText: an address with nothing structured left
// must still display, not turn into an empty line.
func TestComposeFallsBackToStoredText(t *testing.T) {
	addr := Address{Value: "Свободный текст, введённый вручную"}
	if got := addr.Compose(); got != "Свободный текст, введённый вручную" {
		t.Errorf("expected the stored text back, got %q", got)
	}
}

// TestComposeHouseKeepsCorpusAndBuilding covers the provider-side assembly of a
// building identifier from its separate fields.
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
