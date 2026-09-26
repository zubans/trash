package handler

import (
	"net/http"

	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
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
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	view, err := h.penalties.ViewFor(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
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
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
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
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}

	point, err := h.penalties.Revoke(r.Context(), pointID, admin.ID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, point)
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
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := h.penalties.ResetSilentBlockFlag(r.Context(), userID, admin.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "flag cleared"})
}

// RegisterUserRoutes — собственный статус штрафов.
func (h *PenaltyHandler) RegisterUserRoutes(r chi.Router) {
	r.Get("/me/penalty-status", h.MyPenaltyStatus)
}

// RegisterAdminRoutes — штрафные баллы на карточке пользователя.
func (h *PenaltyHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("users.view")).Get("/admin/users/{id}/penalties", h.AdminUserPenalties)
	r.With(can("penalties.edit")).Post("/admin/users/{id}/penalties/reset-silent-flag", h.AdminResetSilentBlockFlag)
	r.With(can("penalties.edit")).Post("/admin/users/{id}/penalties/{point_id}/revoke", h.AdminRevokePoint)
}
