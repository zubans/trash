package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// RoleHandler обслуживает страницу «Роли и права»: справочник ролей, матрицу
// прав и списки носителей.
type RoleHandler struct {
	roles *service.RoleService
}

// NewRoleHandler создаёт RoleHandler.
func NewRoleHandler(roles *service.RoleService) *RoleHandler {
	return &RoleHandler{roles: roles}
}

// writeRoleError переводит ошибки службы ролей в коды ответа: отсутствующая
// роль — 404, занятый код — 409, всё остальное — 400 с текстом, который можно
// показать администратору как есть (тексты службы уже на русском).
func writeRoleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrRoleNotFound):
		http.Error(w, "роль не найдена", http.StatusNotFound)
	case errors.Is(err, repository.ErrRoleExists):
		http.Error(w, "роль с таким кодом уже есть", http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// GetPermissionCatalog отдаёт каталог разделов панели и их действий. Матрица
// прав на фронтенде рисуется по нему, а не по своей копии списка: раздел,
// добавленный в бэкенде, появляется в интерфейсе без правки фронтенда.
func (h *RoleHandler) GetPermissionCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"sections": service.PermissionCatalog(),
		"actions": []map[string]string{
			{"key": service.ActionView, "label": "Просмотр"},
			{"key": service.ActionCreate, "label": "Добавление"},
			{"key": service.ActionEdit, "label": "Изменение"},
			{"key": service.ActionDelete, "label": "Удаление"},
		},
	})
}

// ListRoles отдаёт справочник ролей с правами и числом носителей.
func (h *RoleHandler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.roles.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"roles": roles})
}

// roleRequest — тело создания и правки роли.
type roleRequest struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// CreateRole заводит новую роль.
func (h *RoleHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFrom(r)
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	role, err := h.roles.Create(r.Context(), actor.ID, req.Code, req.Name, req.Description, req.Permissions)
	if err != nil {
		writeRoleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, role)
}

// UpdateRole меняет название, описание и права роли.
func (h *RoleHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFrom(r)
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req roleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	role, err := h.roles.Update(r.Context(), actor.ID, chi.URLParam(r, "code"), req.Name, req.Description, req.Permissions)
	if err != nil {
		writeRoleError(w, err)
		return
	}
	writeJSON(w, role)
}

// DeleteRole удаляет несистемную роль и снимает её со всех носителей.
func (h *RoleHandler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFrom(r)
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.roles.Delete(r.Context(), actor.ID, chi.URLParam(r, "code")); err != nil {
		writeRoleError(w, err)
		return
	}
	writeJSON(w, map[string]string{"message": "role deleted"})
}

// ListRoleUsers отдаёт страницу тех, кому подключена роль.
func (h *RoleHandler) ListRoleUsers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 {
		limit = 20
	}
	users, total, err := h.roles.ListUsers(r.Context(), chi.URLParam(r, "code"), r.URL.Query().Get("search"), limit, offset)
	if err != nil {
		writeRoleError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"users": users, "total": total})
}

// AssignRole подключает роль пользователю.
func (h *RoleHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFrom(r)
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}
	if err := h.roles.AssignUser(r.Context(), actor.ID, chi.URLParam(r, "code"), userID); err != nil {
		writeRoleError(w, err)
		return
	}
	writeJSON(w, map[string]string{"message": "role assigned"})
}

// UnassignRole снимает роль с пользователя.
func (h *RoleHandler) UnassignRole(w http.ResponseWriter, r *http.Request) {
	actor := middleware.UserFrom(r)
	if actor == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}
	if err := h.roles.UnassignUser(r.Context(), actor.ID, chi.URLParam(r, "code"), userID); err != nil {
		writeRoleError(w, err)
		return
	}
	writeJSON(w, map[string]string{"message": "role unassigned"})
}
