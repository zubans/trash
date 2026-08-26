package service

import (
	"fmt"
	"strings"
)

// Address is a postal address held as its parts rather than as one string.
//
// The previous design stored the address as text and recovered its components
// with a regular expression, on both the client and the server. That parse
// accepted only "Россия, Город, Улица, д. <digits>", so a building with a
// корпус or a строение ("12к1", "10 стр. 2"), or any suggestion that arrived
// without a house number, was rejected after the user had already picked it
// from the list. Keeping the parts means nothing has to be parsed back out.
type Address struct {
	// Value is what a person reads: the whole address on one line, apartment
	// included. It is derived from the parts, never parsed to recover them.
	Value string `json:"value"`

	Region string `json:"region,omitempty"`
	City   string `json:"city,omitempty"`
	Street string `json:"street,omitempty"`
	// House keeps whatever the provider gave, including корпус and строение.
	House string `json:"house,omitempty"`
	Flat  string `json:"flat,omitempty"`

	// FiasID identifies the address in the state address register. It is the
	// stable key: two spellings of one address share it.
	FiasID string `json:"fias_id,omitempty"`

	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`

	// Source records which provider produced this, so a support question about
	// a wrong address can be traced to where it came from.
	Source string `json:"source,omitempty"`
}

// Address sources.
const (
	SourceDaData     = "dadata"
	SourceNominatim  = "nominatim"
	SourceUserTyped  = "manual"
	SourceLegacyText = "legacy"
)

// IsDeliverable reports whether the address names a specific building. An
// address that stops at the street is fine to show while someone is typing but
// cannot be delivered to, so it must not be accepted as a pickup address.
func (a Address) IsDeliverable() bool {
	return strings.TrimSpace(a.City) != "" &&
		strings.TrimSpace(a.Street) != "" &&
		strings.TrimSpace(a.House) != ""
}

// HasCoordinates reports whether the address can take part in distance
// matching. Orders are matched to executors by coordinates, so an address
// without them is invisible to the dispatcher.
func (a Address) HasCoordinates() bool {
	return a.Lat != nil && a.Lon != nil
}

// WithFlat returns a copy carrying the apartment, with Value rebuilt to include
// it. Providers return the apartment separately when the user picks a building
// rather than a flat, and the two have to be joined somewhere.
func (a Address) WithFlat(flat string) Address {
	flat = strings.TrimSpace(flat)
	a.Flat = flat
	a.Value = a.Compose()
	return a
}

// Compose renders the one-line form from the parts. This is the only place a
// display string is built, so every screen shows the same address the same way.
func (a Address) Compose() string {
	parts := make([]string, 0, 5)
	for _, p := range []string{a.City, a.Street} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if house := strings.TrimSpace(a.House); house != "" {
		parts = append(parts, "д. "+house)
	}
	if flat := strings.TrimSpace(a.Flat); flat != "" {
		parts = append(parts, "кв. "+flat)
	}

	if len(parts) == 0 {
		// Nothing structured survived; fall back to whatever text we hold so a
		// legacy address is still shown rather than blanked out.
		return strings.TrimSpace(a.Value)
	}
	return strings.Join(parts, ", ")
}

// Validate reports why an address cannot be used for a pickup, in words a
// person can act on.
func (a Address) Validate() error {
	if strings.TrimSpace(a.Value) == "" && !a.IsDeliverable() {
		return fmt.Errorf("укажите адрес")
	}
	if !a.IsDeliverable() {
		return fmt.Errorf("выберите адрес с номером дома из списка подсказок")
	}
	return nil
}
