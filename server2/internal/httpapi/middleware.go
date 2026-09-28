package httpapi

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// WithCommonMiddleware оборачивает handler request-id, структурным
// логированием и восстановлением после паники — общее для всех маршрутов,
// включая заглушки.
func WithCommonMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		r = r.WithContext(WithRequestID(r.Context(), reqID))

		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic obrabotki zaprosa", "err", rec, "path", r.URL.Path, "request_id", reqID)
				WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			}
		}()

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", reqID,
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Hijack пробрасывает http.Hijacker к обёрнутому ResponseWriter. Без этого
// метода statusWriter (embedding даёт его только "по имени", не по
// интерфейсу — http.ResponseWriter в интерфейсе Hijacker не участвует)
// маскирует Hijacker нижнего ResponseWriter, и любой апгрейд поверх этого
// соединения (в частности /ws — WithCommonMiddleware стоит в цепочке перед
// роутером для всех маршрутов, включая realtime.Register) получает от
// coder/websocket.Accept `501 Not Implemented` вместо апгрейда, потому что
// оно явно проверяет ResponseWriter на http.Hijacker и вслепую сдаётся, если
// его нет.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("httpapi: нижний http.ResponseWriter не поддерживает Hijack")
	}
	return hj.Hijack()
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithCORS отражает разрешённый Origin (FRONTEND_ORIGIN) и обрабатывает
// preflight-запросы. Cookie-аутентификация требует credentials, поэтому
// Access-Control-Allow-Origin не может быть "*".
func WithCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowedOrigin == "*" || origin == allowedOrigin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-Workspace-ID, X-Workspace-Slug, X-Client-Version")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
