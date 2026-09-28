// Package mail — минимальный интерфейс отправки писем для authn (коды
// входа) и workspace (приглашения). Настоящая интеграция (Resend/SMTP)
// приходит с T-029; здесь только контракт Sender и dev-заглушка, пишущая в
// лог, чтобы поднять server2 локально без внешней инфраструктуры почты.
package mail

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// Message — тело письма, которое домен просит отправить.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Validate проверяет минимально необходимую форму письма до похода в
// транспорт — пустой получатель или пустая тема почти всегда программная
// ошибка вызывающего домена, а не то, что стоит тихо отправлять дальше.
func (m Message) Validate() error {
	switch {
	case strings.TrimSpace(m.To) == "":
		return errors.New("mail: получатель (To) не может быть пустым")
	case !strings.Contains(m.To, "@"):
		return errors.New("mail: получатель (To) не похож на email")
	case strings.TrimSpace(m.Subject) == "":
		return errors.New("mail: тема (Subject) не может быть пустой")
	default:
		return nil
	}
}

// Sender — абстракция транспорта письма. Решение, ждать ли завершения Send
// и что делать при ошибке (залогировать и продолжить, или отдать её
// вызывающему), остаётся за доменом, который его вызывает.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SenderFunc позволяет подставить функцию там, где ожидается Sender —
// удобно для тестов доменов без отдельного мок-типа.
type SenderFunc func(ctx context.Context, msg Message) error

func (f SenderFunc) Send(ctx context.Context, msg Message) error { return f(ctx, msg) }

// devLogSender пишет письмо в structured log процесса вместо реальной
// отправки — этого достаточно, пока не настроен Resend/SMTP (T-029), или
// пока GOOSAR_DEV_VERIFICATION_CODE вообще снимает нужду слать почту для входа.
type devLogSender struct {
	logger *slog.Logger
}

func (d devLogSender) Send(_ context.Context, msg Message) error {
	attrs := []any{"recipient", msg.To, "subject", msg.Subject, "chars", len(msg.Body)}
	d.logger.Info("почта не настроена: письмо ушло только в лог", attrs...)
	return nil
}

// NewLoggerSender собирает dev-реализацию Sender поверх переданного логгера.
func NewLoggerSender(logger *slog.Logger) Sender {
	return devLogSender{logger: logger}
}

// Validating оборачивает next так, что Send сначала проверяет msg.Validate()
// и возвращает эту ошибку сразу, не доходя до транспорта. Домены оборачивают
// им и devLogSender, и будущий Resend/SMTP-отправитель T-029 одинаково.
func Validating(next Sender) Sender {
	return SenderFunc(func(ctx context.Context, msg Message) error {
		if err := msg.Validate(); err != nil {
			return err
		}
		return next.Send(ctx, msg)
	})
}

// Fanout рассылает одно и то же письмо через все senders по очереди и
// возвращает первую встреченную ошибку, но не прерывается на ней — полезно,
// когда письмо параллельно логируется для отладки и уходит в реальный
// транспорт (T-029: Resend как основной, лог как аудит).
func Fanout(senders ...Sender) Sender {
	return SenderFunc(func(ctx context.Context, msg Message) error {
		var first error
		for _, s := range senders {
			if err := s.Send(ctx, msg); err != nil && first == nil {
				first = err
			}
		}
		return first
	})
}
