package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestMarkRuntimesOfflineByIDs_RespectsConcurrentHeartbeat(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	staleSeed := time.Duration(staleThresholdSeconds*2) * time.Second
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude',
			'online', '', '{}'::jsonb, now() - make_interval(secs => $3))
		RETURNING id
	`, testWorkspaceID, "race-test-runtime", staleSeed.Seconds()).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`,
		runtimeID,
	); err != nil {
		t.Fatalf("simulate concurrent heartbeat: %v", err)
	}

	rows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{parseUUID(runtimeID)},
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows offlined (stale predicate should veto), got %d", len(rows))
	}

	var status string
	var lastSeen time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT status, last_seen_at FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status, &lastSeen); err != nil {
		t.Fatalf("read back runtime: %v", err)
	}
	if status != "online" {
		t.Fatalf("runtime was incorrectly marked offline despite fresh heartbeat: status=%q", status)
	}
	if time.Since(lastSeen) > 30*time.Second {
		t.Fatalf("last_seen_at not preserved: %s ago", time.Since(lastSeen))
	}
}

func TestMarkRuntimesOfflineByIDs_OfflinesGenuinelyStale(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	staleSeed := time.Duration(staleThresholdSeconds*2) * time.Second
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'claude',
			'online', '', '{}'::jsonb, now() - make_interval(secs => $3))
		RETURNING id
	`, testWorkspaceID, "race-test-stale-runtime", staleSeed.Seconds()).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	rows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          []pgtype.UUID{parseUUID(runtimeID)},
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("MarkRuntimesOfflineByIDs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row offlined, got %d", len(rows))
	}

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&status); err != nil {
		t.Fatalf("read back runtime: %v", err)
	}
	if status != "offline" {
		t.Fatalf("genuinely-stale runtime not marked offline: status=%q", status)
	}
}
