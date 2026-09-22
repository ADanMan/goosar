package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	adminAuditActionUserDeactivate = "user.deactivate"
	adminAuditActionUserReactivate = "user.reactivate"
	adminAuditActionMemberRemove   = "workspace_member.remove"
)

type DeploymentUserResponse struct {
	UserID        string  `json:"user_id"`
	Email         string  `json:"email"`
	Name          string  `json:"name"`
	Deactivated   bool    `json:"deactivated"`
	DeactivatedAt *string `json:"deactivated_at"`

	TokenVersion int32 `json:"token_version"`

	RevokedTokens int `json:"revoked_tokens"`

	ClosedConnections int `json:"closed_connections"`
}

func deploymentUserResponse(user db.User, revokedTokens, closedConnections int) DeploymentUserResponse {
	resp := DeploymentUserResponse{
		UserID:            uuidToString(user.ID),
		Email:             user.Email,
		Name:              user.Name,
		Deactivated:       user.DeactivatedAt.Valid,
		TokenVersion:      user.TokenVersion,
		RevokedTokens:     revokedTokens,
		ClosedConnections: closedConnections,
	}
	if user.DeactivatedAt.Valid {
		at := user.DeactivatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.DeactivatedAt = &at
	}
	return resp
}

func (h *Handler) DeactivateDeploymentUser(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}

	if uuidToString(targetUUID) == uuidToString(actorUUID) {
		writeError(w, http.StatusConflict, "cannot deactivate your own account")
		return
	}

	user, err := h.Queries.DeactivateUser(r.Context(), targetUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		slog.Error("deployment users: deactivate failed", "error", err, "target_user_id", uuidToString(targetUUID))
		writeError(w, http.StatusInternalServerError, "failed to deactivate user")
		return
	}

	revoked, closed := h.cutLiveCredentials(r.Context(), targetUUID)

	h.writeUserAdminAudit(r, actorUUID, adminAuditActionUserDeactivate, targetUUID)
	slog.Info("user deactivated",
		"target_user_id", uuidToString(targetUUID),
		"actor_user_id", uuidToString(actorUUID),
		"revoked_tokens", revoked,
		"closed_connections", closed,
	)
	writeJSON(w, http.StatusOK, deploymentUserResponse(user, revoked, closed))
}

func (h *Handler) ReactivateDeploymentUser(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}

	user, err := h.Queries.ReactivateUser(r.Context(), targetUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		slog.Error("deployment users: reactivate failed", "error", err, "target_user_id", uuidToString(targetUUID))
		writeError(w, http.StatusInternalServerError, "failed to reactivate user")
		return
	}

	h.writeUserAdminAudit(r, actorUUID, adminAuditActionUserReactivate, targetUUID)
	slog.Info("user reactivated",
		"target_user_id", uuidToString(targetUUID),
		"actor_user_id", uuidToString(actorUUID),
	)
	writeJSON(w, http.StatusOK, deploymentUserResponse(user, 0, 0))
}

func (h *Handler) cutLiveCredentials(ctx context.Context, userID pgtype.UUID) (revokedTokens, closedConnections int) {
	hashes, err := h.Queries.RevokeAllPersonalAccessTokensByUser(ctx, userID)
	if err != nil {
		slog.Error("deployment users: revoking access tokens failed", "error", err, "target_user_id", uuidToString(userID))
	}
	for _, hash := range hashes {
		h.PATCache.Invalidate(ctx, hash)
	}

	daemonHashes, err := h.Queries.DeleteDaemonTokensByRuntimeOwner(ctx, userID)
	if err != nil {
		slog.Error("deployment users: revoking daemon tokens failed", "error", err, "target_user_id", uuidToString(userID))
	}
	for _, hash := range daemonHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}
	if offline, err := h.Queries.ForceOfflineRuntimesByOwner(ctx, userID); err != nil {
		slog.Error("deployment users: forcing runtimes offline failed", "error", err, "target_user_id", uuidToString(userID))
	} else if len(offline) > 0 {
		slog.Info("runtimes forced offline by offboarding", "target_user_id", uuidToString(userID), "runtimes", len(offline))
	}

	closedConnections = h.disconnectUser(uuidToString(userID), "")
	return len(hashes), closedConnections
}

func (h *Handler) writeUserAdminAudit(r *http.Request, actor pgtype.UUID, action string, target pgtype.UUID) {
	if _, err := h.Queries.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: actor,
		Action:      action,
		TargetType:  "user",
		TargetID:    pgtype.Text{String: uuidToString(target), Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {

		slog.Error("admin audit write failed", "error", err, "action", action, "target_user_id", uuidToString(target))
	}
}
