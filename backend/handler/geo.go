package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"healthlogin/backend/service"
)

// GeoHandler exposes geocoding endpoints.
type GeoHandler struct {
	geocoder  *service.Geocoder
	suggester *service.AddressSuggester
}

// NewGeoHandler creates a GeoHandler.
func NewGeoHandler(geocoder *service.Geocoder, suggester *service.AddressSuggester) *GeoHandler {
	return &GeoHandler{geocoder: geocoder, suggester: suggester}
}

// Geocode handles GET /geo/geocode?q=address.
func (h *GeoHandler) Geocode(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("q")
	if address == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	result, err := h.geocoder.Geocode(address)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// Autocomplete handles GET /geo/autocomplete?q=query.
func (h *GeoHandler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	// Deliberately the old response shape. Installed mobile builds re-check the
	// string against a format of their own before submitting it, so changing
	// what this returns would break clients that are already in people's hands.
	suggestions, err := h.suggester.LegacySuggest(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// Suggest handles GET /geo/suggest?q=query&count=7 and returns addresses with
// their parts kept separate — city, street, house, flat, coordinates and the
// register identifier — so nothing downstream has to parse a string to recover
// them.
func (h *GeoHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	count := 0
	if raw := r.URL.Query().Get("count"); raw != "" {
		count, _ = strconv.Atoi(raw)
	}

	suggestions, err := h.suggester.Suggest(r.Context(), query, count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if suggestions == nil {
		suggestions = []service.Address{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}
