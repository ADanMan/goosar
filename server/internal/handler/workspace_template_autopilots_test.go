package handler

import (
	"context"
	"strings"
	"testing"
)

const validTemplateAutopilotsJSON = `[
  {"slug": "weekly-plan",
   "title": {"ru": "План недели", "en": "Weekly plan"},
   "description": {"ru": "Собери план", "en": "Draft the plan"},
   "issue_title_template": {"ru": "План недели", "en": "Weekly plan"},
   "execution_mode": "create_issue",
   "triggers": [{"kind": "schedule", "cron_expression": "0 9 * * 1", "timezone": "Europe/Moscow",
                 "label": {"ru": "Понедельник", "en": "Monday"}}]}
]`

func TestParseWorkspaceTemplateAutopilots_Valid(t *testing.T) {
	got, err := parseWorkspaceTemplateAutopilots([]byte(validTemplateAutopilotsJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("parsed %d autopilots, want 1", len(got))
	}
	ap := got[0]
	if ap.Slug != "weekly-plan" || ap.ExecutionMode != "create_issue" {
		t.Fatalf("slug/execution_mode = %q/%q", ap.Slug, ap.ExecutionMode)
	}
	if workspaceTemplateText(ap.Title, "ru") != "План недели" {
		t.Fatalf("ru title = %q", workspaceTemplateText(ap.Title, "ru"))
	}
	if len(ap.Triggers) != 1 || ap.Triggers[0].Kind != "schedule" ||
		ap.Triggers[0].CronExpression != "0 9 * * 1" || ap.Triggers[0].Timezone != "Europe/Moscow" {
		t.Fatalf("trigger = %+v", ap.Triggers)
	}
}

func TestParseWorkspaceTemplateAutopilots_EmptyIsNoop(t *testing.T) {
	for _, raw := range []string{"", "null", "[]"} {
		got, err := parseWorkspaceTemplateAutopilots([]byte(raw))
		if err != nil || len(got) != 0 {
			t.Fatalf("parse(%q) = %v, %v; want empty, nil", raw, got, err)
		}
	}
}

func TestParseWorkspaceTemplateAutopilots_Rejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{"api kind", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"api"}]}]`, `"api"`},
		{"unknown kind", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"cron"}]}]`, "schedule or webhook"},
		{"schedule without cron", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"schedule","timezone":"UTC"}]}]`, "cron_expression"},
		{"schedule without timezone", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"schedule","cron_expression":"0 9 * * 1"}]}]`, "timezone"},
		{"unparsable cron", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"schedule","cron_expression":"every monday","timezone":"UTC"}]}]`, "cron"},
		{"unknown timezone", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"schedule","cron_expression":"0 9 * * 1","timezone":"Mars/Olympus"}]}]`, "timezone"},
		{"webhook with cron", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"webhook","cron_expression":"0 9 * * 1"}]}]`, "webhook"},
		{"webhook token in template", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"webhook","webhook_token":"secret"}]}]`, "webhook_token"},
		{"no triggers", `[{"slug":"a","title":{"ru":"A"},"triggers":[]}]`, "trigger"},
		{"missing slug", `[{"title":{"ru":"A"},"triggers":[{"kind":"webhook"}]}]`, "slug"},
		{"bad slug", `[{"slug":"Weekly Plan","title":{"ru":"A"},"triggers":[{"kind":"webhook"}]}]`, "slug"},
		{"duplicate slug", `[{"slug":"a","title":{"ru":"A"},"triggers":[{"kind":"webhook"}]},{"slug":"a","title":{"ru":"B"},"triggers":[{"kind":"webhook"}]}]`, "duplicate"},
		{"missing title", `[{"slug":"a","triggers":[{"kind":"webhook"}]}]`, "title"},
		{"bad execution mode", `[{"slug":"a","title":{"ru":"A"},"execution_mode":"delete_issue","triggers":[{"kind":"webhook"}]}]`, "execution_mode"},
		{"not an array", `{"slug":"a"}`, "array"},
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

func TestWorkspaceTemplateAutopilots_SeedIsValid(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	rows, err := testPool.Query(context.Background(),
		`SELECT key, autopilots FROM workspace_template WHERE enabled = true ORDER BY key`)
	if err != nil {
		t.Fatalf("query templates: %v", err)
	}
	defer rows.Close()

	seeded := map[string]int{}
	for rows.Next() {
		var key string
		var doc []byte
		if err := rows.Scan(&key, &doc); err != nil {
			t.Fatalf("scan: %v", err)
		}
		parsed, err := parseWorkspaceTemplateAutopilots(doc)
		if err != nil {
			t.Fatalf("template %q has an invalid autopilots column: %v", key, err)
		}
		seeded[key] = len(parsed)
	}
	for _, key := range roleCatalogKeys {
		if seeded[key] < 1 {
			t.Errorf("template %q ships %d autopilots, want at least one (20-architecture.md §13)", key, seeded[key])
		}
	}
}

type templateAutopilotRow struct {
	ExternalKey  string
	Title        string
	Status       string
	AssigneeType string
	AssigneeID   string
	CreatedByID  string
	IssueTitle   string
	TriggerKind  string
	Cron         string
	Timezone     string
	HasNextRun   bool
	HasToken     bool
}

func templateAutopilotRows(t *testing.T, wsID string) []templateAutopilotRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT a.external_key, a.title, a.status, a.assignee_type,
		       COALESCE(a.assignee_id::text, ''), a.created_by_id::text,
		       COALESCE(a.issue_title_template, ''),
		       COALESCE(tr.kind, ''), COALESCE(tr.cron_expression, ''), COALESCE(tr.timezone, ''),
		       tr.next_run_at IS NOT NULL, tr.webhook_token IS NOT NULL
		FROM autopilot a
		LEFT JOIN autopilot_trigger tr ON tr.autopilot_id = a.id
		WHERE a.workspace_id = $1 AND a.external_key LIKE 'template:%'
		ORDER BY a.external_key`, wsID)
	if err != nil {
		t.Fatalf("query template autopilots: %v", err)
	}
	defer rows.Close()

	var out []templateAutopilotRow
	for rows.Next() {
		var r templateAutopilotRow
		if err := rows.Scan(&r.ExternalKey, &r.Title, &r.Status, &r.AssigneeType, &r.AssigneeID,
			&r.CreatedByID, &r.IssueTitle, &r.TriggerKind, &r.Cron, &r.Timezone,
			&r.HasNextRun, &r.HasToken); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func setTemplateAutopilots(t *testing.T, key, doc string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace_template SET autopilots = $2 WHERE key = $1`, key, doc); err != nil {
		t.Fatalf("set template autopilots: %v", err)
	}
}

func TestProvisionRoleWorkspaces_CreatesAutopilotsThenSkips(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	ownerID, _ := deploymentUserFixture(t, "role-autopilot-owner")
	grantDeploymentAdminFixture(t, ownerID)

	result, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	total := 0
	for _, o := range result.Outcomes {
		if o.Status != RoleWorkspaceCreated {
			t.Fatalf("outcome %+v", o)
		}
		total += o.Autopilots
		if o.TemplateKey == "hr" && o.Autopilots < 1 {
			t.Errorf("hr provisioned %d autopilots, want at least one", o.Autopilots)
		}
	}
	if total != autopilotCatalogTotal {
		t.Fatalf("provisioned %d autopilots across the catalog, want %d", total, autopilotCatalogTotal)
	}

	var hrWS string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM workspace WHERE template_key = 'hr'`).Scan(&hrWS); err != nil {
		t.Fatalf("find hr workspace: %v", err)
	}
	rows := templateAutopilotRows(t, hrWS)
	if len(rows) == 0 {
		t.Fatal("hr role workspace has no template autopilots")
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.ExternalKey, "template:hr:") {
			t.Errorf("external_key = %q, want template:hr:<slug>", r.ExternalKey)
		}

		if r.Status != "paused" || r.AssigneeID != "" {
			t.Errorf("status/assignee = %q/%q, want paused with no assignee", r.Status, r.AssigneeID)
		}
		if r.CreatedByID != ownerID {
			t.Errorf("created_by_id = %q, want the provisioning owner %q", r.CreatedByID, ownerID)
		}
		if r.IssueTitle == "" {
			t.Errorf("issue_title_template is empty for %q", r.ExternalKey)
		}
		if r.TriggerKind != "schedule" || r.Cron == "" || r.Timezone == "" || !r.HasNextRun {
			t.Errorf("trigger = %+v, want a schedule with cron, timezone and next_run_at", r)
		}
		if r.HasToken {
			t.Errorf("%q carries a webhook token — templates mint none", r.ExternalKey)
		}
	}

	second, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, o := range second.Outcomes {
		if o.Autopilots != 0 {
			t.Errorf("second run created %d autopilots for %q, want 0", o.Autopilots, o.TemplateKey)
		}
	}
	if after := len(templateAutopilotRows(t, hrWS)); after != len(rows) {
		t.Fatalf("second run changed the autopilot count to %d, want %d", after, len(rows))
	}
}

func TestProvisionTemplateAutopilots_ReusesExternalKey(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, validTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-external-key")

	tmpl, err := testHandler.Queries.GetEnabledWorkspaceTemplate(context.Background(), roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	wsUUID := parseUUID(wsID)
	owner := parseUUID(testUserID)

	created, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl, wsUUID, owner)
	if err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	if created != 1 {
		t.Fatalf("created %d autopilots, want 1", created)
	}
	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 1 || rows[0].ExternalKey != "template:"+roleAgentTestTemplateKey+":weekly-plan" {
		t.Fatalf("rows = %+v", rows)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE autopilot SET status = 'archived' WHERE workspace_id = $1`, wsID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	again, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl, wsUUID, owner)
	if err != nil {
		t.Fatalf("second provisioning: %v", err)
	}
	if again != 0 {
		t.Fatalf("second provisioning created %d autopilots, want 0", again)
	}
}

func TestProvisionTemplateAutopilots_WithRoleAgentIsActive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, validTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-with-agent")

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	tmpl, err := testHandler.Queries.GetEnabledWorkspaceTemplate(context.Background(), roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	created, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl,
		parseUUID(wsID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if created != 1 {
		t.Fatalf("created %d autopilots, want 1", created)
	}
	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Status != "paused" || rows[0].AssigneeType != "agent" || rows[0].AssigneeID != agent.ID {
		t.Fatalf("autopilot = %+v, want paused and assigned to the role agent %s", rows[0], agent.ID)
	}
}

func TestTemplateAutopilots_ActivatedWhenRoleAgentAppears(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	setTemplateAutopilots(t, roleAgentTestTemplateKey, validTemplateAutopilotsJSON)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-reconcile")

	tmpl, err := testHandler.Queries.GetEnabledWorkspaceTemplate(context.Background(), roleAgentTestTemplateKey)
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	if _, err := provisionTemplateAutopilots(context.Background(), testHandler.Queries, tmpl,
		parseUUID(wsID), parseUUID(testUserID)); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if rows := templateAutopilotRows(t, wsID); len(rows) != 1 || rows[0].Status != "paused" {
		t.Fatalf("precondition: want one paused autopilot, got %+v", rows)
	}

	if _, err := testPool.Exec(context.Background(), `
		UPDATE autopilot_trigger tr SET next_run_at = now() - interval '3 days'
		FROM autopilot a WHERE a.id = tr.autopilot_id AND a.workspace_id = $1`, wsID); err != nil {
		t.Fatalf("backdate next_run_at: %v", err)
	}

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	rows := templateAutopilotRows(t, wsID)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Status != "paused" || rows[0].AssigneeID != agent.ID {
		t.Fatalf("autopilot = %+v, want paused and assigned to %s after the role agent appeared", rows[0], agent.ID)
	}

	var future bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT bool_and(tr.next_run_at > now())
		FROM autopilot_trigger tr JOIN autopilot a ON a.id = tr.autopilot_id
		WHERE a.workspace_id = $1 AND tr.kind = 'schedule'`, wsID).Scan(&future); err != nil {
		t.Fatalf("read next_run_at: %v", err)
	}
	if !future {
		t.Error("next_run_at is still in the past after activation — the role would display a next run that never comes")
	}
}

func TestReconcileTemplateAutopilots_LeavesForeignRowsAlone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	roleAgentTemplateFixture(t, true)
	wsID := roleAgentTestWorkspace(t, "role-autopilot-foreign")

	helperTestRuntime(t, wsID, helperRuntimeSpec{
		name: "public hermes", provider: "runtime-j", status: "online", visibility: "public",
	})
	listAgentsRequest(t, testUserID, wsID)
	agent := theRoleAgent(t, wsID)

	var handmade string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status,
		                       execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'Ручной автопилот', 'agent', NULL, 'paused', 'create_issue', 'member', $2)
		RETURNING id::text`, wsID, testUserID).Scan(&handmade); err != nil {
		t.Fatalf("insert hand-made autopilot: %v", err)
	}

	n, err := reconcileTemplateAutopilots(context.Background(), testHandler.Queries, parseUUID(wsID), parseUUID(agent.ID))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("reconcile touched %d rows, want 0", n)
	}
	var status, assignee string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, COALESCE(assignee_id::text, '') FROM autopilot WHERE id = $1`, handmade).
		Scan(&status, &assignee); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if status != "paused" || assignee != "" {
		t.Fatalf("hand-made autopilot = %q/%q, want it untouched", status, assignee)
	}
}

func TestProvisionRoleWorkspaces_TopsUpExistingRole(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	lockRoleWorkspaceSingleton(t)
	cleanupDeploymentAdminRows(t)
	cleanupRoleWorkspaces(t)

	ownerID, _ := deploymentUserFixture(t, "role-autopilot-topup-owner")
	grantDeploymentAdminFixture(t, ownerID)

	if _, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var hrWS string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM workspace WHERE template_key = 'hr'`).Scan(&hrWS); err != nil {
		t.Fatalf("find hr workspace: %v", err)
	}
	want := len(templateAutopilotRows(t, hrWS))
	if want == 0 {
		t.Fatal("precondition: hr role has no template autopilots")
	}

	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM autopilot WHERE workspace_id = $1 AND external_key LIKE 'template:%'`, hrWS); err != nil {
		t.Fatalf("drop autopilots: %v", err)
	}

	second, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, o := range second.Outcomes {
		if o.Status != RoleWorkspaceSkipped {
			t.Fatalf("outcome %+v, want the role reported as already provisioned", o)
		}
		if o.TemplateKey == "hr" && o.Autopilots != want {
			t.Errorf("second run topped up %d autopilots for hr, want %d", o.Autopilots, want)
		}
		if o.TemplateKey != "hr" && o.Autopilots != 0 {
			t.Errorf("second run touched %d autopilots for %q, want 0", o.Autopilots, o.TemplateKey)
		}
	}
	if got := len(templateAutopilotRows(t, hrWS)); got != want {
		t.Fatalf("hr has %d template autopilots after the top-up, want %d", got, want)
	}

	third, err := ProvisionRoleWorkspaces(context.Background(), testPool, testHandler.Queries, testSecretBox(t))
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	for _, o := range third.Outcomes {
		if o.Autopilots != 0 {
			t.Errorf("third run created %d autopilots for %q, want 0", o.Autopilots, o.TemplateKey)
		}
	}
}
