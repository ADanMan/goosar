package handler

import (
	"net/http"
	"time"

	"github.com/adanman/goosar/server/internal/service"
)

const (
	cronPreviewInvalidCron     = "invalid_cron"
	cronPreviewInvalidTimezone = "invalid_timezone"
)

func writeCronPreviewError(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "code": code})
}

func (h *Handler) CronPreview(w http.ResponseWriter, r *http.Request) {
	expr := r.URL.Query().Get("expr")
	if expr == "" {
		writeCronPreviewError(w, cronPreviewInvalidCron, "expr is required")
		return
	}
	tz := r.URL.Query().Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	if err := service.ValidateTimezone(tz); err != nil {
		writeCronPreviewError(w, cronPreviewInvalidTimezone, err.Error())
		return
	}

	const previewCount = 3

	occurrences, err := service.NextOccurrencesAfterUTC(expr, tz, time.Now().UTC(), previewCount)
	if err != nil {
		writeCronPreviewError(w, cronPreviewInvalidCron, err.Error())
		return
	}
	nextRuns := make([]string, 0, len(occurrences))
	for _, at := range occurrences {
		nextRuns = append(nextRuns, at.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": nextRuns})
}
