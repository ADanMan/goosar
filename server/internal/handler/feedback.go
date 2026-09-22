package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var feedbackImageRegex = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)

const (
	feedbackMaxMessageLen   = 10000
	feedbackHourlyRateLimit = 10

	feedbackBodyLimit = 64 * 1024
)

type CreateFeedbackRequest struct {
	Message string `json:"message"`
	URL     string `json:"url"`

	Kind        string  `json:"kind"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type FeedbackResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, feedbackBodyLimit)
	var req CreateFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(message) > feedbackMaxMessageLen {
		writeError(w, http.StatusBadRequest, "message too long")
		return
	}

	count, err := h.Queries.CountRecentFeedbackByUser(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("count recent feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to check rate limit")
		return
	}
	if count >= feedbackHourlyRateLimit {
		writeError(w, http.StatusTooManyRequests, "too many feedback submissions, please try again later")
		return
	}

	platform, version, clientOS := middleware.ClientMetadataFromContext(r.Context())
	metadata := map[string]any{
		"url":        req.URL,
		"platform":   platform,
		"version":    version,
		"os":         clientOS,
		"user_agent": r.UserAgent(),
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {

		metaBytes = []byte("{}")
	}

	var workspaceID pgtype.UUID
	if req.WorkspaceID != nil && *req.WorkspaceID != "" {
		ws, ok := parseUUIDOrBadRequest(w, *req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		workspaceID = ws
	}

	fb, err := h.Queries.CreateFeedback(r.Context(), db.CreateFeedbackParams{
		UserID:      parseUUID(userID),
		Message:     message,
		Metadata:    metaBytes,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("create feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit feedback")
		return
	}

	slog.Info("feedback submitted", append(logger.RequestAttrs(r), "feedback_id", uuidToString(fb.ID))...)

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "general"
	}

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.FeedbackSubmitted(
		userID,
		uuidToString(fb.WorkspaceID),
		kind,
		len(message),
		feedbackImageRegex.MatchString(message),
		platform,
		version,
	))

	writeJSON(w, http.StatusCreated, FeedbackResponse{
		ID:        uuidToString(fb.ID),
		CreatedAt: timestampToString(fb.CreatedAt),
	})
}
