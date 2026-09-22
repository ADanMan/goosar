package logger

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/lmittmann/tint"

	"github.com/adanman/goosar/server/internal/middleware"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

const (
	FormatText = "text"
	FormatJSON = "json"
)

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

func envAny(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func Format() string {
	if strings.EqualFold(envAny("GOOSAR_LOG_FORMAT", "LOG_FORMAT"), FormatJSON) {
		return FormatJSON
	}
	return FormatText
}

func Level() slog.Level {
	if level, ok := parseLevel(envAny("GOOSAR_LOG_LEVEL", "LOG_LEVEL")); ok {
		return level
	}
	return defaultLevel()
}

func defaultLevel() slog.Level {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return slog.LevelInfo
	}
	return slog.LevelDebug
}

func NewHandler(w io.Writer, color bool) slog.Handler {
	level := Level()
	if Format() == FormatJSON {
		return slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: jsonTime})
	}
	return tint.NewHandler(w, &tint.Options{
		Level:      level,
		TimeFormat: timeFormat,
		NoColor:    !color,
	})
}

func jsonTime(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.TimeKey {
		return slog.String(slog.TimeKey, a.Value.Time().Format(timeFormat))
	}
	return a
}

func Startup(log *slog.Logger) {
	log.Info("logging configured", "level", strings.ToLower(Level().String()), "format", Format())
}

func Init() {
	slog.SetDefault(slog.New(NewHandler(os.Stderr, isTerminal(os.Stderr))))
	if raw := envAny("GOOSAR_LOG_LEVEL", "LOG_LEVEL"); raw != "" {
		if _, ok := parseLevel(raw); !ok {
			slog.Warn("invalid log level, using default", "value", raw, "level", strings.ToLower(Level().String()))
		}
	}
	Startup(slog.Default())
}

func NewLogger(component string) *slog.Logger {
	return slog.New(NewHandler(os.Stderr, isTerminal(os.Stderr))).With("component", component)
}

func StderrIsTerminal() bool {
	return isTerminal(os.Stderr)
}

func NewWriterLoggerDefault(component string, w io.Writer) *slog.Logger {
	base := slog.New(NewHandler(w, false))
	slog.SetDefault(base)
	return base.With("component", component)
}

func RequestAttrs(r *http.Request) []any {
	attrs := make([]any, 0, 10)
	if rid := chimw.GetReqID(r.Context()); rid != "" {
		attrs = append(attrs, "request_id", rid)
	}
	if uid := r.Header.Get("X-User-ID"); uid != "" {
		attrs = append(attrs, "user_id", uid)
	}
	platform, version, os := middleware.ClientMetadataFromContext(r.Context())
	if platform != "" {
		attrs = append(attrs, "client_platform", platform)
	}
	if version != "" {
		attrs = append(attrs, "client_version", version)
	}
	if os != "" {
		attrs = append(attrs, "client_os", os)
	}
	return attrs
}

func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
