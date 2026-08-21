package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"healthlogin/backend/middleware"
	"healthlogin/backend/repository"
	"healthlogin/backend/service"
)

// ChatHandler holds dependencies for chat room endpoints.
type ChatHandler struct {
	chatService *service.ChatService
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(chatService *service.ChatService) *ChatHandler {
	return &ChatHandler{chatService: chatService}
}

// GetMessagesHandler retrieves history of messages.
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

	messages, err := h.chatService.GetMessages(orderID, user.ID)
	if err != nil {
		log.Printf("[GetMessagesHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// SendMessageHandler saves and broadcasts a chat message via REST (classic HTTP
// fallback for clients that cannot send over WebSocket, e.g. mobile WebViews).
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

	msg, err := h.chatService.SendMessage(orderID, user.ID, req.Text)
	if err != nil {
		log.Printf("[SendMessageHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "forbidden") {
			status = http.StatusForbidden
		} else if strings.Contains(err.Error(), "locked") {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

// WebSocketHandler upgrades request and processes chat loop.
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

	h.chatService.HandleWS(w, r, orderID, user.ID, user.Role)
}

// MarkReadHandler marks all messages in a chat as read.
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

	updatedIDs, err := h.chatService.MarkMessagesAsRead(orderID, user.ID)
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

// GetUnreadSummaryHandler returns unread order IDs for the authenticated user.
func (h *ChatHandler) GetUnreadSummaryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orderIDs, err := h.chatService.GetUnreadOrderIDs(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"unread_order_ids": orderIDs,
	})
}

// UploadAttachmentHandler handles POST /api/chats/{order_id}/upload for file/photo attachments.
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

	// Limit upload size to 25MB
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		http.Error(w, "file too large (max 25MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	text := strings.TrimSpace(r.FormValue("text"))

	uploadsBaseDir := os.Getenv("UPLOADS_DIR")
	if uploadsBaseDir == "" {
		uploadsBaseDir = "uploads"
	}
	uploadDir := filepath.Join(uploadsBaseDir, "chat")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, "failed to create upload directory", http.StatusInternalServerError)
		return
	}

	ext := filepath.Ext(header.Filename)
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

	// Determine file type category: image vs document
	mimeType := header.Header.Get("Content-Type")
	fileType := "document"
	if strings.HasPrefix(mimeType, "image/") || strings.Contains(strings.ToLower(ext), ".jpg") || strings.Contains(strings.ToLower(ext), ".png") || strings.Contains(strings.ToLower(ext), ".webp") || strings.Contains(strings.ToLower(ext), ".jpeg") {
		fileType = "image"
	}

	fileURL := fmt.Sprintf("/uploads/chat/%s", uniqueFileName)
	fileName := header.Filename

	msg, err := h.chatService.SendMessageWithAttachment(orderID, user.ID, text, fileURL, fileName, fileType, fileSize)
	if err != nil {
		log.Printf("[UploadAttachmentHandler] userID=%s orderID=%s error: %v", user.ID, orderID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

// EditMessageHandler handles PUT /api/chats/{order_id}/messages/{message_id}.
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

	msg, err := h.chatService.EditMessage(messageID, user.ID, orderID, req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// DeleteMessageHandler handles DELETE /api/chats/{order_id}/messages/{message_id}.
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

	if err := h.chatService.DeleteMessage(messageID, user.ID, orderID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUserSupportChatHandler returns or creates the support chat for current user.
func (h *ChatHandler) GetUserSupportChatHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	chat, err := h.chatService.GetOrCreateSupportChat(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chat)
}

// GetSupportMessagesHandler retrieves support messages.
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

	_ = h.chatService.MarkSupportMessagesAsRead(chatID, user.ID)
	messages, err := h.chatService.GetSupportMessages(chatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// SendSupportMessageHandler posts text message to support chat.
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

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	msg, err := h.chatService.SaveSupportMessage(chatID, user.ID, req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// UploadSupportAttachmentHandler uploads an attachment for support chat.
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

	r.ParseMultipartForm(10 << 20) // 10 MB limit
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	fileName := fmt.Sprintf("support_%s_%d%s", chatID.String()[:8], time.Now().UnixNano(), ext)
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "/app/uploads"
	}
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
	msg, err := h.chatService.SaveSupportMessageWithAttachment(chatID, user.ID, text, fileURL, header.Filename, fileType, header.Size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

// GetAdminSupportChatListHandler returns Telegram-style chat list for admin panel.
func (h *ChatHandler) GetAdminSupportChatListHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserKey).(*repository.User)
	if !ok || user == nil || user.Role != "ADMIN" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.chatService.GetAdminSupportChatList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
