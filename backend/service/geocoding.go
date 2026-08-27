package service

import "context"

// GeocodingResult is a resolved address: coordinates plus the canonical line
// they belong to.
type GeocodingResult struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address"`
}

// AutocompleteResult is one suggestion in the legacy autocomplete shape. The
// mobile builds already installed read this from /geo/autocomplete and revalidate
// the string against their own format before submitting, so the shape has to
// survive even though the provider behind it is now DaData.
type AutocompleteResult struct {
	Address string  `json:"address"`
	Display string  `json:"display"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

// AddressResolver turns a free-form address string into coordinates. It is the
// fallback for the paths that are not the interactive suggestion box: order
// creation and registration when the client sent no coordinates, the
// /geo/geocode endpoint kept for installed clients, and the backfill worker.
// The interactive path never uses it — a picked suggestion already carries its
// coordinates.
type AddressResolver interface {
	Resolve(ctx context.Context, query string) (*GeocodingResult, error)
}
