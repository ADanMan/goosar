package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/auth"
)

const AttachmentDownloadTicketParam = "token"

const attachmentDownloadIDParam = "id"

func AttachmentDownloadAuth(sessionOnly ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {

		sessionChain := next
		for i := len(sessionOnly) - 1; i >= 0; i-- {
			sessionChain = sessionOnly[i](sessionChain)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimSpace(r.URL.Query().Get(AttachmentDownloadTicketParam))
			if raw == "" {
				sessionChain.ServeHTTP(w, r)
				return
			}

			ticket, err := auth.VerifyAttachmentDownloadTicket(raw, time.Now())
			if err != nil {
				slog.Warn("attachment download ticket rejected", "path", r.URL.Path, "error", err)
				writeDownloadNavigationError(w, http.StatusUnauthorized, "invalid download ticket")
				return
			}

			if !strings.EqualFold(ticket.AttachmentID, strings.TrimSpace(chi.URLParam(r, attachmentDownloadIDParam))) {
				slog.Warn("attachment download ticket used for a different attachment", "path", r.URL.Path)
				writeDownloadNavigationError(w, http.StatusUnauthorized, "invalid download ticket")
				return
			}

			r.Header.Del("X-Actor-Source")
			r.Header.Set("X-User-ID", ticket.UserID)
			next.ServeHTTP(w, r)
		})
	}
}

func writeDownloadNavigationError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Disposition", "inline")

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	writeError(w, status, msg)
}
