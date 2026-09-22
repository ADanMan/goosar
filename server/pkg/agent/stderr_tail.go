package agent

import (
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/adanman/goosar/server/pkg/redact"
)

const agentStderrTailBytes = 2048

var (
	agentAuthorizationHeaderRe = regexp.MustCompile(`(?im)(authorization\s*:\s*)[^\r\n]+`)
	agentJSONSecretRe          = regexp.MustCompile(`(?i)("(?:token|auth|authorization|api[_-]?key|secret|password)"\s*:\s*)"(?:\\.|[^"\\])*"`)
	agentDiagnosticSecretRe    = regexp.MustCompile(`(?i)(authorization|auth|api[_-]?key|token|secret|password)(\s*[:=]\s*)([^\s,;]+)`)
)

func dropControlRunes(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}

func sanitizeAgentDiagnostic(value string) string {
	clean := dropControlRunes(value)
	clean = agentAuthorizationHeaderRe.ReplaceAllString(clean, `$1[REDACTED]`)
	clean = agentJSONSecretRe.ReplaceAllString(clean, `$1"[REDACTED]"`)
	clean = agentDiagnosticSecretRe.ReplaceAllString(clean, `$1$2[REDACTED]`)
	return redact.Text(clean)
}

type stderrTail struct {
	inner io.Writer
	max   int

	mu    sync.Mutex
	buf   []byte
	total int64
}

func newStderrTail(inner io.Writer, max int) *stderrTail {
	if max <= 0 {
		max = agentStderrTailBytes
	}
	return &stderrTail{inner: inner, max: max}
}

func (s *stderrTail) Write(p []byte) (int, error) {
	if _, err := s.inner.Write(p); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.total += int64(len(p))
	s.buf = append(s.buf, p...)
	if overflow := len(s.buf) - s.max; overflow > 0 {
		s.buf = s.buf[overflow:]
	}
	return len(p), nil
}

func (s *stderrTail) TotalBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

func (s *stderrTail) Tail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(strings.ToValidUTF8(string(s.buf), ""))
}

func withAgentStderr(msg, label, tail string) string {
	if tail == "" {
		return msg
	}
	return msg + "; " + label + " stderr: " + tail
}
