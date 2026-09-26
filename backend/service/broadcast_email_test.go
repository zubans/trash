package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Рассылка по группе. Вместо SMTP — дублёр EmailSender, который запоминает
// получателей: сервис проверяет транспорт только после сбора получателей,
// поэтому по записанным адресам видно, кто попал в рассылку.
type recordingTransport struct {
	sent []string
}

func (t *recordingTransport) SendEmail(to, subject, bodyHTML string) error {
	t.sent = append(t.sent, to)
	return nil
}

func broadcastService(users ...*repository.User) (*AdminService, *recordingTransport) {
	transport := &recordingTransport{}
	return NewAdminService(nil, &mockAdminRepo{users: users}, nil, transport), transport
}

func executorWithEmail(email string, verified bool) *repository.User {
	return &repository.User{ID: uuid.New(), Role: "EXECUTOR", Email: email, EmailVerified: verified}
}

func broadcastTo(svc *AdminService, group string, includeUnverified bool) (*BroadcastEmailResult, error) {
	return svc.SendBroadcastEmail(context.Background(), BroadcastEmailRequest{
		TargetGroup:       group,
		Subject:           "Тестовая рассылка",
		BodyHTML:          "<p>Текст</p>",
		IncludeUnverified: includeUnverified,
	})
}

// Прод-случай: у исполнителей почта есть, но никто не перешёл по ссылке
// подтверждения. Отказ обязан сказать именно это, а не «получателей нет».
func TestBroadcastEmailExplainsUnverifiedAddresses(t *testing.T) {
	svc, transport := broadcastService(
		executorWithEmail("one@example.com", false),
		executorWithEmail("two@example.com", false),
	)
	_, err := broadcastTo(svc, "EXECUTORS", false)
	if err == nil {
		t.Fatal("без подтверждённых адресов рассылка не может завершиться успехом")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("отказ по получателям — ошибка запроса, получено: %v", err)
	}
	if !strings.Contains(err.Error(), "почта указана у 2, но не подтверждена") {
		t.Fatalf("отказ должен назвать число неподтверждённых адресов, получено: %v", err)
	}
	if !strings.Contains(err.Error(), "Включая неподтверждённые адреса") {
		t.Fatalf("отказ должен подсказать, как отправить им, получено: %v", err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("отказ не должен ничего отправлять, отправлено %v", transport.sent)
	}
}

// С отметкой неподтверждённые адреса становятся получателями.
func TestBroadcastEmailIncludesUnverifiedOnRequest(t *testing.T) {
	svc, transport := broadcastService(executorWithEmail("one@example.com", false))
	res, err := broadcastTo(svc, "EXECUTORS", true)
	if err != nil {
		t.Fatalf("с отметкой получатели должны найтись, получено: %v", err)
	}
	if res.Total != 1 || res.Successful != 1 || len(transport.sent) != 1 || transport.sent[0] != "one@example.com" {
		t.Fatalf("ожидалось одно письмо на one@example.com, получено %+v, отправлено %v", res, transport.sent)
	}
}

// Подтверждённый адрес уходит и без отметки: неподтверждённые рядом с ним не
// превращают рассылку в отказ и сами не получают письма.
func TestBroadcastEmailSendsToVerifiedByDefault(t *testing.T) {
	svc, transport := broadcastService(
		executorWithEmail("verified@example.com", true),
		executorWithEmail("pending@example.com", false),
	)
	res, err := broadcastTo(svc, "EXECUTORS", false)
	if err != nil {
		t.Fatalf("подтверждённый адрес должен стать получателем, получено: %v", err)
	}
	if res.Total != 1 || len(transport.sent) != 1 || transport.sent[0] != "verified@example.com" {
		t.Fatalf("ожидалось письмо только на verified@example.com, отправлено %v", transport.sent)
	}
}

// Группа без единого адреса — отказ по запросу, а не пустой «успех».
func TestBroadcastEmailRefusesEmptyGroup(t *testing.T) {
	svc, _ := broadcastService(executorWithEmail("", false))
	if _, err := broadcastTo(svc, "EXECUTORS", true); !errors.Is(err, ErrValidation) {
		t.Fatalf("пустая группа: ожидалась ошибка запроса, получено: %v", err)
	}
}

// Без транспорта рассылка отвечает ErrNotConfigured, а не отправляет ничего и
// не отчитывается об успехе. SMTP больше не создаётся конструктором сам:
// процесс без настроенной почты не должен уметь слать живые письма.
func TestBroadcastEmailWithoutTransportIsNotConfigured(t *testing.T) {
	svc := NewAdminService(nil, &mockAdminRepo{users: []*repository.User{executorWithEmail("one@example.com", true)}}, nil, nil)
	if _, err := broadcastTo(svc, "EXECUTORS", false); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("без транспорта ожидался ErrNotConfigured, получено: %v", err)
	}
}
