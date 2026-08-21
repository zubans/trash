package service

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
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

type unencryptedAuth struct {
	smtp.Auth
}

func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true
	return a.Auth.Start(&s)
}

func (m *SmtpMailSender) SendEmail(to, subject, bodyHTML string) error {
	if m.host == "" {
		log.Printf("[SmtpMailSender] SMTP_HOST not set. Logging email to stdout instead of sending:\nTo: %s\nSubject: %s\nBody: %s\n", to, subject, bodyHTML)
		return nil
	}

	addr := fmt.Sprintf("%s:%s", m.host, m.port)
	log.Printf("[SmtpMailSender] Attempting to send email via SMTP addr=%s, from=%s, to=%s", addr, m.from, to)

	msg := []byte(fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n"+
		"\r\n"+
		"%s\r\n", m.from, to, subject, bodyHTML))

	var auth smtp.Auth
	if m.user != "" && m.password != "" {
		auth = smtp.PlainAuth("", m.user, m.password, m.host)
	}

	// Standard dial & send attempt
	err := smtp.SendMail(addr, auth, m.from, []string{to}, msg)
	if err != nil {
		log.Printf("[SmtpMailSender] smtp.SendMail failed: %v. Attempting fallback dial...", err)
		if strings.Contains(err.Error(), "unencrypted connection") || strings.Contains(err.Error(), "short response") || strings.Contains(err.Error(), "535") || strings.Contains(err.Error(), "103") {
			c, dialErr := smtp.Dial(addr)
			if dialErr != nil {
				log.Printf("[SmtpMailSender] Fallback smtp.Dial failed: %v", dialErr)
				return dialErr
			}
			defer c.Close()

			if auth != nil {
				_ = c.Auth(unencryptedAuth{auth})
			}
			if err := c.Mail(m.from); err != nil {
				log.Printf("[SmtpMailSender] Fallback c.Mail failed: %v", err)
				return err
			}
			if err := c.Rcpt(to); err != nil {
				log.Printf("[SmtpMailSender] Fallback c.Rcpt failed: %v", err)
				return err
			}
			w, err := c.Data()
			if err != nil {
				log.Printf("[SmtpMailSender] Fallback c.Data failed: %v", err)
				return err
			}
			_, err = w.Write(msg)
			if err != nil {
				log.Printf("[SmtpMailSender] Fallback w.Write failed: %v", err)
				return err
			}
			err = w.Close()
			if err != nil {
				log.Printf("[SmtpMailSender] Fallback w.Close failed: %v", err)
				return err
			}
			log.Printf("[SmtpMailSender] Email sent successfully via fallback to %s", to)
			return c.Quit()
		}
	} else {
		log.Printf("[SmtpMailSender] Email sent successfully via SendMail to %s", to)
	}

	return err
}

func (m *SmtpMailSender) SendEmailVerification(toEmail, token string) error {
	verifyURL := fmt.Sprintf("%s/login?token=%s", m.baseURL, token)
	subject := "Подтверждение регистрации на портале moya-usluga.ru"
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
