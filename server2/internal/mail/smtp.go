package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPSecurity — как шифруется соединение с сервером (SMTP_SECURITY).
type SMTPSecurity string

const (
	SMTPSecurityStartTLS SMTPSecurity = "starttls" // обычное соединение, затем STARTTLS (по умолчанию)
	SMTPSecurityTLS      SMTPSecurity = "tls"      // TLS с самого подключения (implicit TLS, обычно порт 465)
	SMTPSecurityNone     SMTPSecurity = "none"     // без шифрования — только для локальных/тестовых серверов
)

// SMTPSender — транспорт почты поверх net/smtp (T-029): своя реализация на
// стандартной библиотеке, а не стороннем клиенте — net/smtp уже умеет EHLO/
// STARTTLS/AUTH PLAIN, чего этому транспорту достаточно (см.
// server2/docs/adr/0001-stack.md).
type SMTPSender struct {
	Host        string
	Port        int
	Username    string
	Password    string
	Security    SMTPSecurity
	From        string
	DialTimeout time.Duration

	// RootCAs — доверенные CA поверх системного пула; nil означает "только
	// системный пул" (обычный случай). Позволяет тестам (и деплоям с
	// собственным internal CA) не трогать доверие ОС. Не читается из
	// окружения — SMTP_SECURITY/host/port достаточно для контракта T-029,
	// это поле только для встраивания в код (тесты, будущие деплои).
	RootCAs *x509.CertPool
}

// NewSMTPSender собирает отправитель; security, не совпадающий ни с одним
// известным значением, тихо сводится к SMTPSecurityStartTLS (наиболее
// распространённый режим у почтовых провайдеров).
func NewSMTPSender(host string, port int, username, password, security, from string) *SMTPSender {
	sec := SMTPSecurity(strings.ToLower(strings.TrimSpace(security)))
	switch sec {
	case SMTPSecurityTLS, SMTPSecurityNone:
	default:
		sec = SMTPSecurityStartTLS
	}
	return &SMTPSender{
		Host: host, Port: port, Username: username, Password: password,
		Security: sec, From: from, DialTimeout: 10 * time.Second,
	}
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if s.Host == "" {
		return errors.New("mail: smtp: SMTP_HOST не настроен")
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	dialer := &net.Dialer{Timeout: s.DialTimeout}

	var conn net.Conn
	var err error
	if s.Security == SMTPSecurityTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.Host, RootCAs: s.RootCAs})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: smtp: соединение с %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: smtp: приветствие сервера: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.Security == SMTPSecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.Host, RootCAs: s.RootCAs}); err != nil {
				return fmt.Errorf("mail: smtp: STARTTLS: %w", err)
			}
		}
	}

	if s.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
				return fmt.Errorf("mail: smtp: аутентификация: %w", err)
			}
		}
	}

	envelopeFrom := bareAddress(s.From)
	if err := client.Mail(envelopeFrom); err != nil {
		return fmt.Errorf("mail: smtp: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("mail: smtp: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: smtp: DATA: %w", err)
	}
	if _, err := w.Write(buildRFC822(s.From, msg)); err != nil {
		_ = w.Close()
		return fmt.Errorf("mail: smtp: запись тела письма: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: smtp: завершение письма: %w", err)
	}
	return client.Quit()
}

// bareAddress извлекает голый email из "Имя <email>" для конверта SMTP
// (MAIL FROM не принимает отображаемое имя); значение как есть, если это уже
// голый адрес или разбор не удался.
func bareAddress(from string) string {
	if addr, err := netmail.ParseAddress(from); err == nil {
		return addr.Address
	}
	return from
}

// buildRFC822 собирает минимальное RFC 822 сообщение: только заголовки,
// нужные получателю (From/To/Subject/Date/MIME), plain text UTF-8 — письма
// этого пакета не нуждаются в HTML-версии.
func buildRFC822(from string, msg Message) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", encodeHeaderWord(msg.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(dotStuff(msg.Body))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// encodeHeaderWord кодирует заголовок как RFC 2047 encoded-word (=?UTF-8?B?...?=),
// если он содержит не-ASCII (русский текст темы) — иначе возвращает как есть.
func encodeHeaderWord(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}

// dotStuff экранирует строки тела, начинающиеся с "." (SMTP DATA считает
// одинокую точку в начале строки концом сообщения) — редкий случай для этих
// писем, но дешёвый и правильный по протоколу.
func dotStuff(body string) string {
	if !strings.Contains(body, "\n.") && !strings.HasPrefix(body, ".") {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}
	return strings.Join(lines, "\n")
}
