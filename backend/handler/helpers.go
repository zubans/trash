package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/upload"
)

// Общие помощники обработчиков. Раньше пользователя из контекста доставали
// тремя функциями и четырьмя десятками inline-приведений, а JSON писали двумя
// способами, из которых один забывал Content-Type. Здесь по одному способу на
// каждое.

// requireUser отдаёт пользователя, положенного в контекст RequireAuth, и сам
// отвечает 401, если его там нет. Второе значение — «можно продолжать».
func requireUser(w http.ResponseWriter, r *http.Request) (*repository.User, bool) {
	user := middleware.UserFrom(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}

// writeJSON пишет ответ с Content-Type и явным статусом. Статус обязателен:
// так 201 у создания не теряется, а 200 не подразумевается молча.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// adminID — id вызывающего для строки аудита; uuid.Nil, если его нет.
func adminID(user *repository.User) uuid.UUID {
	if user == nil {
		return uuid.Nil
	}
	return user.ID
}

// firstNonEmpty возвращает первое непустое (после обрезки пробелов) значение.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseUUIDParam читает параметр маршрута как UUID.
func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, name))
}

// queryBool читает булев параметр запроса в тех написаниях, какие обычно
// встречаются в строке запроса браузера.
func queryBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// parseIDParam читает uuid из параметра маршрута; негодный — 400 с именем.
func parseIDParam(w http.ResponseWriter, r *http.Request, name, what string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		http.Error(w, "invalid "+what, http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}

// decodeBody читает JSON тела; негодный — 400.
func decodeBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// writeMessage — ответ 200 с одной строкой message.
func writeMessage(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusOK, map[string]string{"message": message})
}

// writeUploadError отвечает на отказ приёма файла: слишком большой, нет файла
// или не тот тип — всё это ошибки запроса, 400 с текстом; сбой диска — 500.
func writeUploadError(w http.ResponseWriter, err error, missing string) {
	switch {
	case errors.Is(err, upload.ErrTooLarge):
		http.Error(w, "file too large", http.StatusBadRequest)
	case errors.Is(err, upload.ErrNoFile):
		http.Error(w, missing, http.StatusBadRequest)
	case errors.Is(err, upload.ErrUnsupported):
		http.Error(w, strings.TrimPrefix(err.Error(), upload.ErrUnsupported.Error()+": "), http.StatusBadRequest)
	default:
		log.Printf("[upload] %v", err)
		http.Error(w, "failed to save file", http.StatusInternalServerError)
	}
}
