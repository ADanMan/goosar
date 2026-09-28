package autopilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/task"
	"github.com/adanman/goosar/server2/internal/workspace"
)

func jsonHasKey(b []byte, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

func TestAutopilot_CreateGetListUpdateArchive(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	a, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Nightly cleanup", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
		Subscribers: []SubscriberInput{{UserType: "member", UserID: ownerID}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Status != "active" {
		t.Errorf("Create: status = %q, want active", a.Status)
	}
	if len(a.Subscribers) != 1 || a.Subscribers[0].UserID != ownerID {
		t.Errorf("Create: subscribers = %+v", a.Subscribers)
	}

	got, err := s.Get(ctx, wsID, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Nightly cleanup" || got.AssigneeID != agentID {
		t.Errorf("Get: %+v", got)
	}

	list, total, err := s.List(ctx, wsID, nil)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("List: %d items (total=%d), err=%v", len(list), total, err)
	}

	archived := "archived"
	updated, err := s.Update(ctx, wsID, a.ID, UpdatePatch{Status: archived, StatusSet: true})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != "archived" {
		t.Errorf("Update: status = %q, want archived", updated.Status)
	}

	if _, err := s.Get(ctx, wsID, "00000000-0000-0000-0000-000000000000"); err != ErrNotFound {
		t.Errorf("Get (missing): err = %v, want ErrNotFound", err)
	}
}

func TestAutopilot_AccessControl(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	// участник, не owner/admin — второй account, добавленный как member.
	var memberID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO accounts (acct_email, acct_full_name) VALUES ('member@example.test', 'Member') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatalf("seed second account: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO space_members (workspace_id, account_id, sm_role) VALUES ($1, $2, 'member')`, wsID, memberID); err != nil {
		t.Fatalf("seed second membership: %v", err)
	}

	a, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Access test", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if can, err := s.CanWrite(ctx, a.ID, memberID, httpapi.RoleMember); err != nil || can {
		t.Errorf("CanWrite(member, not creator) = %v, %v; want false, nil", can, err)
	}
	if can, err := s.CanWrite(ctx, a.ID, ownerID, httpapi.RoleOwner); err != nil || !can {
		t.Errorf("CanWrite(owner) = %v, %v; want true, nil", can, err)
	}
	if can, err := s.CanWrite(ctx, a.ID, ownerID, httpapi.RoleMember); err != nil || !can {
		t.Errorf("CanWrite(creator, role=member) = %v, %v; want true, nil (создатель может писать вне зависимости от роли)", can, err)
	}

	if _, err := s.AddCollaborator(ctx, a.ID, memberID, ownerID); err != nil {
		t.Fatalf("AddCollaborator: %v", err)
	}
	if can, err := s.CanWrite(ctx, a.ID, memberID, httpapi.RoleMember); err != nil || !can {
		t.Errorf("CanWrite(collaborator) = %v, %v; want true, nil", can, err)
	}
	if can, err := s.CanManageAccess(ctx, a.ID, memberID, httpapi.RoleMember); err != nil || can {
		t.Errorf("CanManageAccess(collaborator, не создатель) = %v, %v; want false, nil (только создатель/owner/admin)", can, err)
	}
}

func TestAutopilot_ResolveAssignee(t *testing.T) {
	db := newTestDB(t)
	wsID, _ := seedWorkspace(t, db)
	agentID, executorID := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	got, err := s.ResolveAssignee(ctx, wsID, "agent", agentID)
	if err != nil {
		t.Fatalf("ResolveAssignee(agent): %v", err)
	}
	if got.ExecutorID != executorID || got.OperativeID != agentID {
		t.Errorf("ResolveAssignee(agent) = %+v", got)
	}

	if _, err := s.ResolveAssignee(ctx, wsID, "agent", "00000000-0000-0000-0000-000000000000"); err != ErrAssigneeNotFound {
		t.Errorf("ResolveAssignee (missing): err = %v, want ErrAssigneeNotFound", err)
	}

	// squad с лидером-агентом
	var crewID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO crews (workspace_id, crew_title, crew_leader_type, crew_leader_id, crew_creator_account_id)
		VALUES ($1, 'Squad', 'agent', $2, (SELECT account_id FROM space_members WHERE workspace_id = $1 LIMIT 1))
		RETURNING id`, wsID, agentID).Scan(&crewID); err != nil {
		t.Fatalf("seed crew: %v", err)
	}
	gotSquad, err := s.ResolveAssignee(ctx, wsID, "squad", crewID)
	if err != nil {
		t.Fatalf("ResolveAssignee(squad): %v", err)
	}
	if gotSquad.OperativeID != agentID || gotSquad.CrewID != crewID {
		t.Errorf("ResolveAssignee(squad) = %+v", gotSquad)
	}
}

func TestAutopilot_TriggerCRUD_ScheduleAndWebhook(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	a, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Trig test", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cron := "0 9 * * *"
	trig, err := s.CreateTrigger(ctx, a.ID, CreateTriggerParams{Kind: "schedule", CronExpression: &cron})
	if err != nil {
		t.Fatalf("CreateTrigger(schedule): %v", err)
	}
	if trig.NextRunAt == nil {
		t.Error("CreateTrigger(schedule): next_run_at not computed")
	}

	provider := "github"
	wh, err := s.CreateTrigger(ctx, a.ID, CreateTriggerParams{Kind: "webhook", Provider: &provider})
	if err != nil {
		t.Fatalf("CreateTrigger(webhook): %v", err)
	}
	if wh.PlainToken == "" || wh.WebhookPath == nil {
		t.Fatalf("CreateTrigger(webhook): expected a plaintext token and webhook_path, got %+v", wh)
	}

	// после Get — токен больше не виден в открытом виде.
	reGot, err := s.GetTrigger(ctx, a.ID, wh.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if reGot.PlainToken != "" {
		t.Error("GetTrigger: PlainToken should be empty on read (only the create/rotate response carries it)")
	}

	byPath, err := s.triggerByWebhookToken(ctx, *wh.WebhookPath)
	if err != nil {
		t.Fatalf("triggerByWebhookToken: %v", err)
	}
	if byPath.ID != wh.ID || byPath.SentinelWorkspaceID != wsID {
		t.Errorf("triggerByWebhookToken = %+v", byPath)
	}

	rotated, err := s.RotateWebhookToken(ctx, a.ID, wh.ID)
	if err != nil {
		t.Fatalf("RotateWebhookToken: %v", err)
	}
	if rotated.PlainToken == "" || rotated.PlainToken == wh.PlainToken {
		t.Errorf("RotateWebhookToken: expected a new plaintext token, got %q (old %q)", rotated.PlainToken, wh.PlainToken)
	}
	if *rotated.WebhookPath == *wh.WebhookPath {
		t.Error("RotateWebhookToken: webhook_path did not change")
	}
	// старый путь больше не резолвится.
	if _, err := s.triggerByWebhookToken(ctx, *wh.WebhookPath); err != ErrTriggerNotFound {
		t.Errorf("triggerByWebhookToken (старый токен после ротации): err = %v, want ErrTriggerNotFound", err)
	}

	// signing secret: без ключа шифрования — ErrEncryptionUnavailable.
	if _, err := s.SetSigningSecret(ctx, a.ID, wh.ID, "", "at-least-16-characters-secret"); err != ErrEncryptionUnavailable {
		t.Errorf("SetSigningSecret (no key): err = %v, want ErrEncryptionUnavailable", err)
	}
	withSecret, err := s.SetSigningSecret(ctx, a.ID, wh.ID, "test-mcp-secret-key", "at-least-16-characters-secret")
	if err != nil {
		t.Fatalf("SetSigningSecret: %v", err)
	}
	if !withSecret.HasSigningSecret || withSecret.SigningSecretHint == nil {
		t.Errorf("SetSigningSecret: has_signing_secret/hint not set: %+v", withSecret)
	}
	secret, ok, err := s.signingSecretFor(ctx, wh.ID, "test-mcp-secret-key", "")
	if err != nil || !ok || secret != "at-least-16-characters-secret" {
		t.Errorf("signingSecretFor = %q, %v, %v", secret, ok, err)
	}

	// снятие секрета пустой строкой.
	cleared, err := s.SetSigningSecret(ctx, a.ID, wh.ID, "test-mcp-secret-key", "")
	if err != nil {
		t.Fatalf("SetSigningSecret (clear): %v", err)
	}
	if cleared.HasSigningSecret {
		t.Error("SetSigningSecret (clear): has_signing_secret still true")
	}

	if err := s.DeleteTrigger(ctx, a.ID, trig.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	if _, err := s.GetTrigger(ctx, a.ID, trig.ID); err != ErrTriggerNotFound {
		t.Errorf("GetTrigger (deleted): err = %v, want ErrTriggerNotFound", err)
	}

	list, err := s.ListTriggers(ctx, a.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListTriggers: %d triggers (want 1, the webhook one), err=%v", len(list), err)
	}
}

func TestAutopilot_RunsAndDeliveries(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	a, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Run test", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	provider := "generic"
	trig, err := s.CreateTrigger(ctx, a.ID, CreateTriggerParams{Kind: "webhook", Provider: &provider})
	if err != nil {
		t.Fatalf("CreateTrigger: %v", err)
	}

	run, err := s.CreateRun(ctx, CreateRunParams{
		AutopilotID: a.ID, TriggerID: &trig.ID, Source: "webhook", Status: "running",
		TriggerPayload: []byte(`{"event":"push"}`),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.TriggerPayload == nil {
		t.Error("CreateRun: TriggerPayload lost")
	}

	runs, total, err := s.ListRuns(ctx, a.ID, 20, 0)
	if err != nil || total != 1 || len(runs) != 1 {
		t.Fatalf("ListRuns: %d (total %d), err=%v", len(runs), total, err)
	}
	if b, _ := json.Marshal(runs[0]); jsonHasKey(b, "trigger_payload") {
		t.Errorf("ListRuns: trigger_payload leaked into the serialized list form (contract: only in getAutopilotRun): %s", b)
	}

	gotRun, err := s.GetRun(ctx, a.ID, run.ID)
	if err != nil || gotRun.TriggerPayload == nil {
		t.Fatalf("GetRun: %+v, err=%v (want trigger_payload present)", gotRun, err)
	}

	dedupeKey := "delivery-1"
	del, err := s.CreateDelivery(ctx, CreateDeliveryParams{
		WorkspaceID: wsID, AutopilotID: a.ID, TriggerID: trig.ID, Provider: "generic", Event: "push",
		DedupeKey: &dedupeKey, SignatureStatus: "not_required", Status: "dispatched",
		AutopilotRunID: &run.ID, SelectedHeaders: map[string]string{"Content-Type": "application/json"},
		RawBody: strPtr(`{"event":"push"}`),
	})
	if err != nil {
		t.Fatalf("CreateDelivery: %v", err)
	}

	deliveries, total, err := s.ListDeliveries(ctx, a.ID, 20, 0)
	if err != nil || total != 1 || len(deliveries) != 1 {
		t.Fatalf("ListDeliveries: %d (total %d), err=%v", len(deliveries), total, err)
	}

	full, err := s.GetDelivery(ctx, a.ID, del.ID)
	if err != nil {
		t.Fatalf("GetDelivery: %v", err)
	}
	if full.SelectedHeaders["Content-Type"] != "application/json" || full.RawBody == nil {
		t.Errorf("GetDelivery: full form missing detail: %+v", full)
	}

	found, ok, err := s.FindDeliveryByDedupe(ctx, trig.ID, dedupeKey)
	if err != nil || !ok || found.ID != del.ID {
		t.Fatalf("FindDeliveryByDedupe: %+v, %v, %v", found, ok, err)
	}
	if _, ok, err := s.FindDeliveryByDedupe(ctx, trig.ID, "no-such-key"); err != nil || ok {
		t.Errorf("FindDeliveryByDedupe (miss): ok=%v, err=%v", ok, err)
	}
}

func TestAutopilot_Dispatcher_CreateIssueAndRunOnly(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	wsDeps := workspace.New(db, nil, nil, nil, false, nil)
	taskStore := task.NewStore(db)
	dispatchDeps := dispatch.New(nil, nil)
	disp := &Dispatcher{Store: s, Dispatch: dispatchDeps, Tasks: taskStore, Workspace: wsDeps.Store, DB: db}

	runOnly, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Run only", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create(run_only): %v", err)
	}
	result, err := disp.DispatchManual(ctx, runOnly.ID)
	if err != nil {
		t.Fatalf("DispatchManual(run_only): %v", err)
	}
	if result.Run.Status != "running" || result.Run.IssueID != nil {
		t.Errorf("DispatchManual(run_only) run = %+v, want status=running, no issue_id", result.Run)
	}

	createIssue, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Create issue", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "create_issue", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create(create_issue): %v", err)
	}
	result2, err := disp.DispatchManual(ctx, createIssue.ID)
	if err != nil {
		t.Fatalf("DispatchManual(create_issue): %v", err)
	}
	if result2.Run.Status != "issue_created" || result2.Run.IssueID == nil {
		t.Errorf("DispatchManual(create_issue) run = %+v, want status=issue_created with an issue_id", result2.Run)
	}

	updated, err := s.Get(ctx, wsID, createIssue.ID)
	if err != nil {
		t.Fatalf("Get after dispatch: %v", err)
	}
	if updated.LastRunAt == nil {
		t.Error("sen_last_run_at was not updated after a successful dispatch")
	}
	if updated.LastRunStatus == nil || *updated.LastRunStatus != "issue_created" {
		t.Errorf("last_run_status = %v, want issue_created", updated.LastRunStatus)
	}
}

func TestAutopilot_Dispatcher_SkipsInactiveAndMissingAssignee(t *testing.T) {
	db := newTestDB(t)
	wsID, ownerID := seedWorkspace(t, db)
	agentID, _ := seedAgent(t, db, wsID)
	s := NewStore(db)
	ctx := context.Background()

	wsDeps := workspace.New(db, nil, nil, nil, false, nil)
	taskStore := task.NewStore(db)
	dispatchDeps := dispatch.New(nil, nil)
	disp := &Dispatcher{Store: s, Dispatch: dispatchDeps, Tasks: taskStore, Workspace: wsDeps.Store, DB: db}

	paused, err := s.Create(ctx, CreateParams{
		WorkspaceID: wsID, Title: "Paused", AssigneeType: "agent", AssigneeID: agentID,
		ExecutionMode: "run_only", CreatedByType: "member", CreatedByID: ownerID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	paused_, err := s.Update(ctx, wsID, paused.ID, UpdatePatch{Status: "paused", StatusSet: true})
	if err != nil {
		t.Fatalf("Update to paused: %v", err)
	}
	result, err := disp.DispatchManual(ctx, paused_.ID)
	if err != nil {
		t.Fatalf("DispatchManual(paused): %v", err)
	}
	if result.Run.Status != "skipped" || result.Run.ReasonCode == nil || *result.Run.ReasonCode != "autopilot_not_active" {
		t.Errorf("DispatchManual(paused) run = %+v", result.Run)
	}
}
