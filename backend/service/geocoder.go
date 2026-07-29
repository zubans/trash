package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeocodingResult contains coordinates and a formatted address for a geocoded place.
type GeocodingResult struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address"`
}

// GeoCoder is the minimal interface required by services that need coordinate resolution.
type GeoCoder interface {
	Geocode(address string) (*GeocodingResult, error)
}

// AutocompleteResult is a single suggestion returned by the geocoder.
type AutocompleteResult struct {
	Address string  `json:"address"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

// NominatimResponse is a simplified view of a Nominatim JSON result.
type NominatimResponse struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

// Geocoder provides address-to-coordinate resolution and autocomplete
// backed by OpenStreetMap Nominatim.
type Geocoder struct {
	db          *sql.DB
	client      *http.Client
	baseURL     string
	rateLimiter <-chan time.Time
}

// NewGeocoder creates a Geocoder backed by Nominatim (OpenStreetMap).
func NewGeocoder(db *sql.DB) *Geocoder {
	// Nominatim requires a maximum of 1 request per second for free usage.
	return &Geocoder{
		db:          db,
		client:      &http.Client{Timeout: 10 * time.Second},
		baseURL:     "https://nominatim.openstreetmap.org/search",
		rateLimiter: time.Tick(time.Second),
	}
}

// Autocomplete returns address suggestions for a partial query.
// It uses Nominatim search and caches successful coordinates.
func (g *Geocoder) Autocomplete(query string) ([]AutocompleteResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}
	if len(query) < 3 {
		return []AutocompleteResult{}, nil
	}

	<-g.rateLimiter

	u, err := url.Parse(g.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("limit", "5")
	q.Set("addressdetails", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "healthlogin/1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoder returned status %d", resp.StatusCode)
	}

	var results []NominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}

	suggestions := make([]AutocompleteResult, 0, len(results))
	for _, r := range results {
		var lat, lon float64
		if _, err := fmt.Sscanf(r.Lat, "%f", &lat); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(r.Lon, "%f", &lon); err != nil {
			continue
		}
		suggestions = append(suggestions, AutocompleteResult{
			Address: r.DisplayName,
			Lat:     lat,
			Lon:     lon,
		})
		// Cache each suggestion for later use.
		_ = g.saveCache(r.DisplayName, lat, lon)
	}

	return suggestions, nil
}

// Geocode resolves a free-form address to coordinates.
// It first checks the local cache, then falls back to Nominatim.
func (g *Geocoder) Geocode(address string) (*GeocodingResult, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("address is empty")
	}

	// Try cache first.
	if cached, err := g.fromCache(address); err == nil && cached != nil {
		return cached, nil
	}

	<-g.rateLimiter

	u, err := url.Parse(g.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", address)
	q.Set("format", "json")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "healthlogin/1.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocoder returned status %d", resp.StatusCode)
	}

	var results []NominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("address not found")
	}

	var lat, lon float64
	if _, err := fmt.Sscanf(results[0].Lat, "%f", &lat); err != nil {
		return nil, err
	}
	if _, err := fmt.Sscanf(results[0].Lon, "%f", &lon); err != nil {
		return nil, err
	}

	result := &GeocodingResult{
		Lat:     lat,
		Lon:     lon,
		Address: results[0].DisplayName,
	}

	_ = g.saveCache(address, lat, lon)
	_ = g.saveCache(results[0].DisplayName, lat, lon)

	return result, nil
}

func (g *Geocoder) fromCache(query string) (*GeocodingResult, error) {
	if g.db == nil {
		return nil, nil
	}
	var res GeocodingResult
	err := g.db.QueryRow(
		`SELECT lat, lon, address FROM geocoding_cache WHERE query = $1`,
		strings.ToLower(query),
	).Scan(&res.Lat, &res.Lon, &res.Address)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (g *Geocoder) saveCache(query string, lat, lon float64) error {
	if g.db == nil {
		return nil
	}
	_, err := g.db.Exec(
		`INSERT INTO geocoding_cache (query, address, lat, lon)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (query) DO UPDATE SET address = EXCLUDED.address, lat = EXCLUDED.lat, lon = EXCLUDED.lon, created_at = now()`,
		strings.ToLower(query), query, lat, lon,
	)
	return err
}
