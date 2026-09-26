package handler

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
)

// StartShiftRequest содержит полезную нагрузку для начала смены.
type StartShiftRequest struct {
	DurationHours int `json:"duration_hours"`
}

// LocationRequest содержит GPS-координату.
type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ShiftHandler обслуживает HTTP-эндпоинты смен.
type ShiftHandler struct {
	shiftService *service.ShiftService
}

// NewShiftHandler создаёт ShiftHandler.
func NewShiftHandler(shiftService *service.ShiftService) *ShiftHandler {
	return &ShiftHandler{shiftService: shiftService}
}

// StartShift обслуживает POST /executor/shifts.
func (h *ShiftHandler) StartShift(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req StartShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	shift, err := h.shiftService.StartShift(r.Context(), user.ID, req.DurationHours)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, shift)
}

// EndShift обслуживает POST /executor/shifts/end. Завершение раньше срока
// штрафуется так же, как через /early-end: у смены один путь выхода.
func (h *ShiftHandler) EndShift(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	if _, err := h.shiftService.End(r.Context(), user.ID); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// EarlyEndShift обслуживает POST /executor/shifts/early-end: завершает
// активную смену раньше запланированного времени, списывает штраф и отдаёт
// закрытую смену.
func (h *ShiftHandler) EarlyEndShift(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	shift, err := h.shiftService.End(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shift)
}

// RecordLocation обслуживает POST /executor/shifts/location.
func (h *ShiftHandler) RecordLocation(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	var req LocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// «stored» сообщает, была ли позиция действительно принята; правила
	// местоположения могут отклонить перемещение, похожее на смену района, пока
	// идёт их пауза. Старый флаг «is_inside» ушёл вместе с геозоной, которую описывал.
	stored, err := h.shiftService.RecordLocation(r.Context(), user.ID, req.Latitude, req.Longitude)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"stored": stored})
}

// GetActiveShiftHandler обслуживает GET /executor/shifts/active.
func (h *ShiftHandler) GetActiveShiftHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	shift, err := h.shiftService.GetCurrent(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, shift)
}

// GetExecutorHistoryHandler обслуживает GET /executor/history.
func (h *ShiftHandler) GetExecutorHistoryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	history, err := h.shiftService.GetExecutorFinancialHistory(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

// RegisterExecutorRoutes — смены исполнителя и его история.
func (h *ShiftHandler) RegisterExecutorRoutes(r chi.Router) {
	r.Post("/executor/shifts", h.StartShift)
	r.Post("/executor/shifts/end", h.EndShift)
	r.Post("/executor/shifts/early-end", h.EarlyEndShift)
	r.Post("/executor/shifts/location", h.RecordLocation)
	r.Get("/executor/shifts/active", h.GetActiveShiftHandler)
	r.Get("/executor/history", h.GetExecutorHistoryHandler)
}
