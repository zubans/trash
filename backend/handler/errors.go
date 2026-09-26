package handler

import (
	"errors"
	"log"
	"net/http"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// writeDomainError отвечает кодом по классу ошибки сервиса (errors.Is), а не
// по её тексту и не одним кодом на всё:
//
//	не найдено                       — 404
//	чужой заказ, не допущен, не ADMIN — 403
//	состояние или правило не пускает — 409 (в том числе ErrConflict репозитория)
//	не хватает средств, негодный ввод — 422
//	скрипт услуги недоступен         — 503
//
// Всё прочее — сбой, о котором клиенту знать нечего: лог и 500 без внутреннего
// текста. Раньше catch-all отдавал 422 (или 403, или 404) с err.Error() на любую
// ошибку, включая ошибку базы.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrForbidden),
		errors.Is(err, service.ErrAdminRequired),
		errors.Is(err, service.ErrExecutorNotEligible),
		errors.Is(err, service.ErrCustomerNotEligible):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, repository.ErrConflict):
		// Голый ErrConflict репозитория несёт служебный текст; человеку нужен свой.
		msg := "заказ уже изменился, обновите страницу"
		var domain *service.DomainError
		if errors.As(err, &domain) {
			msg = domain.Error()
		}
		http.Error(w, msg, http.StatusConflict)
	case errors.Is(err, service.ErrOrderState), errors.Is(err, service.ErrRule):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, repository.ErrInsufficientFunds):
		http.Error(w, "недостаточно средств", http.StatusUnprocessableEntity)
	case errors.Is(err, service.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, service.ErrBehaviorUnavailable):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	default:
		log.Printf("[api] internal error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
