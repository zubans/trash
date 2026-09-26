package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/repository"
	"healthlogin/backend/service"
	"healthlogin/backend/upload"
)

// ChatHandler хранит зависимости эндпоинтов чат-комнат.
type ChatHandler struct {
	chatService *service.ChatService
	// shopLinks размечает номера покупок в чате поддержки ссылками на их
	// карточки — только для того, кто читает чужой чат, то есть поддержки.
	shopLinks func(ctx context.Context, ownerID uuid.UUID, messages []*repository.Message) error
	// uploadsDir — корень загрузок (UPLOADS_DIR). Вложения чата заказа лежат в
	// chat/, чата поддержки — в support/; ссылка /uploads/<путь> ведёт к файлу
	// относительно этого корня, поэтому старые вложения поддержки, лежавшие в
	// корне, продолжают открываться.
	uploadsDir string
}

// WithShopLinks подключает разметку номеров покупок в чате поддержки.
func (h *ChatHandler) WithShopLinks(links func(ctx context.Context, ownerID uuid.UUID, messages []*repository.Message) error) *ChatHandler {
	h.shopLinks = links
	return h
}

// NewChatHandler создаёт новый ChatHandler. uploadsDir приходит из
// composition root, а не читается из окружения на каждый запрос.
func NewChatHandler(chatService *service.ChatService, uploadsDir string) *ChatHandler {
	if uploadsDir == "" {
		uploadsDir = "uploads"
	}
	return &ChatHandler{chatService: chatService, uploadsDir: uploadsDir}
}

// RegisterUserRoutes — чат заказа и чат поддержки участника.
func (h *ChatHandler) RegisterUserRoutes(r chi.Router) {
	r.Get("/chats/{order_id}/messages", h.GetMessagesHandler)
	r.Post("/chats/{order_id}/messages", h.SendMessageHandler)
	r.Put("/chats/{order_id}/messages/{message_id}", h.EditMessageHandler)
	r.Delete("/chats/{order_id}/messages/{message_id}", h.DeleteMessageHandler)
	r.Post("/chats/{order_id}/upload", h.UploadAttachmentHandler)
	r.Post("/chats/{order_id}/read", h.MarkReadHandler)
	r.Get("/chats/unread-summary", h.GetUnreadSummaryHandler)
	r.Get("/chats/{order_id}/ws", h.WebSocketHandler)
	r.Get("/support/chat", h.GetUserSupportChatHandler)
	r.Get("/support/chats/{chat_id}/messages", h.GetSupportMessagesHandler)
	r.Post("/support/chats/{chat_id}/messages", h.SendSupportMessageHandler)
	r.Post("/support/chats/{chat_id}/upload", h.UploadSupportAttachmentHandler)
}

// RegisterAdminRoutes — чаты поддержки в админ-панели. Кого сюда пускать,
// решает право support_chats.* на маршруте, а не роль.
func (h *ChatHandler) RegisterAdminRoutes(r chi.Router, can func(string) func(http.Handler) http.Handler) {
	r.With(can("support_chats.view")).Get("/admin/support/chats", h.GetAdminSupportChatListHandler)
	r.With(can("support_chats.view")).Get("/admin/support/unread-summary", h.GetAdminSupportUnreadSummaryHandler)
	r.With(can("support_chats.edit")).Post("/admin/support/chats/{chat_id}/ban", h.BanSupportChatHandler)
	r.With(can("support_chats.edit")).Post("/admin/support/chats/{chat_id}/unban", h.UnbanSupportChatHandler)
}

// RegisterFileRoutes — раздача вложений участникам переписки. Вызывающий
// подключает их под RequireAuth.
func (h *ChatHandler) RegisterFileRoutes(r chi.Router) {
	r.Get("/uploads/*", h.ServeAttachmentHandler)
}

// maxAttachmentBytes — жёсткий предел на один загружаемый файл.
const maxAttachmentBytes = 25 << 20

// attachmentTypes — белый список: всё, что браузер мог бы выполнить в
// источнике приложения (html, svg, js, ...), не должно храниться и отдаваться
// обратно, иначе вложение превращается в хранимую XSS. Картинка обязана быть
// картинкой по содержимому; документы — чем угодно, кроме активного содержимого.
var attachmentTypes = map[string][]string{
	".jpg": {"image/"}, ".jpeg": {"image/"}, ".png": {"image/"}, ".webp": {"image/"}, ".gif": {"image/"}, ".heic": nil,
	".pdf": nil, ".doc": nil, ".docx": nil, ".xls": nil, ".xlsx": nil, ".txt": nil, ".csv": nil,
}

// attachmentKind делит вложения на картинки и документы по типу содержимого.
func attachmentKind(contentType string) string {
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	return "document"
}

// ServeAttachmentHandler отдаёт загруженный файл только участнику переписки,
// которой он принадлежит. Раньше вложения раздавал голый файловый сервер с
// включённым листингом каталогов.
func (h *ChatHandler) ServeAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	name := chi.URLParam(r, "*")
	// Отклоняем всё, что не является обычным относительным путём внутри корня загрузок.
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	fileURL := "/uploads/" + name
	allowed, err := h.chatService.CanAccessAttachment(r.Context(), user, fileURL)
	if err != nil {
		http.Error(w, "failed to check access", http.StatusInternalServerError)
		return
	}
	if !allowed {
		// 404, а не 403: сам факт существования файла — уже информация.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	base, err := filepath.Abs(h.uploadsDir)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	full := filepath.Join(base, filepath.FromSlash(name))
	if rel, err := filepath.Rel(base, full); err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Никогда не позволяем браузеру отрисовать хранимый файл в источнике приложения.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(full)+"\"")
	http.ServeFile(w, r, full)
}

// messageQueryFrom читает окно истории из строки запроса.
//
//	?limit=N     сколько сообщений вернуть (ограничено репозиторием)
//	?after=TS    только сообщения новее TS — то, что должен спрашивать опрос
//	?before=TS   самые новые сообщения старше TS — прокрутка назад
//
// Метки времени в RFC3339 — том же формате, в котором API уже отдаёт
// created_at, поэтому клиент может вернуть полученное значение без
// переформатирования. Неразбираемое значение игнорируется, а не отвергается:
// запасной вариант — самая свежая страница, разумный ответ для экрана чата,
// тогда как 400 оставил бы пользователя перед пустой перепиской.
func messageQueryFrom(r *http.Request) repository.MessageQuery {
	var q repository.MessageQuery
	params := r.URL.Query()

	if v := params.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if v := params.Get("after"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			q.After = &ts
		}
	}
	if v := params.Get("before"); v != "" {
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			q.Before = &ts
		}
	}
	return q
}

// GetMessagesHandler отдаёт историю сообщений.
func (h *ChatHandler) GetMessagesHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	messages, err := h.chatService.GetMessages(r.Context(), orderID, user.ID, messageQueryFrom(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

// SendMessageHandler сохраняет и рассылает сообщение чата через REST (классический
// запасной путь для клиентов, которые не умеют слать по WebSocket, например мобильных WebView).
func (h *ChatHandler) SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	msg, err := h.chatService.SendMessage(r.Context(), orderID, user.ID, req.Text)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, msg)
}

// WebSocketHandler апгрейдит запрос и обрабатывает цикл чата.
func (h *ChatHandler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	h.chatService.HandleWS(r.Context(), w, r, orderID, user.ID)
}

// MarkReadHandler отмечает все сообщения чата прочитанными.
func (h *ChatHandler) MarkReadHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	updatedIDs, err := h.chatService.MarkMessagesAsRead(r.Context(), orderID, user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"updated_ids": updatedIDs,
	})
}

// GetUnreadSummaryHandler возвращает ID заказов с непрочитанным для аутентифицированного пользователя.
func (h *ChatHandler) GetUnreadSummaryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderIDs, err := h.chatService.GetUnreadOrderIDs(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"unread_order_ids": orderIDs,
	})
}

// UploadAttachmentHandler обслуживает POST /api/chats/{order_id}/upload для файлов и фото.
func (h *ChatHandler) UploadAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	saved, err := upload.Save(w, r, upload.Options{
		Field: "file", MaxBytes: maxAttachmentBytes, Dir: filepath.Join(h.uploadsDir, "chat"),
		Name:   fmt.Sprintf("%s_%d", uuid.New().String(), time.Now().Unix()),
		Accept: upload.ByExtension(attachmentTypes),
	})
	if err != nil {
		writeUploadError(w, err, "file is required")
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	fileURL := "/uploads/chat/" + saved.Name

	msg, err := h.chatService.SendMessageWithAttachment(r.Context(), orderID, user.ID, text, fileURL,
		saved.ClientName, attachmentKind(saved.ContentType), saved.Size)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, msg)
}

// EditMessageHandler обслуживает PUT /api/chats/{order_id}/messages/{message_id}.
func (h *ChatHandler) EditMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderIDStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	msgIDStr := chi.URLParam(r, "message_id")
	messageID, err := uuid.Parse(msgIDStr)
	if err != nil {
		http.Error(w, "invalid message ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}

	msg, err := h.chatService.EditMessage(r.Context(), messageID, user.ID, orderID, req.Text)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

// DeleteMessageHandler обслуживает DELETE /api/chats/{order_id}/messages/{message_id}.
func (h *ChatHandler) DeleteMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	orderIDStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	msgIDStr := chi.URLParam(r, "message_id")
	messageID, err := uuid.Parse(msgIDStr)
	if err != nil {
		http.Error(w, "invalid message ID", http.StatusBadRequest)
		return
	}

	if err := h.chatService.DeleteMessage(r.Context(), messageID, user.ID, orderID); err != nil {
		writeDomainError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUserSupportChatHandler возвращает или создаёт чат поддержки текущего пользователя.
func (h *ChatHandler) GetUserSupportChatHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	chat, err := h.chatService.GetOrCreateSupportChat(r.Context(), user.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, chat)
}

// GetSupportMessagesHandler отдаёт сообщения поддержки.
func (h *ChatHandler) GetSupportMessagesHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	q := messageQueryFrom(r)
	messages, err := h.chatService.GetSupportMessages(r.Context(), chatID, user, q)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	// Размечать нечего, когда страница пуста, — а опрос почти всегда возвращает
	// пустую страницу, и владельца чата ради неё искать незачем.
	if h.shopLinks != nil && len(messages) > 0 {
		if owner, err := h.chatService.SupportChatOwner(r.Context(), chatID); err == nil && owner != user.ID {
			if err := h.shopLinks(r.Context(), owner, messages); err != nil {
				log.Printf("[chat] cannot link shop orders in support chat %s: %v", chatID, err)
			}
		}
	}
	// Прочитанным помечает только открытие переписки — запрос без курсора, то
	// есть самая свежая страница. Опрос (?after=) и прокрутка назад (?before=)
	// идут по этому же адресу каждые несколько секунд, и раньше каждый из них
	// стоил ещё одного UPDATE по всем сообщениям чата.
	if q.After == nil && q.Before == nil {
		_ = h.chatService.MarkSupportMessagesAsRead(r.Context(), chatID, user)
	}

	writeJSON(w, http.StatusOK, messages)
}

// supportChatWritable отвечает, можно ли сейчас писать в чат поддержки, и сам
// отвечает клиенту 403, если нельзя. Бан чата — мера против его владельца;
// администратора (в любой из его ролей) он не касается.
func (h *ChatHandler) supportChatWritable(w http.ResponseWriter, r *http.Request, user *repository.User, chatID uuid.UUID) bool {
	if user.HasRole(repository.RoleAdmin) {
		return true
	}
	banned, until, err := h.chatService.IsSupportChatBanned(r.Context(), chatID)
	if err != nil || !banned {
		return true
	}
	msg := "Чат поддержки заблокирован администратором"
	if until != nil {
		msg = fmt.Sprintf("Чат заблокирован до %s", until.Format("15:04 02.01.2006"))
	}
	http.Error(w, msg, http.StatusForbidden)
	return false
}

// SendSupportMessageHandler публикует текстовое сообщение в чат поддержки.
func (h *ChatHandler) SendSupportMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	if !h.supportChatWritable(w, r, user, chatID) {
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	msg, err := h.chatService.SaveSupportMessage(r.Context(), chatID, user, req.Text)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

// UploadSupportAttachmentHandler загружает вложение для чата поддержки.
func (h *ChatHandler) UploadSupportAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := requireUser(w, r)
	if !ok {
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	if !h.supportChatWritable(w, r, user, chatID) {
		return
	}

	saved, err := upload.Save(w, r, upload.Options{
		Field: "file", MaxBytes: maxAttachmentBytes, Dir: filepath.Join(h.uploadsDir, "support"),
		Name:   fmt.Sprintf("support_%s_%d", chatID.String()[:8], time.Now().UnixNano()),
		Accept: upload.ByExtension(attachmentTypes),
	})
	if err != nil {
		writeUploadError(w, err, "invalid file")
		return
	}
	fileURL := "/uploads/support/" + saved.Name
	text := r.FormValue("text")
	msg, err := h.chatService.SaveSupportMessageWithAttachment(r.Context(), chatID, user, text, fileURL,
		saved.ClientName, attachmentKind(saved.ContentType), saved.Size)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

// GetAdminSupportChatListHandler возвращает список чатов в стиле Telegram для
// админ-панели. Кого сюда пускать, решает право support_chats.view на маршруте,
// а не роль: модератор с этим правом читает список наравне с админом.
func (h *ChatHandler) GetAdminSupportChatListHandler(w http.ResponseWriter, r *http.Request) {
	list, err := h.chatService.GetAdminSupportChatList(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, list)
}

// BanSupportChatHandler банит чат поддержки на указанный срок («10m», «1h», «forever»).
func (h *ChatHandler) BanSupportChatHandler(w http.ResponseWriter, r *http.Request) {
	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Duration string `json:"duration"` // "10m", "1h", "forever"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Duration = "10m"
	}
	if req.Duration == "" {
		req.Duration = "10m"
	}

	if err := h.chatService.BanSupportChat(r.Context(), chatID, req.Duration); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "banned": true, "duration": req.Duration})
}

// UnbanSupportChatHandler снимает бан с чата поддержки.
func (h *ChatHandler) UnbanSupportChatHandler(w http.ResponseWriter, r *http.Request) {
	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	if err := h.chatService.UnbanSupportChat(r.Context(), chatID); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "success", "banned": false})
}

// GetAdminSupportUnreadSummaryHandler возвращает общее число непрочитанного для
// боковой панели админа. Доступ, как и у списка, охраняет право на маршруте.
func (h *ChatHandler) GetAdminSupportUnreadSummaryHandler(w http.ResponseWriter, r *http.Request) {
	total, err := h.chatService.GetAdminSupportUnreadCount(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"unread_count": total})
}
