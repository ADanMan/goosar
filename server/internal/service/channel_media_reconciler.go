package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/metrics"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	ChannelMediaReconcileSettleDelay = 15 * time.Minute

	channelMediaReconcileSweepInterval = time.Minute

	channelMediaReconcileLease = 2 * time.Minute

	channelMediaReconcileSweepLimit = 50

	channelMediaReconcileBackoffBase = time.Minute
	channelMediaReconcileBackoffCap  = time.Hour

	channelMediaReconcileDeleteTimeout = 30 * time.Second
)

var channelMediaTombstoneRedelete = []time.Duration{
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

type MediaObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

type ChannelMediaReconciler struct {
	Queries *db.Queries
	Storage MediaObjectDeleter
	Logger  *slog.Logger
	Metrics *metrics.ChannelMediaReconcilerMetrics

	deleteTimeout time.Duration
}

func (r *ChannelMediaReconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func (r *ChannelMediaReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(channelMediaReconcileSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

func (r *ChannelMediaReconciler) RunOnce(ctx context.Context) {
	if r.Storage == nil {

		r.logger().Error("channel media reconciler: no storage backend; skipping sweep")
		return
	}
	for settles := 0; settles < channelMediaReconcileSweepLimit; settles++ {
		leaseToken := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		row, err := r.Queries.ClaimNextChannelMediaPendingObjectForReconcile(ctx, db.ClaimNextChannelMediaPendingObjectForReconcileParams{
			LeaseToken:  leaseToken,
			Lease:       pgInterval(channelMediaReconcileLease),
			SettleDelay: pgInterval(ChannelMediaReconcileSettleDelay),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {

			if ctx.Err() != nil {
				return
			}
			r.logger().Warn("channel media reconciler: claim failed", "error", err)
			break
		}
		r.settle(ctx, row, leaseToken)
	}
	if r.Metrics != nil {
		if counts, err := r.Queries.CountChannelMediaPendingObjects(ctx); err == nil {
			r.Metrics.Backlog.Set(float64(counts.PendingObjects))
			r.Metrics.Tombstones.Set(float64(counts.TombstonedObjects))
		}
	}
}

func (r *ChannelMediaReconciler) settle(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) {

	referenced, err := r.Queries.ChannelMediaObjectIsReferenced(ctx, db.ChannelMediaObjectIsReferencedParams{
		ChatMessageID: row.ChatMessageID,
		WorkspaceID:   row.WorkspaceID,
		StorageUrl:    row.StorageUrl,
	})
	if err != nil {
		r.release(ctx, row, leaseToken, err)
		return
	}
	if referenced {
		if row.State == tombstonedState {

			r.logger().Error("channel media reconciler: tombstoned object is referenced by an attachment; keeping it",
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID,
				"storage_url", row.StorageUrl,
				"tombstone_pass", row.TombstonePass)
			if r.Metrics != nil {
				r.Metrics.TombstoneReferenced.Inc()
			}
		}

		if r.clearRow(ctx, row, leaseToken) {
			r.logger().Info("channel media reconciler: kept referenced object",
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID)
			if r.Metrics != nil {
				r.Metrics.RowsReferenced.Inc()
			}
		}
		return
	}

	r.settleDeletedObject(ctx, row, leaseToken)
}

func (r *ChannelMediaReconciler) settleDeletedObject(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) {

	delTimeout := r.deleteTimeout
	if delTimeout == 0 {
		delTimeout = channelMediaReconcileDeleteTimeout
	}
	delCtx, delCancel := context.WithTimeout(ctx, delTimeout)
	err := r.Storage.DeleteObject(delCtx, row.StorageKey)
	delCancel()
	if err != nil {
		if r.Metrics != nil {
			r.Metrics.DeleteFailures.Inc()
		}
		r.release(ctx, row, leaseToken, err)
		return
	}

	if next, idx, ok := r.nextTombstonePass(row); ok {
		if r.tombstoneRow(ctx, row, leaseToken, next, idx) {
			msg := "channel media reconciler: deleted unreferenced object; tombstoned for re-delete"
			if row.State == tombstonedState {

				msg = "channel media reconciler: tombstone re-delete pass done"
			}
			r.logger().Info(msg,
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID,
				"attempt", row.Attempt,
				"tombstone_pass", idx,
				"next_redelete_in", next)
			if r.Metrics != nil && row.State != tombstonedState {

				r.Metrics.ObjectsDeleted.Inc()
			}
		}
		return
	}
	if r.clearRow(ctx, row, leaseToken) {
		r.logger().Info("channel media reconciler: tombstone schedule exhausted; ledger row cleared",
			"storage_key", row.StorageKey,
			"workspace_id", row.WorkspaceID,
			"chat_message_id", row.ChatMessageID,
			"attempt", row.Attempt)
	}
}

const tombstonedState = "tombstoned"

func (r *ChannelMediaReconciler) nextTombstonePass(row db.ChannelMediaPendingObject) (delay time.Duration, idx int, ok bool) {
	idx = 0
	if row.State == tombstonedState {
		idx = int(row.TombstonePass) + 1
	}
	if idx >= len(channelMediaTombstoneRedelete) {
		return 0, 0, false
	}
	return channelMediaTombstoneRedelete[idx], idx, true
}

func (r *ChannelMediaReconciler) tombstoneRow(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID, next time.Duration, idx int) bool {
	if ctx.Err() != nil {

		return false
	}
	n, err := r.Queries.TombstoneChannelMediaPendingObject(ctx, db.TombstoneChannelMediaPendingObjectParams{
		StorageKey:    row.StorageKey,
		WorkspaceID:   row.WorkspaceID,
		LeaseToken:    leaseToken,
		RedeleteDelay: pgInterval(next),
		TombstonePass: int32(idx),
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: tombstone failed", "storage_key", row.StorageKey, "error", err)
		return false
	}
	return n > 0
}

func (r *ChannelMediaReconciler) release(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID, cause error) {
	if ctx.Err() != nil {

		return
	}
	backoff := channelMediaReconcileBackoffBase << min(row.Attempt-1, 10)
	if backoff > channelMediaReconcileBackoffCap || backoff <= 0 {
		backoff = channelMediaReconcileBackoffCap
	}
	r.logger().Warn("channel media reconciler: settle failed; backing off",
		"storage_key", row.StorageKey,
		"workspace_id", row.WorkspaceID,
		"attempt", row.Attempt,
		"backoff", backoff,
		"error", cause)
	if err := r.Queries.ReleaseChannelMediaPendingObject(ctx, db.ReleaseChannelMediaPendingObjectParams{
		StorageKey:  row.StorageKey,
		WorkspaceID: row.WorkspaceID,
		LeaseToken:  leaseToken,
		Backoff:     pgInterval(backoff),
		LastError:   pgtype.Text{String: cause.Error(), Valid: true},
	}); err != nil {

		r.logger().Warn("channel media reconciler: release failed", "storage_key", row.StorageKey, "error", err)
	}
}

func (r *ChannelMediaReconciler) clearRow(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) bool {
	if ctx.Err() != nil {

		return false
	}
	n, err := r.Queries.DeleteChannelMediaPendingObject(ctx, db.DeleteChannelMediaPendingObjectParams{
		StorageKey:  row.StorageKey,
		WorkspaceID: row.WorkspaceID,
		LeaseToken:  leaseToken,
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: clear row failed", "storage_key", row.StorageKey, "error", err)
		return false
	}
	return n > 0
}
