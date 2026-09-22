// Превращение автопилотов роли из шаблона в реальные строки autopilot:
// читает колонку autopilots шаблона и создаёт автопилоты с триггерами в
// той же транзакции, где создаётся ролевое пространство. Идентичность — по
// external_key, повтор ничего не дублирует, а архивный автопилот шаблона
// не восстанавливается. Поддерживаются только триггеры schedule и webhook,
// расписание требует cron-выражения и таймзоны. Секретов в шаблоне нет:
// токен вебхука выпускается только при создании. Пока не опубликован
// рантайм, автопилоты рождаются на паузе без исполнителя и получают
// агента, как только он появляется.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	templateAutopilotExternalKeyPrefix = "template:"

	adminAuditActionRoleWorkspaceAutopilotProvisioned = "role_workspace.autopilot_provisioned"
	adminAuditActionRoleWorkspaceAutopilotSkipped     = "role_workspace.autopilot_skipped"

	templateAutopilotAssigneeRole = "role"

	templateAutopilotRiskL1 = "L1"
	templateAutopilotRiskL2 = "L2"
)

var templateAutopilotSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type workspaceTemplateAutopilot struct {
	Slug               string            `json:"slug"`
	Title              map[string]string `json:"title"`
	Description        map[string]string `json:"description"`
	IssueTitleTemplate map[string]string `json:"issue_title_template"`
	ExecutionMode      string            `json:"execution_mode"`

	AssigneeRole string `json:"assignee_role"`

	Risk     string                              `json:"risk"`
	Triggers []workspaceTemplateAutopilotTrigger `json:"triggers"`
}

type workspaceTemplateAutopilotTrigger struct {
	Kind           string            `json:"kind"`
	CronExpression string            `json:"cron_expression"`
	Timezone       string            `json:"timezone"`
	Label          map[string]string `json:"label"`
}

func templateAutopilotExternalKey(templateKey, slug string) string {
	return templateAutopilotExternalKeyPrefix + templateKey + ":" + slug
}

func parseWorkspaceTemplateAutopilots(raw []byte) ([]workspaceTemplateAutopilot, error) {
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var entries []workspaceTemplateAutopilot
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("template autopilots must be a JSON array of autopilot objects: %w", err)
	}

	seen := make(map[string]bool, len(entries))
	for i := range entries {
		ap := &entries[i]
		if !templateAutopilotSlugPattern.MatchString(ap.Slug) {
			return nil, fmt.Errorf("template autopilot #%d has an invalid slug %q (lowercase letters, digits and dashes)", i+1, ap.Slug)
		}
		if seen[ap.Slug] {
			return nil, fmt.Errorf("template has a duplicate autopilot slug %q", ap.Slug)
		}
		seen[ap.Slug] = true

		if workspaceTemplateText(ap.Title, "ru") == "" {
			return nil, fmt.Errorf("template autopilot %q has no title in any language", ap.Slug)
		}
		if ap.ExecutionMode == "" {
			ap.ExecutionMode = "create_issue"
		}
		if ap.ExecutionMode != "create_issue" && ap.ExecutionMode != "run_only" {
			return nil, fmt.Errorf("template autopilot %q has execution_mode %q, want create_issue or run_only", ap.Slug, ap.ExecutionMode)
		}
		if ap.AssigneeRole == "" {
			ap.AssigneeRole = templateAutopilotAssigneeRole
		}
		if ap.AssigneeRole != templateAutopilotAssigneeRole {
			return nil, fmt.Errorf("template autopilot %q has assignee_role %q, want %q",
				ap.Slug, ap.AssigneeRole, templateAutopilotAssigneeRole)
		}
		if ap.Risk == "" {
			ap.Risk = templateAutopilotRiskL1
		}
		if ap.Risk != templateAutopilotRiskL1 && ap.Risk != templateAutopilotRiskL2 {
			return nil, fmt.Errorf("template autopilot %q has risk %q, want %q or %q",
				ap.Slug, ap.Risk, templateAutopilotRiskL1, templateAutopilotRiskL2)
		}
		if len(ap.Triggers) == 0 {
			return nil, fmt.Errorf("template autopilot %q has no trigger — it would never fire", ap.Slug)
		}
		for _, trigger := range ap.Triggers {
			if err := validateTemplateAutopilotTrigger(ap.Slug, trigger); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

func validateTemplateAutopilotTrigger(slug string, trigger workspaceTemplateAutopilotTrigger) error {
	switch trigger.Kind {
	case "schedule":
		if trigger.CronExpression == "" {
			return fmt.Errorf("template autopilot %q: a schedule trigger requires cron_expression", slug)
		}
		if trigger.Timezone == "" {
			return fmt.Errorf("template autopilot %q: a schedule trigger requires timezone", slug)
		}
		if err := service.ValidateTimezone(trigger.Timezone); err != nil {
			return fmt.Errorf("template autopilot %q: %w", slug, err)
		}
		if _, err := service.ComputeNextRun(trigger.CronExpression, trigger.Timezone); err != nil {
			return fmt.Errorf("template autopilot %q: %w", slug, err)
		}
	case "webhook":
		if trigger.CronExpression != "" || trigger.Timezone != "" {
			return fmt.Errorf("template autopilot %q: a webhook trigger takes no cron_expression or timezone", slug)
		}
	case "api":
		return fmt.Errorf("template autopilot %q: trigger kind %q is deprecated and cannot be provisioned; use schedule or webhook", slug, "api")
	default:
		return fmt.Errorf("template autopilot %q: trigger kind %q must be schedule or webhook", slug, trigger.Kind)
	}
	return nil
}

func provisionTemplateAutopilots(ctx context.Context, q *db.Queries, tmpl db.WorkspaceTemplate, workspaceID, ownerID pgtype.UUID) (int, error) {
	entries, err := parseWorkspaceTemplateAutopilots(tmpl.Autopilots)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}

	roleAgent := roleAgentIDFor(ctx, q, workspaceID, tmpl.Key)

	const lang = helperDefaultContentLang

	created := 0
	for _, entry := range entries {
		assignee := roleAgent

		const status = "paused"
		externalKey := templateAutopilotExternalKey(tmpl.Key, entry.Slug)
		exists, err := q.TemplateAutopilotExists(ctx, db.TemplateAutopilotExistsParams{
			WorkspaceID: workspaceID,
			ExternalKey: pgtype.Text{String: externalKey, Valid: true},
		})
		if err != nil {
			return created, fmt.Errorf("look up autopilot %q: %w", externalKey, err)
		}
		if exists {
			writeRoleWorkspaceAudit(ctx, q, adminAuditActionRoleWorkspaceAutopilotSkipped, tmpl.Key, externalKey)
			continue
		}

		autopilot, err := q.CreateAutopilot(ctx, db.CreateAutopilotParams{
			WorkspaceID:        workspaceID,
			Title:              workspaceTemplateText(entry.Title, lang),
			Description:        templateAutopilotText(entry.Description, lang),
			AssigneeType:       "agent",
			AssigneeID:         assignee,
			Status:             status,
			ExecutionMode:      entry.ExecutionMode,
			IssueTitleTemplate: templateAutopilotText(entry.IssueTitleTemplate, lang),
			CreatedByType:      "member",
			CreatedByID:        ownerID,
			ExternalKey:        pgtype.Text{String: externalKey, Valid: true},
		})
		if err != nil {
			return created, fmt.Errorf("create autopilot %q: %w", externalKey, err)
		}
		for _, trigger := range entry.Triggers {
			if err := createTemplateAutopilotTrigger(ctx, q, autopilot.ID, trigger, lang); err != nil {
				return created, fmt.Errorf("create trigger for autopilot %q: %w", externalKey, err)
			}
		}
		writeRoleWorkspaceAudit(ctx, q, adminAuditActionRoleWorkspaceAutopilotProvisioned, tmpl.Key, externalKey)
		created++
	}
	return created, nil
}

func createTemplateAutopilotTrigger(ctx context.Context, q *db.Queries, autopilotID pgtype.UUID, trigger workspaceTemplateAutopilotTrigger, lang string) error {
	params := db.CreateAutopilotTriggerParams{
		AutopilotID:  autopilotID,
		Kind:         trigger.Kind,
		Enabled:      true,
		Label:        templateAutopilotText(trigger.Label, lang),
		EventFilters: []byte("[]"),
	}
	switch trigger.Kind {
	case "schedule":
		next, err := service.ComputeNextRun(trigger.CronExpression, trigger.Timezone)
		if err != nil {
			return err
		}
		params.CronExpression = pgtype.Text{String: trigger.CronExpression, Valid: true}
		params.Timezone = pgtype.Text{String: trigger.Timezone, Valid: true}
		params.NextRunAt = pgtype.Timestamptz{Time: next, Valid: true}
	case "webhook":
		token, err := generateWebhookToken()
		if err != nil {
			return err
		}
		params.Provider = pgtype.Text{String: "generic", Valid: true}
		params.WebhookToken = pgtype.Text{String: token, Valid: true}
	}
	_, err := q.CreateAutopilotTrigger(ctx, params)
	return err
}

func reconcileTemplateAutopilots(ctx context.Context, q *db.Queries, workspaceID, agentID pgtype.UUID) (int, error) {
	ids, err := q.AssignRoleAgentToTemplateAutopilots(ctx, db.AssignRoleAgentToTemplateAutopilotsParams{
		WorkspaceID: workspaceID,
		AssigneeID:  agentID,
	})
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := refreshTemplateAutopilotNextRun(ctx, q, id); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		slog.Info("role workspaces: template autopilots handed to the role agent (still paused)",
			"workspace_id", uuidToString(workspaceID), "agent_id", uuidToString(agentID), "count", len(ids))
	}
	return len(ids), nil
}

func refreshTemplateAutopilotNextRun(ctx context.Context, q *db.Queries, autopilotID pgtype.UUID) error {
	triggers, err := q.ListAutopilotTriggers(ctx, autopilotID)
	if err != nil {
		return fmt.Errorf("list triggers of autopilot %s: %w", uuidToString(autopilotID), err)
	}
	for _, trigger := range triggers {
		if trigger.Kind != "schedule" || !trigger.CronExpression.Valid || !trigger.Timezone.Valid {
			continue
		}
		next, err := service.ComputeNextRun(trigger.CronExpression.String, trigger.Timezone.String)
		if err != nil {

			slog.Warn("role workspaces: cannot recompute next run for a template autopilot trigger",
				"trigger_id", uuidToString(trigger.ID), "error", err)
			continue
		}
		if _, err := q.UpdateAutopilotTrigger(ctx, db.UpdateAutopilotTriggerParams{
			ID:        trigger.ID,
			NextRunAt: pgtype.Timestamptz{Time: next, Valid: true},
		}); err != nil {
			return fmt.Errorf("refresh next run of trigger %s: %w", uuidToString(trigger.ID), err)
		}
	}
	return nil
}

func roleAgentIDFor(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, templateKey string) pgtype.UUID {
	agent, err := q.GetRoleAgent(ctx, db.GetRoleAgentParams{
		WorkspaceID: workspaceID,
		SystemKey:   pgtype.Text{String: roleAgentSystemKey(templateKey), Valid: true},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("role workspaces: role agent lookup failed",
				"workspace_id", uuidToString(workspaceID), "template", templateKey, "error", err)
		}
		return pgtype.UUID{}
	}
	return agent.ID
}

func templateAutopilotText(m map[string]string, lang string) pgtype.Text {
	value := strings.TrimSpace(workspaceTemplateText(m, lang))
	return pgtype.Text{String: value, Valid: value != ""}
}
