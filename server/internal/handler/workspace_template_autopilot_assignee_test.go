package handler

import (
	"context"
	"strings"
	"testing"
)

const twoTemplateAutopilotsJSON = `[
  {"slug": "weekly-plan",
   "title": {"ru": "План недели"},
   "description": {"ru": "Собери план"},
   "issue_title_template": {"ru": "План недели"},
   "assignee_role": "role", "risk": "L1",
   "triggers": [{"kind": "schedule", "cron_expression": "0 9 * * 1", "timezone": "Europe/Moscow"}]},
  {"slug": "draft-review",
   "title": {"ru": "Проверка черновика"},
   "description": {"ru": "Подготовь черновик"},
   "issue_title_template": {"ru": "Проверка черновика"},
   "assignee_role": "role", "risk": "L2",
   "triggers": [{"kind": "schedule", "cron_expression": "0 10 * * 1-5", "timezone": "Europe/Moscow"}]}
]`

func TestParseWorkspaceTemplateAutopilots_AssigneeRoleAndRisk(t *testing.T) {
	got, err := parseWorkspaceTemplateAutopilots([]byte(twoTemplateAutopilotsJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d entries, want 2", len(got))
	}
	if got[0].AssigneeRole != templateAutopilotAssigneeRole || got[0].Risk != templateAutopilotRiskL1 {
		t.Errorf("entry 1 = %q/%q, want role/L1", got[0].AssigneeRole, got[0].Risk)
	}
	if got[1].AssigneeRole != templateAutopilotAssigneeRole || got[1].Risk != templateAutopilotRiskL2 {
		t.Errorf("entry 2 = %q/%q, want role/L2", got[1].AssigneeRole, got[1].Risk)
	}
}

func TestParseWorkspaceTemplateAutopilots_AssigneeRoleAndRiskDefault(t *testing.T) {
	got, err := parseWorkspaceTemplateAutopilots([]byte(validTemplateAutopilotsJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got[0].AssigneeRole != templateAutopilotAssigneeRole {
		t.Errorf("assignee_role = %q, want the default %q", got[0].AssigneeRole, templateAutopilotAssigneeRole)
	}
	if got[0].Risk != templateAutopilotRiskL1 {
		t.Errorf("risk = %q, want the default %q", got[0].Risk, templateAutopilotRiskL1)
	}
}

func TestParseWorkspaceTemplateAutopilots_RejectsAssigneeRoleAndRisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{
			"unknown assignee_role",
			`[{"slug":"a","title":{"ru":"A"},"assignee_role":"human","triggers":[{"kind":"webhook"}]}]`,
			"assignee_role",
		},
		{
			"unknown risk",
			`[{"slug":"a","title":{"ru":"A"},"risk":"L3","triggers":[{"kind":"webhook"}]}]`,
			"risk",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseWorkspaceTemplateAutopilots([]byte(tc.doc))
			if err == nil {
				t.Fatalf("accepted an invalid document")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestTemplateAutopilots_AssignedToRoleAgentAtProvisioning(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, twoTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-assignee")

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public runtime", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	tmpl, err := testHandler.Queries.GetEnabledWorkspaceTemplate(context.Background(), roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	if _, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl,
		parseUUID(wsID), parseUUID(testUserID)); err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 2 {
		t.Fatalf("provisioned %d template autopilots, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Status != "paused" || r.AssigneeID != agent.ID {
			t.Errorf("autopilot %s = %+v, want paused and assigned to %s", r.ExternalKey, r, agent.ID)
		}
	}
}

func TestTemplateAutopilots_ReconcileAssignsRoleAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, twoTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-reconcile")

	tmpl, err := testHandler.Queries.GetEnabledWorkspaceTemplate(context.Background(), roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	if _, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl,
		parseUUID(wsID), parseUUID(testUserID)); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	for _, r := range templateAutopilotRows(t, wsID) {
		if r.Status != "paused" || r.AssigneeID != "" {
			t.Fatalf("autopilot %s = %+v, want paused and unassigned before the role agent exists", r.ExternalKey, r)
		}
	}

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public runtime", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 2 {
		t.Fatalf("workspace has %d template autopilots, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Status != "paused" || r.AssigneeID != agent.ID {
			t.Errorf("autopilot %s = %+v, want paused and assigned to %s after the role agent appeared", r.ExternalKey, r, agent.ID)
		}
	}
}
