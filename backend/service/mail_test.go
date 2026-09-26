package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// mailBox — ящик в памяти с тем, что важно правилам: ветки, направления и
// отметки администратора. Методы, которых правила не касаются, паникуют.
type mailBox struct {
	repository.MailRepository
	letters     []*repository.Mail
	markedBatch [][]uuid.UUID
}

func (b *mailBox) Get(ctx context.Context, id uuid.UUID) (*repository.Mail, error) {
	for _, m := range b.letters {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (b *mailBox) Reply(ctx context.Context, mail *repository.Mail) error {
	mail.ID = uuid.New()
	b.letters = append(b.letters, mail)
	return nil
}

func (b *mailBox) Send(ctx context.Context, q repository.Querier, mail *repository.Mail) error {
	mail.ID = uuid.New()
	b.letters = append(b.letters, mail)
	return nil
}

func (b *mailBox) ListDirectForUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]*repository.Mail, error) {
	out := []*repository.Mail{}
	for _, m := range b.letters {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (b *mailBox) MarkThreadsReadByAdmin(ctx context.Context, ids []uuid.UUID) error {
	b.markedBatch = append(b.markedBatch, ids)
	return nil
}

func (b *mailBox) MarkThreadReadByAdmin(ctx context.Context, id uuid.UUID) error {
	panic("threads must be marked in one batch")
}

func (b *mailBox) RecipientsByRole(ctx context.Context, role string) ([]uuid.UUID, error) {
	return []uuid.UUID{uuid.New(), uuid.New()}, nil
}

func (b *mailBox) Broadcast(ctx context.Context, mail *repository.Mail, userIDs []uuid.UUID) (int, error) {
	b.letters = append(b.letters, mail)
	return len(userIDs), nil
}

// Ответить можно только в адресную переписку из собственного ящика; тема
// ответа получает один префикс Re:.
func TestMail_ReplyRules(t *testing.T) {
	owner := &repository.User{ID: uuid.New()}
	stranger := &repository.User{ID: uuid.New()}
	direct := &repository.Mail{ID: uuid.New(), UserID: owner.ID, Kind: repository.MailKindDirect, Subject: "Re: Вопрос"}
	direct.ThreadID = &direct.ID
	system := &repository.Mail{ID: uuid.New(), UserID: owner.ID, Kind: repository.MailKindAchievement, Subject: "Значок"}
	box := &mailBox{letters: []*repository.Mail{direct, system}}
	mail := NewMail(box, newMockUserRepo())
	ctx := context.Background()

	if _, err := mail.Reply(ctx, stranger, direct.ID, "чужое"); !errors.Is(err, ErrMailNotFound) {
		t.Fatalf("stranger reply: %v", err)
	}
	if _, err := mail.Reply(ctx, owner, system.ID, "спасибо"); !errors.Is(err, ErrMailNotRepliable) {
		t.Fatalf("reply to a system letter: %v", err)
	}
	if _, err := mail.Reply(ctx, owner, direct.ID, "   "); !errors.Is(err, ErrMailBodyEmpty) {
		t.Fatalf("empty reply: %v", err)
	}
	reply, err := mail.Reply(ctx, owner, direct.ID, " Ответ ")
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.Subject != "Re: Вопрос" || reply.Direction != repository.MailDirectionOut || reply.Body != "Ответ" || *reply.ThreadID != direct.ID {
		t.Fatalf("reply built wrong: %+v", reply)
	}
}

// Рассылкой уходят только новость и акция: любой другой вид становится новостью.
func TestMail_BroadcastKindOverride(t *testing.T) {
	box := &mailBox{}
	mail := NewMail(box, newMockUserRepo())
	sent, err := mail.Broadcast(context.Background(), uuid.New(), BroadcastRequest{Kind: repository.MailKindGift, Subject: "Акция", Body: "текст"})
	if err != nil || sent != 2 {
		t.Fatalf("broadcast: %d %v", sent, err)
	}
	if box.letters[0].Kind != repository.MailKindNews {
		t.Fatalf("kind %q, want NEWS", box.letters[0].Kind)
	}
	if _, err := mail.Broadcast(context.Background(), uuid.New(), BroadcastRequest{Body: "без темы"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("no subject: %v", err)
	}
}

// Открытая переписка гасит все ветки одним оператором, по разу на ветку.
func TestMail_UserMailMarksThreadsInOneBatch(t *testing.T) {
	userID := uuid.New()
	t1, t2 := uuid.New(), uuid.New()
	box := &mailBox{letters: []*repository.Mail{
		{ID: t1, UserID: userID, Kind: repository.MailKindDirect, ThreadID: &t1},
		{ID: uuid.New(), UserID: userID, Kind: repository.MailKindDirect, ThreadID: &t1},
		{ID: t2, UserID: userID, Kind: repository.MailKindDirect, ThreadID: &t2},
	}}
	out, err := NewMail(box, newMockUserRepo()).UserMail(context.Background(), userID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 3 || len(box.markedBatch) != 1 || len(box.markedBatch[0]) != 2 {
		t.Fatalf("messages=%d batches=%v", len(out.Messages), box.markedBatch)
	}
}
