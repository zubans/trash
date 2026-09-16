package photoproof

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler — эндпоинты модуля: жесты для администратора и трек от исполнителя.
// Жесты правит администратор, а не миграция: набор рук и ног — это данные, и
// новый жест не должен требовать выката.
type Handler struct {
	service *Service
	// callerID отдаёт id аутентифицированного пользователя. Он передаётся
	// снаружи, чтобы модуль не зависел от ключей контекста middleware: это
	// единственное, что ему нужно знать о том, кто пришёл.
	callerID func(*http.Request) uuid.UUID
}

// NewHandler создаёт Handler. callerID может быть nil — тогда доступны только
// админские маршруты жестов.
func NewHandler(service *Service, callerID func(*http.Request) uuid.UUID) *Handler {
	return &Handler{service: service, callerID: callerID}
}

// RegisterExecutorRoutes подключает приём точек трека.
func (h *Handler) RegisterExecutorRoutes(r chi.Router) {
	r.Post("/executor/positions", h.RecordPositions)
}

// RecordPositions обслуживает POST /executor/positions: пачка точек трека с
// временем устройства. Через него уходит и то, что накопилось в офлайне.
func (h *Handler) RecordPositions(w http.ResponseWriter, r *http.Request) {
	executor := h.caller(r)
	if executor == uuid.Nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Positions []Position `json:"positions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Positions) > maxPositionsPerRequest {
		http.Error(w, "слишком много точек в одном запросе", http.StatusRequestEntityTooLarge)
		return
	}
	added, err := h.service.RecordPositions(r.Context(), nil, executor, req.Positions)
	if err != nil {
		if errors.Is(err, ErrPositionInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]int{"accepted": added})
}

// maxPositionsPerRequest — потолок одной пачки. Офлайн-очередь шлёт накопленное
// частями; запрос на десять тысяч точек — не очередь, а попытка засыпать трек.
const maxPositionsPerRequest = 500

// RegisterAdminRoutes подключает CRUD жестов. can — проверка права, та же, что
// охраняет остальную панель.
func (h *Handler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("watermarks.view")).Get("/admin/watermark-symbols", h.List)
	r.With(can("watermarks.create")).Post("/admin/watermark-symbols", h.Create)
	r.With(can("watermarks.edit")).Put("/admin/watermark-symbols/{id}", h.Update)
	r.With(can("watermarks.delete")).Delete("/admin/watermark-symbols/{id}", h.Delete)
	r.With(can("watermarks.edit")).Post("/admin/watermark-symbols/{id}/restore", h.Restore)
}

// List обслуживает GET /admin/watermark-symbols?include_deleted=true.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	includeDeleted := r.URL.Query().Get("include_deleted") == "true" || r.URL.Query().Get("include_deleted") == "1"
	symbols, err := h.service.ListSymbols(r.Context(), includeDeleted)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, symbols)
}

// Create обслуживает POST /admin/watermark-symbols.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var symbol Symbol
	if err := json.NewDecoder(r.Body).Decode(&symbol); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.service.CreateSymbol(r.Context(), &symbol); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, symbol)
}

// Update обслуживает PUT /admin/watermark-symbols/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid symbol id", http.StatusBadRequest)
		return
	}
	var symbol Symbol
	if err := json.NewDecoder(r.Body).Decode(&symbol); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	symbol.ID = id
	if err := h.service.UpdateSymbol(r.Context(), &symbol); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, symbol)
}

// Delete обслуживает DELETE /admin/watermark-symbols/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	h.setDeleted(w, r, h.service.DeleteSymbol)
}

// Restore обслуживает POST /admin/watermark-symbols/{id}/restore.
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	h.setDeleted(w, r, h.service.RestoreSymbol)
}

func (h *Handler) setDeleted(w http.ResponseWriter, r *http.Request, action func(ctxIn context.Context, id uuid.UUID) error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid symbol id", http.StatusBadRequest)
		return
	}
	if err := action(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	symbol, err := h.service.GetSymbol(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, symbol)
}

func (h *Handler) caller(r *http.Request) uuid.UUID {
	if h.callerID == nil {
		return uuid.Nil
	}
	return h.callerID(r)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSymbolNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrSymbolCodeTaken):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrSymbolCode), errors.Is(err, ErrSymbolTitle), errors.Is(err, ErrSymbolTooLong):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
