package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"healthlogin/backend/metrics"
	"healthlogin/backend/repository"
)

// AddressSuggester answers address lookups. DaData is the only provider:
// falling back to a coarser one would quietly return addresses with no apartment
// and no reliable house number, which is the failure this design exists to
// remove. When it is not configured, address entry reports that plainly rather
// than degrading into something that looks like it works.
//
// It also resolves a free-form address to coordinates (see Resolve), which is
// the single geocoding path in the system; the coordinate cache keeps that
// fallback path from paying the provider for the same address twice.
type AddressSuggester struct {
	dadata *DaData
	cache  repository.GeocodeCacheRepository
}

// NewAddressSuggester wires the provider and the resolve cache. cache may be nil
// (resolving then always calls the provider), which keeps tests trivial.
func NewAddressSuggester(dadata *DaData, cache repository.GeocodeCacheRepository) *AddressSuggester {
	return &AddressSuggester{dadata: dadata, cache: cache}
}

// Configured reports whether suggestions can be served at all.
func (s *AddressSuggester) Configured() bool { return s.dadata != nil }

// Suggest returns structured suggestions for a partial query.
func (s *AddressSuggester) Suggest(ctx context.Context, query string, count int) ([]Address, error) {
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
	}
	return s.dadata.Suggest(ctx, query, count)
}

// Resolve turns a free-form address string into coordinates. It is the fallback
// for the non-interactive paths — order creation and registration when the
// client sent no coordinates, the /geo/geocode endpoint kept for installed
// clients, and the backfill worker. A picked suggestion already carries its
// coordinates and must never reach this.
//
// It answers from the cache first and otherwise takes the top provider match,
// which is the register's best guess for the typed line.
func (s *AddressSuggester) Resolve(ctx context.Context, query string) (*GeocodingResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("address is empty")
	}
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
	}

	if s.cache != nil {
		if hit, err := s.cache.Lookup(ctx, query); err == nil && hit != nil {
			metrics.UpstreamResult("dadata", "resolve", "cache_hit", 0)
			return &GeocodingResult{Lat: hit.Lat, Lon: hit.Lon, Address: hit.Address}, nil
		}
	}

	suggestions, err := s.dadata.Suggest(ctx, query, 1)
	if err != nil {
		return nil, err
	}
	for _, a := range suggestions {
		if !a.HasCoordinates() {
			continue
		}
		result := &GeocodingResult{Lat: *a.Lat, Lon: *a.Lon, Address: a.Compose()}
		if s.cache != nil {
			_ = s.cache.Save(ctx, query, result.Address, result.Lat, result.Lon)
		}
		return result, nil
	}
	return nil, errors.New("address not found")
}

// LegacySuggest returns the shape the installed mobile clients expect:
// "Россия, Город, Улица, д. N". Those builds re-validate the string against
// their own regular expression before submitting, so the format they were
// released with has to survive even though the provider behind it changed.
func (s *AddressSuggester) LegacySuggest(ctx context.Context, query string) ([]AutocompleteResult, error) {
	if s.dadata == nil {
		return nil, ErrNoAddressProvider
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
// line: the mobile builds already installed still send it, and rows saved before
// the structured columns existed still hold it. Anything picked from the
// suggestion list arrives already split, with its parts from the provider.
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
