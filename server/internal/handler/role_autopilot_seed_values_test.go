package handler

import (
	"context"
	"strings"
	"testing"
)

const roleAutopilotSeedTable = `
hr | onboarding-plan      | 0 9 * * 1      | role | L1
hr | mail-digest          | 0 8 * * 1-5    | role | L1
hr | self-check-reminder  | 0 9 1 * *      | role | L1
hr | policy-updates       | 0 9 * * 3      | role | L1
finance | reconciliation      | 0 9 * * 1     | role | L1
finance | month-close         | 0 9 1 * *     | role | L1
finance | counterparty-check  | 0 11 * * 1-5  | role | L1
legal | deadline-registry | 0 9 * * 1    | role | L1
legal | contract-expiry   | 0 9 * * 1    | role | L1
legal | redline-compare   | 0 * * * 1-5  | role | L1
legal | claims-tracker    | 0 9 * * 1-5  | role | L1
legal | regulation-watch  | 0 9 * * 3    | role | L1
legal | template-audit    | 0 9 1 * *    | role | L1
legal | poa-expiry        | 0 9 * * 1    | role | L1
legal | court-calendar    | 0 8 * * 1-5  | role | L1
sales | client-brief    | 0 9 * * 1     | role | L1
sales | deal-reminders  | 0 9 * * 1-5   | role | L1
sales | lead-triage     | 0 */2 * * 1-5 | role | L1
sales | mail-followup   | 0 10 * * 1-5  | role | L1
sales | pipeline-report | 0 9 * * 5     | role | L1
sales | meeting-prep    | 0 7 * * 1-5   | role | L1
sales | lost-deals      | 0 9 1 * *     | role | L1
sales | proposal-draft  | 0 * * * 1-5   | role | L1
procurement | price-monitor      | 0 9 * * 3     | role | L1
procurement | contract-expiry    | 0 9 * * 1     | role | L1
procurement | request-triage     | 0 */2 * * 1-5 | role | L1
marketing | content-calendar     | 0 9 * * 1    | role | L1
marketing | campaign-report      | 0 9 * * 5    | role | L1
marketing | competitor-watch     | 0 9 * * 3    | role | L1
marketing | mentions-digest      | 0 8 * * 1-5  | role | L1
marketing | brief-draft          | 0 * * * 1-5  | role | L1
marketing | landing-check        | 0 10 * * 1-5 | role | L1
marketing | email-campaign-draft | 0 * * * 1-5  | role | L1
marketing | lead-source-report   | 0 9 * * 1    | role | L1
`

const (
	roleAutopilotSeedTimezone      = "Europe/Moscow"
	roleAutopilotSeedExecutionMode = "create_issue"
)

type roleAutopilotSeedRow struct {
	role, slug, cron, assignee, risk string
}

func parseRoleAutopilotSeedTable(t *testing.T) []roleAutopilotSeedRow {
	t.Helper()
	var rows []roleAutopilotSeedRow
	for _, line := range strings.Split(roleAutopilotSeedTable, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) != 5 {
			t.Fatalf("seed table row %q has %d columns, want 5", line, len(cols))
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		rows = append(rows, roleAutopilotSeedRow{cols[0], cols[1], cols[2], cols[3], cols[4]})
	}
	return rows
}

func TestRoleAutopilotSeed_MatchesBacklogTable(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	want := parseRoleAutopilotSeedTable(t)
	if len(want) != autopilotCatalogTotal {
		t.Fatalf("the seed table has %d rows, want %d", len(want), autopilotCatalogTotal)
	}

	rows, err := testPool.Query(context.Background(),
		`SELECT key, autopilots FROM workspace_template WHERE enabled = true`)
	if err != nil {
		t.Fatalf("query templates: %v", err)
	}
	defer rows.Close()

	got := map[string]workspaceTemplateAutopilot{}
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
		for _, ap := range parsed {
			got[key+"/"+ap.Slug] = ap
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate templates: %v", err)
	}

	for _, w := range want {
		id := w.role + "/" + w.slug
		ap, ok := got[id]
		if !ok {
			t.Errorf("autopilot %s is in the seed table and missing from the seed", id)
			continue
		}
		delete(got, id)
		if ap.AssigneeRole != w.assignee {
			t.Errorf("autopilot %s assignee_role = %q, the table says %q", id, ap.AssigneeRole, w.assignee)
		}
		if ap.Risk != w.risk {
			t.Errorf("autopilot %s risk = %q, the table says %q", id, ap.Risk, w.risk)
		}
		if ap.ExecutionMode != roleAutopilotSeedExecutionMode {
			t.Errorf("autopilot %s execution_mode = %q, want %q", id, ap.ExecutionMode, roleAutopilotSeedExecutionMode)
		}
		if len(ap.Triggers) != 1 {
			t.Errorf("autopilot %s has %d triggers, want exactly one schedule", id, len(ap.Triggers))
			continue
		}
		trig := ap.Triggers[0]
		if trig.Kind != "schedule" {
			t.Errorf("autopilot %s trigger kind = %q, want schedule", id, trig.Kind)
		}
		if trig.CronExpression != w.cron {
			t.Errorf("autopilot %s cron = %q, the table says %q", id, trig.CronExpression, w.cron)
		}
		if trig.Timezone != roleAutopilotSeedTimezone {
			t.Errorf("autopilot %s timezone = %q, want %q", id, trig.Timezone, roleAutopilotSeedTimezone)
		}
	}
	for id := range got {
		if strings.HasPrefix(id, "hr/") || strings.HasPrefix(id, "finance/") ||
			strings.HasPrefix(id, "legal/") || strings.HasPrefix(id, "sales/") ||
			strings.HasPrefix(id, "procurement/") || strings.HasPrefix(id, "marketing/") {
			t.Errorf("autopilot %s is seeded and not in the seed table", id)
		}
	}
}
