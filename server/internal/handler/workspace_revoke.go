package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

func (h *Handler) revokeAndRemoveMember(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID) (revocationResult, error) {
	var empty revocationResult

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	runtimes, err := qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	})
	if err != nil {
		return empty, err
	}

	result := revocationResult{Runtimes: runtimes}

	if len(runtimes) > 0 {
		runtimeIDs := make([]pgtype.UUID, len(runtimes))
		daemonIDs := make([]string, 0, len(runtimes))
		for i, rt := range runtimes {
			runtimeIDs[i] = rt.ID
			if rt.DaemonID.Valid && rt.DaemonID.String != "" {
				daemonIDs = append(daemonIDs, rt.DaemonID.String)
			}
		}

		result.ArchivedAgents, err = qtx.ArchiveAgentsByRuntime(ctx, db.ArchiveAgentsByRuntimeParams{
			ArchivedBy: archivedBy,
			RuntimeIds: runtimeIDs,
		})
		if err != nil {
			return empty, err
		}

		archivedAgentIDs := make([]pgtype.UUID, len(result.ArchivedAgents))
		for i, a := range result.ArchivedAgents {
			archivedAgentIDs[i] = a.ID
		}

		if err := qtx.ReleaseHelperIdentityByAgentIDs(ctx, db.ReleaseHelperIdentityByAgentIDsParams{
			AgentIds:        archivedAgentIDs,
			HelperSystemKey: pgtype.Text{String: helperAgentSystemKey, Valid: true},
		}); err != nil {
			return empty, err
		}

		if err := qtx.ReleaseRoleAgentIdentityByAgentIDs(ctx, archivedAgentIDs); err != nil {
			return empty, err
		}

		result.CancelledTasks, err = qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: runtimeIDs,
			AgentIds:   archivedAgentIDs,
		})
		if err != nil {
			return empty, err
		}

		result.OfflineRuntimeIDs, err = qtx.ForceOfflineRuntimesByIDs(ctx, runtimeIDs)
		if err != nil {
			return empty, err
		}

		if len(daemonIDs) > 0 {
			result.RevokedTokenHashes, err = qtx.DeleteDaemonTokensByWorkspaceAndDaemons(ctx, db.DeleteDaemonTokensByWorkspaceAndDaemonsParams{
				WorkspaceID: workspaceID,
				DaemonIds:   daemonIDs,
			})
			if err != nil {
				return empty, err
			}
		}
	}

	if err := qtx.DeleteChannelUserBindingsByWorkspaceMember(ctx, db.DeleteChannelUserBindingsByWorkspaceMemberParams{
		WorkspaceID:  workspaceID,
		GoosarUserID: userID,
	}); err != nil {
		return empty, err
	}

	if err := qtx.DeleteAgentInvocationTargetsByMember(ctx, db.DeleteAgentInvocationTargetsByMemberParams{
		WorkspaceID: workspaceID,
		TargetID:    userID,
	}); err != nil {
		return empty, err
	}

	if _, err := qtx.DeleteUserConfigOverride(ctx, db.DeleteUserConfigOverrideParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
	}

	if err := qtx.DeleteMember(ctx, memberID); err != nil {
		return empty, err
	}

	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}

	return result, nil
}

type revocationResult struct {
	Runtimes           []db.AgentRuntime
	ArchivedAgents     []db.Agent
	CancelledTasks     []db.AgentTaskQueue
	OfflineRuntimeIDs  []db.ForceOfflineRuntimesByIDsRow
	RevokedTokenHashes []string
}

func (r revocationResult) isEmpty() bool {
	return len(r.Runtimes) == 0
}

func (h *Handler) publishRevocation(ctx context.Context, result revocationResult, workspaceIDStr, actorType, actorIDStr string) {
	if result.isEmpty() {
		return
	}

	for _, hash := range result.RevokedTokenHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}

	if len(result.Runtimes) > 0 {
		runtimeIDs := make([]string, 0, len(result.Runtimes))
		for _, rt := range result.Runtimes {
			runtimeIDs = append(runtimeIDs, uuidToString(rt.ID))
		}
		h.DaemonHub.CloseRuntimeConnections(runtimeIDs)
	}

	if h.TaskService != nil && len(result.CancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(ctx, result.CancelledTasks)
	}

	for _, agent := range result.ArchivedAgents {

		h.publish(protocol.EventAgentArchived, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(agent)),
		})
	}

	if len(result.OfflineRuntimeIDs) > 0 {
		h.publish(protocol.EventDaemonRegister, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"action": "revoke",
		})
	}
}

func logRevocation(result revocationResult, workspaceID, userID string, attrs ...any) {
	if result.isEmpty() {
		return
	}
	base := []any{
		"workspace_id", workspaceID,
		"user_id", userID,
		"runtimes_revoked", len(result.Runtimes),
		"agents_archived", len(result.ArchivedAgents),
		"tasks_cancelled", len(result.CancelledTasks),
		"runtimes_taken_offline", len(result.OfflineRuntimeIDs),
		"daemon_tokens_revoked", len(result.RevokedTokenHashes),
	}
	slog.Info("member runtimes revoked", append(base, attrs...)...)
}

func (h *Handler) disconnectOffboardedMember(r *http.Request, userID, workspaceID pgtype.UUID) {
	h.disconnectUser(uuidToString(userID), uuidToString(workspaceID))
	if _, err := h.Queries.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: parseUUID(requestUserID(r)),
		Action:      adminAuditActionMemberRemove,
		TargetType:  "workspace_member",
		TargetID:    pgtype.Text{String: uuidToString(workspaceID) + ":" + uuidToString(userID), Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {
		slog.Error("admin audit write failed", "error", err, "action", adminAuditActionMemberRemove,
			"workspace_id", uuidToString(workspaceID), "target_user_id", uuidToString(userID))
	}
}
