package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// GeocodingResult contains coordinates and a formatted address for a geocoded place.
type GeocodingResult struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Address string  `json:"address"`
}

// NominatimResponse is a simplified view of a Nominatim JSON result.
type NominatimResponse struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

// Geocoder provides address-to-coordinate resolution.
type Geocoder struct {
	client      *http.Client
	baseURL     string
	rateLimiter <-chan time.Time
}

// NewGeocoder creates a Geocoder backed by Nominatim (OpenStreetMap).
func NewGeocoder() *Geocoder {
	// Nominatim requires a maximum of 1 request per second for free usage.
	return &Geocoder{
		client:      &http.Client{Timeout: 10 * time.Second},
		baseURL:     "https://nominatim.openstreetmap.org/search",
		rateLimiter: time.Tick(time.Second),
	}
}

// Geocode resolves a free-form address to coordinates.
func (g *Geocoder) Geocode(address string) (*GeocodingResult, error) {
	if address == "" {
		return nil, fmt.Errorf("address is empty")
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

	return &GeocodingResult{
		Lat:     lat,
		Lon:     lon,
		Address: results[0].DisplayName,
	}, nil
}
