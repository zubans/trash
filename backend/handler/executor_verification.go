package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"healthlogin/backend/service"
)

// ExecutorVerificationHandler обслуживает заявку исполнителя на верификацию.
type ExecutorVerificationHandler struct {
	verification *service.ExecutorVerificationService
}

// NewExecutorVerificationHandler создаёт ExecutorVerificationHandler.
func NewExecutorVerificationHandler(verification *service.ExecutorVerificationService) *ExecutorVerificationHandler {
	return &ExecutorVerificationHandler{verification: verification}
}

// GetStatus обслуживает GET /executor/verification.
func (h *ExecutorVerificationHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	status, err := h.verification.Status(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Request обслуживает POST /executor/verification: дозаполняет недостающие
// данные и размещает заказ на верификацию. Если чего-то не хватает, отвечает
// 422 со списком полей в missing.
func (h *ExecutorVerificationHandler) Request(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		LastName   string          `json:"last_name"`
		FirstName  string          `json:"first_name"`
		Patronymic string          `json:"patronymic"`
		BirthDate  string          `json:"birth_date"`
		Address    *addressRequest `json:"address"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
	}
	req := service.ExecutorVerificationRequest{
		LastName:   body.LastName,
		FirstName:  body.FirstName,
		Patronymic: body.Patronymic,
		BirthDate:  body.BirthDate,
	}
	if body.Address != nil && (body.Address.Address != "" || body.Address.City != "") {
		addr := body.Address.toAddress()
		req.Address = &addr
	}

	order, err := h.verification.Request(r.Context(), user.ID, req)
	if err != nil {
		var missing *service.VerificationDataMissingError
		if errors.As(err, &missing) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   err.Error(),
				"missing": missing.Fields,
			})
			return
		}
		writeOrderError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(order)
}

// Cancel обслуживает POST /executor/verification/cancel.
func (h *ExecutorVerificationHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.verification.Cancel(r.Context(), user.ID); err != nil {
		writeOrderError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
