package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	cloudWaitlistReasonMaxLen = 500

	patchOnboardingBodyLimit = 16 * 1024
)

type completeOnboardingRequest struct {
	CompletionPath string `json:"completion_path,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
}

var validCompletionPaths = map[string]struct{}{
	analytics.OnboardingPathFull:           {},
	analytics.OnboardingPathRuntimeSkipped: {},
	analytics.OnboardingPathCloudWaitlist:  {},
	analytics.OnboardingPathSkipExisting:   {},
	analytics.OnboardingPathInviteAccept:   {},
}

func (h *Handler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req completeOnboardingRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if req.WorkspaceID != "" {
		wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		req.WorkspaceID = uuidToString(wsUUID)
	}

	before, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid

	user, err := h.Queries.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("complete onboarding: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}

	if firstCompletion {
		path := req.CompletionPath
		if _, ok := validCompletionPaths[path]; !ok {
			path = analytics.OnboardingPathUnknown
		}
		onboardedAt := ""
		if user.OnboardedAt.Valid {
			onboardedAt = user.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			path,
			onboardedAt,
			user.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type patchOnboardingRequest struct {
	Questionnaire *json.RawMessage `json:"questionnaire,omitempty"`
}

type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(data []byte) error {

	if len(data) == 0 || string(data) == "null" {
		*s = nil
		return nil
	}

	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	if single == "" {
		*s = nil
		return nil
	}
	*s = []string{single}
	return nil
}

type questionnaireAnswers struct {
	Source         stringOrSlice `json:"source"`
	SourceOther    string        `json:"source_other"`
	SourceSkipped  bool          `json:"source_skipped"`
	Role           string        `json:"role"`
	RoleOther      string        `json:"role_other"`
	RoleSkipped    bool          `json:"role_skipped"`
	UseCase        stringOrSlice `json:"use_case"`
	UseCaseOther   string        `json:"use_case_other"`
	UseCaseSkipped bool          `json:"use_case_skipped"`
	Version        int           `json:"version"`
}

func (q questionnaireAnswers) sourceResolved() bool {
	return len(q.Source) > 0 || q.SourceSkipped
}

func (q questionnaireAnswers) roleResolved() bool {
	return q.Role != "" || q.RoleSkipped
}

func (q questionnaireAnswers) useCaseResolved() bool {
	return len(q.UseCase) > 0 || q.UseCaseSkipped
}

const questionnaireSchemaVersion = 2

func (q questionnaireAnswers) complete() bool {
	if q.Version != questionnaireSchemaVersion {
		return false
	}
	return q.roleResolved() && q.useCaseResolved()
}

func (h *Handler) PatchOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, patchOnboardingBodyLimit)
	var req patchOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var before questionnaireAnswers
	beforeRaw := []byte("{}")
	if beforeUser, err := h.Queries.GetUser(r.Context(), parseUUID(userID)); err == nil {
		beforeRaw = beforeUser.OnboardingQuestionnaire
		_ = json.Unmarshal(beforeRaw, &before)
	}

	firstTouch := len(beforeRaw) == 0 || string(beforeRaw) == "null" || string(beforeRaw) == "{}"

	params := db.PatchUserOnboardingParams{ID: parseUUID(userID)}
	if req.Questionnaire != nil {
		params.Questionnaire = []byte(*req.Questionnaire)
	}
	user, err := h.Queries.PatchUserOnboarding(r.Context(), params)
	if err != nil {
		slog.Warn("patch onboarding failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update onboarding")
		return
	}

	if firstTouch && req.Questionnaire != nil && len(*req.Questionnaire) > 0 && string(*req.Questionnaire) != "{}" {
		platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingStarted(userID, platform))
	}

	var after questionnaireAnswers
	_ = json.Unmarshal(user.OnboardingQuestionnaire, &after)
	if after.complete() && !before.complete() {
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingQuestionnaireSubmitted(
			userID,
			[]string(after.Source),
			after.Role,
			[]string(after.UseCase),
			after.SourceSkipped,
			after.RoleSkipped,
			after.UseCaseSkipped,
			after.SourceOther != "",
			after.RoleOther != "",
			after.UseCaseOther != "",
		))
	}

	if after.Version == questionnaireSchemaVersion &&
		after.sourceResolved() && !before.sourceResolved() {
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingSourceSubmitted(
			userID,
			[]string(after.Source),
			after.SourceSkipped,
			after.SourceOther != "",
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type joinCloudWaitlistRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

func (h *Handler) JoinCloudWaitlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req joinCloudWaitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(email) > 254 {
		writeError(w, http.StatusBadRequest, "email is too long")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeError(w, http.StatusBadRequest, "email is invalid")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > cloudWaitlistReasonMaxLen {
		writeError(w, http.StatusBadRequest, "reason is too long")
		return
	}

	reasonParam := pgtype.Text{}
	if reason != "" {
		reasonParam = pgtype.Text{String: reason, Valid: true}
	}

	user, err := h.Queries.JoinCloudWaitlist(r.Context(), db.JoinCloudWaitlistParams{
		ID:                  parseUUID(userID),
		CloudWaitlistEmail:  pgtype.Text{String: email, Valid: true},
		CloudWaitlistReason: reasonParam,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join waitlist")
		return
	}

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.CloudWaitlistJoined(userID, reason != ""))

	writeJSON(w, http.StatusOK, userToResponse(user))
}
