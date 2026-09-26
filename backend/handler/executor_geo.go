package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"healthlogin/backend/middleware"
	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
)

type ExecutorGeoHandler struct {
	geoService *service.ExecutorGeoService
}

func NewExecutorGeoHandler(geoService *service.ExecutorGeoService) *ExecutorGeoHandler {
	return &ExecutorGeoHandler{geoService: geoService}
}

func (h *ExecutorGeoHandler) SetLocation(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req service.SetLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	resp, err := h.geoService.SetLocation(r.Context(), user.ID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	status := http.StatusOK
	if !resp.Success {
		status = http.StatusTooManyRequests
	}
	writeJSON(w, status, resp)
}

// FollowDevice возвращает рабочий якорь под управление устройства, в позицию,
// которую клиент только что с него считал. Это кнопка «моё местоположение»:
// только она возобновляет автоматическое позиционирование после того, как
// исполнитель поставил свою метку вручную.
func (h *ExecutorGeoHandler) FollowDevice(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
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
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *ExecutorGeoHandler) GetLocation(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	// Ограничено аутентифицированным исполнителем: местоположение принадлежит
	// user.ID и не может быть запрошено для кого-то другого.
	resp, err := h.geoService.GetLocation(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *ExecutorGeoHandler) GetMapOrders(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	// Координаты читаются из сохранённого местоположения исполнителя, а не из
	// строки запроса, поэтому эндпоинтом нельзя сканировать произвольные районы.
	orders, err := h.geoService.GetMapOrders(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, orders)
}

// GetGeoAlerts обслуживает GET /admin/geo-alerts. Кого сюда пускать, решает
// право shifts.view на маршруте, а не роль: модератор с этим правом видит
// аномалии так же, как администратор.
func (h *ExecutorGeoHandler) GetGeoAlerts(w http.ResponseWriter, r *http.Request) {
	if middleware.UserFrom(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, alerts)
}

// RegisterExecutorRoutes — рабочая позиция исполнителя и карта заказов.
func (h *ExecutorGeoHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Post("/executor/set-location", h.SetLocation)
	// Возобновляет автоматическое позиционирование после ручного выбора.
	r.Post("/executor/follow-device", h.FollowDevice)
	r.Get("/executor/location", h.GetLocation)
	r.Get("/executor/map-orders", h.GetMapOrders)
}

// RegisterAdminRoutes — гео-тревоги.
func (h *ExecutorGeoHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("shifts.view")).Get("/admin/geo-alerts", h.GetGeoAlerts)
}
