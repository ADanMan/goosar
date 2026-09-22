package handler

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	AuditRetentionEnvVar = "GOOSAR_AUDIT_RETENTION_DAYS"

	DefaultAuditRetentionDays = 365

	auditPurgeInterval = 24 * time.Hour
)

func AuditRetentionDays() int {
	raw := strings.TrimSpace(os.Getenv(AuditRetentionEnvVar))
	if raw == "" {
		return DefaultAuditRetentionDays
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("audit retention: value is not an integer, using the default",
			"var", AuditRetentionEnvVar, "value", raw, "default_days", DefaultAuditRetentionDays)
		return DefaultAuditRetentionDays
	}
	return days
}

func (h *Handler) PurgeAuditJournals(ctx context.Context) (int64, error) {
	days := AuditRetentionDays()
	if days <= 0 {
		return 0, nil
	}
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-time.Duration(days) * 24 * time.Hour), Valid: true}

	authRows, err := h.Queries.PurgeAuthAuditBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	adminRows, err := h.Queries.PurgeAdminAuditBefore(ctx, cutoff)
	if err != nil {

		return authRows, err
	}
	return authRows + adminRows, nil
}

func (h *Handler) StartAuditRetentionJob(ctx context.Context) {
	days := AuditRetentionDays()
	if days <= 0 {
		slog.Info("audit retention: disabled — journals are kept indefinitely", "var", AuditRetentionEnvVar)
		return
	}
	slog.Info("audit retention: enabled", "retention_days", days)

	run := func() {
		deleted, err := h.PurgeAuditJournals(ctx)
		if err != nil {
			slog.Error("audit retention: purge failed", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("audit retention: purged expired journal rows", "rows", deleted, "retention_days", days)
		}
	}

	go func() {
		run()
		ticker := time.NewTicker(auditPurgeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
