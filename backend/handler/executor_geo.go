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

// FollowDevice возвращает рабочий якорь под управление устройства, в позицию,
// которую клиент только что с него считал. Это кнопка «моё местоположение»:
// только она возобновляет автоматическое позиционирование после того, как
// исполнитель поставил свою метку вручную.
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

	// Ограничено аутентифицированным исполнителем: местоположение принадлежит
	// user.ID и не может быть запрошено для кого-то другого.
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

	// Координаты читаются из сохранённого местоположения исполнителя, а не из
	// строки запроса, поэтому эндпоинтом нельзя сканировать произвольные районы.
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
