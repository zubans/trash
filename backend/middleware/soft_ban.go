package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// SoftBannedErrorCode — код ответа, по которому клиент показывает экран
// «Аккаунт заблокирован» вместо обычной ошибки.
const SoftBannedErrorCode = "account_soft_banned"

// softBanAllowedRoutes — всё, что доступно пользователю в статусе SOFT_BANNED.
// Ключ — метод и шаблон маршрута chi без префикса /api.
//
// Список разрешающий, а не запрещающий, намеренно: новый маршрут, про который
// забыли, должен оказаться закрытым для заблокированного, а не открытым.
//
// Открыто ровно то, ради чего заблокированного вообще пускают в приложение:
// узнать о блокировке, написать в поддержку и в почту, увидеть баланс и довести
// до конца то, что уже начато, — не оставив второй стороне заказа брошенную
// работу, а себе незакрытую смену.
var softBanAllowedRoutes = map[string]struct{}{
	// Кто я и выход.
	"GET /auth/me":          {},
	"POST /logout":          {},
	"GET /user/profile":     {},
	"GET /customer/profile": {},

	// Поддержка и её вложения.
	"GET /support/chat":                      {},
	"GET /support/chats/{chat_id}/messages":  {},
	"POST /support/chats/{chat_id}/messages": {},
	"POST /support/chats/{chat_id}/upload":   {},
	"GET /uploads/*":                         {},

	// Внутренняя почта.
	"GET /user/mail":             {},
	"GET /user/mail/unread":      {},
	"POST /user/mail/read-all":   {},
	"POST /user/mail/{id}/read":  {},
	"DELETE /user/mail/{id}":     {},
	"GET /user/mail/{id}/thread": {},
	"POST /user/mail/{id}/reply": {},

	// Чат уже взятого заказа.
	"GET /chats/unread-summary":                      {},
	"GET /chats/{order_id}/messages":                 {},
	"POST /chats/{order_id}/messages":                {},
	"PUT /chats/{order_id}/messages/{message_id}":    {},
	"DELETE /chats/{order_id}/messages/{message_id}": {},
	"POST /chats/{order_id}/upload":                  {},
	"POST /chats/{order_id}/read":                    {},
	"GET /chats/{order_id}/ws":                       {},

	// Заказчик: свои заказы до конца — подтвердить, отменить ещё не взятый
	// (чтобы вернуть деньги), оставить чаевые и отзыв.
	"GET /customer/orders":               {},
	"POST /customer/orders/{id}/confirm": {},
	"POST /customer/orders/{id}/dispute": {},
	"POST /customer/orders/{id}/cancel":  {},
	"POST /customer/orders/{id}/tip":     {},
	"POST /orders/{id}/reviews":          {},
	"GET /orders/{id}/reviews/mine":      {},

	// Исполнитель: взятые заказы до конца и закрытие смены.
	"GET /executor/orders/assigned":         {},
	"POST /executor/orders/{id}/execute":    {},
	"POST /executor/orders/{id}/reject":     {},
	"POST /executor/orders/{id}/submission": {},
	"GET /executor/shifts/active":           {},
	"POST /executor/shifts/end":             {},
	"POST /executor/shifts/early-end":       {},
}

// softBanAllows сообщает, открыт ли маршрут запроса пользователю в SOFT_BANNED.
//
// Шаблон маршрута известен здесь потому, что RequireAuth подключается к группе
// маршрутов, а middleware группы chi вызывает уже после разбора маршрута.
// Запрос без разобранного маршрута не пропускается.
func softBanAllows(r *http.Request) bool {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return false
	}
	pattern := rctx.RoutePattern()
	if pattern == "" {
		return false
	}
	// Одни и те же маршруты смонтированы и под /api, и в корне (старые мобильные
	// сборки), а список ведётся один.
	if strings.HasPrefix(pattern, "/api/") {
		pattern = strings.TrimPrefix(pattern, "/api")
	}
	_, ok := softBanAllowedRoutes[r.Method+" "+pattern]
	return ok
}

// writeSoftBanned отвечает 403 с кодом, который клиент узнаёт.
func writeSoftBanned(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   SoftBannedErrorCode,
		"message": "Аккаунт заблокирован. Напишите в службу поддержки.",
	})
}
