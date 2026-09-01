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

	resp, err := h.geoService.SetLocation(r.Context(), user.ID, req)
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

// FollowDevice puts the working anchor back under the device's control, at the
// position the client just read from it. This is the "my location" button: it
// is the only thing that resumes automatic positioning after the executor has
// placed their marker by hand.
func (h *ExecutorGeoHandler) FollowDevice(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	resp, err := h.geoService.FollowDevice(r.Context(), user.ID, req.Lat, req.Lon)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *ExecutorGeoHandler) GetLocation(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Scoped to the authenticated executor: the location belongs to user.ID and
	// cannot be requested for anyone else.
	resp, err := h.geoService.GetLocation(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *ExecutorGeoHandler) GetMapOrders(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Coordinates are read from the executor's stored location, not from the
	// query string, so the endpoint cannot be used to scan arbitrary areas.
	orders, err := h.geoService.GetMapOrders(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	alerts, err := h.geoService.GetGeoAlerts(r.Context(), status, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}
