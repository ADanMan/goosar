// Самостоятельное присоединение к ролевым рабочим пространствам деплоя:
// список доступных ролей, вступление участником и переключатель open_join
// у администратора. Раньше единственным способом попасть в пространство
// было персональное приглашение — теперь сотрудник сам выбирает роль в
// один клик, если администратор открыл вступление. Само вступление пишется
// в обычный аудит запросов, а не в admin_audit — это самообслуживание, а
// не административный акт.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/audit"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const adminAuditActionWorkspaceOpenJoinSet = "workspace.open_join.set"

type JoinTargetEntry struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	TemplateKey string `json:"template_key,omitempty"`
	MemberCount int64  `json:"member_count"`
}

type JoinTargetResult struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	AlreadyMember bool   `json:"already_member"`
}

func (h *Handler) ListJoinTargets(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListOpenJoinTargets(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Error("join targets: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list join targets")
		return
	}
	entries := make([]JoinTargetEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, JoinTargetEntry{
			ID:          uuidToString(row.ID),
			Slug:        row.Slug,
			Name:        row.Name,
			Description: row.Description.String,
			TemplateKey: row.TemplateKey.String,
			MemberCount: row.MemberCount,
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) JoinTarget(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspaceId")
	if !ok {
		return
	}
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	target, err := h.Queries.GetOpenJoinTarget(r.Context(), workspaceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace is not open for joining")
			return
		}
		slog.Error("join target: lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}

	result := JoinTargetResult{
		ID:   uuidToString(target.ID),
		Slug: target.Slug,
		Name: target.Name,
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: target.ID,
		UserID:      user.ID,
		Role:        "member",
	})
	if err != nil {
		if isUniqueViolation(err) {
			result.AlreadyMember = true
			writeJSON(w, http.StatusOK, result)
			return
		}
		slog.Error("join target: membership failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	firstOnboardingCompletion := !user.OnboardedAt.Valid
	onboardedUser, err := qtx.MarkUserOnboarded(r.Context(), user.ID)
	if err != nil {
		slog.Warn("join target: mark user onboarded failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}

	h.auditMembership(r, audit.ActionWorkspaceMemberAdded, userID, userID, result.ID)
	slog.Info("role workspace joined", "user_id", userID, "workspace_id", result.ID, "template_key", target.TemplateKey.String)

	h.publish(protocol.EventMemberAdded, result.ID, "member", userID, map[string]any{
		"member":         memberWithUserResponse(member, user),
		"workspace_name": target.Name,
	})
	h.notifyDaemonWorkspacesChanged(userID)

	if firstOnboardingCompletion {
		onboardedAt := ""
		if onboardedUser.OnboardedAt.Valid {
			onboardedAt = onboardedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID,
			result.ID,
			analytics.OnboardingPathRoleJoin,
			onboardedAt,
			onboardedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) SetDeploymentWorkspaceOpenJoin(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspaceId")
	if !ok {
		return
	}
	var body struct {
		OpenJoin *bool `json:"open_join"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.OpenJoin == nil {
		writeError(w, http.StatusBadRequest, "open_join is required")
		return
	}

	workspace, err := h.Queries.GetWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Error("deployment workspace open_join: lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}

	if !workspace.TemplateKey.Valid {
		writeError(w, http.StatusBadRequest, "open_join applies to provisioned role workspaces only")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	row, err := qtx.SetWorkspaceOpenJoin(r.Context(), db.SetWorkspaceOpenJoinParams{
		ID:       workspaceUUID,
		OpenJoin: *body.OpenJoin,
	})
	if err != nil {
		slog.Error("deployment workspace open_join: update failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}
	if _, err := qtx.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: actorUUID,
		Action:      adminAuditActionWorkspaceOpenJoinSet,
		TargetType:  "workspace",
		TargetID:    pgtype.Text{String: uuidToString(row.ID), Valid: true},
		AfterHash:   pgtype.Text{String: openJoinAuditValue(row.OpenJoin), Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {
		slog.Error("deployment workspace open_join: audit insert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":        uuidToString(row.ID),
		"name":      row.Name,
		"slug":      row.Slug,
		"open_join": row.OpenJoin,
	})
}

func openJoinAuditValue(open bool) string {
	if open {
		return "open_join=true"
	}
	return "open_join=false"
}
