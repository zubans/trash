package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"healthlogin/backend/service"
)

// GeoHandler exposes geocoding endpoints.
type GeoHandler struct {
	suggester *service.AddressSuggester
}

// NewGeoHandler creates a GeoHandler.
func NewGeoHandler(suggester *service.AddressSuggester) *GeoHandler {
	return &GeoHandler{suggester: suggester}
}

// Geocode handles GET /geo/geocode?q=address. It is the resolve path kept for
// installed clients that still send a free-form line; new clients pick a
// suggestion that already carries coordinates and never call this.
func (h *GeoHandler) Geocode(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("q")
	if address == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	result, err := h.suggester.Resolve(r.Context(), address)
	if err != nil {
		writeGeoError(w, err)
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
		writeGeoError(w, err)
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
		writeGeoError(w, err)
		return
	}
	if suggestions == nil {
		suggestions = []service.Address{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// writeGeoError separates "this deployment cannot suggest addresses" from "this
// query failed", so a missing key shows up as a server-side problem rather than
// as the user having typed something wrong.
func writeGeoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNoAddressProvider):
		http.Error(w, "address suggestions are not configured", http.StatusServiceUnavailable)
	case errors.Is(err, service.ErrAddressProviderBusy):
		http.Error(w, "address provider is busy, try again", http.StatusTooManyRequests)
	default:
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	}
}
