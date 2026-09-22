package handler

import (
	"context"
	"strings"
	"testing"
)

var roleCatalogKeys = []string{"hr", "finance", "legal", "sales", "procurement", "marketing"}

var autopilotsPerRole = map[string]int{
	"hr":          4,
	"finance":     3,
	"legal":       8,
	"sales":       8,
	"procurement": 3,
	"marketing":   8,
}

const autopilotCatalogTotal = 34

func TestRoleCatalog_SixRolesWithTheirAutopilots(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	rows, err := testPool.Query(context.Background(),
		`SELECT key, autopilots, enabled FROM workspace_template WHERE enabled = true`)
	if err != nil {
		t.Fatalf("query templates: %v", err)
	}
	defer rows.Close()

	type seeded struct {
		count   int
		entries []workspaceTemplateAutopilot
	}
	got := map[string]seeded{}
	for rows.Next() {
		var key string
		var doc []byte
		var enabled bool
		if err := rows.Scan(&key, &doc, &enabled); err != nil {
			t.Fatalf("scan: %v", err)
		}
		parsed, err := parseWorkspaceTemplateAutopilots(doc)
		if err != nil {
			t.Fatalf("template %q has an invalid autopilots column: %v", key, err)
		}
		got[key] = seeded{count: len(parsed), entries: parsed}
	}

	total := 0
	for _, key := range roleCatalogKeys {
		s, ok := got[key]
		if !ok {
			t.Fatalf("role %q is not in the enabled catalog", key)
		}
		if s.count != autopilotsPerRole[key] {
			t.Errorf("role %q ships %d autopilots, want %d", key, s.count, autopilotsPerRole[key])
		}
		total += s.count
		for _, ap := range s.entries {
			if strings.TrimSpace(workspaceTemplateText(ap.Description, "ru")) == "" {
				t.Errorf("autopilot %s/%s has no ru description — the agent would run an empty prompt", key, ap.Slug)
			}
			if strings.TrimSpace(workspaceTemplateText(ap.IssueTitleTemplate, "ru")) == "" {
				t.Errorf("autopilot %s/%s has no ru issue_title_template", key, ap.Slug)
			}
			if ap.AssigneeRole != templateAutopilotAssigneeRole {
				t.Errorf("autopilot %s/%s has assignee_role %q", key, ap.Slug, ap.AssigneeRole)
			}
			if ap.Risk != templateAutopilotRiskL1 && ap.Risk != templateAutopilotRiskL2 {
				t.Errorf("autopilot %s/%s has risk %q", key, ap.Slug, ap.Risk)
			}
		}
	}
	if total != autopilotCatalogTotal {
		t.Errorf("catalog ships %d autopilots across the six roles, want %d", total, autopilotCatalogTotal)
	}
}

func TestRoleCatalog_NewRoleNames(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	russian := listWorkspaceTemplatesAs(t, "ru-RU,ru;q=0.9")
	for key, want := range map[string]string{"procurement": "Закупки", "marketing": "Маркетолог"} {
		tmpl, ok := templateByKey(russian, key)
		if !ok {
			t.Fatalf("template %q missing from the catalog", key)
		}
		if tmpl.Name != want {
			t.Errorf("template %q ru name = %q, want %q", key, tmpl.Name, want)
		}
		if tmpl.Description == "" {
			t.Errorf("template %q has no ru description", key)
		}
	}
}

func TestProvisionRoleWorkspaces_CatalogPausedAutopilots(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	ownerID, _ := deploymentUserFixture(t, "role-catalog-owner")
	grantDeploymentAdminFixture(t, ownerID)

	result, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	perRole := map[string]int{}
	for _, o := range result.Outcomes {
		if o.Status != RoleWorkspaceCreated {
			t.Fatalf("outcome %+v", o)
		}
		perRole[o.TemplateKey] = o.Autopilots
	}
	total := 0
	for _, key := range roleCatalogKeys {
		if perRole[key] != autopilotsPerRole[key] {
			t.Errorf("role %q provisioned %d autopilots, want %d", key, perRole[key], autopilotsPerRole[key])
		}
		total += perRole[key]
	}
	if total != autopilotCatalogTotal {
		t.Fatalf("provisioned %d autopilots, want %d", total, autopilotCatalogTotal)
	}

	var paused, all int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE status = 'paused' AND assignee_id IS NULL), count(*)
		FROM autopilot a JOIN workspace w ON w.id = a.workspace_id
		WHERE w.template_key IS NOT NULL AND a.external_key LIKE 'template:%'`).Scan(&paused, &all); err != nil {
		t.Fatalf("count autopilots: %v", err)
	}
	if all != autopilotCatalogTotal || paused != all {
		t.Fatalf("%d of %d template autopilots are paused and unassigned, want all %d",
			paused, all, autopilotCatalogTotal)
	}
}

func TestTopUpTemplateAutopilots_AddsNewOnesAndKeepsUIEdits(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, validTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-topup")

	ctx := context.Background()
	before, err := testHandler.Queries.GetEnabledWorkspaceTemplate(ctx, roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	created, err := provisionTemplateAutopilots(ctx, testHandler.Queries, before, parseUUID(wsID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	if created != 1 {
		t.Fatalf("first provisioning created %d autopilots, want 1", created)
	}

	const renamed = "План недели — переименован вручную"
	keptKey := templateAutopilotExternalKey(roleAgentTestTemplateKey, "weekly-plan")
	if _, err := testPool.Exec(ctx,
		`UPDATE autopilot SET title = $1 WHERE workspace_id = $2 AND external_key = $3`,
		renamed, wsID, keptKey); err != nil {
		t.Fatalf("rename in the UI: %v", err)
	}

	setTemplateAutopilots(t, roleAgentTestTemplateKey, twoTemplateAutopilotsJSON)
	after, err := testHandler.Queries.GetEnabledWorkspaceTemplate(ctx, roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("reload template: %v", err)
	}
	topped, err := topUpTemplateAutopilots(ctx, testPool, testHandler.Queries, after, parseUUID(wsID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("top up: %v", err)
	}
	if topped != 1 {
		t.Fatalf("top-up created %d autopilots, want 1 (only the new entry)", topped)
	}

	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 2 {
		t.Fatalf("workspace has %d template autopilots after the top-up, want 2", len(rows))
	}
	for _, r := range rows {
		if r.ExternalKey == keptKey && r.Title != renamed {
			t.Errorf("the renamed autopilot's title is %q, want the member's %q", r.Title, renamed)
		}
	}
}
