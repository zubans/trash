package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/perk"
	"healthlogin/backend/service"
)

// perkRuleError отдаёт отказ правила формой, которую рисует админка: текст у
// поля скрипта и таблицу прогона по сетке, если он успел начаться.
func perkRuleError(w http.ResponseWriter, err error, grid []perk.GridRow) {
	if !errors.Is(err, service.ErrInvalidPerk) {
		writeShopError(w, err)
		return
	}
	writeShopError(w, &service.ShopError{
		Status: http.StatusUnprocessableEntity, Code: service.ShopErrValidation,
		Message: "Правило не прошло проверку",
		Fields:  map[string]string{"source": strings.TrimPrefix(err.Error(), service.ErrInvalidPerk.Error()+": ")},
		Details: map[string]interface{}{"grid": grid},
	})
}

// AdminPerkRules обслуживает GET /admin/shop/perk-rules.
func (h *ShopHandler) AdminPerkRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.rules.List(r.Context())
	if err != nil {
		writeShopError(w, err)
		return
	}
	writeJSON(w, rules)
}

// AdminCheckPerkRule обслуживает POST /admin/shop/perk-rules/check: компилирует
// текст и прогоняет его по сетке, ничего не сохраняя.
func (h *ShopHandler) AdminCheckPerkRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	grid, defaults, err := h.rules.Check(r.Context(), req.Source)
	if err != nil {
		perkRuleError(w, err, grid)
		return
	}
	writeJSON(w, map[string]interface{}{"grid": grid, "defaults": defaults})
}

// AdminCreatePerkRule обслуживает POST /admin/shop/perk-rules.
func (h *ShopHandler) AdminCreatePerkRule(w http.ResponseWriter, r *http.Request) {
	h.savePerkRule(w, r, "", true)
}

// AdminUpdatePerkRule обслуживает PUT /admin/shop/perk-rules/{code}.
func (h *ShopHandler) AdminUpdatePerkRule(w http.ResponseWriter, r *http.Request) {
	h.savePerkRule(w, r, chi.URLParam(r, "code"), false)
}

func (h *ShopHandler) savePerkRule(w http.ResponseWriter, r *http.Request, code string, create bool) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	var req service.SavePerkRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !create {
		req.Code = code
	}
	view, grid, err := h.rules.Save(r.Context(), admin.ID, req, create)
	if err != nil {
		perkRuleError(w, err, grid)
		return
	}
	writeJSON(w, map[string]interface{}{"rule": view, "grid": grid})
}
