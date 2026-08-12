package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type mockChatRepo struct {
	chats    []*repository.Chat
	messages []*repository.Message
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
	return nil, nil
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
