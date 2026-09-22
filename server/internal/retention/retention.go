// Пакет retention применяет политику хранения данных развёртывания.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	EnvChat = "GOOSAR_RETENTION_CHAT"

	EnvTasks = "GOOSAR_RETENTION_TASKS"

	EnvClosedIssues = "GOOSAR_RETENTION_CLOSED_ISSUES"

	EnvActivity = "GOOSAR_RETENTION_ACTIVITY"

	EnvAttachmentGrace = "GOOSAR_ATTACHMENT_PURGE_GRACE"
)

const DefaultAttachmentGrace = 7 * 24 * time.Hour

const BatchSize = 5000

type Policy struct {
	Chat            time.Duration
	Tasks           time.Duration
	ClosedIssues    time.Duration
	Activity        time.Duration
	AttachmentGrace time.Duration
}

func PolicyFromEnv() Policy {
	return Policy{
		Chat:            envDuration(EnvChat, 0),
		Tasks:           envDuration(EnvTasks, 0),
		ClosedIssues:    envDuration(EnvClosedIssues, 0),
		Activity:        envDuration(EnvActivity, 0),
		AttachmentGrace: envDuration(EnvAttachmentGrace, DefaultAttachmentGrace),
	}
}

func (p Policy) Enabled() bool {
	return p.Chat > 0 || p.Tasks > 0 || p.ClosedIssues > 0 || p.Activity > 0 || p.AttachmentGrace > 0
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn("retention: value is not a duration, using the default",
			"var", name, "value", raw, "default", fallback.String())
		return fallback
	}
	if value < 0 {
		slog.Warn("retention: value is negative, using the default",
			"var", name, "value", raw, "default", fallback.String())
		return fallback
	}
	return value
}

type ObjectStore interface {
	KeyFromURL(rawURL string) string
	DeleteKeys(ctx context.Context, keys []string)
}

type Report struct {
	ChatSessions       int64
	Tasks              int64
	ClosedIssues       int64
	ActivityRows       int64
	VerificationCodes  int64
	AttachmentObjects  int64
	AttachmentRevived  int64
	DryRun             bool
	AttachmentSkipped  bool
	AttachmentSkipWhy  string
	countsAreEstimates bool
}

func (r Report) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("chat_sessions", r.ChatSessions),
		slog.Int64("tasks", r.Tasks),
		slog.Int64("closed_issues", r.ClosedIssues),
		slog.Int64("activity_rows", r.ActivityRows),
		slog.Int64("verification_codes", r.VerificationCodes),
		slog.Int64("attachment_objects", r.AttachmentObjects),
		slog.Bool("dry_run", r.DryRun),
	)
}

func (r Report) Empty() bool {
	return r.ChatSessions == 0 && r.Tasks == 0 && r.ClosedIssues == 0 &&
		r.ActivityRows == 0 && r.VerificationCodes == 0 && r.AttachmentObjects == 0
}

func Run(ctx context.Context, queries *db.Queries, store ObjectStore, policy Policy, dryRun bool) (Report, error) {
	report := Report{DryRun: dryRun, countsAreEstimates: dryRun}

	windows := []struct {
		window time.Duration
		count  func(context.Context, float64) (int64, error)
		purge  func(context.Context, db.Queries, float64) (int64, error)
		into   *int64
		name   string
	}{
		{
			window: policy.Chat,
			count:  queries.CountChatSessionsOlderThan,
			purge: func(ctx context.Context, q db.Queries, secs float64) (int64, error) {
				return q.PurgeChatMessages(ctx, db.PurgeChatMessagesParams{RetentionSecs: secs, MaxRows: BatchSize})
			},
			into: &report.ChatSessions,
			name: "chat",
		},
		{
			window: policy.Tasks,
			count:  queries.CountCompletedTasksOlderThan,
			purge: func(ctx context.Context, q db.Queries, secs float64) (int64, error) {
				return q.PurgeCompletedTasks(ctx, db.PurgeCompletedTasksParams{RetentionSecs: secs, MaxRows: BatchSize})
			},
			into: &report.Tasks,
			name: "tasks",
		},
		{
			window: policy.ClosedIssues,
			count:  queries.CountClosedIssuesOlderThan,
			purge: func(ctx context.Context, q db.Queries, secs float64) (int64, error) {
				return q.PurgeClosedIssues(ctx, db.PurgeClosedIssuesParams{RetentionSecs: secs, MaxRows: BatchSize})
			},
			into: &report.ClosedIssues,
			name: "closed_issues",
		},
		{
			window: policy.Activity,
			count:  queries.CountActivityLogOlderThan,
			purge: func(ctx context.Context, q db.Queries, secs float64) (int64, error) {
				return q.PurgeActivityLog(ctx, db.PurgeActivityLogParams{RetentionSecs: secs, MaxRows: BatchSize})
			},
			into: &report.ActivityRows,
			name: "activity",
		},
	}

	for _, w := range windows {
		if w.window <= 0 {
			continue
		}
		secs := w.window.Seconds()
		if dryRun {
			n, err := w.count(ctx, secs)
			if err != nil {
				return report, fmt.Errorf("retention: count %s: %w", w.name, err)
			}
			*w.into = n
			continue
		}
		n, err := w.purge(ctx, *queries, secs)
		if err != nil {
			return report, fmt.Errorf("retention: purge %s: %w", w.name, err)
		}
		*w.into = n
	}

	if dryRun {
		n, err := queries.CountExpiredVerificationCodes(ctx)
		if err != nil {
			return report, fmt.Errorf("retention: count verification codes: %w", err)
		}
		report.VerificationCodes = n
	} else {
		before, err := queries.CountExpiredVerificationCodes(ctx)
		if err != nil {
			return report, fmt.Errorf("retention: count verification codes: %w", err)
		}
		if err := queries.DeleteExpiredVerificationCodes(ctx); err != nil {
			return report, fmt.Errorf("retention: purge verification codes: %w", err)
		}
		after, err := queries.CountExpiredVerificationCodes(ctx)
		if err != nil {
			return report, fmt.Errorf("retention: recount verification codes: %w", err)
		}
		report.VerificationCodes = before - after
	}

	if policy.AttachmentGrace > 0 {
		if store == nil {
			report.AttachmentSkipped = true
			report.AttachmentSkipWhy = "no storage backend configured (set S3_BUCKET or LOCAL_UPLOAD_DIR)"
		} else if err := sweepAttachments(ctx, queries, store, policy.AttachmentGrace, dryRun, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func sweepAttachments(ctx context.Context, queries *db.Queries, store ObjectStore, grace time.Duration, dryRun bool, report *Report) error {
	rows, err := queries.ListAttachmentTombstones(ctx, db.ListAttachmentTombstonesParams{
		GraceSecs: grace.Seconds(),
		MaxRows:   BatchSize,
	})
	if err != nil {
		return fmt.Errorf("retention: list attachment tombstones: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	ids := make([]pgtype.UUID, 0, len(rows))
	byID := make(map[string]string, len(rows))
	for _, row := range rows {
		ids = append(ids, row.AttachmentID)
		byID[row.AttachmentID.String()] = row.Url
	}

	revived, err := queries.ListRevivedAttachmentTombstones(ctx, ids)
	if err != nil {
		return fmt.Errorf("retention: check revived attachments: %w", err)
	}
	report.AttachmentRevived = int64(len(revived))

	if dryRun {
		report.AttachmentObjects = int64(len(rows) - len(revived))
		return nil
	}

	revivedSet := make(map[string]struct{}, len(revived))
	for _, id := range revived {
		revivedSet[id.String()] = struct{}{}
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, isRevived := revivedSet[row.AttachmentID.String()]; isRevived {
			continue
		}
		if key := store.KeyFromURL(row.Url); key != "" {
			keys = append(keys, key)
		}
	}
	store.DeleteKeys(ctx, keys)

	deleted, err := queries.DeleteAttachmentTombstones(ctx, ids)
	if err != nil {
		return fmt.Errorf("retention: clear attachment tombstones: %w", err)
	}
	report.AttachmentObjects = deleted

	if len(revived) > 0 {
		if _, err := queries.DropAttachmentTombstones(ctx, revived); err != nil {
			return fmt.Errorf("retention: drop revived tombstones: %w", err)
		}
	}
	return nil
}
