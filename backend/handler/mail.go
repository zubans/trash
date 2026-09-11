package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// MailHandler обслуживает внутреннюю почту: ящик пользователя, переписку с
// администрацией и рассылки.
//
// Почта отделена от чата намеренно. Чат живёт при заказе, двусторонен и
// исчезает вместе с заказом из виду; письмо адресовано человеку, переживает
// заказ и приходит тому, у кого заказов нет вовсе. Поэтому здесь и уведомление
// другое: не всплывающее окно, а конвертик, который ждёт, пока его откроют.
type MailHandler struct {
	mail  repository.MailRepository
	users repository.UserRepository
}

// NewMailHandler создаёт MailHandler.
func NewMailHandler(mail repository.MailRepository, users repository.UserRepository) *MailHandler {
	return &MailHandler{mail: mail, users: users}
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

// maxMailBody ограничивает письмо. Ограничение есть потому, что тело письма
// приходит от клиента и хранится целиком: без потолка одно обращение может
// занять столько места, сколько весь ящик.
const maxMailBody = 8000

// GetMail обслуживает GET /user/mail.
func (h *MailHandler) GetMail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mail, err := h.mail.ListForUser(r.Context(), user.ID, 100)
	if err != nil {
		http.Error(w, "cannot load mail", http.StatusInternalServerError)
		return
	}
	unread, _ := h.mail.UnreadCount(r.Context(), user.ID)
	writeJSON(w, map[string]interface{}{"messages": mail, "unread": unread})
}

// GetMailUnread обслуживает GET /user/mail/unread — счётчик для значка в меню.
func (h *MailHandler) GetMailUnread(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	unread, err := h.mail.UnreadCount(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "cannot count mail", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"unread": unread})
}

// MarkMailRead обслуживает POST /user/mail/{id}/read.
func (h *MailHandler) MarkMailRead(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	if err := h.mail.MarkRead(r.Context(), id, user.ID); err != nil {
		http.Error(w, "cannot mark as read", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkAllMailRead обслуживает POST /user/mail/read-all.
func (h *MailHandler) MarkAllMailRead(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.mail.MarkAllRead(r.Context(), user.ID); err != nil {
		http.Error(w, "cannot mark as read", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteMail обслуживает DELETE /user/mail/{id}. Удаление мягкое: письмо о
// выданном подарке — след выдачи, и он не должен исчезать из базы оттого, что
// получатель смахнул карточку.
func (h *MailHandler) DeleteMail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	if err := h.mail.Delete(r.Context(), id, user.ID); err != nil {
		http.Error(w, "cannot delete", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetMailThread обслуживает GET /user/mail/{id}/thread — переписка целиком.
// Открытие ветки считается прочтением: человек видит её всю, включая ответы.
func (h *MailHandler) GetMailThread(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	root, err := h.mail.Get(r.Context(), id)
	if err != nil || root.UserID != user.ID {
		// Чужой ящик и несуществующее письмо отвечают одинаково: перебор id не
		// должен рассказывать, какие письма есть на свете.
		http.Error(w, "mail not found", http.StatusNotFound)
		return
	}
	threadID := id
	if root.ThreadID != nil {
		threadID = *root.ThreadID
	}
	messages, err := h.mail.Thread(r.Context(), threadID)
	if err != nil {
		http.Error(w, "cannot load thread", http.StatusInternalServerError)
		return
	}
	if err := h.mail.MarkRead(r.Context(), threadID, user.ID); err != nil {
		log.Printf("[mail] cannot mark thread %s read: %v", threadID, err)
	}
	writeJSON(w, map[string]interface{}{"thread_id": threadID, "messages": messages})
}

// ReplyMail обслуживает POST /user/mail/{id}/reply — ответ пользователя
// администрации. Отвечать можно только в адресную переписку: письмо о выданной
// ачивке написало ядро, и адресата у ответа на него нет.
func (h *MailHandler) ReplyMail(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r)
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid mail id", http.StatusBadRequest)
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(body.Body)
	if text == "" {
		http.Error(w, "текст ответа пуст", http.StatusBadRequest)
		return
	}
	if len(text) > maxMailBody {
		http.Error(w, "текст ответа слишком длинный", http.StatusBadRequest)
		return
	}

	parent, err := h.mail.Get(r.Context(), id)
	if err != nil || parent.UserID != user.ID {
		http.Error(w, "mail not found", http.StatusNotFound)
		return
	}
	if parent.Kind != repository.MailKindDirect || parent.ThreadID == nil {
		http.Error(w, "на это письмо нельзя ответить", http.StatusConflict)
		return
	}

	reply := &repository.Mail{
		UserID:    user.ID,
		Subject:   replySubject(parent.Subject),
		Body:      text,
		Direction: repository.MailDirectionOut,
		ThreadID:  parent.ThreadID,
		SenderID:  &user.ID,
	}
	if err := h.mail.Reply(r.Context(), reply); err != nil {
		http.Error(w, "cannot send reply", http.StatusInternalServerError)
		return
	}
	writeJSON(w, reply)
}

// replySubject строит тему ответа. Префикс не удваивается: переписка из десяти
// реплик не должна называться «Re: Re: Re: …».
func replySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "Re:"
	}
	if strings.HasPrefix(subject, "Re: ") {
		return subject
	}
	return "Re: " + subject
}

// --- Админ -------------------------------------------------------------------

// AdminBroadcastMail обслуживает POST /admin/mail/broadcast — новость или акция
// во внутренние ящики.
func (h *MailHandler) AdminBroadcastMail(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Role    string `json:"role"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Subject) == "" {
		http.Error(w, "subject is required", http.StatusBadRequest)
		return
	}
	kind := body.Kind
	if kind != repository.MailKindPromo && kind != repository.MailKindNews {
		// Рассылкой можно послать только новость или акцию: письма о выдачах
		// пишет ядро, и подделывать их вручную незачем.
		kind = repository.MailKindNews
	}

	recipients, err := h.mail.RecipientsByRole(r.Context(), body.Role)
	if err != nil {
		log.Printf("[mail] cannot resolve broadcast recipients (role %q): %v", body.Role, err)
		http.Error(w, "cannot resolve recipients", http.StatusInternalServerError)
		return
	}
	sent, err := h.mail.Broadcast(r.Context(), &repository.Mail{
		Kind: kind, Subject: body.Subject, Body: body.Body, SenderID: &admin.ID,
	}, recipients)
	if err != nil {
		http.Error(w, "cannot send", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %s broadcast %s mail to %d users (role %q)", admin.ID, kind, sent, body.Role)
	writeJSON(w, map[string]int{"sent": sent})
}

// AdminSendMail обслуживает POST /admin/mail/users/{id} — адресное письмо
// одному человеку. Это начало переписки: получатель увидит его в ящике и
// сможет ответить, и ответ придёт сюда же.
func (h *MailHandler) AdminSendMail(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r)
	if admin == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	var body struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		// ThreadID продолжает начатую переписку. Пусто — начинается новая.
		ThreadID string `json:"thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(body.Body)
	subject := strings.TrimSpace(body.Subject)
	if text == "" {
		http.Error(w, "текст письма пуст", http.StatusBadRequest)
		return
	}
	if len(text) > maxMailBody {
		http.Error(w, "текст письма слишком длинный", http.StatusBadRequest)
		return
	}

	recipient, err := h.users.FindByID(r.Context(), userID)
	if err != nil || recipient == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	mail := &repository.Mail{
		UserID:    userID,
		Kind:      repository.MailKindDirect,
		Subject:   subject,
		Body:      text,
		Direction: repository.MailDirectionIn,
		SenderID:  &admin.ID,
	}

	if strings.TrimSpace(body.ThreadID) != "" {
		threadID, err := uuid.Parse(strings.TrimSpace(body.ThreadID))
		if err != nil {
			http.Error(w, "invalid thread id", http.StatusBadRequest)
			return
		}
		root, err := h.mail.Get(r.Context(), threadID)
		if err != nil || root.UserID != userID {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		mail.ThreadID = &threadID
		if subject == "" {
			mail.Subject = replySubject(root.Subject)
		}
		if err := h.mail.Reply(r.Context(), mail); err != nil {
			http.Error(w, "cannot send", http.StatusInternalServerError)
			return
		}
		// Отвечая, администратор ветку и прочитал: держать её в списке
		// неотвеченных после ответа значит показывать долг, которого нет.
		if err := h.mail.MarkThreadReadByAdmin(r.Context(), threadID); err != nil {
			log.Printf("[mail] cannot mark thread %s read by admin: %v", threadID, err)
		}
		log.Printf("[AUDIT] admin %s replied in mail thread %s to user %s", admin.ID, threadID, userID)
		writeJSON(w, mail)
		return
	}

	if mail.Subject == "" {
		http.Error(w, "тема письма обязательна", http.StatusBadRequest)
		return
	}
	if err := h.mail.Send(r.Context(), nil, mail); err != nil {
		http.Error(w, "cannot send", http.StatusInternalServerError)
		return
	}
	log.Printf("[AUDIT] admin %s sent direct mail %s to user %s", admin.ID, mail.ID, userID)
	writeJSON(w, mail)
}

// AdminListMailDialogs обслуживает GET /admin/mail/dialogs — список переписок.
func (h *MailHandler) AdminListMailDialogs(w http.ResponseWriter, r *http.Request) {
	onlyUnanswered := r.URL.Query().Get("unanswered") == "1"
	dialogs, err := h.mail.ListDialogs(r.Context(), onlyUnanswered, 200)
	if err != nil {
		http.Error(w, "cannot load dialogs", http.StatusInternalServerError)
		return
	}
	unread, _ := h.mail.AdminUnreadCount(r.Context())
	writeJSON(w, map[string]interface{}{"dialogs": dialogs, "unread": unread})
}

// AdminMailUnread обслуживает GET /admin/mail/unread — счётчик ответов, на
// которые никто не посмотрел.
func (h *MailHandler) AdminMailUnread(w http.ResponseWriter, r *http.Request) {
	unread, err := h.mail.AdminUnreadCount(r.Context())
	if err != nil {
		http.Error(w, "cannot count mail", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"unread": unread})
}

// AdminUserMail обслуживает GET /admin/mail/users/{id} — переписка с одним
// человеком целиком. Открытие переписки помечает ответы прочитанными: список
// неотвеченных — это то, что администратор ещё не открывал.
func (h *MailHandler) AdminUserMail(w http.ResponseWriter, r *http.Request) {
	userID, err := parseUUIDParam(r, "id")
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	messages, err := h.mail.ListDirectForUser(r.Context(), userID, 500)
	if err != nil {
		http.Error(w, "cannot load mail", http.StatusInternalServerError)
		return
	}
	seen := make(map[uuid.UUID]struct{})
	for _, m := range messages {
		if m.ThreadID == nil {
			continue
		}
		if _, ok := seen[*m.ThreadID]; ok {
			continue
		}
		seen[*m.ThreadID] = struct{}{}
		if err := h.mail.MarkThreadReadByAdmin(r.Context(), *m.ThreadID); err != nil {
			log.Printf("[mail] cannot mark thread %s read by admin: %v", *m.ThreadID, err)
		}
	}
	var recipientName, recipientPhone string
	if user, err := h.users.FindByID(r.Context(), userID); err == nil && user != nil {
		recipientName = strings.TrimSpace(user.LastName + " " + user.FirstName)
		recipientPhone = user.Phone
	}
	writeJSON(w, map[string]interface{}{
		"messages": messages,
		"user":     map[string]string{"id": userID.String(), "full_name": recipientName, "phone": recipientPhone},
	})
}
