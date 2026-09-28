// Package mail описывает интерфейс отправки почты, используемый authn
// (коды входа) и workspace (приглашения). Реальная интеграция с
// Resend/SMTP — T-029; здесь только интерфейс и dev-реализация, которая
// пишет письма в лог, чтобы server2/cmd/server поднимался локально без
// внешней инфраструктуры.
package mail

import (
	"context"
	"log/slog"
)

// Message — простое текстовое письмо.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender отправляет письма. Ошибка отправки не должна ломать вызывающий
// HTTP-ответ там, где контракт это оговаривает (например приглашение в
// пространство отправляется асинхронно, ошибка только логируется) —
// решение о том, ждать ли Send и как реагировать на ошибку, принимает
// вызывающий домен.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LoggerSender — dev-реализация: пишет письмо в structured log вместо
// отправки. Используется, пока не настроен Resend/SMTP (T-029) или пока
// GOOSAR_DEV_VERIFICATION_CODE делает почту для логина не нужной.
type LoggerSender struct {
	Logger *slog.Logger
}

func NewLoggerSender(logger *slog.Logger) *LoggerSender {
	return &LoggerSender{Logger: logger}
}

func (s *LoggerSender) Send(_ context.Context, msg Message) error {
	s.Logger.Info("dev-письмо (email transport не настроен)",
		"to", msg.To, "subject", msg.Subject, "body", msg.Body)
	return nil
}
