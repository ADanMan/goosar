package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type HeartbeatScheduler interface {
	Schedule(ctx context.Context, rt db.AgentRuntime) error
}

type PassthroughHeartbeatScheduler struct {
	queries *db.Queries
}

func NewPassthroughHeartbeatScheduler(queries *db.Queries) *PassthroughHeartbeatScheduler {
	return &PassthroughHeartbeatScheduler{queries: queries}
}

func (p *PassthroughHeartbeatScheduler) Schedule(ctx context.Context, rt db.AgentRuntime) error {
	if rt.Status == "online" && rt.LastSeenAt.Valid {
		rows, err := p.queries.TouchAgentRuntimeLastSeen(ctx, rt.ID)
		if err != nil {
			return err
		}
		if rows > 0 {
			return nil
		}

	}
	_, err := p.queries.MarkAgentRuntimeOnline(ctx, rt.ID)
	return err
}

type BatchedHeartbeatScheduler struct {
	queries      *db.Queries
	fallback     *PassthroughHeartbeatScheduler
	tickInterval time.Duration

	mu      sync.Mutex
	pending map[pgtype.UUID]struct{}

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

const DefaultHeartbeatBatchInterval = 30 * time.Second

func NewBatchedHeartbeatScheduler(queries *db.Queries, tickInterval time.Duration) *BatchedHeartbeatScheduler {
	if tickInterval <= 0 {
		tickInterval = DefaultHeartbeatBatchInterval
	}
	return &BatchedHeartbeatScheduler{
		queries:      queries,
		fallback:     NewPassthroughHeartbeatScheduler(queries),
		tickInterval: tickInterval,
		pending:      make(map[pgtype.UUID]struct{}),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

func (b *BatchedHeartbeatScheduler) Schedule(ctx context.Context, rt db.AgentRuntime) error {

	if rt.Status != "online" || !rt.LastSeenAt.Valid {
		return b.fallback.Schedule(ctx, rt)
	}
	b.mu.Lock()
	b.pending[rt.ID] = struct{}{}
	b.mu.Unlock()
	return nil
}

func (b *BatchedHeartbeatScheduler) Run(ctx context.Context) {
	defer close(b.doneCh)
	t := time.NewTicker(b.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-b.stopCh:

			drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			b.flushOnce(drainCtx)
			cancel()
			return
		case <-ctx.Done():
			drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			b.flushOnce(drainCtx)
			cancel()
			return
		case <-t.C:
			b.flushOnce(ctx)
		}
	}
}

func (b *BatchedHeartbeatScheduler) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
	<-b.doneCh
	finalCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	b.flushOnce(finalCtx)
	cancel()
}

func (b *BatchedHeartbeatScheduler) FlushNow(ctx context.Context) {
	b.flushOnce(ctx)
}

func (b *BatchedHeartbeatScheduler) PendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *BatchedHeartbeatScheduler) flushOnce(ctx context.Context) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	ids := make([]pgtype.UUID, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	b.pending = make(map[pgtype.UUID]struct{})
	b.mu.Unlock()

	rows, err := b.queries.TouchAgentRuntimesLastSeenBatch(ctx, ids)
	if err != nil {

		slog.Warn("heartbeat batch flush failed",
			"scheduled", len(ids), "error", err)
		return
	}
	if int(rows) < len(ids) {

		slog.Info("heartbeat batch flush: some runtimes raced to offline",
			"scheduled", len(ids), "affected", rows)
	}
}
