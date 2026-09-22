// Ручки уровня deployment-admin: список держателей роли, заявки на выдачу
// и отзыв (через второй канал подтверждения) и журнал аудита. Роль стоит
// выше владельцев рабочих пространств и управляет политикой всего деплоя.
// Состав меняется только по заявке, которую отдельной командой подтверждает
// оператор, и каждое такое изменение пишет запись в admin_audit.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const deploymentAdminsLockKey = "deployment:admins"

const (
	adminAuditActionBootstrapGrant = "deployment_admin.bootstrap_grant"
	adminAuditActionPolicySet      = "deployment_policy.set"

	adminAuditActionConfigRead         = "config.read"
	adminAuditActionWorkspaceConfigSet = "workspace_config.set"
	adminAuditActionUserOverrideSet    = "user_config_override.set"
	adminAuditActionUserOverrideDelete = "user_config_override.delete"

	adminAuditActionWorkspaceCreateBypass = "workspace.create.deployment_admin_bypass"
)

const (
	defaultAdminAuditLimit = 50
	maxAdminAuditLimit     = 200
)

type DeploymentAdminEntry struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	GrantedBy string    `json:"granted_by,omitempty"`
	GrantedAt time.Time `json:"granted_at"`
}

type AdminAuditEntry struct {
	ID          string    `json:"id"`
	ActorUserID string    `json:"actor_user_id,omitempty"`
	Action      string    `json:"action"`
	TargetType  string    `json:"target_type"`
	TargetID    string    `json:"target_id,omitempty"`
	BeforeHash  string    `json:"before_hash,omitempty"`
	AfterHash   string    `json:"after_hash,omitempty"`
	RequestID   string    `json:"request_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (h *Handler) isDeploymentAdmin(ctx context.Context, userUUID pgtype.UUID) (bool, error) {
	_, err := h.Queries.GetDeploymentAdmin(ctx, userUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (h *Handler) requireDeploymentAdmin(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return pgtype.UUID{}, false
	}
	isAdmin, err := h.isDeploymentAdmin(r.Context(), userUUID)
	if err != nil {
		slog.Error("deployment admin gate: lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check deployment administrator role")
		return pgtype.UUID{}, false
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "deployment administrator role required")
		return pgtype.UUID{}, false
	}
	return userUUID, true
}

func adminAuditRequestID(r *http.Request) pgtype.Text {
	id := chimw.GetReqID(r.Context())
	return pgtype.Text{String: id, Valid: id != ""}
}

func deploymentAdminEntryFromRow(row db.ListDeploymentAdminsRow) DeploymentAdminEntry {
	return DeploymentAdminEntry{
		UserID:    uuidToString(row.UserID),
		Email:     row.Email.String,
		Name:      row.Name.String,
		GrantedBy: optionalUUIDString(row.GrantedBy),
		GrantedAt: row.GrantedAt.Time,
	}
}

func (h *Handler) ListDeploymentAdmins(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListDeploymentAdmins(r.Context())
	if err != nil {
		slog.Error("deployment admins: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list deployment administrators")
		return
	}
	entries := make([]DeploymentAdminEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, deploymentAdminEntryFromRow(row))
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) AddDeploymentAdmin(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(body.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	target, err := h.Queries.GetUserByEmail(r.Context(), email)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "no user with this email — the role can only be granted to an existing user")
		return
	}
	if err != nil {
		slog.Error("deployment admins: user lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to look up user")
		return
	}

	if row, err := h.Queries.GetDeploymentAdmin(r.Context(), target.ID); err == nil {
		writeJSON(w, http.StatusOK, DeploymentAdminEntry{
			UserID:    uuidToString(row.UserID),
			Email:     target.Email,
			Name:      target.Name,
			GrantedBy: optionalUUIDString(row.GrantedBy),
			GrantedAt: row.GrantedAt.Time,
		})
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("deployment admins: role lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to file the grant request")
		return
	}

	pending, created, err := h.fileDeploymentAdminPending(r.Context(), actorUUID,
		deploymentAdminPendingActionGrant, target.ID, adminAuditRequestID(r))
	if err != nil {
		slog.Error("deployment admins: file grant request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to file the grant request")
		return
	}
	slog.Info("deployment admin grant requested — pending server confirmation",
		"target_user_id", uuidToString(target.ID),
		"actor_user_id", uuidToString(actorUUID),
		"pending_id", uuidToString(pending.ID),
		"already_filed", !created,
	)
	writeJSON(w, http.StatusAccepted, deploymentAdminPendingResponse(pending, target.Email))
}

func (h *Handler) RemoveDeploymentAdmin(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}

	if _, err := h.Queries.GetDeploymentAdmin(r.Context(), targetUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user is not a deployment administrator")
			return
		}
		slog.Error("deployment admins: target lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to file the revoke request")
		return
	}
	count, err := h.Queries.CountDeploymentAdmins(r.Context())
	if err != nil {
		slog.Error("deployment admins: count failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to file the revoke request")
		return
	}

	if count <= 1 {
		writeError(w, http.StatusConflict, "cannot remove the last deployment administrator")
		return
	}

	pending, created, err := h.fileDeploymentAdminPending(r.Context(), actorUUID,
		deploymentAdminPendingActionRevoke, targetUUID, adminAuditRequestID(r))
	if err != nil {
		slog.Error("deployment admins: file revoke request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to file the revoke request")
		return
	}
	slog.Info("deployment admin revoke requested — pending server confirmation",
		"target_user_id", uuidToString(targetUUID),
		"actor_user_id", uuidToString(actorUUID),
		"pending_id", uuidToString(pending.ID),
		"already_filed", !created,
	)
	writeJSON(w, http.StatusAccepted, deploymentAdminPendingResponse(pending, ""))
}

func (h *Handler) ListDeploymentAdminPending(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListDeploymentAdminPending(r.Context())
	if err != nil {
		slog.Error("deployment admins: list pending failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list pending requests")
		return
	}
	entries := make([]DeploymentAdminPendingResponse, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		entries = append(entries, DeploymentAdminPendingResponse{
			Status:       "pending",
			RequestID:    id,
			Action:       row.Action,
			TargetUserID: uuidToString(row.TargetUserID),
			TargetEmail:  row.TargetEmail.String,
			RequestedBy:  uuidToString(row.RequestedBy),
			RequestedAt:  row.RequestedAt.Time,
			ConfirmHint:  deploymentAdminConfirmHint(id),
		})
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) callerIsDeploymentAdmin(r *http.Request, userID string) bool {
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false
	}
	isAdmin, err := h.isDeploymentAdmin(r.Context(), userUUID)
	if err != nil {
		slog.Error("deployment admin check failed — treating the caller as a non-admin", "error", err)
		return false
	}
	return isAdmin
}

func (h *Handler) deploymentHasNoAdmins(ctx context.Context) bool {
	count, err := h.Queries.CountDeploymentAdmins(ctx)
	if err != nil {
		slog.Warn("deployment admin seed: count failed — skipping the sign-up seed", "error", err)
		return false
	}
	return count == 0
}
