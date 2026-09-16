package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

type sentEmail struct{ to, subject, body string }

type fakeEmailSender struct {
	mu   sync.Mutex
	sent []sentEmail
}

func (f *fakeEmailSender) SendEmail(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentEmail{to, subject, body})
	return nil
}

func mailbox(t *testing.T, f *disputeFixture, userID uuid.UUID) []*repository.Mail {
	t.Helper()
	mails, err := repository.NewMailRepository(f.db).ListForUser(context.Background(), userID, 50)
	if err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	return mails
}

// Стороны спора узнают об открытии и закрытии письмом во внутреннюю почту, а
// тот, у кого e-mail подтверждён, — ещё и письмом наружу.
func TestDisputeNotificationsIntegration(t *testing.T) {
	f := newDisputeFixture(t)
	f.cleanupPenalties(t)
	ctx := context.Background()

	email := &fakeEmailSender{}
	notifier := NewDisputeNotifier(repository.NewMailRepository(f.db), repository.New(f.db), email)
	notifier.background = func(fn func()) { fn() }
	f.srv.WithPenalties(newIntegrationPenaltyService(f.db, f.srv)).WithDisputeNotifier(notifier)
	t.Cleanup(func() {
		_, _ = f.db.Exec(`DELETE FROM user_mail WHERE user_id IN ($1, $2)`, f.customerID, f.executorID)
	})

	customerEmail := "cust-" + f.customerID.String()[:8] + "@dispute.test"
	if _, err := f.db.Exec(`UPDATE users SET email = $1, email_verified = TRUE WHERE id = $2`, customerEmail, f.customerID); err != nil {
		t.Fatal(err)
	}
	// У исполнителя адрес есть, но не подтверждён — наружу ему не пишем.
	if _, err := f.db.Exec(`UPDATE users SET email = $1, email_verified = FALSE WHERE id = $2`,
		"exec-"+f.executorID.String()[:8]+"@dispute.test", f.executorID); err != nil {
		t.Fatal(err)
	}

	dispute, err := f.srv.OpenDispute(ctx, f.customerID, f.order.ID, "мешки у подъезда")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	executorMail := mailbox(t, f, f.executorID)
	if len(executorMail) != 1 || executorMail[0].Kind != repository.MailKindSystem || executorMail[0].RefID != f.order.ID.String() ||
		!strings.Contains(executorMail[0].Body, "мешки у подъезда") {
		t.Fatalf("executor mailbox after opening: %+v", executorMail)
	}
	if len(mailbox(t, f, f.customerID)) != 0 {
		t.Fatal("the customer was notified about their own dispute")
	}
	if len(email.sent) != 0 {
		t.Fatalf("e-mail sent to an unverified address: %+v", email.sent)
	}

	arbiterID := seedExecutor(t, f.db)
	if _, err := f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, repository.DisputeDecisionCustomer, "фото нет"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	customerMail := mailbox(t, f, f.customerID)
	if len(customerMail) != 1 || !strings.Contains(customerMail[0].Body, "прав заказчик") || !strings.Contains(customerMail[0].Body, "фото нет") {
		t.Fatalf("customer mailbox after resolution: %+v", customerMail)
	}
	executorMail = mailbox(t, f, f.executorID)
	if len(executorMail) != 2 || !strings.Contains(executorMail[0].Body, "штрафной балл") {
		t.Fatalf("executor mailbox after resolution: %+v", executorMail)
	}
	if len(email.sent) != 1 || email.sent[0].to != customerEmail {
		t.Fatalf("e-mails after resolution: %+v", email.sent)
	}

	// Отклонённое решение никому ничего не пишет.
	if _, err := f.srv.ResolveDispute(ctx, dispute.ID, arbiterID, repository.DisputeDecisionExecutor, ""); err == nil {
		t.Fatal("resolved a closed dispute")
	}
	if len(mailbox(t, f, f.customerID)) != 1 || len(email.sent) != 1 {
		t.Fatal("a rejected resolution sent notifications")
	}
}
