package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const (
	sweepInterval = 30 * time.Second

	staleThresholdSeconds = handler.RuntimeStaleGraceSeconds

	offlineRuntimeTTLSeconds = 7 * 24 * 3600.0

	dispatchTimeoutSeconds = 300.0

	runningTimeoutSeconds = handler.StuckTaskGraceSeconds

	queuedTTLSeconds = 2 * 3600.0

	queuedExpireBatchSize = 500

	chatFinalizeGraceSeconds = 60.0

	chatFinalizeBatchSize = 100

	defaultSweeperBootGrace = staleThresholdSeconds * time.Second

	defaultRuntimeReconnectGrace = 3 * time.Hour

	minimumRuntimeReconnectGrace = time.Duration(staleThresholdSeconds) * time.Second

	offlineTaskFailBatchSize = 500

	reconnectRetryExpireBatchSize = 500
)

func runtimeReconnectGraceFromEnv() time.Duration {
	grace := envDuration("GOOSAR_RUNTIME_RECONNECT_GRACE", defaultRuntimeReconnectGrace)
	if grace < minimumRuntimeReconnectGrace {
		slog.Warn("runtime reconnect grace is shorter than heartbeat freshness; clamping",
			"configured", grace, "minimum", minimumRuntimeReconnectGrace)
		return minimumRuntimeReconnectGrace
	}
	return grace
}

func runRuntimeSweeper(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	opts := sweepTickOptions{BootedAt: time.Now(), Grace: sweeperBootGraceFromEnv(), ReconnectGrace: runtimeReconnectGraceFromEnv()}
	slog.Info("runtime sweeper: in-flight tasks survive an offline runtime for the reconnect grace",
		"reconnect_grace", opts.ReconnectGrace.String())
	if opts.Grace > 0 {
		slog.Info("runtime sweeper: runtime staleness sweeps suspended until daemons can reconnect",
			"boot_grace", opts.Grace.String())
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepTick(ctx, queries, liveness, taskSvc, bus, opts)
		}
	}
}

type sweepTickOptions struct {
	BootedAt time.Time

	Grace time.Duration

	ReconnectGrace time.Duration
}

func sweepTick(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus, opts sweepTickOptions) {

	if withinSweeperBootGrace(opts.BootedAt, opts.Grace, time.Now()) {
		slog.Debug("runtime sweeper: inside boot grace, skipping runtime staleness sweeps",
			"boot_grace", opts.Grace.String(),
			"elapsed", time.Since(opts.BootedAt).String())
	} else {
		sweepStaleRuntimes(ctx, queries, liveness, taskSvc, bus)

		sweepOfflineRuntimeTasks(ctx, queries, taskSvc, opts.ReconnectGrace)
		sweepExpiredRuntimeReconnectRetries(ctx, queries, taskSvc, opts.ReconnectGrace)
		sweepStaleTasks(ctx, queries, taskSvc, bus, opts.ReconnectGrace)
	}
	sweepExpiredQueuedTasks(ctx, queries, taskSvc)
	sweepDeferredChatFinalizations(ctx, queries, taskSvc)
	gcRuntimes(ctx, queries, bus)
}

func withinSweeperBootGrace(bootedAt time.Time, grace time.Duration, now time.Time) bool {
	return grace > 0 && now.Sub(bootedAt) < grace
}

func sweeperBootGraceFromEnv() time.Duration {
	return envNonNegativeDuration("GOOSAR_SWEEPER_BOOT_GRACE", defaultSweeperBootGrace)
}

func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus) {
	candidates, err := queries.SelectStaleOnlineRuntimes(ctx, staleThresholdSeconds)
	if err != nil {
		slog.Warn("runtime sweeper: failed to list stale online runtimes", "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	toOffline := filterStaleRuntimesByLiveness(ctx, candidates, liveness)
	if len(toOffline) == 0 {
		return
	}

	staleRows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          toOffline,
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to mark stale runtimes offline", "error", err)
		return
	}
	if len(staleRows) == 0 {

		return
	}
	if taskSvc != nil && taskSvc.Analytics != nil {
		for _, row := range staleRows {
			obsmetrics.RecordEvent(taskSvc.Analytics, taskSvc.Metrics, analytics.RuntimeOffline(
				util.UUIDToString(row.OwnerID),
				util.UUIDToString(row.WorkspaceID),
				util.UUIDToString(row.ID),
				row.DaemonID.String,
				row.Provider,
			))
		}
	}

	workspaces := make(map[string]bool)
	for _, row := range staleRows {
		wsID := util.UUIDToString(row.WorkspaceID)
		workspaces[wsID] = true
	}

	if liveness.Available() {
		for _, row := range staleRows {
			liveness.Forget(ctx, util.UUIDToString(row.ID))
		}
	}

	slog.Info("runtime sweeper: marked stale runtimes offline", "count", len(staleRows), "workspaces", len(workspaces))

	for wsID := range workspaces {
		bus.Publish(events.Event{
			Type:        protocol.EventDaemonRegister,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload: map[string]any{
				"action": "stale_sweep",
			},
		})
	}
}

func filterStaleRuntimesByLiveness(ctx context.Context, candidates []db.SelectStaleOnlineRuntimesRow, liveness handler.LivenessStore) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(candidates))
	if !liveness.Available() {
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	idStrs := make([]string, len(candidates))
	for i, c := range candidates {
		idStrs[i] = util.UUIDToString(c.ID)
	}
	alive, ok := liveness.IsAliveBatch(ctx, idStrs)
	if !ok {

		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	for i, c := range candidates {
		if alive[idStrs[i]] {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

func gcRuntimes(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	deleted, err := queries.DeleteStaleOfflineRuntimes(ctx, offlineRuntimeTTLSeconds)
	if err != nil {
		slog.Warn("runtime GC: failed to delete stale offline runtimes", "error", err)
		return
	}
	if len(deleted) == 0 {
		return
	}

	gcWorkspaces := make(map[string]bool)
	for _, row := range deleted {
		gcWorkspaces[util.UUIDToString(row.WorkspaceID)] = true
	}

	slog.Info("runtime GC: deleted stale offline runtimes", "count", len(deleted), "workspaces", len(gcWorkspaces))

	for wsID := range gcWorkspaces {
		bus.Publish(events.Event{
			Type:        protocol.EventDaemonRegister,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload: map[string]any{
				"action": "runtime_gc",
			},
		})
	}
}

func sweepOfflineRuntimeTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, reconnectGrace time.Duration) {
	failedTasks, err := queries.FailTasksForOfflineRuntimes(ctx, db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: reconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to clean up long-offline tasks", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}
	slog.Info("runtime sweeper: failed tasks beyond reconnect grace", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

func sweepExpiredRuntimeReconnectRetries(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, reconnectGrace time.Duration) {
	failedTasks, err := queries.FailExpiredRuntimeReconnectRetries(ctx, db.FailExpiredRuntimeReconnectRetriesParams{
		ReconnectGraceSecs: reconnectGrace.Seconds(),
		RuntimeStaleSecs:   staleThresholdSeconds,
		MaxPerTick:         reconnectRetryExpireBatchSize,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to expire reconnect retries", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}
	slog.Info("runtime sweeper: expired reconnect retries", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

func sweepStaleTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus, reconnectGrace time.Duration) {
	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: dispatchTimeoutSeconds,
		RunningTimeoutSecs:  runningTimeoutSeconds,

		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: reconnectGrace.Seconds(),
	})
	if err != nil {
		slog.Warn("task sweeper: failed to clean up stale tasks", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: failed stale tasks", "count", len(failedTasks))
	taskSvc.CaptureLeaseExpiredTasks(ctx, failedTasks)
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

func sweepExpiredQueuedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	failedTasks, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    queuedTTLSeconds,
		MaxPerTick: queuedExpireBatchSize,
	})
	if err != nil {
		slog.Warn("task sweeper: failed to expire stale queued tasks", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: expired stale queued tasks", "count", len(failedTasks))
	taskSvc.CaptureQueuedExpiredTasks(ctx, failedTasks)
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

func sweepDeferredChatFinalizations(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	rows, err := queries.ListChatFinalizeDeferredExpired(ctx, db.ListChatFinalizeDeferredExpiredParams{
		GraceSecs:  chatFinalizeGraceSeconds,
		MaxPerTick: chatFinalizeBatchSize,
	})
	if err != nil {
		slog.Warn("chat finalize sweeper: list deferred failed", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	for _, t := range rows {
		taskSvc.FinalizeDeferredCancelledChat(ctx, t.ID)
	}
	slog.Info("chat finalize sweeper: settled deferred cancellations", "count", len(rows))
}

func broadcastFailedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus, tasks []db.AgentTaskQueue) {
	if taskSvc != nil {
		taskSvc.HandleFailedTasks(ctx, tasks)
		return
	}

	processedIssues := make(map[string]bool)
	affectedAgents := make(map[string]pgtype.UUID)
	for _, t := range tasks {
		failureReason := "agent_error"
		if t.FailureReason.Valid && t.FailureReason.String != "" {
			failureReason = t.FailureReason.String
		}
		workspaceID := ""
		if t.IssueID.Valid {
			if тикет, err := queries.GetIssue(ctx, t.IssueID); err == nil {
				workspaceID = util.UUIDToString(тикет.WorkspaceID)
				issueKey := util.UUIDToString(t.IssueID)
				if тикет.Status == "in_progress" && !processedIssues[issueKey] {
					processedIssues[issueKey] = true
					if hasActive, herr := queries.HasActiveTaskForIssue(ctx, t.IssueID); herr == nil && !hasActive {
						queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: t.IssueID, Status: "todo", WorkspaceID: тикет.WorkspaceID})
					}
				}
			}
		}
		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":        util.UUIDToString(t.ID),
				"agent_id":       util.UUIDToString(t.AgentID),
				"issue_id":       util.UUIDToString(t.IssueID),
				"status":         "failed",
				"failure_reason": failureReason,
			},
		})
		affectedAgents[util.UUIDToString(t.AgentID)] = t.AgentID
	}
	for _, agentID := range affectedAgents {
		reconcileAgentStatus(ctx, queries, bus, agentID)
	}
}

func reconcileAgentStatus(ctx context.Context, queries *db.Queries, bus *events.Bus, agentID pgtype.UUID) {
	агент, err := queries.RefreshAgentStatusFromTasks(ctx, agentID)
	if err != nil {
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(агент.WorkspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"agent_id": util.UUIDToString(агент.ID), "status": агент.Status},
	})
}
