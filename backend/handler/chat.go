package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// ChatHandler хранит зависимости эндпоинтов чат-комнат.
type ChatHandler struct {
	chatService *service.ChatService
}

// NewChatHandler создаёт новый ChatHandler.
func NewChatHandler(chatService *service.ChatService) *ChatHandler {
	return &ChatHandler{chatService: chatService}
}

// maxAttachmentBytes — жёсткий предел на один загружаемый файл.
const maxAttachmentBytes = 25 << 20

// allowedAttachmentExtensions — белый список: всё, что браузер мог бы выполнить
// в источнике приложения (html, svg, js, ...), не должно храниться и отдаваться
// обратно, иначе вложение превращается в хранимую XSS.
var allowedAttachmentExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".heic": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".txt": true, ".csv": true,
}

// uploadsBaseDir единообразно разрешает корень загрузок для любого пути загрузки.
func uploadsBaseDir() string {
	if dir := os.Getenv("UPLOADS_DIR"); dir != "" {
		return dir
	}
	return "uploads"
}

// safeExtension проверяет присланное клиентом имя файла и возвращает
// нормализованное расширение, под которым файл будет сохранён.
func safeExtension(fileName string) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		return "", errors.New("файл без расширения не поддерживается")
	}
	if !allowedAttachmentExtensions[ext] {
		return "", errors.New("недопустимый тип файла")
	}
	return ext, nil
}

// writeChatError сопоставляет ошибки сервиса с кодами HTTP по тождеству ошибки,
// а не по совпадению текста сообщения.
func writeChatError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrChatLocked):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

// ServeAttachmentHandler отдаёт загруженный файл только участнику переписки,
// которой он принадлежит. Раньше вложения раздавал голый файловый сервер с
// включённым листингом каталогов.
func (h *ChatHandler) ServeAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := chi.URLParam(r, "*")
	// Отклоняем всё, что не является обычным относительным путём внутри корня загрузок.
	if name == "" || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	fileURL := "/uploads/" + name
	allowed, err := h.chatService.CanAccessAttachment(r.Context(), user.ID, user.Role, fileURL)
	if err != nil {
		http.Error(w, "failed to check access", http.StatusInternalServerError)
		return
	}
	if !allowed {
		// 404, а не 403: сам факт существования файла — уже информация.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	base, err := filepath.Abs(uploadsBaseDir())
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
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		log.Printf("[GetMessagesHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// SendMessageHandler сохраняет и рассылает сообщение чата через REST (классический
// запасной путь для клиентов, которые не умеют слать по WebSocket, например мобильных WebView).
func (h *ChatHandler) SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		log.Printf("[SendMessageHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		writeChatError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

// WebSocketHandler апгрейдит запрос и обрабатывает цикл чата.
func (h *ChatHandler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	h.chatService.HandleWS(r.Context(), w, r, orderID, user.ID, user.Role)
}

// MarkReadHandler отмечает все сообщения чата прочитанными.
func (h *ChatHandler) MarkReadHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"updated_ids": updatedIDs,
	})
}

// GetUnreadSummaryHandler возвращает ID заказов с непрочитанным для аутентифицированного пользователя.
func (h *ChatHandler) GetUnreadSummaryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDs, err := h.chatService.GetUnreadOrderIDs(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"unread_order_ids": orderIDs,
	})
}

// UploadAttachmentHandler обслуживает POST /api/chats/{order_id}/upload для файлов и фото.
func (h *ChatHandler) UploadAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := chi.URLParam(r, "order_id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid order ID", http.StatusBadRequest)
		return
	}

	// MaxBytesReader ограничивает то, что реально доходит до диска; аргумент
	// ParseMultipartForm задаёт лишь размер буфера в памяти, а остаток уходит во временные файлы.
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "file too large (max 25MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext, err := safeExtension(header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	text := strings.TrimSpace(r.FormValue("text"))

	uploadDir := filepath.Join(uploadsBaseDir(), "chat")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, "failed to create upload directory", http.StatusInternalServerError)
		return
	}

	uniqueFileName := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)
	dstPath := filepath.Join(uploadDir, uniqueFileName)

	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	fileSize, err := io.Copy(dst, file)
	if err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	// Определяем категорию файла: изображение или документ
	mimeType := header.Header.Get("Content-Type")
	fileType := "document"
	if strings.HasPrefix(mimeType, "image/") || strings.Contains(strings.ToLower(ext), ".jpg") || strings.Contains(strings.ToLower(ext), ".png") || strings.Contains(strings.ToLower(ext), ".webp") || strings.Contains(strings.ToLower(ext), ".jpeg") {
		fileType = "image"
	}

	fileURL := fmt.Sprintf("/uploads/chat/%s", uniqueFileName)
	fileName := header.Filename

	msg, err := h.chatService.SendMessageWithAttachment(r.Context(), orderID, user.ID, text, fileURL, fileName, fileType, fileSize)
	if err != nil {
		log.Printf("[UploadAttachmentHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

// EditMessageHandler обслуживает PUT /api/chats/{order_id}/messages/{message_id}.
func (h *ChatHandler) EditMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// DeleteMessageHandler обслуживает DELETE /api/chats/{order_id}/messages/{message_id}.
func (h *ChatHandler) DeleteMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUserSupportChatHandler возвращает или создаёт чат поддержки текущего пользователя.
func (h *ChatHandler) GetUserSupportChatHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	chat, err := h.chatService.GetOrCreateSupportChat(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chat)
}

// GetSupportMessagesHandler отдаёт сообщения поддержки.
func (h *ChatHandler) GetSupportMessagesHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	messages, err := h.chatService.GetSupportMessages(r.Context(), chatID, user.ID, user.Role, messageQueryFrom(r))
	if err != nil {
		writeChatError(w, err)
		return
	}
	_ = h.chatService.MarkSupportMessagesAsRead(r.Context(), chatID, user.ID, user.Role)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// SendSupportMessageHandler публикует текстовое сообщение в чат поддержки.
func (h *ChatHandler) SendSupportMessageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	if user.Role != "ADMIN" {
		banned, until, err := h.chatService.IsSupportChatBanned(r.Context(), chatID)
		if err == nil && banned {
			msg := "Чат поддержки заблокирован администратором"
			if until != nil {
				msg = fmt.Sprintf("Чат заблокирован до %s", until.Format("15:04 02.01.2006"))
			}
			http.Error(w, msg, http.StatusForbidden)
			return
		}
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	msg, err := h.chatService.SaveSupportMessage(r.Context(), chatID, user.ID, user.Role, req.Text)
	if err != nil {
		writeChatError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// UploadSupportAttachmentHandler загружает вложение для чата поддержки.
func (h *ChatHandler) UploadSupportAttachmentHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	chatIDStr := chi.URLParam(r, "chat_id")
	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		http.Error(w, "invalid chat ID", http.StatusBadRequest)
		return
	}

	if user.Role != "ADMIN" {
		banned, until, err := h.chatService.IsSupportChatBanned(r.Context(), chatID)
		if err == nil && banned {
			msg := "Чат поддержки заблокирован администратором"
			if until != nil {
				msg = fmt.Sprintf("Чат заблокирован до %s", until.Format("15:04 02.01.2006"))
			}
			http.Error(w, msg, http.StatusForbidden)
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "file too large (max 25MB)", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext, err := safeExtension(header.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fileName := fmt.Sprintf("support_%s_%d%s", chatID.String()[:8], time.Now().UnixNano(), ext)
	uploadsDir := uploadsBaseDir()
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		http.Error(w, "failed to create upload directory", http.StatusInternalServerError)
		return
	}
	dstPath := filepath.Join(uploadsDir, fileName)
	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	fileURL := fmt.Sprintf("/uploads/%s", fileName)
	fileType := "document"
	mime := header.Header.Get("Content-Type")
	if strings.HasPrefix(mime, "image/") {
		fileType = "image"
	}

	text := r.FormValue("text")
	msg, err := h.chatService.SaveSupportMessageWithAttachment(r.Context(), chatID, user.ID, user.Role, text, fileURL, header.Filename, fileType, header.Size)
	if err != nil {
		writeChatError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// GetAdminSupportChatListHandler возвращает список чатов в стиле Telegram для админ-панели.
func (h *ChatHandler) GetAdminSupportChatListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil || user.Role != "ADMIN" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.chatService.GetAdminSupportChatList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "banned": true, "duration": req.Duration})
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "banned": false})
}

// GetAdminSupportUnreadSummaryHandler возвращает общее число непрочитанного для боковой панели админа.
func (h *ChatHandler) GetAdminSupportUnreadSummaryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil || user.Role != "ADMIN" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	total, err := h.chatService.GetAdminSupportUnreadCount(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"unread_count": total})
}
