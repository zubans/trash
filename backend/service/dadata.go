package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"healthlogin/backend/metrics"
)

// DaData suggests Russian addresses from the state address register.
//
// It replaces Nominatim for address entry for three reasons the old setup could
// not solve: it knows apartments, which OpenStreetMap does not hold at all; it
// returns coordinates with the suggestion, so picking an address no longer
// needs a second geocoding round trip; and it is meant to be typed at, unlike
// the public Nominatim instance, whose usage policy forbids autocomplete and
// which was therefore throttled here to one request per second for the whole
// platform.
type DaData struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

const daDataSuggestURL = "https://suggestions.dadata.ru/suggestions/api/4_1/rs/suggest/address"

// ErrNoAddressProvider is returned when no suggestion provider is configured.
var ErrNoAddressProvider = errors.New("address suggestions are not configured")

// ErrAddressProviderBusy reports that the provider rate-limited the request, so
// the caller should back off and retry rather than treat it as "not found".
var ErrAddressProviderBusy = errors.New("address provider is busy, try again")

// NewDaData builds a suggester from DADATA_API_KEY. It returns nil when the key
// is absent so the process still starts; address entry and resolution then
// report ErrNoAddressProvider until the key is set, rather than the whole
// service failing to boot.
func NewDaData() *DaData {
	key := strings.TrimSpace(os.Getenv("DADATA_API_KEY"))
	if key == "" {
		return nil
	}
	return &DaData{
		// Address entry is interactive: a suggestion that arrives after the
		// next keystroke is worthless, so the timeout is short.
		client:  &http.Client{Timeout: 4 * time.Second},
		apiKey:  key,
		baseURL: daDataSuggestURL,
	}
}

// daDataRequest is the documented request body.
type daDataRequest struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	// Locations restricts results to Russia. Without it the register also
	// offers addresses in neighbouring countries it carries.
	Locations []map[string]string `json:"locations,omitempty"`
	Language  string              `json:"language,omitempty"`
}

type daDataResponse struct {
	Suggestions []struct {
		Value             string `json:"value"`
		UnrestrictedValue string `json:"unrestricted_value"`
		Data              struct {
			Region     string `json:"region_with_type"`
			City       string `json:"city_with_type"`
			Settlement string `json:"settlement_with_type"`
			Street     string `json:"street_with_type"`
			House      string `json:"house"`
			HouseType  string `json:"house_type"`
			Block      string `json:"block"`
			BlockType  string `json:"block_type"`
			Flat       string `json:"flat"`
			FiasID     string `json:"fias_id"`
			GeoLat     string `json:"geo_lat"`
			GeoLon     string `json:"geo_lon"`
		} `json:"data"`
	} `json:"suggestions"`
}

// Suggest returns address suggestions for a partial query.
func (d *DaData) Suggest(ctx context.Context, query string, count int) ([]Address, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query is empty")
	}
	if len([]rune(query)) < 3 {
		return []Address{}, nil
	}
	if count <= 0 || count > 20 {
		count = 7
	}

	body, err := json.Marshal(daDataRequest{
		Query:     query,
		Count:     count,
		Locations: []map[string]string{{"country": "Россия"}},
		Language:  "ru",
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+d.apiKey)

	started := time.Now()
	resp, err := d.client.Do(req)
	if err != nil {
		metrics.UpstreamCall("dadata", "suggest", time.Since(started), err)
		return nil, err
	}
	defer resp.Body.Close()
	// A rejected key and an exhausted quota both look like "no suggestions" to
	// the user, so they are separate label values rather than one error bucket.
	metrics.UpstreamResult("dadata", "suggest", daDataOutcome(resp.StatusCode), time.Since(started))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// The key itself is never included in the error: it would end up in the
		// logs and, through the handler, in a client's error message.
		return nil, errors.New("address provider rejected the credentials")
	case http.StatusTooManyRequests:
		return nil, ErrAddressProviderBusy
	default:
		return nil, fmt.Errorf("address provider returned status %d", resp.StatusCode)
	}

	var parsed daDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := make([]Address, 0, len(parsed.Suggestions))
	for _, s := range parsed.Suggestions {
		city := s.Data.City
		if city == "" {
			// Villages and settlements have no city, and dropping them would
			// make every address outside a city unusable.
			city = s.Data.Settlement
		}

		addr := Address{
			Region: s.Data.Region,
			City:   city,
			Street: s.Data.Street,
			House:  composeHouse(s.Data.House, s.Data.HouseType, s.Data.Block, s.Data.BlockType),
			Flat:   s.Data.Flat,
			FiasID: s.Data.FiasID,
			Source: SourceDaData,
		}
		if lat, err := strconv.ParseFloat(s.Data.GeoLat, 64); err == nil {
			addr.Lat = &lat
		}
		if lon, err := strconv.ParseFloat(s.Data.GeoLon, 64); err == nil {
			addr.Lon = &lon
		}

		// Value comes from the provider so the list reads the way the register
		// spells it; Compose covers the rare suggestion with no parts at all.
		addr.Value = strings.TrimSpace(s.Value)
		if addr.Value == "" {
			addr.Value = addr.Compose()
		}
		out = append(out, addr)
	}
	return out, nil
}

// composeHouse keeps the building identifier whole. A house is not always a
// number: "12 к. 1" and "10 стр. 2" are ordinary Russian addresses, and the old
// regular expression rejected every one of them.
func composeHouse(house, houseType, block, blockType string) string {
	house = strings.TrimSpace(house)
	if house == "" {
		return ""
	}
	// The type prefix is dropped for plain houses ("д. 12" is added back when
	// the line is composed) but kept for anything else, such as "влд. 5".
	if t := strings.TrimSpace(houseType); t != "" && t != "д" {
		house = t + ". " + house
	}
	if b := strings.TrimSpace(block); b != "" {
		bt := strings.TrimSpace(blockType)
		if bt == "" {
			bt = "к"
		}
		house += " " + bt + ". " + b
	}
	return house
}

// daDataOutcome maps an HTTP status onto the outcome label used in metrics.
func daDataOutcome(status int) string {
	switch status {
	case http.StatusOK:
		return "ok"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "http_error"
	}
}
