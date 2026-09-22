package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func TestIsDuplicatePendingTaskErr(t *testing.T) {
	dup := &pgconn.PgError{Code: "23505", ConstraintName: "idx_one_pending_task_per_issue_agent"}
	if !isDuplicatePendingTaskErr(dup) {
		t.Fatal("expected the pending-task unique-index violation to be recognized")
	}
	if isDuplicatePendingTaskErr(&pgconn.PgError{Code: "23505", ConstraintName: "agent_workspace_name_unique"}) {
		t.Fatal("a different unique constraint must not be treated as a duplicate pending task")
	}
	if isDuplicatePendingTaskErr(errors.New("boom")) {
		t.Fatal("a non-pg error must not be treated as a duplicate pending task")
	}

	wrapped := fmt.Errorf("%w: %v", ErrDuplicatePendingTask, dup)
	if !errors.Is(wrapped, ErrDuplicatePendingTask) {
		t.Fatal("the wrapped sentinel must stay detectable via errors.Is")
	}
}

func TestEnqueueTaskForMentionCoalescesDuplicatePendingTask(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	issueStruct := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	if _, err := svc.EnqueueTaskForMention(ctx, issueStruct, util.MustParseUUID(agentID), pgtype.UUID{}); err != nil {
		t.Fatalf("first EnqueueTaskForMention: %v", err)
	}

	_, err := svc.EnqueueTaskForMention(ctx, issueStruct, util.MustParseUUID(agentID), pgtype.UUID{})
	if !errors.Is(err, ErrDuplicatePendingTask) {
		t.Fatalf("second EnqueueTaskForMention: err = %v, want ErrDuplicatePendingTask", err)
	}

	for _, leak := range []string{"idx_one_pending_task_per_issue_agent", "23505", "SQLSTATE", "duplicate key"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("duplicate error leaked %q: %v", leak, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched')`,
		issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	if n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1", n)
	}
}
