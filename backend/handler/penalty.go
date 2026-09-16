package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"healthlogin/backend/service"
)

// PenaltyHandler обслуживает штрафные баллы: карточку пользователя в админке и
// то, что о своих штрафах знает он сам.
type PenaltyHandler struct {
	penalties *service.PenaltyService
}

// NewPenaltyHandler создаёт PenaltyHandler.
func NewPenaltyHandler(penalties *service.PenaltyService) *PenaltyHandler {
	return &PenaltyHandler{penalties: penalties}
}

// MyPenaltyStatus обслуживает GET /me/penalty-status.
//
// Отдаёт период фото-подтверждения по ролям и мягкий бан. Тихая блокировка
// здесь не упоминается: она на то и тихая.
func (h *PenaltyHandler) MyPenaltyStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	view, err := h.penalties.ViewFor(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

// AdminUserPenalties обслуживает GET /admin/users/{id}/penalties.
func (h *PenaltyHandler) AdminUserPenalties(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	view, err := h.penalties.AdminViewFor(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

// AdminRevokePoint обслуживает POST /admin/users/{id}/penalties/{point_id}/revoke.
func (h *PenaltyHandler) AdminRevokePoint(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	pointID, err := parseUUIDParam(r, "point_id")
	if err != nil {
		http.Error(w, "invalid point id", http.StatusBadRequest)
		return
	}
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	point, err := h.penalties.Revoke(r.Context(), pointID, admin.ID, userID)
	switch {
	case err == nil:
		writeJSON(w, point)
	case errors.Is(err, service.ErrPenaltyPointNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrPenaltyPointNotLive):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// AdminResetSilentBlockFlag обслуживает
// POST /admin/users/{id}/penalties/reset-silent-flag: снимает флаг прошлой
// тихой блокировки, после которого следующий балл был бы рецидивом.
func (h *PenaltyHandler) AdminResetSilentBlockFlag(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.penalties.ResetSilentBlockFlag(r.Context(), userID, admin.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "flag cleared"})
}
