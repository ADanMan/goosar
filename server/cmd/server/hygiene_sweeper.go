package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/leader"
	"github.com/adanman/goosar/server/internal/retention"
	"github.com/adanman/goosar/server/internal/storage"
	"github.com/adanman/goosar/server/internal/uploadgc"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	hygieneSweepInterval = time.Hour

	defaultCronRetention = 30 * 24 * time.Hour

	cronPurgeBatchSize = 10000

	exportReclaimBatchSize = 100
)

func runHygieneSweeper(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, store storage.Storage) {
	interval := envDurationPositive("GOOSAR_HYGIENE_SWEEP_INTERVAL", hygieneSweepInterval)
	cronRetention := cronRetentionFromEnv()
	grace := uploadGCGraceFromEnv()
	policy := retention.PolicyFromEnv()

	slog.Info("hygiene sweeper: starting",
		"interval", interval.String(),
		"cron_audit_retention", cronRetention.String(),
		"upload_gc_grace", grace.String(),
		"retention_policy_enabled", policy.Enabled())

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hygieneTick(ctx, pool, queries, store, cronRetention, grace, policy)
		}
	}
}

func hygieneTick(ctx context.Context, pool *pgxpool.Pool, queries *db.Queries, store storage.Storage, cronRetention, grace time.Duration, policy retention.Policy) bool {
	won, err := leader.TryRun(ctx, pool, leader.KeyHygieneSweep, func(ctx context.Context) error {
		purgeCronExecutions(ctx, queries, cronRetention)
		sweepOrphanUploads(ctx, queries, store, grace)

		applyRetentionPolicy(ctx, queries, store, policy)
		reapStaleExports(ctx, queries)
		reclaimExpiredExports(ctx, queries)
		return nil
	})
	if err != nil {
		slog.Warn("hygiene sweeper: leader election failed, skipping this tick", "error", err)
		return false
	}
	if !won {
		slog.Debug("hygiene sweeper: another replica is running this tick")
	}
	return won
}

func purgeCronExecutions(ctx context.Context, queries *db.Queries, retention time.Duration) {
	if retention <= 0 {
		return
	}
	deleted, err := queries.PurgeCronExecutions(ctx, db.PurgeCronExecutionsParams{
		RetentionSecs: retention.Seconds(),
		MaxRows:       cronPurgeBatchSize,
	})
	if err != nil {
		slog.Warn("cron audit GC: purge failed", "error", err)
		return
	}
	if deleted == 0 {
		return
	}
	slog.Info("cron audit GC: purged sys_cron_executions rows",
		"count", deleted, "retention", retention.String())
}

func sweepOrphanUploads(ctx context.Context, queries *db.Queries, store uploadgc.ObjectStore, grace time.Duration) {
	if grace <= 0 || store == nil {
		return
	}
	res, err := uploadgc.Sweep(ctx, queries, store, uploadgc.Options{Grace: grace})
	if err != nil {
		slog.Warn("upload GC: sweep failed", "error", err)
		return
	}
	res.Log(slog.Default(), grace, false)
}

func cronRetentionFromEnv() time.Duration {
	return envDurationNonNegative("GOOSAR_SCHEDULER_AUDIT_RETENTION", defaultCronRetention)
}

func uploadGCGraceFromEnv() time.Duration {
	return envDurationNonNegative("GOOSAR_UPLOAD_GC_GRACE", uploadgc.DefaultGrace)
}

func applyRetentionPolicy(ctx context.Context, queries *db.Queries, store retention.ObjectStore, policy retention.Policy) {
	if !policy.Enabled() {
		return
	}
	report, err := retention.Run(ctx, queries, store, policy, false)
	if err != nil {
		slog.Warn("retention: pass failed", "error", err, "partial", report)
		return
	}
	if report.AttachmentSkipped {
		slog.Warn("retention: attachment sweep skipped", "reason", report.AttachmentSkipWhy)
	}
	if report.Empty() {
		return
	}
	slog.Info("retention: purged expired data", "report", report)
	if _, err := queries.InsertAdminAudit(ctx, db.InsertAdminAuditParams{
		Action:     "retention.purge",
		TargetType: "deployment",
		TargetID:   pgtype.Text{String: retentionAuditTarget(report), Valid: true},
	}); err != nil {
		slog.Error("retention: journal write failed", "error", err)
	}
}

func retentionAuditTarget(r retention.Report) string {
	return fmt.Sprintf("chat=%d tasks=%d closed_issues=%d activity=%d codes=%d attachments=%d",
		r.ChatSessions, r.Tasks, r.ClosedIssues, r.ActivityRows, r.VerificationCodes, r.AttachmentObjects)
}

func reapStaleExports(ctx context.Context, queries *db.Queries) {
	reaped, err := queries.ReapStaleExportJobs(ctx, handler.ExportTimeout().Seconds())
	if err != nil {
		slog.Warn("export reaper: failed", "error", err)
		return
	}
	for _, row := range reaped {
		if row.FilePath.Valid {
			_ = os.Remove(row.FilePath.String)
		}

		archive := handler.ExportArchivePath(util.UUIDToString(row.WorkspaceID), util.UUIDToString(row.ID))
		if err := os.Remove(archive + handler.ExportPartialSuffix); err != nil && !os.IsNotExist(err) {
			slog.Warn("export reaper: removing a partial archive failed", "error", err, "job_id", util.UUIDToString(row.ID))
		}
	}
	if len(reaped) > 0 {
		slog.Info("export reaper: released stale export slots", "jobs", len(reaped))
	}
}

func reclaimExpiredExports(ctx context.Context, queries *db.Queries) {
	window := handler.ExportRetention()
	if window <= 0 {
		return
	}
	rows, err := queries.ListExpiredExportArtifacts(ctx, db.ListExpiredExportArtifactsParams{
		RetentionSecs: window.Seconds(),
		MaxRows:       exportReclaimBatchSize,
	})
	if err != nil {
		slog.Warn("export GC: listing expired archives failed", "error", err)
		return
	}
	var removed int
	for _, row := range rows {
		if !row.FilePath.Valid {
			continue
		}
		if err := os.Remove(row.FilePath.String); err != nil && !os.IsNotExist(err) {

			slog.Warn("export GC: removing an archive failed", "error", err, "path", row.FilePath.String)
			continue
		}
		if err := queries.ForgetExportArtifact(ctx, row.ID); err != nil {
			slog.Warn("export GC: clearing the archive pointer failed", "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("export GC: reclaimed expired archives", "count", removed, "retention", window.String())
	}
}
