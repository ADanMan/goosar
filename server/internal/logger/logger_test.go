package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

var rfc3339Line = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}(Z|[+-]\d{2}:\d{2})`)

func TestTextHandlerStampsRFC3339WithOffset(t *testing.T) {
	t.Setenv("GOOSAR_LOG_FORMAT", "text")
	var buf bytes.Buffer
	slog.New(NewHandler(&buf, false)).Info("hello")
	if !rfc3339Line.MatchString(buf.String()) {
		t.Fatalf("text line %q does not start with an RFC3339 timestamp", buf.String())
	}
}

func TestJSONHandlerEmitsOneObjectPerLine(t *testing.T) {
	t.Setenv("GOOSAR_LOG_FORMAT", "json")
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, false)).With("component", "server")
	log.Info("hello", "request_id", "req-1", "workspace_id", "ws-1")

	line := strings.TrimSpace(buf.String())
	if strings.Contains(line, "\n") {
		t.Fatalf("json format must emit one object per line, got %q", line)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("json line not parsable: %v (%q)", err, line)
	}
	for _, key := range []string{"time", "level", "msg", "component", "request_id", "workspace_id"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("json line missing %q: %v", key, got)
		}
	}
	if _, err := time.Parse(timeFormat, got["time"].(string)); err != nil {
		t.Fatalf("time %q is not RFC3339 with offset: %v", got["time"], err)
	}
}

func TestFormatDefaultsToTextAndAcceptsBothEnvNames(t *testing.T) {
	t.Setenv("GOOSAR_LOG_FORMAT", "")
	t.Setenv("LOG_FORMAT", "")
	if got := Format(); got != FormatText {
		t.Fatalf("default format = %q, want text", got)
	}
	t.Setenv("LOG_FORMAT", "json")
	if got := Format(); got != FormatJSON {
		t.Fatalf("LOG_FORMAT=json → %q", got)
	}
	t.Setenv("GOOSAR_LOG_FORMAT", "text")
	if got := Format(); got != FormatText {
		t.Fatalf("GOOSAR_LOG_FORMAT must win over LOG_FORMAT, got %q", got)
	}
}

func TestLevelDefaultsToInfoInProduction(t *testing.T) {
	t.Setenv("GOOSAR_LOG_LEVEL", "")
	t.Setenv("LOG_LEVEL", "")

	t.Setenv("APP_ENV", "production")
	if got := Level(); got != slog.LevelInfo {
		t.Fatalf("production default level = %v, want INFO", got)
	}
	t.Setenv("APP_ENV", "development")
	if got := Level(); got != slog.LevelDebug {
		t.Fatalf("development default level = %v, want DEBUG", got)
	}
}

func TestLevelInvalidValueFallsBackToTheEnvironmentDefault(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("GOOSAR_LOG_LEVEL", "verbose")
	if got := Level(); got != slog.LevelInfo {
		t.Fatalf("invalid level on production = %v, want INFO (never DEBUG)", got)
	}
	t.Setenv("GOOSAR_LOG_LEVEL", "warn")
	if got := Level(); got != slog.LevelWarn {
		t.Fatalf("GOOSAR_LOG_LEVEL=warn → %v", got)
	}
}

func TestInitLogsEffectiveLevelAndFormat(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("GOOSAR_LOG_LEVEL", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("GOOSAR_LOG_FORMAT", "json")
	var buf bytes.Buffer
	Startup(slog.New(NewHandler(&buf, false)))
	line := buf.String()
	if !strings.Contains(line, `"level":"info"`) || !strings.Contains(line, `"format":"json"`) {
		t.Fatalf("startup line must name level and format, got %q", line)
	}
}

func TestNewWriterLoggerDefault(t *testing.T) {
	t.Setenv("GOOSAR_LOG_FORMAT", "text")

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	log := NewWriterLoggerDefault("daemon", &buf)

	log.Error("boom", "code", 42)
	slog.Warn("global-line")

	out := buf.String()
	if !strings.Contains(out, "boom") {
		t.Errorf("component logger output missing message: %q", out)
	}
	if !strings.Contains(out, "component=daemon") {
		t.Errorf("output missing component tag: %q", out)
	}
	if !strings.Contains(out, "global-line") {
		t.Errorf("global slog.Warn did not reach the writer: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("output contains ANSI color escapes, want NoColor: %q", out)
	}
}
