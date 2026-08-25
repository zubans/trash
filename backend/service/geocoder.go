package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// stringField extracts a string value from a JSON object decoded into map[string]interface{}.
func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.Itoa(int(s))
	case int:
		return strconv.Itoa(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatCanonicalAddress builds a standardized Russian address from a Nominatim result.
// It prefers structured address components over the free-form display name.
func formatCanonicalAddress(addr map[string]interface{}) string {
	country := strings.TrimSpace(stringField(addr, "country"))
	if country == "" {
		country = "Россия"
	}

	city := strings.TrimSpace(stringField(addr, "city"))
	if city == "" {
		city = strings.TrimSpace(stringField(addr, "town"))
	}
	if city == "" {
		city = strings.TrimSpace(stringField(addr, "village"))
	}
	if city == "" {
		city = strings.TrimSpace(stringField(addr, "hamlet"))
	}
	if city == "" {
		city = strings.TrimSpace(stringField(addr, "county"))
	}
	if city == "" {
		city = strings.TrimSpace(stringField(addr, "state"))
	}
	// Nominatim often returns administrative wrappers like
	// "городской округ Курск" or "муниципальное образование Москва".
	// Strip the wrapper to keep the canonical address readable.
	city = strings.TrimPrefix(city, "городской округ ")
	city = strings.TrimPrefix(city, "муниципальное образование ")
	city = strings.TrimPrefix(city, "городское поселение ")
	city = strings.TrimSpace(city)

	road := strings.TrimSpace(stringField(addr, "road"))
	if road == "" {
		road = strings.TrimSpace(stringField(addr, "street"))
	}
	if road == "" {
		road = strings.TrimSpace(stringField(addr, "pedestrian"))
	}
	if road == "" {
		road = strings.TrimSpace(stringField(addr, "footway"))
	}

	houseNumber := strings.TrimSpace(stringField(addr, "house_number"))

	if road == "" {
		if city == "" {
			return country
		}
		return fmt.Sprintf("%s, %s", country, city)
	}

	if houseNumber == "" {
		return fmt.Sprintf("%s, %s, %s", country, city, road)
	}

	return fmt.Sprintf("%s, %s, %s, д. %s", country, city, road, houseNumber)
}

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
	Display string  `json:"display"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

// NominatimResponse is a simplified view of a Nominatim JSON result.
type NominatimResponse struct {
	Lat         string                 `json:"lat"`
	Lon         string                 `json:"lon"`
	DisplayName string                 `json:"display_name"`
	Address     map[string]interface{} `json:"address"`
}

// Geocoder provides address-to-coordinate resolution and autocomplete
// backed by OpenStreetMap Nominatim.
type Geocoder struct {
	db      *sql.DB
	client  *http.Client
	baseURL string
	// upstreamSlot serialises calls to Nominatim, which allows one request per
	// second on the free tier. It is a buffered channel rather than a bare
	// time.Tick receive so that a caller can give up instead of queueing
	// forever: an unbounded queue on a shared limiter meant one client could
	// stall address lookup — and therefore order creation — for everyone.
	upstreamSlot chan struct{}
}

// upstreamWaitTimeout bounds how long a caller waits for its turn at the
// upstream geocoder before giving up.
const upstreamWaitTimeout = 3 * time.Second

// ErrGeocoderBusy reports that the shared upstream slot did not free up in time.
var ErrGeocoderBusy = errors.New("geocoder is busy, try again")

// acquireUpstream waits for the shared once-per-second slot. It returns
// ErrGeocoderBusy rather than blocking indefinitely.
func (g *Geocoder) acquireUpstream() error {
	select {
	case <-g.upstreamSlot:
		// Refill the slot a second from now, keeping the upstream rate at 1/s.
		time.AfterFunc(time.Second, func() {
			select {
			case g.upstreamSlot <- struct{}{}:
			default:
			}
		})
		return nil
	case <-time.After(upstreamWaitTimeout):
		return ErrGeocoderBusy
	}
}

// NewGeocoder creates a Geocoder backed by Nominatim (OpenStreetMap).
func NewGeocoder(db *sql.DB) *Geocoder {
	// Nominatim allows one request per second on the free tier; the single slot
	// below enforces that.
	slot := make(chan struct{}, 1)
	slot <- struct{}{}

	return &Geocoder{
		db:           db,
		client:       &http.Client{Timeout: 10 * time.Second},
		baseURL:      "https://nominatim.openstreetmap.org/search",
		upstreamSlot: slot,
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

	if err := g.acquireUpstream(); err != nil {
		return nil, err
	}

	u, err := url.Parse(g.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("limit", "5")
	q.Set("addressdetails", "1")
	q.Set("countrycodes", "ru")
	q.Set("accept-language", "ru")
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

		canonical := formatCanonicalAddress(r.Address)

		// Use the canonical form for both the stored address and the UI display,
		// so the user sees a clean "Country, City, Street, House" format instead
		// of Nominatim's free-form display_name.
		suggestions = append(suggestions, AutocompleteResult{
			Address: canonical,
			Display: canonical,
			Lat:     lat,
			Lon:     lon,
		})
		// Cache each suggestion for later use.
		_ = g.saveCache(r.DisplayName, lat, lon)
		_ = g.saveCache(canonical, lat, lon)
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

	if err := g.acquireUpstream(); err != nil {
		return nil, err
	}

	u, err := url.Parse(g.baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", address)
	q.Set("format", "json")
	q.Set("limit", "1")
	q.Set("countrycodes", "ru")
	q.Set("accept-language", "ru")
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
