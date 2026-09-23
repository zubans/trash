package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// PassportHandler — паспорт, статус «проверенный» и согласие на обработку
// персональных данных (implementation_plan_delivery_passport.md §2–§4).
type PassportHandler struct {
	passports *service.PassportService
}

// NewPassportHandler создаёт PassportHandler.
func NewPassportHandler(passports *service.PassportService) *PassportHandler {
	return &PassportHandler{passports: passports}
}

// RegisterUserRoutes — маршруты владельца и исполнителя заказа верификации.
func (h *PassportHandler) RegisterUserRoutes(r chi.Router) {
	r.Get("/me/passport", h.Mine)
	r.Get("/me/passport/data", h.MineForEdit)
	r.Put("/me/passport", h.SaveMine)
	r.Get("/me/passport/photo/key", h.MinePhotoKey)
	r.Post("/me/passport/photo", h.SaveMinePhoto)
	r.Post("/me/pd-consent", h.AcceptConsent)
	r.Put("/executor/orders/{id}/passport", h.SaveFromVerification)
	r.Get("/executor/orders/{id}/passport/photo/key", h.VerificationPhotoKey)
	r.Post("/executor/orders/{id}/passport/photo", h.SavePhotoFromVerification)
}

// RegisterAdminRoutes — паспорт в карточке пользователя. Смотреть и править —
// разные права: модератор смотрит, правит администратор.
func (h *PassportHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	// Статус без паспортных данных: есть ли паспорт и фото, стоит ли
	// «проверенный». Его видит тот, кто ставит отметку.
	r.With(can("checks.view")).Get("/admin/users/{id}/passport/status", h.AdminStatus)
	// Очередь заявок на статус: её ведёт поддержка, а не карточка отдельного
	// пользователя.
	r.With(can("checks.view")).Get("/admin/check-requests", h.AdminCheckRequests)
	r.With(can("passports.view")).Get("/admin/users/{id}/passport", h.AdminView)
	r.With(can("passports.view")).Get("/admin/users/{id}/passport/photo", h.AdminPhoto)
	r.With(can("passports.edit")).Put("/admin/users/{id}/passport", h.AdminSave)
	r.With(can("passports.edit")).Post("/admin/users/{id}/passport/photo", h.AdminSavePhoto)
	r.With(can("passports.edit")).Delete("/admin/users/{id}/passport", h.AdminDelete)
	r.With(can("checks.edit")).Post("/admin/users/{id}/checked", h.AdminSetChecked)
}

func writePassportError(w http.ResponseWriter, err error) {
	var pe *service.PassportError
	if errors.As(err, &pe) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(pe.Status)
		_ = json.NewEncoder(w).Encode(pe)
		return
	}
	log.Printf("[passport] %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (h *PassportHandler) caller(w http.ResponseWriter, r *http.Request) *repository.User {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
	return user
}

func decodePassport(w http.ResponseWriter, r *http.Request) (service.PassportData, bool) {
	var data service.PassportData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return data, false
	}
	return data, true
}

// readPhoto читает фото из multipart-поля file с потолком размера.
func readPhoto(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required (max 10MB)", http.StatusBadRequest)
		return nil, false
	}
	defer file.Close()
	photo, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusBadRequest)
		return nil, false
	}
	return photo, true
}

// deviceTakenAt — время съёмки по часам телефона. Его присылает приложение
// вместе со снимком; нет его — подпись снимка не проверяется.
func deviceTakenAt(r *http.Request) time.Time {
	value := r.FormValue("taken_at")
	if value == "" {
		return time.Time{}
	}
	takenAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return takenAt
}

// writeCaptureKey отдаёт ключ подписи снимка. Ключ не кешируется: он секрет.
func writeCaptureKey(w http.ResponseWriter, id uuid.UUID, key []byte) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"id": id.String(), "key": base64.StdEncoding.EncodeToString(key)})
}

func respondOK(w http.ResponseWriter) {
	writeJSON(w, map[string]bool{"ok": true})
}

// Mine обслуживает GET /me/passport — паспорт маской.
func (h *PassportHandler) Mine(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	mask, err := h.passports.Mine(r.Context(), user)
	if err != nil {
		writePassportError(w, err)
		return
	}
	writeJSON(w, mask)
}

// MineForEdit обслуживает GET /me/passport/data — свои данные целиком, чтобы
// форма правки открылась заполненной.
func (h *PassportHandler) MineForEdit(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	data, err := h.passports.MineForEdit(r.Context(), user)
	if err != nil {
		writePassportError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, data)
}

// SaveMine обслуживает PUT /me/passport.
func (h *PassportHandler) SaveMine(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	data, ok := decodePassport(w, r)
	if !ok {
		return
	}
	if err := h.passports.SaveMine(r.Context(), user, data); err != nil {
		writePassportError(w, err)
		return
	}
	h.Mine(w, r)
}

// SaveMinePhoto обслуживает POST /me/passport/photo.
func (h *PassportHandler) SaveMinePhoto(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	photo, ok := readPhoto(w, r)
	if !ok {
		return
	}
	if err := h.passports.SaveMinePhoto(r.Context(), user, photo, deviceTakenAt(r)); err != nil {
		writePassportError(w, err)
		return
	}
	h.Mine(w, r)
}

// MinePhotoKey обслуживает GET /me/passport/photo/key.
func (h *PassportHandler) MinePhotoKey(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	id, key, err := h.passports.MinePhotoKey(r.Context(), user)
	if err != nil {
		writePassportError(w, err)
		return
	}
	writeCaptureKey(w, id, key)
}

// VerificationPhotoKey обслуживает GET /executor/orders/{id}/passport/photo/key.
func (h *PassportHandler) VerificationPhotoKey(w http.ResponseWriter, r *http.Request) {
	executor := h.caller(w, r)
	if executor == nil {
		return
	}
	orderID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}
	id, key, err := h.passports.VerificationPhotoKey(r.Context(), orderID, executor.ID)
	if err != nil {
		writePassportError(w, err)
		return
	}
	writeCaptureKey(w, id, key)
}

// AcceptConsent обслуживает POST /me/pd-consent.
func (h *PassportHandler) AcceptConsent(w http.ResponseWriter, r *http.Request) {
	user := h.caller(w, r)
	if user == nil {
		return
	}
	if err := h.passports.AcceptConsent(r.Context(), user); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}

// SaveFromVerification обслуживает PUT /executor/orders/{id}/passport.
func (h *PassportHandler) SaveFromVerification(w http.ResponseWriter, r *http.Request) {
	executor := h.caller(w, r)
	if executor == nil {
		return
	}
	orderID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}
	data, ok := decodePassport(w, r)
	if !ok {
		return
	}
	if err := h.passports.SaveFromVerification(r.Context(), orderID, executor.ID, data); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}

// SavePhotoFromVerification обслуживает POST /executor/orders/{id}/passport/photo.
func (h *PassportHandler) SavePhotoFromVerification(w http.ResponseWriter, r *http.Request) {
	executor := h.caller(w, r)
	if executor == nil {
		return
	}
	orderID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid order id", http.StatusBadRequest)
		return
	}
	photo, ok := readPhoto(w, r)
	if !ok {
		return
	}
	if err := h.passports.SavePhotoFromVerification(r.Context(), orderID, executor.ID, photo, deviceTakenAt(r)); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}

// AdminStatus обслуживает GET /admin/users/{id}/passport/status.
func (h *PassportHandler) AdminStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	status, err := h.passports.Status(r.Context(), userID)
	if err != nil {
		writePassportError(w, err)
		return
	}
	writeJSON(w, status)
}

// AdminCheckRequests обслуживает GET /admin/check-requests.
func (h *PassportHandler) AdminCheckRequests(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	requests, err := h.passports.CheckRequests(r.Context(), limit)
	if err != nil {
		writePassportError(w, err)
		return
	}
	writeJSON(w, requests)
}

// AdminView обслуживает GET /admin/users/{id}/passport.
func (h *PassportHandler) AdminView(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	full, err := h.passports.AdminView(r.Context(), admin.ID, userID)
	if err != nil {
		writePassportError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, full)
}

// AdminPhoto обслуживает GET /admin/users/{id}/passport/photo — фото
// расшифровывается на лету и не кешируется.
func (h *PassportHandler) AdminPhoto(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	photo, err := h.passports.AdminPhoto(r.Context(), admin.ID, userID)
	if err != nil {
		writePassportError(w, err)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(photo))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(photo)
}

// AdminSave обслуживает PUT /admin/users/{id}/passport.
func (h *PassportHandler) AdminSave(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	data, ok := decodePassport(w, r)
	if !ok {
		return
	}
	if err := h.passports.AdminSave(r.Context(), admin.ID, userID, data); err != nil {
		writePassportError(w, err)
		return
	}
	h.AdminView(w, r)
}

// AdminSavePhoto обслуживает POST /admin/users/{id}/passport/photo.
func (h *PassportHandler) AdminSavePhoto(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	photo, ok := readPhoto(w, r)
	if !ok {
		return
	}
	if err := h.passports.AdminSavePhoto(r.Context(), admin.ID, userID, photo); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}

// AdminDelete обслуживает DELETE /admin/users/{id}/passport.
func (h *PassportHandler) AdminDelete(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if err := h.passports.AdminDelete(r.Context(), admin.ID, userID); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}

// AdminSetChecked обслуживает POST /admin/users/{id}/checked.
func (h *PassportHandler) AdminSetChecked(w http.ResponseWriter, r *http.Request) {
	admin := h.caller(w, r)
	if admin == nil {
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	var req struct {
		Checked bool   `json:"checked"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.passports.SetChecked(r.Context(), admin.ID, userID, req.Checked, req.Reason); err != nil {
		writePassportError(w, err)
		return
	}
	respondOK(w)
}
