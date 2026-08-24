package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockChatRepo struct {
	chats         []*repository.Chat
	messages      []*repository.Message
	supportOwners map[uuid.UUID]uuid.UUID
}

func (m *mockChatRepo) GetChatByOrderID(orderID uuid.UUID) (*repository.Chat, error) {
	for _, c := range m.chats {
		if c.OrderID == orderID {
			return c, nil
		}
	}
	return nil, nil
}

func (m *mockChatRepo) CreateChat(orderID uuid.UUID) (*repository.Chat, error) {
	c := &repository.Chat{
		ID:       uuid.New(),
		OrderID:  orderID,
		IsActive: true,
	}
	m.chats = append(m.chats, c)
	return c, nil
}

func (m *mockChatRepo) SaveMessage(chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	msg := &repository.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      text,
		CreatedAt: time.Now(),
	}
	m.messages = append(m.messages, msg)
	return msg, nil
}

func (m *mockChatRepo) GetMessages(chatID uuid.UUID) ([]*repository.Message, error) {
	var list []*repository.Message
	for _, msg := range m.messages {
		if msg.ChatID == chatID {
			list = append(list, msg)
		}
	}
	return list, nil
}

func (m *mockChatRepo) DeactivateChat(chatID uuid.UUID) error {
	for _, c := range m.chats {
		if c.ID == chatID {
			c.IsActive = false
			return nil
		}
	}
	return errors.New("chat not found")
}

func (m *mockChatRepo) GetUnreadOrderIDs(userID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) MarkMessagesAsDelivered(chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) MarkMessagesAsRead(chatID, recipientID uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *mockChatRepo) SaveMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	fn := fileName
	msg := &repository.Message{
		ID:        uuid.New(),
		ChatID:    chatID,
		SenderID:  senderID,
		Text:      text,
		FileURL:   &fileURL,
		FileName:  &fn,
		FileType:  &fileType,
		FileSize:  &fileSize,
		CreatedAt: time.Now(),
	}
	m.messages = append(m.messages, msg)
	return msg, nil
}

func (m *mockChatRepo) DeleteMessage(messageID, senderID uuid.UUID) error {
	return nil
}

func (m *mockChatRepo) UpdateMessage(messageID, senderID uuid.UUID, newText string) (*repository.Message, error) {
	for _, msg := range m.messages {
		if msg.ID == messageID && msg.SenderID == senderID {
			msg.Text = newText
			now := time.Now()
			msg.UpdatedAt = &now
			return msg, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockChatRepo) GetOrCreateSupportChat(userID uuid.UUID) (*repository.SupportChat, error) {
	return &repository.SupportChat{ID: uuid.New(), UserID: userID}, nil
}
func (m *mockChatRepo) GetSupportMessages(chatID uuid.UUID) ([]*repository.Message, error) {
	return nil, nil
}
func (m *mockChatRepo) SaveSupportMessage(chatID, senderID uuid.UUID, text string) (*repository.Message, error) {
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text}, nil
}
func (m *mockChatRepo) SaveSupportMessageWithAttachment(chatID, senderID uuid.UUID, text, fileURL, fileName, fileType string, fileSize int64) (*repository.Message, error) {
	return &repository.Message{ID: uuid.New(), ChatID: chatID, SenderID: senderID, Text: text}, nil
}
func (m *mockChatRepo) GetAdminSupportChatList() ([]*repository.SupportChatListItem, error) {
	return nil, nil
}
func (m *mockChatRepo) MarkSupportMessagesAsRead(chatID, readerID uuid.UUID) error {
	return nil
}

func TestChatService_GetMessagesAccessControl(t *testing.T) {
	chatRepo := &mockChatRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewChatService(chatRepo, orderRepo)

	customerID := uuid.New()
	executorID := uuid.New()
	strangerID := uuid.New()

	// Create order and assign executor
	standardVariantID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	order, _ := orderRepo.CreateOrderWithHold(customerID, standardVariantID, false, false, 100.00, "")
	_ = orderRepo.AssignOrder(order.ID, executorID)

	// Create chat session
	chat, _ := chatRepo.CreateChat(order.ID)
	_, _ = chatRepo.SaveMessage(chat.ID, customerID, "Hello!")

	// Case 1: Customer should access messages
	msgs, err := srv.GetMessages(order.ID, customerID)
	if err != nil || len(msgs) != 1 {
		t.Errorf("expected customer to access messages, got err: %v, len: %d", err, len(msgs))
	}

	// Case 2: Executor should access messages
	msgs, err = srv.GetMessages(order.ID, executorID)
	if err != nil || len(msgs) != 1 {
		t.Errorf("expected executor to access messages, got err: %v", err)
	}

	// Case 3: Stranger should NOT access messages (should return error)
	_, err = srv.GetMessages(order.ID, strangerID)
	if err == nil {
		t.Error("expected error for stranger accessing messages")
	}
}

func TestChatService_EditAndDeleteMessage(t *testing.T) {
	chatRepo := &mockChatRepo{}
	orderRepo := &mockOrderRepo{}
	srv := NewChatService(chatRepo, orderRepo)

	customerID := uuid.New()
	orderID := uuid.New()
	chat, _ := chatRepo.CreateChat(orderID)
	msg, _ := chatRepo.SaveMessage(chat.ID, customerID, "Initial Message")

	// Test EditMessage
	editedMsg, err := srv.EditMessage(msg.ID, customerID, orderID, "Edited Message Text")
	if err != nil {
		t.Fatalf("unexpected error editing message: %v", err)
	}
	if editedMsg.Text != "Edited Message Text" {
		t.Errorf("expected text 'Edited Message Text', got '%s'", editedMsg.Text)
	}
	if editedMsg.UpdatedAt == nil {
		t.Errorf("expected UpdatedAt timestamp to be set")
	}

	// Test DeleteMessage
	err = srv.DeleteMessage(msg.ID, customerID, orderID)
	if err != nil {
		t.Fatalf("unexpected error deleting message: %v", err)
	}
}

// --- support chat methods required by repository.ChatRepository ---

func (m *mockChatRepo) SupportChatOwner(chatID uuid.UUID) (uuid.UUID, error) {
	if m.supportOwners == nil {
		return uuid.Nil, errors.New("support chat not found")
	}
	owner, ok := m.supportOwners[chatID]
	if !ok {
		return uuid.Nil, errors.New("support chat not found")
	}
	return owner, nil
}

func (m *mockChatRepo) CanAccessAttachment(userID uuid.UUID, fileURL string) (bool, error) {
	return false, nil
}

func (m *mockChatRepo) BanSupportChat(chatID uuid.UUID, duration string) error { return nil }

func (m *mockChatRepo) UnbanSupportChat(chatID uuid.UUID) error { return nil }

func (m *mockChatRepo) IsSupportChatBanned(chatID uuid.UUID) (bool, *time.Time, error) {
	return false, nil, nil
}

func (m *mockChatRepo) GetAdminSupportUnreadCount() (int, error) { return 0, nil }

// TestChatService_SupportChatOwnership verifies that a support conversation is
// only readable and writable by the user it belongs to (and by admins).
func TestChatService_SupportChatOwnership(t *testing.T) {
	owner := uuid.New()
	stranger := uuid.New()
	chatID := uuid.New()

	chatRepo := &mockChatRepo{supportOwners: map[uuid.UUID]uuid.UUID{chatID: owner}}
	svc := NewChatService(chatRepo, &mockOrderRepo{})

	if _, err := svc.GetSupportMessages(chatID, stranger, "CUSTOMER"); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger must not read the chat, got %v", err)
	}
	if _, err := svc.SaveSupportMessage(chatID, stranger, "CUSTOMER", "hi"); !errors.Is(err, ErrForbidden) {
		t.Errorf("stranger must not write to the chat, got %v", err)
	}
	if _, err := svc.GetSupportMessages(chatID, owner, "CUSTOMER"); err != nil {
		t.Errorf("owner must be able to read the chat: %v", err)
	}
	if _, err := svc.GetSupportMessages(chatID, stranger, "ADMIN"); err != nil {
		t.Errorf("admin must be able to read any chat: %v", err)
	}
}
