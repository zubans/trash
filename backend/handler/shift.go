package handler

import (
	"encoding/json"
	"net/http"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// StartShiftRequest contains the payload for starting a shift.
type StartShiftRequest struct {
	DurationHours int `json:"duration_hours"`
}

// LocationRequest contains a GPS coordinate.
type LocationRequest struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ShiftHandler handles shift-related HTTP endpoints.
type ShiftHandler struct {
	shiftService *service.ShiftService
}

// NewShiftHandler creates a ShiftHandler.
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

// StartShift handles POST /executor/shifts.
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

	shift, err := h.shiftService.Start(user.ID, req.DurationHours)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(shift)
}

// EndShift handles POST /executor/shifts/end.
func (h *ShiftHandler) EndShift(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.shiftService.End(user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// EarlyEndShift handles POST /executor/shifts/early-end.
// It ends the active shift before its planned time and charges a penalty.
func (h *ShiftHandler) EarlyEndShift(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shift, err := h.shiftService.EarlyEnd(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shift)
}

// RecordLocation handles POST /executor/shifts/location.
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

	if err := h.shiftService.RecordLocation(user.ID, req.Latitude, req.Longitude); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetActiveShiftHandler handles GET /executor/shifts/active.
func (h *ShiftHandler) GetActiveShiftHandler(w http.ResponseWriter, r *http.Request) {
	user := shiftUserFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	shift, err := h.shiftService.GetCurrent(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shift)
}

// Alias method names expected by main.go.
func (h *ShiftHandler) StartShiftHandler(w http.ResponseWriter, r *http.Request) { h.StartShift(w, r) }
func (h *ShiftHandler) EndShiftHandler(w http.ResponseWriter, r *http.Request)   { h.EndShift(w, r) }
func (h *ShiftHandler) EarlyEndShiftHandler(w http.ResponseWriter, r *http.Request) {
	h.EarlyEndShift(w, r)
}
func (h *ShiftHandler) UploadLocationHandler(w http.ResponseWriter, r *http.Request) {
	h.RecordLocation(w, r)
}
