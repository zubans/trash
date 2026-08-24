package service

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"regexp"
	"strings"
)

type MailSender interface {
	SendEmailVerification(toEmail, token string) error
	SendPasswordResetCode(toEmail, code string) error
}

type SmtpMailSender struct {
	host     string
	port     string
	user     string
	password string
	from     string
	baseURL  string
}

func NewSmtpMailSender() *SmtpMailSender {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "system@moya-usluga.ru"
	}
	baseURL := os.Getenv("APP_BASE_URL")
	if baseURL == "" {
		baseURL = "https://moya-usluga.ru"
	}

	return &SmtpMailSender{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		baseURL:  baseURL,
	}
}

// validRecipient rejects addresses that could inject extra SMTP headers.
// The message is assembled by string concatenation, so a CR/LF in the address
// would let the caller add arbitrary headers such as Bcc.
var validRecipient = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func (m *SmtpMailSender) SendEmail(to, subject, bodyHTML string) error {
	to = strings.TrimSpace(to)
	if !validRecipient.MatchString(to) {
		return fmt.Errorf("invalid recipient address")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("invalid subject")
	}

	if m.host == "" {
		// The body may contain a verification token or a reset code, so it is
		// never written to the log.
		log.Printf("[SmtpMailSender] SMTP_HOST not set; refusing to send mail to %s (subject: %s)", to, subject)
		return fmt.Errorf("mail transport is not configured")
	}

	addr := fmt.Sprintf("%s:%s", m.host, m.port)
	log.Printf("[SmtpMailSender] Attempting to send email via SMTP addr=%s, from=%s, to=%s", addr, m.from, to)

	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))

	msg := []byte(fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n"+
		"\r\n"+
		"%s\r\n", m.from, to, encodedSubject, bodyHTML))

	var auth smtp.Auth
	if m.user != "" && m.password != "" {
		auth = smtp.PlainAuth("", m.user, m.password, m.host)
	}

	// A single attempt over an authenticated, encrypted connection. The previous
	// fallback path re-sent the message over a plain connection while telling the
	// standard library that TLS was on, which leaked the SMTP password in clear
	// text whenever the first attempt failed.
	if err := smtp.SendMail(addr, auth, m.from, []string{to}, msg); err != nil {
		log.Printf("[SmtpMailSender] failed to send email to %s: %v", to, err)
		return err
	}

	log.Printf("[SmtpMailSender] Email sent successfully to %s", to)
	return nil
}

func (m *SmtpMailSender) SendEmailVerification(toEmail, token string) error {
	verifyURL := fmt.Sprintf("%s/login?token=%s", m.baseURL, token)
	subject := "Подтверждение электронной почты"
	body := fmt.Sprintf(`
		<div style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #e2e8f0; border-radius: 12px;">
			<h2 style="color: #4f46e5;">Подтверждение электронной почты</h2>
			<p>Здравствуйте!</p>
			<p>Для подтверждения вашего аккаунта и активации возможностей на сервисе <strong>moya-usluga.ru</strong> перейдите по ссылке ниже:</p>
			<p style="margin: 24px 0;">
				<a href="%s" style="background-color: #4f46e5; color: #ffffff; padding: 12px 24px; text-decoration: none; border-radius: 8px; font-weight: bold; display: inline-block;">Подтвердить Email</a>
			</p>
			<p style="color: #64748b; font-size: 14px;">Или скопируйте ссылку в адресную строку браузера:<br>%s</p>
			<hr style="border: none; border-top: 1px solid #e2e8f0; margin: 20px 0;" />
			<p style="color: #94a3b8; font-size: 12px;">Это автоматическое письмо от службы поддержки system@moya-usluga.ru. Пожалуйста, не отвечайте на него.</p>
		</div>
	`, verifyURL, verifyURL)

	return m.SendEmail(toEmail, subject, body)
}

func (m *SmtpMailSender) SendPasswordResetCode(toEmail, code string) error {
	subject := "Код сброса пароля moya-usluga.ru"
	body := fmt.Sprintf(`
		<div style="font-family: Arial, sans-serif; max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #e2e8f0; border-radius: 12px;">
			<h2 style="color: #4f46e5;">Восстановление пароля</h2>
			<p>Здравствуйте!</p>
			<p>Вы запросили сброс пароля на сервисе <strong>moya-usluga.ru</strong>.</p>
			<p>Ваш одноразовый код восстановления:</p>
			<div style="background-color: #f1f5f9; font-size: 28px; font-weight: bold; letter-spacing: 4px; color: #0f172a; padding: 16px; text-align: center; border-radius: 8px; margin: 20px 0;">
				%s
			</div>
			<p style="color: #64748b; font-size: 14px;">Код действителен в течение 30 минут. Если вы не запрашивали сброс пароля, просто проигнорируйте это письмо.</p>
			<hr style="border: none; border-top: 1px solid #e2e8f0; margin: 20px 0;" />
			<p style="color: #94a3b8; font-size: 12px;">Служба безопасности moya-usluga.ru (system@moya-usluga.ru)</p>
		</div>
	`, code)

	return m.SendEmail(toEmail, subject, body)
}
