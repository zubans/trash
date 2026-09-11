package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Рассылка по группе. Вместо транспорта — заглушка, которая не является
// SMTP-отправителем: сервис проверяет транспорт сразу после сбора получателей,
// поэтому его отказ значит «получатели нашлись», и ни одно письмо не уходит.
//
// nil сюда передавать нельзя: NewAdminService подставляет вместо него
// настоящий SMTP, и при заданном SMTP_HOST тест слал бы живые письма.
const transportUnavailable = "email transport is not available"

type noTransport struct{}

func (noTransport) SendEmailVerification(toEmail, token string) error { return nil }
func (noTransport) SendPasswordResetCode(toEmail, code string) error  { return nil }

func broadcastService(users ...*repository.User) *AdminService {
	return NewAdminService(nil, &mockAdminRepo{users: users}, nil, "secret", noTransport{})
}

func executorWithEmail(email string, verified bool) *repository.User {
	return &repository.User{ID: uuid.New(), Role: "EXECUTOR", Email: email, EmailVerified: verified}
}

func broadcastTo(t *testing.T, svc *AdminService, group string, includeUnverified bool) error {
	t.Helper()
	_, err := svc.SendBroadcastEmail(context.Background(), BroadcastEmailRequest{
		TargetGroup:       group,
		Subject:           "Тестовая рассылка",
		BodyHTML:          "<p>Текст</p>",
		IncludeUnverified: includeUnverified,
	})
	if err == nil {
		t.Fatal("без транспорта рассылка не может завершиться успехом")
	}
	return err
}

// Прод-случай: у исполнителей почта есть, но никто не перешёл по ссылке
// подтверждения. Отказ обязан сказать именно это, а не «получателей нет».
func TestBroadcastEmailExplainsUnverifiedAddresses(t *testing.T) {
	svc := broadcastService(
		executorWithEmail("one@example.com", false),
		executorWithEmail("two@example.com", false),
	)
	err := broadcastTo(t, svc, "EXECUTORS", false)
	if !strings.Contains(err.Error(), "почта указана у 2, но не подтверждена") {
		t.Fatalf("отказ должен назвать число неподтверждённых адресов, получено: %v", err)
	}
	if !strings.Contains(err.Error(), "Включая неподтверждённые адреса") {
		t.Fatalf("отказ должен подсказать, как отправить им, получено: %v", err)
	}
}

// С отметкой неподтверждённые адреса становятся получателями.
func TestBroadcastEmailIncludesUnverifiedOnRequest(t *testing.T) {
	svc := broadcastService(executorWithEmail("one@example.com", false))
	if err := broadcastTo(t, svc, "EXECUTORS", true); err.Error() != transportUnavailable {
		t.Fatalf("с отметкой получатели должны найтись, получено: %v", err)
	}
}

// Подтверждённый адрес уходит и без отметки: неподтверждённые рядом с ним не
// превращают рассылку в отказ.
func TestBroadcastEmailSendsToVerifiedByDefault(t *testing.T) {
	svc := broadcastService(
		executorWithEmail("verified@example.com", true),
		executorWithEmail("pending@example.com", false),
	)
	if err := broadcastTo(t, svc, "EXECUTORS", false); err.Error() != transportUnavailable {
		t.Fatalf("подтверждённый адрес должен стать получателем, получено: %v", err)
	}
}

// Адресов нет вовсе — и отметка тут не поможет. Заказчик с подтверждённой
// почтой в рассылку исполнителям не попадает.
func TestBroadcastEmailWithoutAnyAddress(t *testing.T) {
	svc := broadcastService(
		&repository.User{ID: uuid.New(), Role: "EXECUTOR"},
		&repository.User{ID: uuid.New(), Role: "CUSTOMER", Email: "customer@example.com", EmailVerified: true},
	)
	for _, include := range []bool{false, true} {
		err := broadcastTo(t, svc, "EXECUTORS", include)
		if err.Error() != "у выбранной группы нет ни одного адреса электронной почты" {
			t.Fatalf("include_unverified=%v: ждали отказ «нет адресов», получено: %v", include, err)
		}
	}
}

// Пустой ручной список: группы тут нет, и отказ должен говорить о списке.
func TestBroadcastEmailEmptyCustomList(t *testing.T) {
	_, err := broadcastService().SendBroadcastEmail(context.Background(), BroadcastEmailRequest{
		TargetGroup:  "CUSTOM_EMAILS",
		CustomEmails: []string{" ", ""},
		Subject:      "Тестовая рассылка",
		BodyHTML:     "<p>Текст</p>",
	})
	if err == nil || err.Error() != "список адресов пуст: укажите хотя бы один адрес" {
		t.Fatalf("ждали отказ «список пуст», получено: %v", err)
	}
}
