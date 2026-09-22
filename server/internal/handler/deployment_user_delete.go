package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/logger"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const adminAuditActionUserDelete = "user.delete"

const TombstoneUserName = "Удалённый пользователь"

type DeleteDeploymentUserResponse struct {
	UserID string `json:"user_id"`

	Email string `json:"email"`
	Name  string `json:"name"`

	RemovedMemberships int64 `json:"removed_memberships"`

	RevokedTokens     int `json:"revoked_tokens"`
	ClosedConnections int `json:"closed_connections"`
}

func (h *Handler) DeleteDeploymentUser(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "userId"), "userId")
	if !ok {
		return
	}

	if uuidToString(targetUUID) == uuidToString(actorUUID) {
		writeError(w, http.StatusConflict, "cannot delete your own account")
		return
	}

	target, err := h.Queries.GetUser(r.Context(), targetUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		slog.Error("deployment users: loading the target failed", "error", err, "target_user_id", uuidToString(targetUUID))
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	_, err = h.Queries.GetDeploymentAdmin(r.Context(), targetUUID)
	switch {
	case err == nil:
		writeError(w, http.StatusConflict, "revoke this account's deployment administrator role before deleting it")
		return
	case !errors.Is(err, pgx.ErrNoRows):
		slog.Error("deployment users: admin check failed", "error", err, "target_user_id", uuidToString(targetUUID))
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	removed, err := h.eraseUser(r, targetUUID, target.Email)
	if err != nil {
		slog.Error("deployment users: erasure failed", "error", err, "target_user_id", uuidToString(targetUUID))
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	revoked, closed := h.cutLiveCredentials(r.Context(), targetUUID)

	h.writeUserAdminAudit(r, actorUUID, adminAuditActionUserDelete, targetUUID)
	slog.Info("user deleted (personal data anonymised)",
		append(logger.RequestAttrs(r),
			"target_user_id", uuidToString(targetUUID),
			"actor_user_id", uuidToString(actorUUID),
			"removed_memberships", removed.memberships,
			"revoked_tokens", revoked,
			"closed_connections", closed,
		)...)

	writeJSON(w, http.StatusOK, DeleteDeploymentUserResponse{
		UserID:             uuidToString(targetUUID),
		Email:              removed.user.Email,
		Name:               removed.user.Name,
		RemovedMemberships: removed.memberships,
		RevokedTokens:      revoked,
		ClosedConnections:  closed,
	})
}

type erasureResult struct {
	user        db.User
	memberships int64
	workspaces  []pgtype.UUID
}

func (h *Handler) eraseUser(r *http.Request, userID pgtype.UUID, email string) (erasureResult, error) {
	var result erasureResult
	ctx := r.Context()

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteUserVerificationCodes(ctx, email); err != nil {
		return result, err
	}
	if err := qtx.DeleteUserInvitations(ctx, db.DeleteUserInvitationsParams{
		UserID: userID,
		Email:  email,
	}); err != nil {
		return result, err
	}
	for _, step := range []func() error{

		func() error { return qtx.DropUserIdentitiesForUser(ctx, userID) },
		func() error { return qtx.DeleteUserChannelBindings(ctx, userID) },
		func() error { return qtx.DeleteUserComposioConnections(ctx, userID) },
		func() error { return qtx.DeleteUserMcpCredentials(ctx, userID) },
		func() error { return qtx.DeleteUserNotificationPreferences(ctx, userID) },
		func() error { return qtx.DeleteUserInboxItems(ctx, userID) },
		func() error { return qtx.DeleteUserFeedback(ctx, userID) },
	} {
		if err := step(); err != nil {
			return result, err
		}
	}

	workspaces, err := qtx.ListMemberWorkspaceIDs(ctx, userID)
	if err != nil {
		return result, err
	}
	memberships, err := qtx.DeleteUserMemberships(ctx, userID)
	if err != nil {
		return result, err
	}
	result.memberships = memberships
	result.workspaces = workspaces

	user, err := qtx.AnonymizeUser(ctx, db.AnonymizeUserParams{
		UserID:        userID,
		TombstoneName: TombstoneUserName,
	})
	if err != nil {
		return result, err
	}
	result.user = user

	if err := tx.Commit(ctx); err != nil {
		return result, err
	}

	for _, workspaceID := range result.workspaces {
		h.MembershipCache.Invalidate(ctx, uuidToString(userID), uuidToString(workspaceID))
	}
	return result, nil
}
