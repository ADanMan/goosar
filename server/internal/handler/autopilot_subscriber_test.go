package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func installAutopilotSubscriberInsertFailure(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	functionName := fmt.Sprintf("autopilot_subscriber_fail_fn_%d", suffix)
	triggerName := fmt.Sprintf("autopilot_subscriber_fail_%d", suffix)
	t.Cleanup(func() {
		testPool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON autopilot_subscriber`, triggerName))
		testPool.Exec(ctx, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	RAISE EXCEPTION 'forced autopilot subscriber insert failure';
END;
$$;
`, functionName)); err != nil {
		t.Fatalf("install failure function: %v", err)
	}
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE INSERT ON autopilot_subscriber
FOR EACH ROW EXECUTE FUNCTION %s();
`, triggerName, functionName)); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
}

func TestCreateAutopilotPersistsMemberSubscribers(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Subscriber template autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = resp.ID
	if len(resp.Subscribers) != 1 {
		t.Fatalf("subscribers in response = %d, want 1", len(resp.Subscribers))
	}
	if resp.Subscribers[0].UserType != "member" || resp.Subscribers[0].UserID != testUserID {
		t.Fatalf("subscribers[0] = %+v, want member/%s", resp.Subscribers[0], testUserID)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1
	`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	if count != 1 {
		t.Fatalf("autopilot_subscriber rows = %d, want 1", count)
	}
}

func TestCreateAutopilotRejectsNonMemberSubscriberType(t *testing.T) {
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Bad subscriber type",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "agent", "user_id": agentID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for non-member subscriber, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotRejectsForeignSubscriber(t *testing.T) {
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Foreign subscriber",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": "00000000-0000-0000-0000-000000000000"},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for foreign member subscriber, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotRollsBackWhenSubscriberInsertFails(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Subscriber rollback create %d", time.Now().UnixNano())

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	installAutopilotSubscriberInsertFailure(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("CreateAutopilot: expected 500 for forced subscriber insert failure, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot
		WHERE workspace_id = $1 AND title = $2
	`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count rolled-back autopilots: %v", err)
	}
	if count != 0 {
		t.Fatalf("autopilot rows after failed subscriber insert = %d, want 0", count)
	}
}

func TestUpdateAutopilotFullReplaceSubscribers(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Replace subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"subscribers": []map[string]any{},
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if len(updated.Subscribers) != 0 {
		t.Fatalf("subscribers after empty replace = %d, want 0", len(updated.Subscribers))
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count after replace: %v", err)
	}
	if count != 0 {
		t.Fatalf("DB rows after empty replace = %d, want 0", count)
	}
}

func TestUpdateAutopilotRollsBackWhenSubscriberInsertFails(t *testing.T) {
	ctx := context.Background()
	originalTitle := fmt.Sprintf("Subscriber rollback update %d", time.Now().UnixNano())
	updatedTitle := originalTitle + " changed"
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          originalTitle,
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	installAutopilotSubscriberInsertFailure(t)

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"title": updatedTitle,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("UpdateAutopilot: expected 500 for forced subscriber insert failure, got %d: %s", w.Code, w.Body.String())
	}

	var gotTitle string
	if err := testPool.QueryRow(ctx, `SELECT title FROM autopilot WHERE id = $1`, autopilotID).Scan(&gotTitle); err != nil {
		t.Fatalf("load autopilot title after rollback: %v", err)
	}
	if gotTitle != originalTitle {
		t.Fatalf("autopilot title after failed subscriber replace = %q, want %q", gotTitle, originalTitle)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count subscribers after rollback: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscriber rows after failed replace = %d, want 1", count)
	}
}

func TestUpdateAutopilotPreservesSubscribersWhenOmitted(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Preserve subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Preserve subscribers autopilot (renamed)",
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count after omitted PATCH: %v", err)
	}
	if count != 1 {
		t.Fatalf("DB rows after omitted PATCH = %d, want 1 (subscribers must not have been touched)", count)
	}
}

func TestAutopilotDispatchFansOutSubscribersToIssue(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot subscriber fanout %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Subscriber fanout autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var subscriberReason string
	if err := testPool.QueryRow(ctx, `
		SELECT reason
		FROM issue_subscriber
		WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2
	`, issueID, testUserID).Scan(&subscriberReason); err != nil {
		t.Fatalf("query autopilot-fanned subscriber: %v", err)
	}
	if subscriberReason != "autopilot" {
		t.Fatalf("subscriber reason = %q, want %q", subscriberReason, "autopilot")
	}
}

func TestAutopilotDispatchNotifiesSubscribersOnCreate(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot subscriber inbox %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Subscriber inbox autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var inboxCount int
	var inboxType, inboxTitle string
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE issue_id = $1 AND recipient_id = $2 AND type = 'issue_subscribed'
	`, issueID, testUserID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox_item rows for subscriber = %d, want 1", inboxCount)
	}

	if err := testPool.QueryRow(ctx, `
		SELECT type, title FROM inbox_item
		WHERE issue_id = $1 AND recipient_id = $2 AND type = 'issue_subscribed'
	`, issueID, testUserID).Scan(&inboxType, &inboxTitle); err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inboxType != "issue_subscribed" {
		t.Fatalf("inbox type = %q, want issue_subscribed", inboxType)
	}
	if inboxTitle != title {
		t.Fatalf("inbox title = %q, want %q (issue title)", inboxTitle, title)
	}
}

func TestAutopilotDispatchSkipsInboxWhenNoSubscribers(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot no-subscriber inbox %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "No-subscriber autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var inboxCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE issue_id = $1 AND type = 'issue_subscribed'
	`, issueID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if inboxCount != 0 {
		t.Fatalf("issue_subscribed inbox rows = %d, want 0 (no subscribers)", inboxCount)
	}
}

func TestDeleteAutopilotArchivesAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	var taskID string
	defer func() {
		if taskID != "" {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID)
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, runtime_id FROM agent
		WHERE workspace_id = $1 AND runtime_id IS NOT NULL
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Delete-with-subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	var before int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&before); err != nil {
		t.Fatalf("count subscribers before delete: %v", err)
	}
	if before != 1 {
		t.Fatalf("subscriber rows before delete = %d, want 1", before)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'completed')
		RETURNING id
	`, autopilotID).Scan(&runID); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, status, priority, autopilot_run_id
		)
		VALUES ($1, $2, 'completed', 0, $3)
		RETURNING id
	`, agentID, runtimeID, runID).Scan(&taskID); err != nil {
		t.Fatalf("create linked task: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", autopilotID)
	testHandler.DeleteAutopilot(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilot: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot WHERE id = $1`, autopilotID).Scan(&status); err != nil {
		t.Fatalf("load autopilot after delete: %v", err)
	}
	if status != "archived" {
		t.Fatalf("autopilot status after delete = %q, want archived", status)
	}

	var after int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&after); err != nil {
		t.Fatalf("count subscribers after delete: %v", err)
	}
	if after != 1 {
		t.Fatalf("subscriber rows after delete = %d, want 1 (archival preserves config)", after)
	}

	var runRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_run WHERE id = $1 AND autopilot_id = $2`, runID, autopilotID).Scan(&runRows); err != nil {
		t.Fatalf("count run after delete: %v", err)
	}
	if runRows != 1 {
		t.Fatalf("autopilot_run rows after delete = %d, want 1 (archival preserves history)", runRows)
	}

	var taskRunID string
	if err := testPool.QueryRow(ctx, `SELECT autopilot_run_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskRunID); err != nil {
		t.Fatalf("load linked task after delete: %v", err)
	}
	if taskRunID != runID {
		t.Fatalf("task autopilot_run_id after delete = %q, want %q", taskRunID, runID)
	}
}
