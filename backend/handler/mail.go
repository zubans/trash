package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"healthlogin/backend/service"
)

// MailHandler обслуживает внутреннюю почту: ящик пользователя, переписку с
// администрацией и рассылки. Правила — кто кому может ответить, как
// называется ответ, какой вид у рассылки — в service.Mail.
type MailHandler struct {
	mail *service.Mail
}

// NewMailHandler создаёт MailHandler.
func NewMailHandler(mail *service.Mail) *MailHandler {
	return &MailHandler{mail: mail}
}

// RegisterUserRoutes подключает ящик пользователя. Маршруты описаны здесь, а не
// в main.go, чтобы e2e-тест поднимал ровно ту же разводку, что и сервер: копия
// маршрутов в тесте проверяла бы копию.
func (h *MailHandler) RegisterUserRoutes(r chi.Router) {
	r.Get("/user/mail", h.GetMail)
	r.Get("/user/mail/unread", h.GetMailUnread)
	r.Post("/user/mail/read-all", h.MarkAllMailRead)
	r.Post("/user/mail/{id}/read", h.MarkMailRead)
	r.Delete("/user/mail/{id}", h.DeleteMail)
	// Переписка: ветка письма целиком и ответ в неё. Отвечать можно только в
	// адресное письмо администрации — см. ReplyMail.
	r.Get("/user/mail/{id}/thread", h.GetMailThread)
	r.Post("/user/mail/{id}/reply", h.ReplyMail)
}

// RegisterAdminRoutes подключает админскую часть почты. can — проверка права,
// та же, что охраняет остальную панель.
func (h *MailHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("broadcasts.create")).Post("/admin/mail/broadcast", h.AdminBroadcastMail)
	// Адресная переписка с пользователем. Отдельный раздел прав, а не рассылки:
	// рассылка уходит списку и ответа не подразумевает, а здесь администратор
	// разговаривает с человеком.
	r.With(can("mail.view")).Get("/admin/mail/dialogs", h.AdminListMailDialogs)
	r.With(can("mail.view")).Get("/admin/mail/unread", h.AdminMailUnread)
	r.With(can("mail.view")).Get("/admin/mail/users/{id}", h.AdminUserMail)
	r.With(can("mail.create")).Post("/admin/mail/users/{id}", h.AdminSendMail)
}

// GetMail обслуживает GET /user/mail?limit=&offset=.
func (h *MailHandler) GetMail(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	inbox, err := h.mail.Inbox(r.Context(), user.ID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inbox)
}

// GetMailUnread обслуживает GET /user/mail/unread — счётчик для значка в меню.
func (h *MailHandler) GetMailUnread(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	unread, err := h.mail.Unread(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": unread})
}

// MarkMailRead обслуживает POST /user/mail/{id}/read.
func (h *MailHandler) MarkMailRead(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "mail id")
	if !ok {
		return
	}
	if err := h.mail.MarkRead(r.Context(), user.ID, id); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllMailRead обслуживает POST /user/mail/read-all.
func (h *MailHandler) MarkAllMailRead(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := h.mail.MarkAllRead(r.Context(), user.ID); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteMail обслуживает DELETE /user/mail/{id}.
func (h *MailHandler) DeleteMail(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "mail id")
	if !ok {
		return
	}
	if err := h.mail.Delete(r.Context(), user.ID, id); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetMailThread обслуживает GET /user/mail/{id}/thread — переписка целиком.
func (h *MailHandler) GetMailThread(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "mail id")
	if !ok {
		return
	}
	thread, err := h.mail.Thread(r.Context(), user.ID, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

// ReplyMail обслуживает POST /user/mail/{id}/reply — ответ пользователя
// администрации.
func (h *MailHandler) ReplyMail(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := parseIDParam(w, r, "id", "mail id")
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	reply, err := h.mail.Reply(r.Context(), user, id, body.Body)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

// --- Админ -------------------------------------------------------------------

// AdminBroadcastMail обслуживает POST /admin/mail/broadcast — новость или акция
// во внутренние ящики.
func (h *MailHandler) AdminBroadcastMail(w http.ResponseWriter, r *http.Request) {
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req service.BroadcastRequest
	if !decodeBody(w, r, &req) {
		return
	}
	sent, err := h.mail.Broadcast(r.Context(), admin.ID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
}

// AdminSendMail обслуживает POST /admin/mail/users/{id} — адресное письмо
// одному человеку или ответ в начатую переписку (thread_id).
func (h *MailHandler) AdminSendMail(w http.ResponseWriter, r *http.Request) {
	admin, ok := requireUser(w, r)
	if !ok {
		return
	}
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	var req service.DirectMailRequest
	if !decodeBody(w, r, &req) {
		return
	}
	mail, err := h.mail.SendDirect(r.Context(), admin.ID, userID, req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mail)
}

// AdminListMailDialogs обслуживает GET /admin/mail/dialogs?unanswered=1&limit=&offset=.
func (h *MailHandler) AdminListMailDialogs(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	dialogs, err := h.mail.Dialogs(r.Context(), r.URL.Query().Get("unanswered") == "1", limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dialogs)
}

// AdminMailUnread обслуживает GET /admin/mail/unread — счётчик ответов, на
// которые никто не посмотрел.
func (h *MailHandler) AdminMailUnread(w http.ResponseWriter, r *http.Request) {
	unread, err := h.mail.AdminUnread(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": unread})
}

// AdminUserMail обслуживает GET /admin/mail/users/{id}?limit=&offset= —
// переписка с одним человеком целиком.
func (h *MailHandler) AdminUserMail(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseIDParam(w, r, "id", "user id")
	if !ok {
		return
	}
	limit, offset := pageParams(r)
	out, err := h.mail.UserMail(r.Context(), userID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
