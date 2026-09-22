package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type fakeObjectDeleter struct {
	mu       sync.Mutex
	deleted  []string
	err      error
	onDelete func(key string)
}

func (f *fakeObjectDeleter) DeleteObject(_ context.Context, key string) error {
	f.mu.Lock()
	hook := f.onDelete
	f.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeObjectDeleter) deletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

type reconcilerFixture struct {
	pool        *pgxpool.Pool
	workspaceID string
	sessionID   string
	messageID   string
}

func seedReconcilerFixture(t *testing.T, pool *pgxpool.Pool) reconcilerFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	f := reconcilerFixture{pool: pool}

	var userID, runtimeID, agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Reconciler test", fmt.Sprintf("media-reconciler-%d@goosar.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description) VALUES ($1, $2, '') RETURNING id`,
		"Reconciler test", fmt.Sprintf("media-reconciler-%d", suffix)).Scan(&f.workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, f.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, f.workspaceID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'local', 'goosar_daemon', $3) RETURNING id`,
		f.workspaceID, fmt.Sprintf("media-reconciler-rt-%d", suffix), userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4) RETURNING id`,
		f.workspaceID, fmt.Sprintf("media-reconciler-agent-%d", suffix), runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'reconciler test') RETURNING id`,
		f.workspaceID, agentID, userID).Scan(&f.sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_ingested)
		VALUES ($1, 'user', '[Image]', TRUE) RETURNING id`, f.sessionID).Scan(&f.messageID); err != nil {
		t.Fatalf("create chat message: %v", err)
	}
	return f
}

func (f reconcilerFixture) seedLedgerRow(t *testing.T, key, url, state string, age time.Duration) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO channel_media_pending_object (storage_key, workspace_id, chat_message_id, storage_url, state, created_at, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, now() - $6::interval, now() - $6::interval)
	`, key, f.workspaceID, f.messageID, url, state, age.String()); err != nil {
		t.Fatalf("seed ledger row: %v", err)
	}
}

func (f reconcilerFixture) rowState(t *testing.T, key string) (state string, attempt int, exists bool) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(), `
		SELECT state, attempt FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&state, &attempt)
	if err != nil {
		return "", 0, false
	}
	return state, attempt, true
}

func (f reconcilerFixture) tombstonePass(t *testing.T, key string) int {
	t.Helper()
	var pass int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT tombstone_pass FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&pass); err != nil {
		t.Fatalf("read tombstone_pass: %v", err)
	}
	return pass
}

func (f reconcilerFixture) makeDue(t *testing.T, key string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
	`, key); err != nil {
		t.Fatalf("make row due: %v", err)
	}
}

func (f reconcilerFixture) deadlines(t *testing.T, key string) (lease, next, dbNow time.Time) {
	t.Helper()
	var l, n pgtype.Timestamptz
	if err := f.pool.QueryRow(context.Background(), `
		SELECT lease_expires_at, next_attempt_at, now() FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&l, &n, &dbNow); err != nil {
		t.Fatalf("read deadlines: %v", err)
	}
	return l.Time, n.Time, dbNow
}

func (f reconcilerFixture) bindAttachment(t *testing.T, url string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO attachment (workspace_id, chat_session_id, chat_message_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		SELECT $1, $2, $3, 'member', creator_id, 'img.png', $4, 'image/png', 1
		FROM chat_session WHERE id = $2
	`, f.workspaceID, f.sessionID, f.messageID, url); err != nil {
		t.Fatalf("bind attachment: %v", err)
	}
}

func TestChannelMediaReconciler_SettlesThreeStates(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/referenced", "https://cdn.test/referenced", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	f.bindAttachment(t, "https://cdn.test/referenced")
	f.seedLedgerRow(t, "ws/lark/orphan", "https://cdn.test/orphan", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	f.seedLedgerRow(t, "ws/lark/young", "https://cdn.test/young", "pending", time.Minute)

	rec.RunOnce(context.Background())

	if _, _, exists := f.rowState(t, "ws/lark/referenced"); exists {
		t.Fatal("referenced row must be cleared")
	}

	if state, _, exists := f.rowState(t, "ws/lark/orphan"); !exists || state != "tombstoned" {
		t.Fatalf("orphan row = (%q, %v), want a 'tombstoned' row after its delete", state, exists)
	}
	if state, _, exists := f.rowState(t, "ws/lark/young"); !exists || state != "pending" {
		t.Fatalf("young row = (%q, %v), want untouched 'pending'", state, exists)
	}
	deleted := deleter.deletedKeys()
	if len(deleted) != 1 || deleted[0] != "ws/lark/orphan" {
		t.Fatalf("deleted keys = %v, want only the orphan", deleted)
	}
}

func TestChannelMediaReconciler_ReclaimsExpiredLease(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/crashed", "https://cdn.test/crashed", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object
		SET state = 'deleting', lease_token = $2, lease_expires_at = now() - interval '1 minute'
		WHERE storage_key = $1
	`, "ws/lark/crashed", util.MustParseUUID("99999999-9999-4999-8999-999999999999")); err != nil {
		t.Fatalf("simulate crashed claim: %v", err)
	}

	rec.RunOnce(context.Background())

	if state, _, exists := f.rowState(t, "ws/lark/crashed"); !exists || state != "tombstoned" {
		t.Fatalf("expired-lease row = (%q, %v), want reclaimed and settled to 'tombstoned'", state, exists)
	}
	if deleted := deleter.deletedKeys(); len(deleted) != 1 || deleted[0] != "ws/lark/crashed" {
		t.Fatalf("deleted keys = %v, want the reclaimed key", deleted)
	}
}

func TestChannelMediaReconciler_DeleteFailureBacksOffThenRetries(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{err: errors.New("storage unavailable")}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/flaky", "https://cdn.test/flaky", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	rec.RunOnce(context.Background())

	state, attempt, exists := f.rowState(t, "ws/lark/flaky")
	if !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row after failed delete = (%q, attempt=%d, %v), want ('deleting', 1, true)", state, attempt, exists)
	}
	var due bool
	if err := pool.QueryRow(context.Background(), `
		SELECT next_attempt_at > now() FROM channel_media_pending_object WHERE storage_key = $1
	`, "ws/lark/flaky").Scan(&due); err != nil || !due {
		t.Fatalf("failed delete must back off next_attempt_at (future=%v, err=%v)", due, err)
	}

	rec.RunOnce(context.Background())
	if _, attempt2, _ := f.rowState(t, "ws/lark/flaky"); attempt2 != 1 {
		t.Fatalf("backoff violated: attempt = %d, want still 1", attempt2)
	}

	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() WHERE storage_key = $1
	`, "ws/lark/flaky"); err != nil {
		t.Fatalf("expire backoff: %v", err)
	}
	deleter.mu.Lock()
	deleter.err = nil
	deleter.mu.Unlock()
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, "ws/lark/flaky"); !exists || state != "tombstoned" {
		t.Fatalf("retried delete row = (%q, %v), want settled to 'tombstoned'", state, exists)
	}
}

func TestChannelMediaReconciler_LeavesFreshPendingToBind(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/inflight", "https://cdn.test/inflight", "pending", time.Second)
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, "ws/lark/inflight"); !exists || state != "pending" {
		t.Fatalf("in-flight row = (%q, %v), want untouched so the bind can claim it", state, exists)
	}

	tag, err := pool.Exec(context.Background(), `
		DELETE FROM channel_media_pending_object WHERE storage_key = $1 AND state = 'pending'
	`, "ws/lark/inflight")
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("bind-side claim failed: rows=%d err=%v", tag.RowsAffected(), err)
	}
	if len(deleter.deletedKeys()) != 0 {
		t.Fatalf("nothing should have been deleted: %v", deleter.deletedKeys())
	}
}

func TestChannelMediaReconciler_SettleInvariantDwarfsPipelineBudgets(t *testing.T) {

	const maxPipelineBudget = 45 * time.Second
	if ChannelMediaReconcileSettleDelay < 10*maxPipelineBudget {
		t.Fatalf("settle %v must be >= 10x the largest pipeline budget %v", ChannelMediaReconcileSettleDelay, maxPipelineBudget)
	}
	if channelMediaReconcileLease <= 0 || channelMediaReconcileLease >= ChannelMediaReconcileSettleDelay {
		t.Fatalf("lease %v must be positive and well under settle %v", channelMediaReconcileLease, ChannelMediaReconcileSettleDelay)
	}

	if channelMediaReconcileLease < 2*channelMediaReconcileDeleteTimeout {
		t.Fatalf("lease %v must be >= 2x the per-delete timeout %v", channelMediaReconcileLease, channelMediaReconcileDeleteTimeout)
	}
}

func TestChannelMediaReconciler_NilStorageSkipsSweepWithoutClaiming(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: nil}

	f.seedLedgerRow(t, "ws/lark/nil-storage", "https://cdn.test/nil-storage", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	rec.RunOnce(context.Background())

	state, attempt, exists := f.rowState(t, "ws/lark/nil-storage")
	if !exists || state != "pending" || attempt != 0 {
		t.Fatalf("row = (%q, attempt=%d, %v), want untouched 'pending' when storage is missing", state, attempt, exists)
	}
}

func TestChannelMediaReconciler_WrongWorkspaceCannotReleaseOrDelete(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	q := db.New(pool)

	key := "ws/lark/tenancy"
	f.seedLedgerRow(t, key, "https://cdn.test/tenancy", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	lease := util.MustParseUUID("88888888-8888-4888-8888-888888888888")
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET state = 'deleting', lease_token = $2 WHERE storage_key = $1
	`, key, lease); err != nil {
		t.Fatalf("claim row: %v", err)
	}
	otherWorkspace := util.MustParseUUID("77777777-7777-4777-8777-777777777777")

	if err := q.ReleaseChannelMediaPendingObject(context.Background(), db.ReleaseChannelMediaPendingObjectParams{
		StorageKey:  key,
		WorkspaceID: otherWorkspace,
		LeaseToken:  lease,
		Backoff:     pgInterval(time.Hour),
		LastError:   pgtype.Text{String: "cross-tenant", Valid: true},
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	n, err := q.DeleteChannelMediaPendingObject(context.Background(), db.DeleteChannelMediaPendingObjectParams{
		StorageKey:  key,
		WorkspaceID: otherWorkspace,
		LeaseToken:  lease,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != 0 {
		t.Fatalf("cross-workspace delete removed %d rows, want 0", n)
	}

	var state string
	var leaseStillHeld bool
	if err := pool.QueryRow(context.Background(), `
		SELECT state, lease_token IS NOT NULL FROM channel_media_pending_object WHERE storage_key = $1
	`, key).Scan(&state, &leaseStillHeld); err != nil {
		t.Fatalf("load row: %v", err)
	}
	if state != "deleting" || !leaseStillHeld {
		t.Fatalf("row = (state=%q, lease_held=%v), want untouched ('deleting', true)", state, leaseStillHeld)
	}
}

type blockingDeleter struct{}

func (blockingDeleter) DeleteObject(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestChannelMediaReconciler_StalledDeleteIsBoundedAndBacksOff(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: blockingDeleter{}, deleteTimeout: 50 * time.Millisecond}

	f.seedLedgerRow(t, "ws/lark/stalled", "https://cdn.test/stalled", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	start := time.Now()
	rec.RunOnce(context.Background())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stalled delete wedged the sweep for %v", elapsed)
	}
	state, attempt, exists := f.rowState(t, "ws/lark/stalled")
	if !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row = (%q, attempt=%d, %v), want released 'deleting' with backoff", state, attempt, exists)
	}
}

func TestChannelMediaReconciler_CancelledSweepIsQuiet(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/cancelled"
	f.seedLedgerRow(t, key, "https://cdn.test/cancelled", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	var logs bytes.Buffer
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{
		Queries: db.New(pool),
		Storage: deleter,

		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec.RunOnce(ctx)

	if logs.Len() != 0 {
		t.Fatalf("cancelled sweep logged %q, want no warnings", logs.String())
	}
	if deleted := deleter.deletedKeys(); len(deleted) != 0 {
		t.Fatalf("cancelled sweep deleted %v, want nothing", deleted)
	}
	if state, attempt, exists := f.rowState(t, key); !exists || state != "pending" || attempt != 0 {
		t.Fatalf("row = (%q, attempt=%d, %v), want an untouched ('pending', 0)", state, attempt, exists)
	}
}

func TestChannelMediaReconciler_CancelledSettleIsQuiet(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/cancelled-mid-settle"
	f.seedLedgerRow(t, key, "https://cdn.test/cancelled-mid-settle", "pending", ChannelMediaReconcileSettleDelay+time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	deleter := &fakeObjectDeleter{err: context.Canceled}
	deleter.onDelete = func(string) { cancel() }
	var logs bytes.Buffer
	rec := &ChannelMediaReconciler{
		Queries: db.New(pool),
		Storage: deleter,
		Logger:  slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	defer cancel()

	rec.RunOnce(ctx)

	if logs.Len() != 0 {
		t.Fatalf("sweep cancelled during a delete logged %q, want no warnings", logs.String())
	}

	if state, attempt, exists := f.rowState(t, key); !exists || state != "deleting" || attempt != 1 {
		t.Fatalf("row = (%q, attempt=%d, %v), want a still-claimed ('deleting', 1)", state, attempt, exists)
	}
}

func TestChannelMediaReconciler_TailRowIsUnclaimedUntilItsTurn(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	var tailState string
	var tailAttempt int
	deleter.onDelete = func(key string) {
		if key != "ws/lark/first" {
			return
		}
		tailState, tailAttempt, _ = f.rowState(t, "ws/lark/second")
	}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}

	f.seedLedgerRow(t, "ws/lark/first", "https://cdn.test/first", "pending", ChannelMediaReconcileSettleDelay+2*time.Minute)
	f.seedLedgerRow(t, "ws/lark/second", "https://cdn.test/second", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())

	if tailState != "pending" || tailAttempt != 0 {
		t.Fatalf("tail row during the first delete = (%q, attempt=%d), want an unclaimed ('pending', 0)", tailState, tailAttempt)
	}

	if deleted := deleter.deletedKeys(); len(deleted) != 2 {
		t.Fatalf("deleted keys = %v, want both rows settled in one sweep", deleted)
	}
	for _, key := range []string{"ws/lark/first", "ws/lark/second"} {
		state, attempt, exists := f.rowState(t, key)
		if !exists || state != "tombstoned" || attempt != 1 {
			t.Fatalf("row %s = (%q, attempt=%d, %v), want ('tombstoned', 1, true) — one claim, one delete", key, state, attempt, exists)
		}
	}
}

type reappearingDeleter struct {
	mu           sync.Mutex
	present      bool
	deleteCalls  int
	lateMaterial func(call int) bool
}

func (d *reappearingDeleter) DeleteObject(_ context.Context, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteCalls++
	d.present = false

	if d.lateMaterial != nil && d.lateMaterial(d.deleteCalls) {
		d.present = true
	}
	return nil
}

func (d *reappearingDeleter) state(t *testing.T) (present bool, calls int) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.present, d.deleteCalls
}

func TestChannelMediaReconciler_LatePutAfterDeleteIsReclaimedByTombstone(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)

	deleter := &reappearingDeleter{present: true, lateMaterial: func(call int) bool { return call == 1 }}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/late-put"
	f.seedLedgerRow(t, key, "https://cdn.test/late-put", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())
	present, calls := deleter.state(t)
	if calls != 1 || !present {
		t.Fatalf("after first pass: calls=%d present=%v, want 1/true (late PUT landed)", calls, present)
	}
	state, _, exists := f.rowState(t, key)
	if !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want a surviving 'tombstoned' row to catch the late object", state, exists)
	}

	for pass := 2; pass <= len(channelMediaTombstoneRedelete); pass++ {
		if _, err := pool.Exec(context.Background(), `
			UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
		`, key); err != nil {
			t.Fatalf("make tombstone due: %v", err)
		}
		rec.RunOnce(context.Background())
		if _, calls := deleter.state(t); calls != pass {
			t.Fatalf("pass %d: delete calls = %d, want %d", pass, calls, pass)
		}
	}

	if present, _ := deleter.state(t); present {
		t.Fatal("late-materialized object survived the tombstone schedule")
	}
	if _, err := pool.Exec(context.Background(), `
		UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
	`, key); err != nil {
		t.Fatalf("make tombstone due: %v", err)
	}
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want cleared once the re-delete schedule is exhausted", state)
	}
}

func TestChannelMediaReconciler_TombstoneSchedulesThenClears(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/tombstone-walk"
	f.seedLedgerRow(t, key, "https://cdn.test/tombstone-walk", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	for pass := 1; pass <= len(channelMediaTombstoneRedelete); pass++ {
		rec.RunOnce(context.Background())
		state, _, exists := f.rowState(t, key)
		if !exists || state != "tombstoned" {
			t.Fatalf("pass %d: row = (%q, %v), want 'tombstoned'", pass, state, exists)
		}
		if _, err := pool.Exec(context.Background(), `
			UPDATE channel_media_pending_object SET next_attempt_at = now() - interval '1 second' WHERE storage_key = $1
		`, key); err != nil {
			t.Fatalf("make tombstone due: %v", err)
		}
	}
	rec.RunOnce(context.Background())
	if _, _, exists := f.rowState(t, key); exists {
		t.Fatal("row must clear after the schedule is exhausted")
	}
	if got := len(deleter.deletedKeys()); got != len(channelMediaTombstoneRedelete)+1 {
		t.Fatalf("delete calls = %d, want one per pass plus the final one", got)
	}
}

func TestChannelMediaReconciler_TombstoneKeepsReferencedObject(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	m := metrics.NewChannelMediaReconcilerMetrics()
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter, Metrics: m}
	key := "ws/lark/tombstone-vs-attachment"
	url := "https://cdn.test/tombstone-vs-attachment"
	f.seedLedgerRow(t, key, url, "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want 'tombstoned'", state, exists)
	}

	f.bindAttachment(t, url)
	f.makeDue(t, key)
	rec.RunOnce(context.Background())

	if got := len(deleter.deletedKeys()); got != 1 {
		t.Fatalf("delete calls = %d, want the referenced object left alone (only the first delete)", got)
	}
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want it cleared once the object is known to be referenced", state)
	}
	if got := testutil.ToFloat64(m.TombstoneReferenced); got != 1 {
		t.Fatalf("tombstone-referenced anomaly counter = %v, want 1", got)
	}
}

func TestChannelMediaReconciler_DeadlinesComeFromTheDatabaseClock(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	key := "ws/lark/db-clock"
	f.seedLedgerRow(t, key, "https://cdn.test/db-clock", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	var leaseAhead time.Duration
	deleter := &fakeObjectDeleter{err: errors.New("storage down")}
	deleter.onDelete = func(string) {
		lease, _, dbNow := f.deadlines(t, key)
		leaseAhead = lease.Sub(dbNow)
	}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	rec.RunOnce(context.Background())

	if leaseAhead <= 0 || leaseAhead > channelMediaReconcileLease {
		t.Fatalf("lease expires %v after the database's now(), want (0, %v]", leaseAhead, channelMediaReconcileLease)
	}

	_, next, dbNow := f.deadlines(t, key)
	if ahead := next.Sub(dbNow); ahead <= 0 || ahead > channelMediaReconcileBackoffBase {
		t.Fatalf("backoff lands %v after the database's now(), want (0, %v]", ahead, channelMediaReconcileBackoffBase)
	}

	deleter.err = nil
	deleter.onDelete = nil
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	_, next, dbNow = f.deadlines(t, key)
	if ahead := next.Sub(dbNow); ahead <= 0 || ahead > channelMediaTombstoneRedelete[0] {
		t.Fatalf("re-delete lands %v after the database's now(), want (0, %v]", ahead, channelMediaTombstoneRedelete[0])
	}
}

func TestChannelMediaReconciler_TombstonePassSurvivesDeleteFailure(t *testing.T) {
	pool := newCancelFinalizePool(t)
	f := seedReconcilerFixture(t, pool)
	deleter := &fakeObjectDeleter{}
	rec := &ChannelMediaReconciler{Queries: db.New(pool), Storage: deleter}
	key := "ws/lark/tombstone-pass-survives"
	f.seedLedgerRow(t, key, "https://cdn.test/tombstone-pass-survives", "pending", ChannelMediaReconcileSettleDelay+time.Minute)

	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 0 {
		t.Fatalf("tombstone_pass = %d after the first delete, want 0", got)
	}

	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 1 {
		t.Fatalf("tombstone_pass = %d after one re-delete, want 1", got)
	}

	deleter.err = errors.New("storage unavailable")
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if state, _, exists := f.rowState(t, key); !exists || state != "tombstoned" {
		t.Fatalf("row = (%q, %v), want the failed re-delete to leave a tombstone", state, exists)
	}
	if got := f.tombstonePass(t, key); got != 1 {
		t.Fatalf("tombstone_pass = %d after a failed re-delete, want it held at 1", got)
	}

	deleter.err = nil
	f.makeDue(t, key)
	rec.RunOnce(context.Background())
	if got := f.tombstonePass(t, key); got != 2 {
		t.Fatalf("tombstone_pass = %d after recovery, want the schedule to resume at 2", got)
	}

	for range channelMediaTombstoneRedelete[2:] {
		f.makeDue(t, key)
		rec.RunOnce(context.Background())
	}
	if state, _, exists := f.rowState(t, key); exists {
		t.Fatalf("row = %q, want it dropped once the schedule is exhausted", state)
	}
}
