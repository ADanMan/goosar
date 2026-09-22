package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

type webhookTriggerIDKeyType struct{}

var webhookTriggerIDKey = webhookTriggerIDKeyType{}

func SetWebhookTriggerID(r *http.Request, triggerID string) {
	if triggerID == "" {
		return
	}
	*r = *r.WithContext(context.WithValue(r.Context(), webhookTriggerIDKey, triggerID))
}

func webhookTriggerIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(webhookTriggerIDKey).(string)
	return v
}

const webhookIngressPathPrefix = "/api/webhooks/autopilots/"

func redactWebhookPath(path string) string {
	if !strings.HasPrefix(path, webhookIngressPathPrefix) {
		return path
	}
	rest := path[len(webhookIngressPathPrefix):]
	if rest == "" {
		return path
	}

	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return webhookIngressPathPrefix + "[redacted]" + rest[slash:]
	}
	return webhookIngressPathPrefix + "[redacted]"
}

type boundedBuffer struct {
	buf bytes.Buffer
	cap int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remain := b.cap - b.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		b.buf.Write(p[:remain])
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }

const softNotFoundBodyCaptureLimit = 256

var softNotFoundMarkers = []string{
	"runtime not found",
	"task not found",
}

const maxRequestIDLen = 64

func RequestID(next http.Handler) http.Handler {
	chained := chimw.RequestID(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get(chimw.RequestIDHeader); id != "" && !safeRequestID(id) {
			r.Header.Del(chimw.RequestIDHeader)
		}
		chained.ServeHTTP(w, r)
	})
}

func safeRequestID(id string) bool {
	if len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '/', c == '+', c == '=', c == ':':
		default:
			return false
		}
	}
	return true
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		if rid := chimw.GetReqID(r.Context()); rid != "" {
			w.Header().Set("X-Request-ID", rid)
		}

		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		bodyPrefix := &boundedBuffer{cap: softNotFoundBodyCaptureLimit}
		ww.Tee(bodyPrefix)

		next.ServeHTTP(ww, r)

		duration := time.Since(start)
		status := ww.Status()

		attrs := []any{
			"method", r.Method,
			"path", redactWebhookPath(r.URL.Path),
			"status", status,
			"duration", duration.Round(time.Microsecond).String(),
		}
		if rid := chimw.GetReqID(r.Context()); rid != "" {
			attrs = append(attrs, "request_id", rid)
		}
		if uid := r.Header.Get("X-User-ID"); uid != "" {
			attrs = append(attrs, "user_id", uid)
		}
		if tid := webhookTriggerIDFromContext(r.Context()); tid != "" {
			attrs = append(attrs, "webhook_trigger_id", tid)
		}
		if platform, version, os := ClientMetadataFromContext(r.Context()); platform != "" || version != "" || os != "" {
			if platform != "" {
				attrs = append(attrs, "client_platform", platform)
			}
			if version != "" {
				attrs = append(attrs, "client_version", version)
			}
			if os != "" {
				attrs = append(attrs, "client_os", os)
			}
		}

		switch {
		case status >= 500:
			slog.Error("http request", attrs...)
		case status == http.StatusNotFound && isSoftNotFound(bodyPrefix.Bytes()):

			slog.Info("http request", attrs...)
		case status >= 400:
			slog.Warn("http request", attrs...)
		default:
			slog.Info("http request", attrs...)
		}
	})
}

func isSoftNotFound(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	lower := strings.ToLower(string(body))
	for _, marker := range softNotFoundMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
