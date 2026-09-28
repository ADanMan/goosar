package mail

import (
	"log/slog"

	"github.com/adanman/goosar/server2/internal/config"
)

// FromConfig выбирает транспорт письма (contract, «Почта»): SMTP
// приоритетнее Resend, если задан SMTP_HOST, иначе Resend, если задан
// RESEND_API_KEY, иначе — только dev-логгер (server2/docs/adr/0001-stack.md).
// Config.MailProvider() — единая точка этого выбора (используется и здесь, и
// в Configured, и в /api/config).
// Логгер всегда подключён вторым получателем через Fanout, так что письмо
// видно в structured log процесса, даже когда настроен реальный транспорт —
// удобно при локальной отладке и не требует отдельного почтового ящика.
func FromConfig(cfg config.Config, logger *slog.Logger) Sender {
	logSender := NewLoggerSender(logger)

	switch cfg.MailProvider() {
	case "resend":
		return Validating(Fanout(NewResendSender(cfg.ResendAPIKey, cfg.ResendFromEmail), logSender))
	case "smtp":
		return Validating(Fanout(NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPTLS, cfg.SMTPFromEmail, cfg.SMTPTLSInsecure, cfg.SMTPEhloName), logSender))
	default:
		return Validating(logSender)
	}
}

// Configured — true, если настроен настоящий транспорт (не только
// dev-логгер). authn.handleSendCode использует это, чтобы честно ответить
// 503 "email delivery not configured on this instance" (contract: POST
// /auth/send-code) вместо того чтобы промолчать письмом только в лог.
func Configured(cfg config.Config) bool {
	p := cfg.MailProvider()
	return p == "resend" || p == "smtp"
}
