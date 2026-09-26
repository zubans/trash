package service

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// Mail — внутренняя почта: ящик пользователя, переписка с администрацией и
// рассылки. Кто кому может ответить, как называется ответ и какой вид у
// рассылки — правила этого сервиса; обработчик только разбирает запрос.
//
// Почта отделена от чата намеренно. Чат живёт при заказе, двусторонен и
// исчезает вместе с заказом из виду; письмо адресовано человеку, переживает
// заказ и приходит тому, у кого заказов нет вовсе.
type Mail struct {
	repo  repository.MailRepository
	users repository.UserRepository
}

// NewMail создаёт Mail.
func NewMail(repo repository.MailRepository, users repository.UserRepository) *Mail {
	return &Mail{repo: repo, users: users}
}

// maxMailBody ограничивает письмо. Ограничение есть потому, что тело письма
// приходит от клиента и хранится целиком: без потолка одно обращение может
// занять столько места, сколько весь ящик.
const maxMailBody = 8000

// Размеры страниц по умолчанию — прежние потолки списков, чтобы существующие
// клиенты видели ту же первую страницу.
const (
	defaultInboxPage    = 100
	defaultDialogsPage  = 200
	defaultUserMailPage = 500
)

// Ошибки почты, на которые смотрят обработчик и тесты.
var (
	// ErrMailNotFound — письма нет или оно в чужом ящике. Одна ошибка на оба
	// случая: перебор id не должен рассказывать, какие письма есть на свете.
	ErrMailNotFound = notFoundError("mail not found")
	// ErrMailNotRepliable — отвечать можно только в адресную переписку: письмо
	// о выданной ачивке написало ядро, и адресата у ответа на него нет.
	ErrMailNotRepliable = stateError("на это письмо нельзя ответить")
	// ErrMailBodyEmpty и ErrMailBodyTooLong — границы текста.
	ErrMailBodyEmpty   = validationError("текст письма пуст")
	ErrMailBodyTooLong = validationError("текст письма слишком длинный")
	// ErrMailSubjectRequired — у нового письма нет темы.
	ErrMailSubjectRequired = validationError("тема письма обязательна")
	// ErrMailThreadNotFound — ветки нет или она принадлежит другому человеку.
	ErrMailThreadNotFound = notFoundError("thread not found")
)

// Inbox — страница ящика вместе со счётчиком непрочитанного.
type Inbox struct {
	Messages []*repository.Mail `json:"messages"`
	Unread   int                `json:"unread"`
}

// Inbox — ящик пользователя, свежие письма первыми.
func (m *Mail) Inbox(ctx context.Context, userID uuid.UUID, limit, offset int) (*Inbox, error) {
	messages, err := m.repo.ListForUser(ctx, userID, pageLimit(limit, defaultInboxPage, 200), pageOffset(offset))
	if err != nil {
		return nil, err
	}
	unread, _ := m.repo.UnreadCount(ctx, userID)
	return &Inbox{Messages: messages, Unread: unread}, nil
}

// Unread — счётчик для значка в меню.
func (m *Mail) Unread(ctx context.Context, userID uuid.UUID) (int, error) {
	return m.repo.UnreadCount(ctx, userID)
}

// MarkRead помечает письмо (или ветку) прочитанным.
func (m *Mail) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	return m.repo.MarkRead(ctx, id, userID)
}

// MarkAllRead помечает весь ящик прочитанным.
func (m *Mail) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return m.repo.MarkAllRead(ctx, userID)
}

// Delete убирает письмо из ящика. Удаление мягкое: письмо о выданном подарке —
// след выдачи, и он не должен исчезать из базы оттого, что получатель смахнул
// карточку.
func (m *Mail) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return m.repo.Delete(ctx, id, userID)
}

// Thread — переписка целиком.
type Thread struct {
	ThreadID uuid.UUID          `json:"thread_id"`
	Messages []*repository.Mail `json:"messages"`
}

// Thread отдаёт ветку письма. Открытие ветки считается прочтением: человек
// видит её всю, включая ответы.
func (m *Mail) Thread(ctx context.Context, userID, id uuid.UUID) (*Thread, error) {
	root, err := m.own(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	threadID := id
	if root.ThreadID != nil {
		threadID = *root.ThreadID
	}
	messages, err := m.repo.Thread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if err := m.repo.MarkRead(ctx, threadID, userID); err != nil {
		log.Printf("[mail] cannot mark thread %s read: %v", threadID, err)
	}
	return &Thread{ThreadID: threadID, Messages: messages}, nil
}

// own отдаёт письмо, если оно лежит в ящике этого пользователя.
func (m *Mail) own(ctx context.Context, userID, id uuid.UUID) (*repository.Mail, error) {
	mail, err := m.repo.Get(ctx, id)
	if err != nil || mail.UserID != userID {
		return nil, ErrMailNotFound
	}
	return mail, nil
}

// checkBody проверяет границы текста и отдаёт его без пробелов по краям.
func checkBody(body string) (string, error) {
	text := strings.TrimSpace(body)
	if text == "" {
		return "", ErrMailBodyEmpty
	}
	if len(text) > maxMailBody {
		return "", ErrMailBodyTooLong
	}
	return text, nil
}

// Reply — ответ пользователя администрации в адресную переписку.
func (m *Mail) Reply(ctx context.Context, user *repository.User, id uuid.UUID, body string) (*repository.Mail, error) {
	text, err := checkBody(body)
	if err != nil {
		return nil, err
	}
	parent, err := m.own(ctx, user.ID, id)
	if err != nil {
		return nil, err
	}
	if parent.Kind != repository.MailKindDirect || parent.ThreadID == nil {
		return nil, ErrMailNotRepliable
	}
	reply := &repository.Mail{
		UserID:    user.ID,
		Subject:   replySubject(parent.Subject),
		Body:      text,
		Direction: repository.MailDirectionOut,
		ThreadID:  parent.ThreadID,
		SenderID:  &user.ID,
	}
	if err := m.repo.Reply(ctx, reply); err != nil {
		return nil, err
	}
	return reply, nil
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

// BroadcastRequest — новость или акция во внутренние ящики.
type BroadcastRequest struct {
	Kind    string `json:"kind"`
	Role    string `json:"role"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Broadcast рассылает письмо по роли и возвращает число получателей.
// Рассылкой можно послать только новость или акцию: письма о выдачах пишет
// ядро, и подделывать их вручную незачем — любой другой вид становится новостью.
func (m *Mail) Broadcast(ctx context.Context, adminID uuid.UUID, req BroadcastRequest) (int, error) {
	if strings.TrimSpace(req.Subject) == "" {
		return 0, validationError("subject is required")
	}
	kind := req.Kind
	if kind != repository.MailKindPromo && kind != repository.MailKindNews {
		kind = repository.MailKindNews
	}
	recipients, err := m.repo.RecipientsByRole(ctx, req.Role)
	if err != nil {
		return 0, err
	}
	sent, err := m.repo.Broadcast(ctx, &repository.Mail{
		Kind: kind, Subject: req.Subject, Body: req.Body, SenderID: &adminID,
	}, recipients)
	if err != nil {
		return 0, err
	}
	log.Printf("[AUDIT] admin %s broadcast %s mail to %d users (role %q)", adminID, kind, sent, req.Role)
	return sent, nil
}

// DirectMailRequest — адресное письмо одному человеку.
type DirectMailRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// ThreadID продолжает начатую переписку. Пусто — начинается новая.
	ThreadID *uuid.UUID `json:"thread_id,omitempty"`
}

// SendDirect пишет человеку от имени администрации: начало переписки или
// продолжение существующей ветки. Получатель увидит письмо в ящике и сможет
// ответить, и ответ придёт сюда же.
func (m *Mail) SendDirect(ctx context.Context, adminID, userID uuid.UUID, req DirectMailRequest) (*repository.Mail, error) {
	text, err := checkBody(req.Body)
	if err != nil {
		return nil, err
	}
	subject := strings.TrimSpace(req.Subject)
	if recipient, err := m.users.FindByID(ctx, userID); err != nil || recipient == nil {
		return nil, ErrUserNotFound
	}
	mail := &repository.Mail{
		UserID:    userID,
		Kind:      repository.MailKindDirect,
		Subject:   subject,
		Body:      text,
		Direction: repository.MailDirectionIn,
		SenderID:  &adminID,
	}
	if req.ThreadID != nil {
		root, err := m.repo.Get(ctx, *req.ThreadID)
		if err != nil || root.UserID != userID {
			return nil, ErrMailThreadNotFound
		}
		mail.ThreadID = req.ThreadID
		if subject == "" {
			mail.Subject = replySubject(root.Subject)
		}
		if err := m.repo.Reply(ctx, mail); err != nil {
			return nil, err
		}
		// Отвечая, администратор ветку и прочитал: держать её в списке
		// неотвеченных после ответа значит показывать долг, которого нет.
		if err := m.repo.MarkThreadReadByAdmin(ctx, *req.ThreadID); err != nil {
			log.Printf("[mail] cannot mark thread %s read by admin: %v", *req.ThreadID, err)
		}
		log.Printf("[AUDIT] admin %s replied in mail thread %s to user %s", adminID, *req.ThreadID, userID)
		return mail, nil
	}
	if mail.Subject == "" {
		return nil, ErrMailSubjectRequired
	}
	if err := m.repo.Send(ctx, nil, mail); err != nil {
		return nil, err
	}
	log.Printf("[AUDIT] admin %s sent direct mail %s to user %s", adminID, mail.ID, userID)
	return mail, nil
}

// Dialogs — страница переписок со счётчиком неотвеченного.
type Dialogs struct {
	Dialogs []*repository.MailDialog `json:"dialogs"`
	Unread  int                      `json:"unread"`
}

// Dialogs — список переписок для администратора.
func (m *Mail) Dialogs(ctx context.Context, onlyUnanswered bool, limit, offset int) (*Dialogs, error) {
	dialogs, err := m.repo.ListDialogs(ctx, onlyUnanswered, pageLimit(limit, defaultDialogsPage, 500), pageOffset(offset))
	if err != nil {
		return nil, err
	}
	unread, _ := m.repo.AdminUnreadCount(ctx)
	return &Dialogs{Dialogs: dialogs, Unread: unread}, nil
}

// AdminUnread — счётчик ответов, на которые никто не посмотрел.
func (m *Mail) AdminUnread(ctx context.Context) (int, error) {
	return m.repo.AdminUnreadCount(ctx)
}

// MailRecipient — с кем переписка.
type MailRecipient struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

// UserMail — переписка с одним человеком целиком.
type UserMail struct {
	Messages []*repository.Mail `json:"messages"`
	User     MailRecipient      `json:"user"`
}

// UserMail — переписка с одним человеком. Открытие переписки помечает ответы
// прочитанными: список неотвеченных — это то, что администратор ещё не открывал.
func (m *Mail) UserMail(ctx context.Context, userID uuid.UUID, limit, offset int) (*UserMail, error) {
	messages, err := m.repo.ListDirectForUser(ctx, userID, pageLimit(limit, defaultUserMailPage, 500), pageOffset(offset))
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]struct{}{}
	threads := make([]uuid.UUID, 0)
	for _, msg := range messages {
		if msg.ThreadID == nil {
			continue
		}
		if _, ok := seen[*msg.ThreadID]; ok {
			continue
		}
		seen[*msg.ThreadID] = struct{}{}
		threads = append(threads, *msg.ThreadID)
	}
	if err := m.repo.MarkThreadsReadByAdmin(ctx, threads); err != nil {
		log.Printf("[mail] cannot mark threads of %s read by admin: %v", userID, err)
	}
	out := &UserMail{Messages: messages, User: MailRecipient{ID: userID.String()}}
	if user, err := m.users.FindByID(ctx, userID); err == nil && user != nil {
		out.User.FullName = strings.TrimSpace(user.LastName + " " + user.FirstName)
		out.User.Phone = user.Phone
	}
	return out, nil
}
