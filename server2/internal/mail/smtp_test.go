package mail

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTPServer — минимальный SMTP-сервер на net.Listener для тестов
// SMTPSender: понимает ровно ту последовательность команд, которую шлёт
// net/smtp (EHLO, опционально STARTTLS+EHLO, опционально AUTH PLAIN, MAIL
// FROM, RCPT TO, DATA, QUIT), достаточно для проверки транспорта T-029 без
// поднятия настоящего почтового сервера.
type fakeSMTPServer struct {
	ln       net.Listener
	tlsConf  *tls.Config // nil — STARTTLS не предлагается
	authWant string      // ожидаемый AUTH PLAIN identity\0user\0pass, "" — AUTH не предлагается

	mu       sync.Mutex
	dataSeen string
	authSeen bool
}

func newFakeSMTPServer(t *testing.T, tlsConf *tls.Config, authWant string) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	s := &fakeSMTPServer{ln: ln, tlsConf: tlsConf, authWant: authWant}
	go s.serveOne(t)
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTPServer) addr() (host string, port int) {
	tcp := s.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", tcp.Port
}

func (s *fakeSMTPServer) serveOne(t *testing.T) {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	s.handle(t, conn)
}

func (s *fakeSMTPServer) handle(t *testing.T, conn net.Conn) {
	defer conn.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	writeLine(rw, "220 fake.smtp.test ESMTP")

	for {
		line, err := readLine(rw)
		if err != nil {
			return
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			exts := []string{"250-fake.smtp.test"}
			if s.tlsConf != nil {
				exts = append(exts, "250-STARTTLS")
			}
			if s.authWant != "" {
				exts = append(exts, "250-AUTH PLAIN")
			}
			exts = append(exts, "250 OK")
			for _, e := range exts {
				writeLine(rw, e)
			}
		case strings.HasPrefix(upper, "STARTTLS"):
			writeLine(rw, "220 go ahead")
			tlsConn := tls.Server(conn, s.tlsConf)
			if err := tlsConn.Handshake(); err != nil {
				t.Errorf("fake smtp: tls handshake: %v", err)
				return
			}
			conn = tlsConn
			rw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN "):]))
			if err != nil {
				writeLine(rw, "501 bad base64")
				continue
			}
			s.mu.Lock()
			s.authSeen = string(raw) == s.authWant
			s.mu.Unlock()
			if string(raw) != s.authWant {
				writeLine(rw, "535 auth failed")
			} else {
				writeLine(rw, "235 auth ok")
			}
		case strings.HasPrefix(upper, "MAIL FROM"):
			writeLine(rw, "250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			writeLine(rw, "250 OK")
		case upper == "DATA":
			writeLine(rw, "354 go ahead")
			var body strings.Builder
			for {
				dl, err := readLine(rw)
				if err != nil {
					return
				}
				if dl == "." {
					break
				}
				body.WriteString(dl)
				body.WriteString("\n")
			}
			s.mu.Lock()
			s.dataSeen = body.String()
			s.mu.Unlock()
			writeLine(rw, "250 message accepted")
		case strings.HasPrefix(upper, "QUIT"):
			writeLine(rw, "221 bye")
			return
		default:
			writeLine(rw, "500 unrecognized command")
		}
	}
}

func writeLine(rw *bufio.ReadWriter, s string) {
	_, _ = rw.WriteString(s + "\r\n")
	_ = rw.Flush()
}

func readLine(rw *bufio.ReadWriter) (string, error) {
	line, err := rw.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// selfSignedTLSConfig генерирует одноразовый сертификат для STARTTLS — ровно
// столько, сколько нужно crypto/tls, чтобы завершить handshake на стороне
// тестового сервера, плюс пул, которым клиент (SMTPSender.RootCAs) объявляет
// этот сертификат доверенным вместо системного пула ОС.
func selfSignedTLSConfig(t *testing.T) (serverConf *tls.Config, clientRoots *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("tls.X509KeyPair: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return &tls.Config{Certificates: []tls.Certificate{cert}}, roots
}

func TestSMTPSenderPlainNoSecurity(t *testing.T) {
	srv := newFakeSMTPServer(t, nil, "")
	host, port := srv.addr()

	sender := NewSMTPSender(host, port, "", "", "none", "Goosar <noreply@goosar.test>")
	err := sender.Send(context.Background(), Message{To: "user@example.test", Subject: "Тест", Body: "Привет"})
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	time.Sleep(20 * time.Millisecond) // дать серверной горутине дописать dataSeen
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.Contains(srv.dataSeen, "Привет") {
		t.Errorf("dataSeen = %q, expected it to contain the body", srv.dataSeen)
	}
	if !strings.Contains(srv.dataSeen, "Subject: =?UTF-8?B?") {
		t.Errorf("dataSeen = %q, expected an RFC 2047 encoded subject", srv.dataSeen)
	}
}

func TestSMTPSenderStartTLSAndAuth(t *testing.T) {
	tlsConf, clientRoots := selfSignedTLSConfig(t)
	srv := newFakeSMTPServer(t, tlsConf, "\x00bob\x00s3cret")
	host, port := srv.addr()

	sender := NewSMTPSender(host, port, "bob", "s3cret", "starttls", "noreply@goosar.test")
	sender.RootCAs = clientRoots
	err := sender.Send(context.Background(), Message{To: "user@example.test", Subject: "s", Body: "b"})
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.authSeen {
		t.Error("expected the server to see a successful AUTH PLAIN")
	}
}

func TestSMTPSenderRequiresHost(t *testing.T) {
	sender := NewSMTPSender("", 587, "", "", "starttls", "noreply@goosar.test")
	if err := sender.Send(context.Background(), Message{To: "a@b.test", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("Send: expected error when SMTP_HOST is empty")
	}
}

func TestBareAddress(t *testing.T) {
	cases := map[string]string{
		"Goosar <noreply@goosar.test>": "noreply@goosar.test",
		"noreply@goosar.test":          "noreply@goosar.test",
	}
	for in, want := range cases {
		if got := bareAddress(in); got != want {
			t.Errorf("bareAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeHeaderWord(t *testing.T) {
	if got := encodeHeaderWord("plain ascii"); got != "plain ascii" {
		t.Errorf("ascii subject should pass through unchanged, got %q", got)
	}
	got := encodeHeaderWord("Код входа")
	if !strings.HasPrefix(got, "=?UTF-8?B?") || !strings.HasSuffix(got, "?=") {
		t.Errorf("expected RFC 2047 encoded-word, got %q", got)
	}
}
