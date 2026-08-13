package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"healthlogin/backend/service"
)

type ExecutorGeoHandler struct {
	geoService *service.ExecutorGeoService
}

func NewExecutorGeoHandler(geoService *service.ExecutorGeoService) *ExecutorGeoHandler {
	return &ExecutorGeoHandler{geoService: geoService}
}

func (h *ExecutorGeoHandler) SetLocation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req service.SetLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	resp, err := h.geoService.SetLocation(user.ID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if !resp.Success {
		w.WriteHeader(http.StatusTooManyRequests)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *ExecutorGeoHandler) GetMapOrders(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lon, err2 := strconv.ParseFloat(lonStr, 64)
	if err1 != nil || err2 != nil {
		// Fallback to default location
		lat = 55.7558
		lon = 37.6173
	}

	orders, err := h.geoService.GetMapOrders(user.ID, lat, lon)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

func (h *ExecutorGeoHandler) GetGeoAlerts(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil || user.Role != "ADMIN" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	alerts, err := h.geoService.GetGeoAlerts(status, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}
