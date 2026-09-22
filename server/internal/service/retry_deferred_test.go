package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestCreateRetryTaskFireAtControlsDeferral(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, failure_reason, session_id, work_dir)
		VALUES ($1, $2, $3, 'failed', 0, 2, 2, 'agent_error.provider_network', 'src-session', '/tmp/src-workdir')
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&parentID); err != nil {
		t.Fatalf("insert parent task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
	})

	cases := []struct {
		name            string
		fireAt          pgtype.Timestamptz
		maxAttempts     pgtype.Int4
		wantStatus      string
		wantFireAt      bool
		wantMaxAttempts int32
	}{

		{"deferred final tier persists budget", pgtype.Timestamptz{Time: time.Now().Add(5 * time.Second), Valid: true}, pgtype.Int4{Int32: 3, Valid: true}, "deferred", true, 3},

		{"queued immediate tier inherits budget", pgtype.Timestamptz{}, pgtype.Int4{}, "queued", false, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			child, err := q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID, FireAt: tc.fireAt, MaxAttempts: tc.maxAttempts})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, child.ID) })

			if child.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", child.Status, tc.wantStatus)
			}
			if child.FireAt.Valid != tc.wantFireAt {
				t.Errorf("fire_at valid = %v, want %v", child.FireAt.Valid, tc.wantFireAt)
			}
			if child.Attempt != 3 {
				t.Errorf("attempt = %d, want 3 (parent attempt 2 + 1)", child.Attempt)
			}
			if child.MaxAttempts != tc.wantMaxAttempts {
				t.Errorf("max_attempts = %d, want %d", child.MaxAttempts, tc.wantMaxAttempts)
			}

			if child.ForceFreshSession {
				t.Errorf("force_fresh_session = true, want false (provider_network resumes) for %s", util.UUIDToString(child.ID))
			}
		})
	}
}

func TestFailTaskProviderNetworkBudget(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	cases := []struct {
		name         string
		attempt      int32
		maxAttempts  int32
		wantChild    bool
		wantAttempt  int32
		wantMax      int32
		wantDeferred bool
	}{

		{"final tier persists raised budget", 2, 2, true, 3, 3, true},

		{"first retry is immediate", 1, 2, true, 2, 3, false},

		{"disabled budget is never revived", 1, 1, false, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID pgtype.UUID
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts, session_id, work_dir)
				VALUES ($1, $2, $3, 'running', 0, $4, $5, 'src-session', '/tmp/src-workdir')
				RETURNING id
			`, agentID, runtimeID, issueID, tc.attempt, tc.maxAttempts).Scan(&parentID); err != nil {
				t.Fatalf("insert parent task: %v", err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1`, parentID)
			})

			if _, err := svc.FailTask(ctx, parentID, "API Error: Connection closed mid-response.", "src-session", "/tmp/src-workdir", "agent_error.provider_network", false); err != nil {
				t.Fatalf("FailTask: %v", err)
			}

			var (
				childAttempt, childMax int32
				childStatus            string
				n                      int
			)
			row := pool.QueryRow(ctx, `SELECT count(*), coalesce(max(attempt),0), coalesce(max(max_attempts),0), coalesce(max(status),'') FROM agent_task_queue WHERE parent_task_id = $1`, parentID)
			if err := row.Scan(&n, &childAttempt, &childMax, &childStatus); err != nil {
				t.Fatalf("read child: %v", err)
			}
			if !tc.wantChild {
				if n != 0 {
					t.Fatalf("expected no retry child, got %d", n)
				}
				return
			}
			if n != 1 {
				t.Fatalf("expected exactly one retry child, got %d", n)
			}
			if childAttempt != tc.wantAttempt {
				t.Errorf("child attempt = %d, want %d", childAttempt, tc.wantAttempt)
			}
			if childMax != tc.wantMax {
				t.Errorf("child max_attempts = %d, want %d (self-consistent budget)", childMax, tc.wantMax)
			}
			gotDeferred := childStatus == "deferred"
			if gotDeferred != tc.wantDeferred {
				t.Errorf("child status = %q, want deferred=%v", childStatus, tc.wantDeferred)
			}
		})
	}
}
