package service

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/resend/resend-go/v2"

	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

const maxSubjectFieldRunes = 60

const (
	TransportSMTP   = "smtp"
	TransportResend = "resend"

	TransportDev = "dev"

	TransportNone = "none"
)

var ErrEmailNotConfigured = errors.New("email transport is not configured")

type EmailService struct {
	client          *resend.Client
	fromEmail       string
	smtpHost        string
	smtpPort        string
	smtpUsername    string
	smtpPassword    string
	smtpTLSInsecure bool
	smtpTLSImplicit bool
	smtpEHLOName    string
}

type smtpAuthClient interface {
	Auth(smtp.Auth) error
	Extension(string) (bool, string)
}

type smtpClientAdapter struct {
	client *smtp.Client
}

func (a smtpClientAdapter) Auth(auth smtp.Auth) error {
	return a.client.Auth(auth)
}

func (a smtpClientAdapter) Extension(name string) (bool, string) {
	return a.client.Extension(name)
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}

type loginAuth struct {
	username string
	password string
	host     string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLocalhost(server.Name) {
		return "", nil, fmt.Errorf("unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, fmt.Errorf("wrong host name: %q does not match expected %q", server.Name, a.host)
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}

	raw := strings.TrimSpace(string(fromServer))
	challenge := strings.ToLower(raw)
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
		challenge = strings.ToLower(strings.TrimSpace(string(decoded)))
	}

	switch {
	case strings.Contains(challenge, "username") || strings.Contains(challenge, "user name"):
		return []byte(a.username), nil
	case strings.Contains(challenge, "password"):
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected LOGIN challenge %q", raw)
	}
}

func smtpAuthWithFallback(c smtpAuthClient, host, username, password string) (bool, error) {
	plainErr := c.Auth(smtp.PlainAuth("", username, password, host))
	if plainErr == nil {
		return false, nil
	}

	msg := strings.ToLower(plainErr.Error())
	if !strings.Contains(msg, "unrecognized authentication type") && !strings.Contains(msg, "504 5.7.4") {
		return false, plainErr
	}

	ok, authLine := c.Extension("AUTH")
	if !ok || !strings.Contains(strings.ToUpper(authLine), "LOGIN") {
		return false, plainErr
	}
	return true, plainErr
}

func resolveFromEmail(smtpHost string) string {
	resendFrom := strings.TrimSpace(os.Getenv("RESEND_FROM_EMAIL"))
	if smtpHost == "" {
		if resendFrom != "" {
			return resendFrom
		}
		return "noreply@goosar.ru"
	}
	if smtpFrom := strings.TrimSpace(os.Getenv("SMTP_FROM_EMAIL")); smtpFrom != "" {
		return smtpFrom
	}
	return resendFrom
}

func strictMailEnv() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return true
	}
	profile, err := deliveryprofile.FromEnv()
	return err == nil && profile.IsPerimeter()
}

func (s *EmailService) Transport() string {
	switch {
	case s.smtpHost != "":
		return TransportSMTP
	case s.client != nil:
		return TransportResend
	case strictMailEnv():
		return TransportNone
	default:
		return TransportDev
	}
}

func ValidateEmailConfig() error {
	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if smtpHost == "" {
		return nil
	}
	if strings.TrimSpace(resolveFromEmail(smtpHost)) == "" {
		return errors.New("SMTP_HOST is set but no sender address is configured — set SMTP_FROM_EMAIL (or RESEND_FROM_EMAIL)")
	}
	return nil
}

func (s *EmailService) openSMTPClient() (*smtp.Client, error) {
	addr := net.JoinHostPort(s.smtpHost, s.smtpPort)

	tlsCfg := &tls.Config{
		ServerName:         s.smtpHost,
		InsecureSkipVerify: s.smtpTLSInsecure, //nolint:gosec // opt-in via SMTP_TLS_INSECURE=true
	}

	var conn net.Conn
	var err error
	if s.smtpTLSImplicit {
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if err = conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, s.smtpHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}

	if s.smtpEHLOName != "" {
		if err = c.Hello(s.smtpEHLOName); err != nil {
			c.Close()
			return nil, fmt.Errorf("smtp EHLO %s: %w", s.smtpEHLOName, err)
		}
	}

	if !s.smtpTLSImplicit {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(tlsCfg); err != nil {
				c.Close()
				return nil, fmt.Errorf("smtp starttls: %w", err)
			}
		}
	}

	return c, nil
}

func NewEmailService() *EmailService {
	apiKey := os.Getenv("RESEND_API_KEY")
	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if smtpPort == "" {
		smtpPort = "25"
	}
	smtpUsername := os.Getenv("SMTP_USERNAME")
	smtpPassword := os.Getenv("SMTP_PASSWORD")
	smtpTLSInsecure := os.Getenv("SMTP_TLS_INSECURE") == "true"
	from := resolveFromEmail(smtpHost)

	var smtpEHLOName string
	if smtpHost != "" {
		smtpEHLOName = strings.TrimSpace(os.Getenv("SMTP_EHLO_NAME"))
		if smtpEHLOName == "" {
			hostname, hostErr := os.Hostname()
			if hostErr != nil {

				fmt.Fprintf(os.Stderr, "EmailService: os.Hostname() failed (%v); SMTP EHLO falls back to \"localhost\" — set SMTP_EHLO_NAME for strict relays\n", hostErr)
			}
			smtpEHLOName = hostname
		}
	}

	smtpTLSMode := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS")))
	smtpTLSImplicit := smtpTLSMode == "implicit" || smtpTLSMode == "smtps" || smtpTLSMode == "ssl"
	if smtpTLSMode == "" && smtpPort == "465" {
		smtpTLSImplicit = true
	}
	if smtpTLSMode != "" && !smtpTLSImplicit && smtpTLSMode != "starttls" {
		fmt.Fprintf(os.Stderr, "EmailService: SMTP_TLS=%q not recognized, falling back to starttls\n", smtpTLSMode)
	}

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	switch {
	case smtpHost != "":
		tlsLabel := "starttls"
		if smtpTLSImplicit {
			tlsLabel = "implicit-tls"
		}
		fmt.Fprintf(os.Stderr, "EmailService: SMTP relay %s:%s (%s) from=%s\n", smtpHost, smtpPort, tlsLabel, from)
	case client != nil:
		fmt.Fprintf(os.Stderr, "EmailService: Resend API from=%s\n", from)
	default:
		fmt.Fprintln(os.Stderr, "EmailService: DEV mode — codes printed to the container log (set GOOSAR_DEV_VERIFICATION_CODE in .env for a fixed local code)")
	}

	return &EmailService{
		client:          client,
		fromEmail:       from,
		smtpHost:        smtpHost,
		smtpPort:        smtpPort,
		smtpUsername:    smtpUsername,
		smtpPassword:    smtpPassword,
		smtpTLSInsecure: smtpTLSInsecure,
		smtpTLSImplicit: smtpTLSImplicit,
		smtpEHLOName:    smtpEHLOName,
	}
}

func (s *EmailService) sendSMTP(to, subject, htmlBody string) error {
	if strings.TrimSpace(s.fromEmail) == "" {
		return fmt.Errorf("SMTP_FROM_EMAIL or RESEND_FROM_EMAIL is required when SMTP_HOST is set")
	}

	c, err := s.openSMTPClient()
	if err != nil {
		return err
	}
	defer c.Close()

	if s.smtpUsername != "" {
		fallbackToLogin, authErr := smtpAuthWithFallback(smtpClientAdapter{client: c}, s.smtpHost, s.smtpUsername, s.smtpPassword)
		if authErr != nil {
			if !fallbackToLogin {
				return fmt.Errorf("smtp auth: %w", authErr)
			}

			c.Close()
			c, err = s.openSMTPClient()
			if err != nil {
				return fmt.Errorf("smtp auth: plain auth failed (%v); login reconnect failed: %w", authErr, err)
			}
			defer c.Close()

			if err = c.Auth(&loginAuth{username: s.smtpUsername, password: s.smtpPassword, host: s.smtpHost}); err != nil {
				return fmt.Errorf("smtp auth: plain auth failed (%v); login auth fallback failed: %w", authErr, err)
			}
		}
	}

	has8Bit, _ := c.Extension("8BITMIME")
	encodedSubject := mime.QEncoding.Encode("utf-8", subject)
	msgID := fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), s.smtpHost)

	var bodyBytes []byte
	var cte string
	if has8Bit {
		bodyBytes = []byte(htmlBody)
		cte = "8bit"
	} else {
		var buf strings.Builder
		qpw := quotedprintable.NewWriter(&buf)
		_, _ = qpw.Write([]byte(htmlBody))
		_ = qpw.Close()
		bodyBytes = []byte(buf.String())
		cte = "quoted-printable"
	}

	if err = c.Mail(s.fromEmail); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp RCPT TO <%s>: %w", to, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	headers := "From: " + s.fromEmail + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + encodedSubject + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: " + msgID + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: " + cte + "\r\n" +
		"\r\n"
	if _, err = fmt.Fprintf(w, "%s%s", headers, bodyBytes); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp end data: %w", err)
	}
	return c.Quit()
}

func loginLinkURLs(linkToken string) (webURL, deepURL string) {
	if linkToken == "" {
		return "", ""
	}

	origin := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if origin == "" {
		return "", ""
	}
	escaped := url.QueryEscape(linkToken)
	return origin + "/auth/link#lt=" + escaped,
		"goosar://auth/callback?link_token=" + escaped
}

func (s *EmailService) SendVerificationCode(to, code, linkToken, lang string) error {
	webURL, deepURL := loginLinkURLs(linkToken)
	subject, body := buildVerificationEmail(code, webURL, deepURL, lang)

	if s.smtpHost != "" {
		return s.sendSMTP(to, subject, body)
	}
	if s.client == nil {
		if strictMailEnv() {
			return ErrEmailNotConfigured
		}

		if webURL != "" {
			fmt.Fprintf(os.Stderr, "[DEV] Verification code for %s: %s (login link: %s)\n", to, code, webURL)
		} else {
			fmt.Fprintf(os.Stderr, "[DEV] Verification code for %s: %s\n", to, code)
		}
		return nil
	}
	params := &resend.SendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: subject,
		Html:    body,
	}
	_, err := s.client.Emails.Send(params)
	return err
}

func (s *EmailService) SendInvitationEmail(to, inviterName, workspaceName, invitationID, lang string) error {

	var inviteURL string
	if appURL := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")); appURL != "" {
		inviteURL = fmt.Sprintf("%s/invite/%s", appURL, invitationID)
	}

	if s.smtpHost != "" {
		params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL, lang)
		return s.sendSMTP(to, params.Subject, params.Html)
	}
	if s.client == nil {
		if strictMailEnv() {
			return ErrEmailNotConfigured
		}
		fmt.Fprintf(os.Stderr, "[DEV] Invitation email to %s: %s invited you to %s — %s\n", to, inviterName, workspaceName, inviteURL)
		return nil
	}
	params := buildInvitationParams(s.fromEmail, to, inviterName, workspaceName, inviteURL, lang)
	_, err := s.client.Emails.Send(params)
	return err
}

func sanitizeSubjectField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := b.String()
	if utf8.RuneCountInString(cleaned) <= maxSubjectFieldRunes {
		return cleaned
	}
	runes := []rune(cleaned)
	return string(runes[:maxSubjectFieldRunes-1]) + "…"
}
