package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func seedTransferMember(t *testing.T, label string) string {
	t.Helper()
	ctx := context.Background()
	var uid string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@goosar.test", label, time.Now().UnixNano())).Scan(&uid); err != nil {
		t.Fatalf("seed %s user: %v", label, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, uid); err != nil {
		t.Fatalf("seed %s member: %v", label, err)
	}
	return uid
}

func seedScheduleTriggerPublishedBy(t *testing.T, apID, memberID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		apID, memberID).Scan(&id); err != nil {
		t.Fatalf("seed schedule trigger: %v", err)
	}
	return id
}

func triggerPublisherMember(t *testing.T, triggerID string) string {
	t.Helper()
	row, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), parseUUID(triggerID))
	if err != nil {
		t.Fatalf("load trigger: %v", err)
	}
	if row.PublishedByType.Valid && row.PublishedByType.String == "member" && row.PublishedByID.Valid {
		return uuidToString(row.PublishedByID)
	}
	return ""
}

func patchAutopilot(t *testing.T, apID string, body map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, body)
	req = withURLParam(req, "id", apID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func patchTrigger(t *testing.T, apID, triggerID string, body map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+triggerID, body)
	req = withURLParams(req, "id", apID, "triggerId", triggerID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilotTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilot_PromptEditTransfersAllTriggers(t *testing.T) {
	creatorA := seedTransferMember(t, "prompt-creator")
	agentID := createWebhookTestAgent(t, "PromptEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig1 := seedScheduleTriggerPublishedBy(t, apID, creatorA)
	trig2 := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchAutopilot(t, apID, map[string]any{"description": "Rewritten prompt: do the new thing"})

	if got := triggerPublisherMember(t, trig1); got != testUserID {
		t.Errorf("trigger1 publisher = %s, want editor %s (prompt edit must transfer)", got, testUserID)
	}
	if got := triggerPublisherMember(t, trig2); got != testUserID {
		t.Errorf("trigger2 publisher = %s, want editor %s (prompt edit bumps all triggers)", got, testUserID)
	}
}

func TestUpdateAutopilot_CosmeticTitleEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "title-creator")
	agentID := createWebhookTestAgent(t, "TitleEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchAutopilot(t, apID, map[string]any{"title": "Renamed automation"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (cosmetic title edit must not transfer)", got, creatorA)
	}
}

func TestUpdateAutopilotTrigger_SubstantiveEditTransfersOnlyThatTrigger(t *testing.T) {
	creatorA := seedTransferMember(t, "trig-creator")
	agentID := createWebhookTestAgent(t, "TrigEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig1 := seedScheduleTriggerPublishedBy(t, apID, creatorA)
	trig2 := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchTrigger(t, apID, trig1, map[string]any{"enabled": false})

	if got := triggerPublisherMember(t, trig1); got != testUserID {
		t.Errorf("trigger1 publisher = %s, want editor %s (substantive edit must transfer)", got, testUserID)
	}
	if got := triggerPublisherMember(t, trig2); got != creatorA {
		t.Errorf("trigger2 publisher = %s, want unchanged creator %s (editing a sibling must not transfer)", got, creatorA)
	}
}

func TestUpdateAutopilotTrigger_LabelOnlyEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "label-creator")
	agentID := createWebhookTestAgent(t, "LabelEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchTrigger(t, apID, trig, map[string]any{"label": "just a nickname"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (label-only edit must not transfer)", got, creatorA)
	}
}

func TestUpdateAutopilotTrigger_NoOpEditDoesNotTransfer(t *testing.T) {
	creatorA := seedTransferMember(t, "noop-creator")
	agentID := createWebhookTestAgent(t, "NoOpEdit Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := seedScheduleTriggerPublishedBy(t, apID, creatorA)

	patchTrigger(t, apID, trig, map[string]any{"cron_expression": "0 * * * *"})

	if got := triggerPublisherMember(t, trig); got != creatorA {
		t.Errorf("trigger publisher = %s, want unchanged creator %s (no-op edit must not transfer)", got, creatorA)
	}
}
