package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adanman/goosar/server/internal/attribution"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func seedAttributionFixture(t *testing.T, pool *pgxpool.Pool) (workspaceID, userID, agentID, issueID string) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Attr User', $1) RETURNING id`,
		fmt.Sprintf("attr-%d@goosar.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('attr ws', $1) RETURNING id`,
		fmt.Sprintf("attr-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'attr-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'attr-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority)
		VALUES ($1, 'attr issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id`, workspaceID, userID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return workspaceID, userID, agentID, issueID
}

func TestEnqueueTaskForIssueStampsDirectHumanAttribution(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, evidenceRef pgtype.UUID
	var evidenceKind pgtype.Text
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, trigger_evidence_kind, trigger_evidence_ref_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &evidenceKind, &evidenceRef); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userID).Bytes {
		t.Errorf("originator_user_id = %s, want %s", util.UUIDToString(originator), userID)
	}

	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator %s", util.UUIDToString(accountable), util.UUIDToString(originator))
	}
	if evidenceKind.String != string(attribution.EvidenceIssueAssignment) {
		t.Errorf("trigger_evidence_kind = %q, want issue_assignment", evidenceKind.String)
	}
	if !evidenceRef.Valid || evidenceRef.Bytes != util.MustParseUUID(issueID).Bytes {
		t.Errorf("trigger_evidence_ref_id = %s, want issue %s", util.UUIDToString(evidenceRef), issueID)
	}
}

func TestEnqueueTaskForIssueWithHandoffAttributesToActor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)

	var actorID string
	suffix := time.Now().UnixNano()
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Actor', $1) RETURNING id`,
		fmt.Sprintf("actor-%d@goosar.test", suffix)).Scan(&actorID); err != nil {
		t.Fatalf("seed actor user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, actorID); err != nil {
		t.Fatalf("seed actor member: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssueWithHandoff(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(creatorID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}, "", util.MustParseUUID(actorID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssueWithHandoff: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("accountable_user_id = %s, want actor %s (not creator %s)", util.UUIDToString(accountable), actorID, creatorID)
	}

	if !originator.Valid || originator.Bytes != accountable.Bytes {
		t.Errorf("originator_user_id = %s, want == accountable (actor) %s", util.UUIDToString(originator), util.UUIDToString(accountable))
	}
}

func TestMergeCommentIntoPendingTask_KeepsAccountableEqualsOriginator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userA, agentID, issueID := seedAttributionFixture(t, pool)

	var userB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Attr User B', $1) RETURNING id`,
		fmt.Sprintf("attr-b-%d@goosar.test", time.Now().UnixNano())).Scan(&userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userB) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, workspaceID, userB); err != nil {
		t.Fatalf("add member B: %v", err)
	}

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $3, 'delegation')
		RETURNING id`, agentID, issueID, userA).Scan(&taskID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'B comment') RETURNING id`, issueID, workspaceID, userB).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 util.MustParseUUID(issueID),
		AgentID:                 util.MustParseUUID(agentID),
		NewTriggerCommentID:     util.MustParseUUID(commentID),
		NewOriginatorUserID:     util.MustParseUUID(userB),
		NewAccountableUserID:    util.MustParseUUID(userB),
		NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
		NewTriggerEvidenceRefID: util.MustParseUUID(commentID),
	}); err != nil {
		t.Fatalf("MergeCommentIntoPendingTask: %v", err)
	}

	var originator, accountable pgtype.UUID
	var source, evidenceKind pgtype.Text
	if err := pool.QueryRow(ctx,
		`SELECT originator_user_id, accountable_user_id, originator_source, trigger_evidence_kind FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&originator, &accountable, &source, &evidenceKind); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userB).Bytes {
		t.Errorf("originator = %s, want re-stamped to B %s", util.UUIDToString(originator), userB)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable = %s, want == originator (B); one-way invariant violated on merge", util.UUIDToString(accountable))
	}

	if source.String != "direct_human" {
		t.Errorf("originator_source = %q, want re-stamped to direct_human (stale snapshot left behind)", source.String)
	}
	if evidenceKind.String != "comment" {
		t.Errorf("trigger_evidence_kind = %q, want re-stamped to comment", evidenceKind.String)
	}
}

func TestAttributionForMergedComment_HonorsFailClosedPolicy(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, ownerID, agentID, issueID := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'autonomous ping') RETURNING id`,
		issueID, workspaceID, agentID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = true WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("set fail-closed: %v", err)
	}
	if _, err := svc.AttributionForMergedComment(ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(commentID), false, agent); !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("fail-closed merge must return ErrAttributionFailClosed, got %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = false WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("clear fail-closed: %v", err)
	}
	attr, err := svc.AttributionForMergedComment(ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(commentID), false, agent)
	if err != nil {
		t.Fatalf("fail-open merge must not error, got %v", err)
	}
	if attr.Source != attribution.SourceOwnerFallback {
		t.Errorf("fail-open unattributable merge source = %q, want owner_fallback", attr.Source)
	}
	if !attr.AccountableUserID.Valid || attr.AccountableUserID.Bytes != util.MustParseUUID(ownerID).Bytes {
		t.Errorf("owner_fallback accountable = %s, want agent owner %s", util.UUIDToString(attr.AccountableUserID), ownerID)
	}
	if attr.UserID.Valid {
		t.Errorf("owner_fallback must not set originator (authorization stays NULL), got %s", util.UUIDToString(attr.UserID))
	}
}

func TestAttributionInvariantCheck_RejectsBypass(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, userA, agentID, issueID := seedAttributionFixture(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, 'comment_source')`,
		agentID, issueID, userA); err == nil {
		t.Fatal("expected the CHECK to reject originator set with NULL accountable, but insert succeeded")
	}

	var userB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Check B', $1) RETURNING id`,
		fmt.Sprintf("check-b-%d@goosar.test", time.Now().UnixNano())).Scan(&userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userB) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $4, 'comment_source')`,
		agentID, issueID, userA, userB); err == nil {
		t.Fatal("expected the CHECK to reject originator != accountable, but insert succeeded")
	}

	var okTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $3, 'direct_human') RETURNING id`,
		agentID, issueID, userA).Scan(&okTaskID); err != nil {
		t.Fatalf("equal originator/accountable must be accepted, got %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, okTaskID) })
}

func TestAttributionInvariantCheck_RejectsUnbackfilledLegacyRows(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, userA, agentID, issueID := seedAttributionFixture(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3)`,
		agentID, issueID, userA); err == nil {
		t.Fatal("expected the strict CHECK to reject an unbackfilled legacy row, but insert succeeded")
	}
}

func TestTriggerOwnerAttribution_ScheduleTriggerCreator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'trigger-owner-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		autopilotID, creatorID).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}

	got := triggerOwnerAttribution(ctx, q,
		util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
		attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
	if got.Source != attribution.SourceTriggerOwner {
		t.Fatalf("source = %q, want trigger_owner", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("trigger_owner is audit-only; originator must stay NULL, got %s", util.UUIDToString(got.UserID))
	}
	if !got.AccountableUserID.Valid || got.AccountableUserID.Bytes != util.MustParseUUID(creatorID).Bytes {
		t.Errorf("accountable = %s, want trigger creator %s", util.UUIDToString(got.AccountableUserID), creatorID)
	}
}

func TestTriggerOwnerAttribution_LegacyTriggerFallsBackToRuleOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'legacy-trigger-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, publisherID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression)
		VALUES ($1, 'schedule', true, '0 * * * *') RETURNING id`,
		autopilotID).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3)`, autopilotID, workspaceID, publisherID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	got := triggerOwnerAttribution(ctx, q,
		util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
		attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
	if got.Source != attribution.SourceRuleOwner {
		t.Fatalf("source = %q, want rule_owner (legacy trigger falls back)", got.Source)
	}
	if !got.AccountableUserID.Valid || got.AccountableUserID.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable = %s, want rule publisher %s", util.UUIDToString(got.AccountableUserID), publisherID)
	}
}

func seedExtraMember(t *testing.T, pool *pgxpool.Pool, workspaceID, label string) string {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@goosar.test", label, suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed %s user: %v", label, err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed %s member: %v", label, err)
	}
	return userID
}

func TestTriggerOwnerAttribution_TransfersToSubstantiveEditor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, agentID, _ := seedAttributionFixture(t, pool)
	editorB := seedExtraMember(t, pool, workspaceID, "editor-b")
	editorC := seedExtraMember(t, pool, workspaceID, "editor-c")

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'transfer-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorA).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	seedTrigger := func(cron string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
			VALUES ($1, 'schedule', true, $2, 'member', $3) RETURNING id`,
			autopilotID, cron, creatorA).Scan(&id); err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		return id
	}
	trigger1 := seedTrigger("0 * * * *")
	trigger2 := seedTrigger("0 0 * * *")

	accountableOf := func(triggerID string) string {
		got := triggerOwnerAttribution(ctx, q,
			util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
			attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
		if got.Source != attribution.SourceTriggerOwner {
			t.Fatalf("trigger %s: source = %q, want trigger_owner", triggerID, got.Source)
		}
		if got.UserID.Valid {
			t.Fatalf("trigger_owner is audit-only; originator must stay NULL, got %s", util.UUIDToString(got.UserID))
		}
		return util.UUIDToString(got.AccountableUserID)
	}

	if a := accountableOf(trigger1); a != creatorA {
		t.Fatalf("trigger1 baseline accountable = %s, want creator %s", a, creatorA)
	}
	if a := accountableOf(trigger2); a != creatorA {
		t.Fatalf("trigger2 baseline accountable = %s, want creator %s", a, creatorA)
	}

	if err := q.SetAutopilotTriggerPublisher(ctx, db.SetAutopilotTriggerPublisherParams{
		ID:              util.MustParseUUID(trigger1),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorB),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublisher: %v", err)
	}

	if a := accountableOf(trigger1); a != editorB {
		t.Fatalf("after edit, trigger1 accountable = %s, want editor %s (responsibility must transfer)", a, editorB)
	}

	if a := accountableOf(trigger2); a != creatorA {
		t.Fatalf("trigger2 accountable = %s, want creator %s (editing a sibling must not transfer)", a, creatorA)
	}

	if err := q.SetAutopilotTriggerPublishersByAutopilot(ctx, db.SetAutopilotTriggerPublishersByAutopilotParams{
		AutopilotID:     util.MustParseUUID(autopilotID),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorC),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublishersByAutopilot: %v", err)
	}
	if a := accountableOf(trigger1); a != editorC {
		t.Fatalf("after autopilot-level edit, trigger1 accountable = %s, want %s", a, editorC)
	}
	if a := accountableOf(trigger2); a != editorC {
		t.Fatalf("after autopilot-level edit, trigger2 accountable = %s, want %s", a, editorC)
	}
}

func TestEnqueueTaskForIssueAutopilotOriginStampsRuleOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var ruleVersionID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES (gen_random_uuid(), $1, 'member', $2) RETURNING id, autopilot_id`,
		workspaceID, publisherID).Scan(&ruleVersionID, &autopilotID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9001, 'autopilot', $3) RETURNING id`,
		workspaceID, agentID, autopilotID).Scan(&issueID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceRuleOwner) {
		t.Errorf("originator_source = %q, want rule_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("autopilot run must NOT set originator (authorization stays NULL), got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable_user_id = %s, want rule publisher %s", util.UUIDToString(accountable), publisherID)
	}
	if !ruleVersion.Valid || ruleVersion.Bytes != util.MustParseUUID(ruleVersionID).Bytes {
		t.Errorf("rule_version_id = %s, want %s", util.UUIDToString(ruleVersion), ruleVersionID)
	}
}

func TestEnqueueTaskForIssueAutopilotOriginWithoutVersionOwnerFallback(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, ownerID, agentID, _ := seedAttributionFixture(t, pool)

	var issueID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9002, 'autopilot', gen_random_uuid()) RETURNING id, origin_id`,
		workspaceID, agentID).Scan(&issueID, &autopilotID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id FROM agent_task_queue WHERE id = $1`,
		task.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceOwnerFallback) {
		t.Errorf("originator_source = %q, want owner_fallback", source.String)
	}
	if originator.Valid {
		t.Errorf("owner_fallback is audit-only; originator must stay NULL, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(ownerID).Bytes {
		t.Errorf("accountable_user_id = %s, want agent owner %s", util.UUIDToString(accountable), ownerID)
	}
}

func TestEnqueueTaskFailClosedRefusesUnattributed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, agentID, _ := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = TRUE WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("set fail-closed: %v", err)
	}

	var issueID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9003, 'autopilot', gen_random_uuid()) RETURNING id, origin_id`,
		workspaceID, agentID).Scan(&issueID, &autopilotID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	_, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("EnqueueTaskForIssue error = %v, want ErrAttributionFailClosed", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Errorf("fail-closed must not enqueue any task, found %d", count)
	}
}

func seedRunOnlyAutopilot(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, creatorID string) (autopilotID, runID string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'run-only ap', 'agent', $2, 'active', 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status) VALUES ($1, 'manual', 'running') RETURNING id`,
		autopilotID).Scan(&runID); err != nil {
		t.Fatalf("seed autopilot run: %v", err)
	}
	return autopilotID, runID
}

func TestDispatchRunOnlyScheduleStampsRuleOwnerRow(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, runID := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, publisherID)

	var ruleVersionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3) RETURNING id`, autopilotID, workspaceID, publisherID).Scan(&ruleVersionID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if err := svc.dispatchRunOnly(ctx, ap, &run, pgtype.UUID{}); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceRuleOwner) {
		t.Errorf("originator_source = %q, want rule_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("run_only autopilot must NOT set originator, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable_user_id = %s, want publisher %s", util.UUIDToString(accountable), publisherID)
	}
	if !ruleVersion.Valid || ruleVersion.Bytes != util.MustParseUUID(ruleVersionID).Bytes {
		t.Errorf("rule_version_id = %s, want %s", util.UUIDToString(ruleVersion), ruleVersionID)
	}
}

func TestDispatchRunOnlyManualStampsDirectHuman(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, runID := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, publisherID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3)`, autopilotID, workspaceID, publisherID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}
	var actorID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Trigger', $1) RETURNING id`,
		fmt.Sprintf("trigger-%d@goosar.test", time.Now().UnixNano())).Scan(&actorID); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, actorID); err != nil {
		t.Fatalf("seed actor member: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if err := svc.dispatchRunOnly(ctx, ap, &run, util.MustParseUUID(actorID)); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("originator_user_id = %s, want triggering member %s", util.UUIDToString(originator), actorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (actor)", util.UUIDToString(accountable))
	}
	if ruleVersion.Valid {
		t.Errorf("manual direct_human must not set rule_version_id, got %s", util.UUIDToString(ruleVersion))
	}
}

func TestDispatchRunOnlyScheduleTransfersToEditor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, agentID, _ := seedAttributionFixture(t, pool)
	editorB := seedExtraMember(t, pool, workspaceID, "dispatch-editor-b")

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'dispatch-transfer-ap', 'agent', $2, 'active', 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorA).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		autopilotID, creatorA).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	if err := q.SetAutopilotTriggerPublisher(ctx, db.SetAutopilotTriggerPublisherParams{
		ID:              util.MustParseUUID(triggerID),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorB),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublisher: %v", err)
	}

	var runID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status)
		VALUES ($1, $2, 'schedule', 'running') RETURNING id`,
		autopilotID, triggerID).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if err := svc.dispatchRunOnly(ctx, ap, &run, pgtype.UUID{}); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceTriggerOwner) {
		t.Errorf("originator_source = %q, want trigger_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("schedule dispatch must NOT set originator, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(editorB).Bytes {
		t.Errorf("accountable_user_id = %s, want editor %s (dispatch must follow the transferred publisher, not creator %s)",
			util.UUIDToString(accountable), editorB, creatorA)
	}
}

func TestEnqueueTaskForIssueAutopilotManualStampsDirectHuman(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES (gen_random_uuid(), $1, 'member', $2) RETURNING autopilot_id`,
		workspaceID, publisherID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9101, 'autopilot', $3) RETURNING id`,
		workspaceID, agentID, autopilotID).Scan(&issueID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var actorID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Trigger', $1) RETURNING id`,
		fmt.Sprintf("trig2-%d@goosar.test", time.Now().UnixNano())).Scan(&actorID); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	task, err := svc.EnqueueTaskForIssueWithHandoff(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	}, "", util.MustParseUUID(actorID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssueWithHandoff: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("originator_user_id = %s, want actor %s", util.UUIDToString(originator), actorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (actor)", util.UUIDToString(accountable))
	}
	if ruleVersion.Valid {
		t.Errorf("manual direct_human must not set rule_version_id, got %s", util.UUIDToString(ruleVersion))
	}
}

func TestRecordAutopilotRuleVersionRepublishReattributes(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, memberA, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, _ := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, memberA)

	var memberB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Editor', $1) RETURNING id`,
		fmt.Sprintf("editor-%d@goosar.test", time.Now().UnixNano())).Scan(&memberB); err != nil {
		t.Fatalf("seed member B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberB) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, memberB); err != nil {
		t.Fatalf("seed member B membership: %v", err)
	}

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	verParams := db.GetActiveAutopilotRuleVersionParams{WorkspaceID: ap.WorkspaceID, AutopilotID: ap.ID}

	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", util.MustParseUUID(memberA)); err != nil {
		t.Fatalf("record v1: %v", err)
	}
	active, err := q.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil || active.PublishedByType != "member" || active.PublishedByID.Bytes != util.MustParseUUID(memberA).Bytes {
		t.Fatalf("v1 active = %+v (err %v), want member A", active, err)
	}

	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", util.MustParseUUID(memberB)); err != nil {
		t.Fatalf("record v2: %v", err)
	}
	attr := ruleOwnerAttribution(ctx, q, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, ap.ID)
	if attr.Source != attribution.SourceRuleOwner || attr.AccountableUserID.Bytes != util.MustParseUUID(memberB).Bytes {
		t.Errorf("after republish, dispatch attribution = %+v, want rule_owner accountable = member B", attr)
	}

	if err := RecordAutopilotRuleVersion(ctx, q, ap, "system", pgtype.UUID{}); err != nil {
		t.Fatalf("record v3 (system): %v", err)
	}
	active, err = q.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil || active.PublishedByType != "system" || active.PublishedByID.Valid {
		t.Errorf("v3 active = %+v (err %v), want system publisher with NULL id", active, err)
	}

	sysAttr := ruleOwnerAttribution(ctx, q, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, ap.ID)
	if sysAttr.Source != attribution.SourceUnattributed || sysAttr.AccountableUserID.Valid {
		t.Errorf("system-published version must yield unattributed, got %+v", sysAttr)
	}
}

func TestApplyAttributionFallbackRefusesOnMissingOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, _, _ := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	unattr := attribution.Unattributed(attribution.EvidenceIssueAssignment, util.MustParseUUID(workspaceID))
	_, err := svc.applyAttributionFallback(ctx, unattr, db.Agent{WorkspaceID: util.MustParseUUID(workspaceID)})
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("missing owner: err = %v, want ErrAttributionFailClosed", err)
	}
}

func TestApplyAttributionFallbackRefusesOnPolicyReadFailure(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, ownerID, _, _ := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	unattr := attribution.Unattributed(attribution.EvidenceIssueAssignment, pgtype.UUID{})
	missingWs := pgtype.UUID{Bytes: [16]byte{0xDE, 0xAD, 0xBE, 0xEF}, Valid: true}
	_, err := svc.applyAttributionFallback(ctx, unattr, db.Agent{WorkspaceID: missingWs, OwnerID: util.MustParseUUID(ownerID)})
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("policy read failure: err = %v, want ErrAttributionFailClosed", err)
	}
}

func TestApplyAttributionFallbackPreciseUntouched(t *testing.T) {
	svc := &TaskService{}
	precise := attribution.DirectHumanRun(pgtype.UUID{Bytes: [16]byte{0x11}, Valid: true}, attribution.EvidenceComment, pgtype.UUID{})
	got, err := svc.applyAttributionFallback(context.Background(), precise, db.Agent{})
	if err != nil {
		t.Fatalf("precise attribution must not error: %v", err)
	}
	if got.Source != attribution.SourceDirectHuman || got != precise {
		t.Errorf("precise attribution must pass through unchanged, got %+v", got)
	}
}

func TestRerunIssueAttributesToRerunningMember(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)

	var rerunnerID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Rerunner', $1) RETURNING id`,
		fmt.Sprintf("rerunner-%d@goosar.test", time.Now().UnixNano())).Scan(&rerunnerID); err != nil {
		t.Fatalf("seed rerunner: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, rerunnerID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, rerunnerID); err != nil {
		t.Fatalf("seed rerunner member: %v", err)
	}

	issueStruct := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(creatorID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	orig, err := svc.EnqueueTaskForIssue(ctx, issueStruct)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue (original): %v", err)
	}

	task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), orig.ID, pgtype.UUID{}, util.MustParseUUID(rerunnerID), nil)
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, rerunOf pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rerun_of_task_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &rerunOf); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(rerunnerID).Bytes {
		t.Errorf("originator_user_id = %s, want rerunner %s (not creator %s)", util.UUIDToString(originator), rerunnerID, creatorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (rerunner)", util.UUIDToString(accountable))
	}
	if !rerunOf.Valid || rerunOf.Bytes != orig.ID.Bytes {
		t.Errorf("rerun_of_task_id = %s, want original task %s", util.UUIDToString(rerunOf), util.UUIDToString(orig.ID))
	}
}

func TestEnqueueChatTaskStampsChatEvidence(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}

	var source, evidenceKind pgtype.Text
	var originator, accountable, evidenceRef pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, trigger_evidence_kind, trigger_evidence_ref_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &evidenceKind, &evidenceRef); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userID).Bytes {
		t.Errorf("originator_user_id = %s, want sender %s", util.UUIDToString(originator), userID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator", util.UUIDToString(accountable))
	}
	if evidenceKind.String != string(attribution.EvidenceChat) {
		t.Errorf("trigger_evidence_kind = %q, want chat", evidenceKind.String)
	}
	if !evidenceRef.Valid || evidenceRef.Bytes != util.MustParseUUID(chatSessionID).Bytes {
		t.Errorf("trigger_evidence_ref_id = %s, want chat session %s", util.UUIDToString(evidenceRef), chatSessionID)
	}
}

func TestEnqueueChatTaskDefersForChannelMediaAndPromotesWhenReady(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID, priorMessageID, messageID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'first') RETURNING id`, chatSessionID).Scan(&priorMessageID); err != nil {
		t.Fatalf("seed prior message: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	priorTask, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("enqueue prior chat task: %v", err)
	}
	deadline := time.Now().Add(time.Minute)
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content, channel_media_pending_until)
		VALUES ($1, 'user', '[Image]', $2) RETURNING id`, chatSessionID, deadline).Scan(&messageID); err != nil {
		t.Fatalf("seed pending media message: %v", err)
	}

	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}
	if task.Status != "deferred" || !task.FireAt.Valid {
		t.Fatalf("task = status %q fire_at %v, want durable deferred task", task.Status, task.FireAt)
	}
	if !task.ChatInputTaskID.Valid || task.ChatInputTaskID.Bytes != task.ID.Bytes {
		t.Fatalf("task input owner = %s, want self %s", util.UUIDToString(task.ChatInputTaskID), util.UUIDToString(task.ID))
	}
	if task.FireAt.Time.Before(deadline.Add(-time.Second)) || task.FireAt.Time.After(deadline.Add(time.Second)) {
		t.Fatalf("task fire_at = %v, want media deadline %v", task.FireAt.Time, deadline)
	}
	var linkedTaskID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, messageID).Scan(&linkedTaskID); err != nil {
		t.Fatalf("load sealed media message: %v", err)
	}
	if !linkedTaskID.Valid || linkedTaskID.Bytes != task.ID.Bytes {
		t.Fatalf("message task_id = %s, want %s", util.UUIDToString(linkedTaskID), util.UUIDToString(task.ID))
	}
	if err := pool.QueryRow(ctx, `SELECT task_id FROM chat_message WHERE id = $1`, priorMessageID).Scan(&linkedTaskID); err != nil {
		t.Fatalf("load prior sealed message: %v", err)
	}
	if !linkedTaskID.Valid || linkedTaskID.Bytes != priorTask.ID.Bytes {
		t.Fatalf("prior message task_id = %s, want unchanged owner %s", util.UUIDToString(linkedTaskID), util.UUIDToString(priorTask.ID))
	}

	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            util.MustParseUUID(messageID),
		ChatSessionID: util.MustParseUUID(chatSessionID),
	}); err != nil {
		t.Fatalf("clear pending media: %v", err)
	}
	if err := svc.PromoteChannelChatTasksIfMediaReady(ctx, util.MustParseUUID(chatSessionID)); err != nil {
		t.Fatalf("PromoteChannelChatTasksIfMediaReady: %v", err)
	}
	var status string
	var fireAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `SELECT status, fire_at FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&status, &fireAt); err != nil {
		t.Fatalf("load promoted task: %v", err)
	}
	if status != "queued" || fireAt.Valid {
		t.Fatalf("promoted task = status %q fire_at %v, want queued with no deadline", status, fireAt)
	}
}
