package service

import (
	"context"
	"fmt"
	"strings"
)

// AddressSuggester answers address lookups from whichever provider is
// configured. DaData is used when a key is present; otherwise the old Nominatim
// path stays in place so a deployment without a key keeps working.
type AddressSuggester struct {
	dadata   *DaData
	geocoder *Geocoder
}

// NewAddressSuggester wires the providers.
func NewAddressSuggester(dadata *DaData, geocoder *Geocoder) *AddressSuggester {
	return &AddressSuggester{dadata: dadata, geocoder: geocoder}
}

// UsesDaData reports which provider is live, for the startup log.
func (s *AddressSuggester) UsesDaData() bool { return s.dadata != nil }

// Suggest returns structured suggestions for a partial query.
func (s *AddressSuggester) Suggest(ctx context.Context, query string, count int) ([]Address, error) {
	if s.dadata != nil {
		return s.dadata.Suggest(ctx, query, count)
	}
	if s.geocoder == nil {
		return nil, ErrNoAddressProvider
	}

	legacy, err := s.geocoder.Autocomplete(query)
	if err != nil {
		return nil, err
	}
	out := make([]Address, 0, len(legacy))
	for _, l := range legacy {
		addr := parseLegacyCanonical(l.Address)
		addr.Source = SourceNominatim
		if l.Lat != 0 || l.Lon != 0 {
			lat, lon := l.Lat, l.Lon
			addr.Lat, addr.Lon = &lat, &lon
		}
		out = append(out, addr)
	}
	return out, nil
}

// LegacySuggest returns the shape the installed mobile clients expect:
// "Россия, Город, Улица, д. N". Those builds re-validate the string against
// their own regular expression before submitting, so the format they were
// released with has to survive even though the provider behind it changed.
func (s *AddressSuggester) LegacySuggest(ctx context.Context, query string) ([]AutocompleteResult, error) {
	if s.dadata == nil {
		return s.geocoder.Autocomplete(query)
	}

	suggestions, err := s.dadata.Suggest(ctx, query, 7)
	if err != nil {
		return nil, err
	}

	out := make([]AutocompleteResult, 0, len(suggestions))
	for _, a := range suggestions {
		// An older client cannot use an address without a house number: its
		// own check rejects the string on submit, so offering it only produces
		// a dead end.
		if !a.IsDeliverable() {
			continue
		}
		line := fmt.Sprintf("Россия, %s, %s, д. %s",
			strings.TrimSpace(a.City), strings.TrimSpace(a.Street), strings.TrimSpace(a.House))

		result := AutocompleteResult{Address: line, Display: line}
		if a.Lat != nil {
			result.Lat = *a.Lat
		}
		if a.Lon != nil {
			result.Lon = *a.Lon
		}
		out = append(out, result)
	}
	return out, nil
}

// parseLegacyCanonical recovers the parts of an address that was stored as one
// line, for the Nominatim fallback and for addresses saved before the structured
// columns existed. New addresses never go through this: their parts come from
// the provider.
func parseLegacyCanonical(line string) Address {
	addr := Address{Value: strings.TrimSpace(line), Source: SourceLegacyText}

	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// Drop a leading country, which the old canonical form always carried.
	if len(parts) > 0 && strings.EqualFold(parts[0], "Россия") {
		parts = parts[1:]
	}

	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "д. "):
			addr.House = strings.TrimSpace(strings.TrimPrefix(p, "д. "))
		case strings.HasPrefix(p, "кв. "):
			addr.Flat = strings.TrimSpace(strings.TrimPrefix(p, "кв. "))
		case addr.City == "":
			addr.City = p
		case addr.Street == "":
			addr.Street = p
		}
	}

	// "д. 12 кв. 5" arrives as one component when the flat was appended without
	// a comma, which is how registration used to build it.
	if house, flat, found := strings.Cut(addr.House, " кв. "); found {
		addr.House = strings.TrimSpace(house)
		addr.Flat = strings.TrimSpace(flat)
	}
	return addr
}
