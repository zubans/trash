package handler

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/service"
)

// GeoHandler exposes geocoding endpoints.
type GeoHandler struct {
	geocoder *service.Geocoder
}

// NewGeoHandler creates a GeoHandler.
func NewGeoHandler(geocoder *service.Geocoder) *GeoHandler {
	return &GeoHandler{geocoder: geocoder}
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
