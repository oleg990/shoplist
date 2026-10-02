package auth

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net/smtp"
	"strings"
)

// Mailer отправляет письмо с кодом входа.
type Mailer interface {
	SendLoginCode(ctx context.Context, email, code string) error
}

// LogMailer пишет код в лог. Только для разработки: в проде код попал бы в логи.
type LogMailer struct{ Log *slog.Logger }

func (m LogMailer) SendLoginCode(_ context.Context, email, code string) error {
	m.Log.Warn("dev mailer: login code (not sent by email)", "email", email, "code", code)
	return nil
}

// SMTPMailer отправляет письма через SMTP (порт 587 со STARTTLS).
type SMTPMailer struct {
	Host, Port, User, Password, From string
}

func (m SMTPMailer) SendLoginCode(_ context.Context, email, code string) error {
	subject := mime.QEncoding.Encode("utf-8", "Код входа в ShopList")
	body := fmt.Sprintf("Ваш код для входа в ShopList: %s\r\n\r\nКод действует 10 минут. Если вы его не запрашивали, просто проигнорируйте письмо.\r\n", code)
	msg := strings.Join([]string{
		"From: " + m.From,
		"To: " + email,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	var auth smtp.Auth
	if m.User != "" {
		auth = smtp.PlainAuth("", m.User, m.Password, m.Host)
	}
	return smtp.SendMail(m.Host+":"+m.Port, auth, m.From, []string{email}, []byte(msg))
}
