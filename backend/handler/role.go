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

// writeRoleError переводит ошибки службы ролей в коды ответа. Занятый код —
// 409 со своим текстом; всё остальное — по классу через writeDomainError:
// отсутствующая роль — 404, назначение ADMIN не администратором — 403,
// проверки ввода (ErrValidation) — 422 с текстом, сбой — 500 без текста.
func writeRoleError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrRoleExists) {
		http.Error(w, "роль с таким кодом уже есть", http.StatusConflict)
		return
	}
	writeDomainError(w, err)
}

// GetPermissionCatalog отдаёт каталог разделов панели и их действий. Матрица
// прав на фронтенде рисуется по нему, а не по своей копии списка: раздел,
// добавленный в бэкенде, появляется в интерфейсе без правки фронтенда.
func (h *RoleHandler) GetPermissionCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"roles": roles})
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
	writeJSON(w, http.StatusCreated, role)
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
	writeJSON(w, http.StatusOK, role)
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
	writeJSON(w, http.StatusOK, map[string]string{"message": "role deleted"})
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
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users, "total": total})
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
	writeJSON(w, http.StatusOK, map[string]string{"message": "role assigned"})
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
	writeJSON(w, http.StatusOK, map[string]string{"message": "role unassigned"})
}

// RegisterAdminRoutes — роли и права. Каталог прав открыт любому, кто вошёл в
// панель: матрица рисуется по нему.
func (h *RoleHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.Get("/admin/permissions", h.GetPermissionCatalog)
	r.With(can("roles.view")).Get("/admin/roles", h.ListRoles)
	r.With(can("roles.create")).Post("/admin/roles", h.CreateRole)
	r.With(can("roles.edit")).Put("/admin/roles/{code}", h.UpdateRole)
	r.With(can("roles.delete")).Delete("/admin/roles/{code}", h.DeleteRole)
	r.With(can("roles.view")).Get("/admin/roles/{code}/users", h.ListRoleUsers)
	r.With(can("roles.edit")).Post("/admin/roles/{code}/users", h.AssignRole)
	r.With(can("roles.edit")).Delete("/admin/roles/{code}/users/{user_id}", h.UnassignRole)
}
