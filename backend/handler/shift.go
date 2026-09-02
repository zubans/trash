package handler

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
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

func shiftUserFromContext(r *http.Request) *repository.User {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		return nil
	}
	return user
}

// StartShift обслуживает POST /executor/shifts.
func (h *ShiftHandler) StartShift(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req StartShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	shift, err := h.shiftService.Start(r.Context(), user.ID, req.DurationHours)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(shift)
}

// EndShift обслуживает POST /executor/shifts/end.
func (h *ShiftHandler) EndShift(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.shiftService.End(r.Context(), user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// EarlyEndShift обслуживает POST /executor/shifts/early-end.
// Он завершает активную смену раньше запланированного времени и списывает штраф.
func (h *ShiftHandler) EarlyEndShift(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shift, err := h.shiftService.EarlyEnd(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shift)
}

// RecordLocation обслуживает POST /executor/shifts/location.
func (h *ShiftHandler) RecordLocation(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"stored": stored})
}

// GetActiveShiftHandler обслуживает GET /executor/shifts/active.
func (h *ShiftHandler) GetActiveShiftHandler(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shift, err := h.shiftService.GetCurrent(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shift)
}

// GetExecutorHistoryHandler обслуживает GET /executor/history.
func (h *ShiftHandler) GetExecutorHistoryHandler(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	history, err := h.shiftService.GetExecutorFinancialHistory(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// Псевдонимы имён методов, которых ожидает main.go.
func (h *ShiftHandler) StartShiftHandler(w http.ResponseWriter, r *http.Request) { h.StartShift(w, r) }
func (h *ShiftHandler) EndShiftHandler(w http.ResponseWriter, r *http.Request)   { h.EndShift(w, r) }
func (h *ShiftHandler) EarlyEndShiftHandler(w http.ResponseWriter, r *http.Request) {
	h.EarlyEndShift(w, r)
}
func (h *ShiftHandler) UploadLocationHandler(w http.ResponseWriter, r *http.Request) {
	h.RecordLocation(w, r)
}
