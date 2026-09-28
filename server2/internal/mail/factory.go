package mail

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/config"
)

// FromConfig выбирает транспорт письма по MAIL_PROVIDER (T-029, см.
// server2/docs/decisions.md раздел T-029 за выбор имени переменной):
// "resend" | "smtp" | иначе — только dev-логгер (server2/docs/adr/0001-stack.md).
// Логгер всегда подключён вторым получателем через Fanout, так что письмо
// видно в structured log процесса, даже когда настроен реальный транспорт —
// удобно при локальной отладке и не требует отдельного почтового ящика.
// Validating заворачивает результат снаружи один раз, а не на каждый
// транспорт по отдельности.
func FromConfig(cfg config.Config, logger *slog.Logger) Sender {
	logSender := NewLoggerSender(logger)
	from := formatFrom(cfg.MailFromName, cfg.MailFromEmail)

	switch cfg.MailProvider {
	case "resend":
		return Validating(Fanout(NewResendSender(cfg.ResendAPIKey, from), logSender))
	case "smtp":
		return Validating(Fanout(NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPSecurity, from), logSender))
	default:
		return Validating(logSender)
	}
}

// Configured — true, если MAIL_PROVIDER называет настоящий транспорт (не
// только dev-логгер). authn.handleSendCode использует это, чтобы честно
// ответить 503 "email delivery not configured on this instance" (contract:
// POST /auth/send-code) вместо того чтобы промолчать письмом только в лог.
func Configured(cfg config.Config) bool {
	return cfg.MailProvider == "resend" || cfg.MailProvider == "smtp"
}

func formatFrom(name, email string) string {
	if name == "" {
		return email
	}
	return name + " <" + email + ">"
}
