package handler

import (
	"errors"
	"net/http"
	"strconv"

	"healthlogin/backend/service"

	"github.com/go-chi/chi/v5"
)

// GeoHandler открывает эндпоинты геокодирования.
type GeoHandler struct {
	suggester *service.AddressSuggester
}

// NewGeoHandler создаёт GeoHandler.
func NewGeoHandler(suggester *service.AddressSuggester) *GeoHandler {
	return &GeoHandler{suggester: suggester}
}

// Geocode обслуживает GET /geo/geocode?q=address. Это путь разрешения адреса,
// оставленный для установленных клиентов, которые всё ещё шлют свободную
// строку; новые клиенты выбирают подсказку с координатами и сюда не ходят.
func (h *GeoHandler) Geocode(w http.ResponseWriter, r *http.Request) {
	address := r.URL.Query().Get("q")
	if address == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	result, err := h.suggester.Resolve(r.Context(), address)
	if err != nil {
		writeGeoError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// Autocomplete обслуживает GET /geo/autocomplete?q=query.
func (h *GeoHandler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	// Форма ответа намеренно старая. Установленные мобильные сборки перепроверяют
	// строку по собственному формату перед отправкой, поэтому изменение того, что
	// здесь возвращается, сломало бы клиентов, уже находящихся у людей на руках.
	suggestions, err := h.suggester.LegacySuggest(r.Context(), query)
	if err != nil {
		writeGeoError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, suggestions)
}

// Suggest обслуживает GET /geo/suggest?q=query&count=7 и возвращает адреса с
// раздельными частями — город, улица, дом, квартира, координаты и
// идентификатор в реестре, — чтобы ниже по потоку ничему не приходилось
// разбирать строку ради их восстановления.
func (h *GeoHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}

	count := 0
	if raw := r.URL.Query().Get("count"); raw != "" {
		count, _ = strconv.Atoi(raw)
	}

	suggestions, err := h.suggester.Suggest(r.Context(), query, count)
	if err != nil {
		writeGeoError(w, err)
		return
	}
	if suggestions == nil {
		suggestions = []service.Address{}
	}

	writeJSON(w, http.StatusOK, suggestions)
}

// writeGeoError отделяет «эта установка не умеет подсказывать адреса» от «этот
// запрос не удался», чтобы отсутствующий ключ выглядел проблемой на стороне
// сервера, а не опечаткой пользователя.
func writeGeoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNoAddressProvider):
		http.Error(w, "address suggestions are not configured", http.StatusServiceUnavailable)
	case errors.Is(err, service.ErrAddressProviderBusy):
		http.Error(w, "address provider is busy, try again", http.StatusTooManyRequests)
	default:
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	}
}

// RegisterPublicRoutes — подсказки адресов. Провайдер адресов — платный
// общий внешний сервис: неограниченный анонимный доступ к нему жжёт квоту и
// замедляет ввод адреса для всех, поэтому все три маршрута под ограничителем.
func (h *GeoHandler) RegisterPublicRoutes(r chi.Router, limit func(http.Handler) http.Handler) {
	r.With(limit).Get("/geo/geocode", h.Geocode)
	r.With(limit).Get("/geo/autocomplete", h.Autocomplete)
	r.With(limit).Get("/geo/suggest", h.Suggest)
}
