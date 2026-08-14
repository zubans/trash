package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAutocomplete_KurskGrigorova(t *testing.T) {
	mockData := []NominatimResponse{
		{
			Lat:         "51.7920430",
			Lon:         "36.1902234",
			DisplayName: "улица Генерала Григорова, Серебряные холмы, Трепельный, Центральный округ, городской округ Курск, Курская область, Центральный федеральный округ, 305000, Россия",
			Address: map[string]interface{}{
				"road":          "улица Генерала Григорова",
				"residential":   "Серебряные холмы",
				"suburb":        "Трепельный",
				"city_district": "Центральный округ",
				"city":          "городской округ Курск",
				"state":         "Курская область",
				"country":       "Россия",
				"country_code":  "ru",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockData)
	}))
	defer server.Close()

	g := NewGeocoder(nil)
	g.baseURL = server.URL

	suggestions, err := g.Autocomplete("Курск Григорова")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions, got none")
	}

	got := suggestions[0].Address
	want := "Россия, Курск, улица Генерала Григорова"
	if got != want {
		t.Errorf("address = %q, want %q", got, want)
	}
	if suggestions[0].Display != want {
		t.Errorf("display = %q, want %q", suggestions[0].Display, want)
	}
}

func TestAutocomplete_KurskGrigorova40(t *testing.T) {
	mockData := []NominatimResponse{
		{
			Lat:         "51.7925000",
			Lon:         "36.1905000",
			DisplayName: "40, улица Генерала Григорова, Серебряные холмы, Трепельный, Центральный округ, городской округ Курск, Курская область, Центральный федеральный округ, 305000, Россия",
			Address: map[string]interface{}{
				"house_number":  "40",
				"road":          "улица Генерала Григорова",
				"residential":   "Серебряные холмы",
				"suburb":        "Трепельный",
				"city_district": "Центральный округ",
				"city":          "городской округ Курск",
				"state":         "Курская область",
				"country":       "Россия",
				"country_code":  "ru",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockData)
	}))
	defer server.Close()

	g := NewGeocoder(nil)
	g.baseURL = server.URL

	suggestions, err := g.Autocomplete("Курск Григорова 40")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions, got none")
	}

	got := suggestions[0].Address
	want := "Россия, Курск, улица Генерала Григорова, д. 40"
	if got != want {
		t.Errorf("address = %q, want %q", got, want)
	}
	if suggestions[0].Display != want {
		t.Errorf("display = %q, want %q", suggestions[0].Display, want)
	}
}

func TestGeocode(t *testing.T) {
	mockData := []NominatimResponse{
		{
			Lat:         "55.7558",
			Lon:         "37.6173",
			DisplayName: "Москва, Россия",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockData)
	}))
	defer server.Close()

	g := NewGeocoder(nil)
	g.baseURL = server.URL

	res, err := g.Geocode("Москва")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Lat != 55.7558 || res.Lon != 37.6173 {
		t.Errorf("expected 55.7558, 37.6173, got %f, %f", res.Lat, res.Lon)
	}

	// Geocode empty address
	_, err = g.Geocode("")
	if err == nil {
		t.Error("expected error for empty address")
	}
}
