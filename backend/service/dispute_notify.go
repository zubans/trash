package service

import (
	"context"
	"fmt"
	"html"
	"log"
	"strings"

	"github.com/google/uuid"

	"healthlogin/backend/repository"
)

// EmailSender отправляет одно письмо наружу. Ему удовлетворяет *SmtpMailSender.
type EmailSender interface {
	SendEmail(to, subject, bodyHTML string) error
}

// DisputeNotifier сообщает сторонам спора о его открытии и закрытии: письмом во
// внутреннюю почту и, если адрес подтверждён, на e-mail.
//
// Уведомления отправляются после коммита действия, а не внутри него:
// откаченное решение не должно никому ничего сообщить, а упавшая отправка — не
// должна откатывать решение. Внутренняя почта — надёжный канал, e-mail — лишь
// дублирующий: он уходит в фоне, и его сбой только пишется в лог.
type DisputeNotifier struct {
	mail  repository.MailRepository
	users repository.UserRepository
	email EmailSender
	// background запускает отправку e-mail. По умолчанию — горутина; тесты
	// подменяют на синхронный вызов.
	background func(func())
}

// NewDisputeNotifier создаёт DisputeNotifier. email может быть nil — тогда
// письма уходят только во внутреннюю почту.
func NewDisputeNotifier(mail repository.MailRepository, users repository.UserRepository, email EmailSender) *DisputeNotifier {
	return &DisputeNotifier{mail: mail, users: users, email: email, background: func(fn func()) { go fn() }}
}

// WithDisputeNotifier подключает уведомления по спорам.
func (s *OrderService) WithDisputeNotifier(n *DisputeNotifier) *OrderService {
	s.disputeNotifier = n
	return s
}

// orderRef — короткий номер заказа, по которому его узнают в письме.
func orderRef(orderID uuid.UUID) string {
	return "№" + strings.ToUpper(orderID.String()[:8])
}

// DisputeOpened уведомляет исполнителя: заказчик оспорил выполнение.
func (n *DisputeNotifier) DisputeOpened(ctx context.Context, d *repository.Dispute) {
	if n == nil || d == nil {
		return
	}
	n.send(ctx, d.ExecutorID, d.OrderID,
		"Заказчик оспорил выполнение заказа "+orderRef(d.OrderID),
		fmt.Sprintf("Заказчик заявил, что заказ %s не выполнен: «%s».\n\n"+
			"Спор передан на разбор. Если заказ действительно не выполнен, признайте это в карточке заказа: "+
			"заказ будет отменён без штрафного балла. Если заказ выполнен — дождитесь решения; заказчик может "+
			"закрыть спор сам, подтвердив выполнение.", orderRef(d.OrderID), d.Claim))
}

// DisputeClosed уведомляет обе стороны о закрытии спора — каждую своими словами.
func (n *DisputeNotifier) DisputeClosed(ctx context.Context, d *repository.Dispute) {
	if n == nil || d == nil {
		return
	}
	ref := orderRef(d.OrderID)
	subject := "Спор по заказу " + ref + " закрыт"
	var toCustomer, toExecutor string

	switch d.Closure {
	case repository.DisputeClosureCustomerConfirmed:
		toCustomer = "Вы подтвердили выполнение заказа " + ref + ". Спор закрыт, оплата передана исполнителю."
		toExecutor = "Заказчик подтвердил выполнение заказа " + ref + ". Спор закрыт, оплата зачислена."
	case repository.DisputeClosureExecutorConceded:
		toCustomer = "Исполнитель признал, что заказ " + ref + " не выполнен. Заказ отменён, деньги возвращены на ваш баланс."
		toExecutor = "Вы признали, что заказ " + ref + " не выполнен. Заказ отменён, штрафной балл не начислен."
	case repository.DisputeClosureArbitration:
		switch d.Decision {
		case repository.DisputeDecisionExecutor:
			toCustomer = "Решение по заказу " + ref + ": прав исполнитель. Оплата передана исполнителю, вам начислен штрафной балл."
			toExecutor = "Решение по заказу " + ref + ": прав исполнитель. Оплата зачислена."
		case repository.DisputeDecisionCustomer:
			toCustomer = "Решение по заказу " + ref + ": прав заказчик. Заказ отменён, деньги возвращены на ваш баланс."
			toExecutor = "Решение по заказу " + ref + ": прав заказчик. Заказ отменён, вам начислен штрафной балл."
		case repository.DisputeDecisionUnknown:
			toCustomer = "Решение по заказу " + ref + ": установить, кто прав, не удалось. Деньги возвращены на ваш баланс, вам начислен штрафной балл."
			toExecutor = "Решение по заказу " + ref + ": установить, кто прав, не удалось. Оплата зачислена, вам начислен штрафной балл."
		}
		if d.ResolutionNote != "" {
			note := "\n\nКомментарий арбитра: «" + d.ResolutionNote + "»."
			toCustomer += note
			toExecutor += note
		}
	}
	if toCustomer == "" {
		return
	}
	n.send(ctx, d.CustomerID, d.OrderID, subject, toCustomer)
	n.send(ctx, d.ExecutorID, d.OrderID, subject, toExecutor)
}

func (n *DisputeNotifier) send(ctx context.Context, userID, orderID uuid.UUID, subject, body string) {
	if n.mail != nil {
		if err := n.mail.Send(ctx, nil, &repository.Mail{
			UserID:  userID,
			Kind:    repository.MailKindSystem,
			Subject: subject,
			Body:    body,
			RefType: "order",
			RefID:   orderID.String(),
		}); err != nil {
			log.Printf("[dispute] cannot mail user %s about order %s: %v", userID, orderID, err)
		}
	}
	if n.email == nil || n.users == nil {
		return
	}
	user, err := n.users.FindByID(ctx, userID)
	if err != nil || user == nil || user.Email == "" || !user.EmailVerified {
		return
	}
	address := user.Email
	htmlBody := "<div style=\"font-family: Arial, sans-serif; max-width: 600px;\"><h3>" + html.EscapeString(subject) + "</h3><p>" +
		strings.ReplaceAll(html.EscapeString(body), "\n", "<br>") + "</p></div>"
	n.background(func() {
		if err := n.email.SendEmail(address, subject, htmlBody); err != nil {
			log.Printf("[dispute] cannot e-mail user %s about order %s: %v", userID, orderID, err)
		}
	})
}
