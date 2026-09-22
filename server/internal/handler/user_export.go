package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/dataexport"
)

func (h *Handler) ExportMyData(w http.ResponseWriter, r *http.Request) {
	userID := requestUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	name := "hermes-my-data-" + time.Now().UTC().Format("20060102") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")

	manifest, err := dataexport.WriteUser(r.Context(), h.DB, userID, w, dataexport.Options{
		MaxBytes: ExportMaxBytes(),
	})
	if err != nil {
		slog.Error("subject export: streaming failed", "error", err, "user_id", userID)
		return
	}
	h.writeUserExportAudit(r, userUUID)
	slog.Info("subject export: archive streamed", "user_id", userID, "entities", len(manifest.Counts))
}

func (h *Handler) writeUserExportAudit(r *http.Request, actor pgtype.UUID) {

	h.writeUserAdminAudit(r, actor, adminAuditActionUserExport, actor)
}
