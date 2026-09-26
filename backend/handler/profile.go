package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// ProfileHandler обслуживает самообслуживание пользователя: профиль,
// сохранённые адреса и публичные настройки. Раньше это лежало в AdminHandler,
// хотя ни один из маршрутов не админский.
type ProfileHandler struct {
	profiles *service.ProfileService
}

// NewProfileHandler создаёт ProfileHandler.
func NewProfileHandler(profiles *service.ProfileService) *ProfileHandler {
	return &ProfileHandler{profiles: profiles}
}

// GetPublicSettingsHandler возвращает публичные системные настройки (например, валюту).
func (h *ProfileHandler) GetPublicSettingsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := h.profiles.PublicSettings(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, map[string]string{
		"currency":                 settings["currency"],
		"shift_early_exit_penalty": settings["shift_early_exit_penalty"],
		"executor_location_send_interval_seconds": settings["executor_location_send_interval_seconds"],
		// Должны ли приложения исполнителей сообщать своё положение во время смены.
		// Именно эти отчёты держат сохранённую позицию свежей для карты и
		// автоматического подбора. Геозона, по которой это названо, исчезла; ключ
		// сохранён, чтобы у существующих установок осталась их настройка.
		"geofence_tracking_enabled": settings["geofence_tracking_enabled"],
	})
}

// GetProfileHandler возвращает профиль аутентифицированного пользователя, включая адрес заказчика.
func (h *ProfileHandler) GetProfileHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireActor(w, r)
	if !ok {
		return
	}
	profile, err := h.profiles.GetProfile(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, profile)
}

// writeAddresses отдаёт список сохранённых адресов в том виде, какого ждёт
// страница профиля. Предел адресов — 409 со своим текстом; остальные ошибки —
// по классу, как везде.
func writeAddresses(w http.ResponseWriter, addresses []repository.Address, err error) {
	if err != nil {
		if errors.Is(err, repository.ErrAddressLimitReached) {
			http.Error(w, "можно сохранить не более 2 адресов", http.StatusConflict)
			return
		}
		writeDomainError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"addresses": addresses})
}

// AddAddressHandler сохраняет адрес подачи для аутентифицированного заказчика.
func (h *ProfileHandler) AddAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireActor(w, r)
	if !ok {
		return
	}
	var req addressRequest
	if !decodeBody(w, r, &req) {
		return
	}
	addresses, err := h.profiles.AddAddress(r.Context(), user.ID, req.toAddress())
	writeAddresses(w, addresses, err)
}

// DeleteAddressHandler удаляет один из сохранённых адресов вызывающего. Клиент
// адресует его по id или по номеру строки; что именно пришло, разбирает сервис.
func (h *ProfileHandler) DeleteAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireActor(w, r)
	if !ok {
		return
	}
	addresses, err := h.profiles.DeleteAddress(r.Context(), user.ID, chi.URLParam(r, "id"))
	writeAddresses(w, addresses, err)
}

// SetDefaultAddressHandler отмечает, с какого сохранённого адреса начинаются новые заказы.
func (h *ProfileHandler) SetDefaultAddressHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireActor(w, r)
	if !ok {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}
	if !decodeBody(w, r, &req) {
		return
	}

	var (
		addresses []repository.Address
		err       error
	)
	if id, parseErr := uuid.Parse(req.ID); parseErr == nil {
		addresses, err = h.profiles.SetDefaultAddress(r.Context(), user.ID, id)
	} else {
		addresses, err = h.profiles.SetDefaultAddressByValue(r.Context(), user.ID, req.Address)
	}
	writeAddresses(w, addresses, err)
}

// addressRequest принимает обе формы, которые может прислать клиент: одну
// строку, которую до сих пор шлют установленные мобильные сборки, и части,
// приходящие прямо из списка подсказок. Именно отправка частей пропускает
// корпус или строение, ведь их не приходится выпарсивать обратно из строки.
type addressRequest struct {
	Address string   `json:"address"`
	Region  string   `json:"region"`
	City    string   `json:"city"`
	Street  string   `json:"street"`
	House   string   `json:"house"`
	Flat    string   `json:"flat"`
	FiasID  string   `json:"fias_id"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	Source  string   `json:"source"`
}

// toAddress предпочитает части и откатывается к разбору строки.
func (r addressRequest) toAddress() service.Address {
	if r.City == "" && r.Street == "" && r.House == "" {
		addr := service.ParseAddressLine(r.Address)
		// Квартира, присланная рядом с легаси-строкой, всё равно применяется: именно
		// так её отправляет старый экран регистрации.
		if r.Flat != "" {
			addr = addr.WithFlat(r.Flat)
		}
		return addr
	}

	return service.Address{
		Value:  r.Address,
		Region: r.Region,
		City:   r.City,
		Street: r.Street,
		House:  r.House,
		Flat:   r.Flat,
		FiasID: r.FiasID,
		Lat:    r.Lat,
		Lon:    r.Lon,
		Source: firstNonEmpty(r.Source, service.SourceDaData),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
