// Провижининг ролевых рабочих пространств деплоя из каталога шаблонов
// (hr/finance/legal/sales): один и тот же идемпотентный проход и при
// старте сервера, и по команде админской CLI. Пространство ищется по
// template_key, повтор ничего не создаёт; владельцем становится первый
// администратор деплоя по дате выдачи роли, а без единого администратора
// или без ключа шифрования MCP запуск ничего не создаёт и явно объясняет
// почему. Каждое созданное и каждое пропущенное пространство пишет строку
// в admin_audit. Общий агент роли, автопилоты шаблона и прочее — вне рамок
// этого файла.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const RoleWorkspacesEnvVar = "GOOSAR_ROLE_WORKSPACES"

const (
	RoleWorkspacesAuto = "auto"

	RoleWorkspacesOff = "off"
)

const (
	RoleWorkspaceCreated = "created"
	RoleWorkspaceSkipped = "skipped"
	RoleWorkspaceError   = "error"
)

const (
	adminAuditActionRoleWorkspaceProvisioned = "role_workspace.provisioned"
	adminAuditActionRoleWorkspaceSkipped     = "role_workspace.skipped"
)

func roleWorkspaceLockKey(templateKey string) string {
	return "role_workspace:" + templateKey
}

func ParseRoleWorkspacesMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return RoleWorkspacesAuto, nil
	case RoleWorkspacesAuto:
		return RoleWorkspacesAuto, nil
	case RoleWorkspacesOff:
		return RoleWorkspacesOff, nil
	default:
		return "", fmt.Errorf("%s must be %q or %q, got %q", RoleWorkspacesEnvVar, RoleWorkspacesAuto, RoleWorkspacesOff, strings.TrimSpace(raw))
	}
}

type RoleWorkspaceOutcome struct {
	TemplateKey string
	Status      string
	WorkspaceID string

	Autopilots int

	Detail string
}

type RoleWorkspaceProvisionResult struct {
	Outcomes []RoleWorkspaceOutcome
	Created  int
	Skipped  int
	Errors   int
	Deferred string
}

func ProvisionRoleWorkspaces(ctx context.Context, txs txStarter, queries *db.Queries, secretBox *secretbox.Box) (RoleWorkspaceProvisionResult, error) {
	var result RoleWorkspaceProvisionResult

	if secretBox == nil {
		return result, fmt.Errorf("role workspace provisioning requires GOOSAR_MCP_SECRET_KEY to be set")
	}

	templates, err := queries.ListEnabledWorkspaceTemplates(ctx)
	if err != nil {
		return result, fmt.Errorf("role workspaces: list templates: %w", err)
	}
	if len(templates) == 0 {
		return result, nil
	}

	admins, err := queries.ListDeploymentAdmins(ctx)
	if err != nil {
		return result, fmt.Errorf("role workspaces: list deployment admins: %w", err)
	}
	if len(admins) == 0 {
		result.Deferred = fmt.Sprintf("no deployment administrator yet: a role workspace is owned by the first administrator, so provisioning is deferred until %s resolves or an administrator is granted", DeploymentAdminEmailsEnvVar)
		slog.Warn("role workspaces: nothing provisioned — the deployment has no administrator yet; the run repeats on the next start",
			"templates", len(templates), "env_var", DeploymentAdminEmailsEnvVar)
		return result, nil
	}

	owner := admins[0].UserID

	if signupIsUnbounded() {
		slog.Warn("role workspaces: provisioning roles CLOSED because registration is unbounded — set ALLOWED_EMAILS/ALLOWED_EMAIL_DOMAINS or ALLOW_SIGNUP=false, then open each role with PATCH /api/deployment/workspaces/{id}")
	}

	h := &Handler{MCPSecretBox: secretBox}

	for _, tmpl := range templates {
		outcome := provisionOneRoleWorkspace(ctx, txs, queries, h, tmpl, owner)
		result.Outcomes = append(result.Outcomes, outcome)
		switch outcome.Status {
		case RoleWorkspaceCreated:
			result.Created++
		case RoleWorkspaceSkipped:
			result.Skipped++
		default:
			result.Errors++
		}
	}
	slog.Info("role workspaces: provisioning run finished",
		"created", result.Created, "skipped", result.Skipped, "errors", result.Errors)
	return result, nil
}

func signupIsUnbounded() bool {
	if os.Getenv("ALLOW_SIGNUP") == "false" {
		return false
	}
	return strings.TrimSpace(os.Getenv("ALLOWED_EMAILS")) == "" &&
		strings.TrimSpace(os.Getenv("ALLOWED_EMAIL_DOMAINS")) == ""
}

func provisionOneRoleWorkspace(ctx context.Context, txs txStarter, queries *db.Queries, h *Handler, tmpl db.WorkspaceTemplate, owner pgtype.UUID) RoleWorkspaceOutcome {
	out := RoleWorkspaceOutcome{TemplateKey: tmpl.Key}

	templateKey := pgtype.Text{String: tmpl.Key, Valid: true}
	existing, err := queries.GetWorkspaceByTemplateKey(ctx, templateKey)
	if err == nil {
		return roleWorkspaceExisting(ctx, txs, queries, tmpl, existing.ID, owner)
	}
	if !isNotFound(err) {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("lookup failed: %v", err)
		slog.Error("role workspaces: lookup failed", "template", tmpl.Key, "error", err)
		return out
	}

	slug, err := pickRoleWorkspaceSlug(ctx, queries, tmpl.Key)
	if err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = err.Error()
		slog.Error("role workspaces: no usable slug", "template", tmpl.Key, "error", err)
		return out
	}

	name := workspaceTemplateText(parseWorkspaceTemplateLangMap(tmpl.DisplayName), "ru")
	if name == "" {
		name = tmpl.Key
	}
	description := workspaceTemplateText(parseWorkspaceTemplateLangMap(tmpl.Description), "ru")

	tx, err := txs.Begin(ctx)
	if err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("begin transaction: %v", err)
		return out
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	if err := qtx.LockConfigLayerKey(ctx, roleWorkspaceLockKey(tmpl.Key)); err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("lock: %v", err)
		return out
	}

	if again, err := qtx.GetWorkspaceByTemplateKey(ctx, templateKey); err == nil {
		return roleWorkspaceSkipped(ctx, queries, tmpl.Key, again.ID)
	} else if !isNotFound(err) {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("lookup failed: %v", err)
		slog.Error("role workspaces: lookup failed", "template", tmpl.Key, "error", err)
		return out
	}

	ws, err := qtx.CreateRoleWorkspace(ctx, db.CreateRoleWorkspaceParams{
		Name:        name,
		Slug:        slug,
		Description: pgtype.Text{String: description, Valid: description != ""},

		IssuePrefix: generateIssuePrefix(tmpl.Key),
		TemplateKey: templateKey,

		OpenJoin: !signupIsUnbounded(),
	})
	if err != nil {
		out.Status = RoleWorkspaceError
		if isUniqueViolation(err) {

			out.Detail = fmt.Sprintf("slug %q is taken or the role was provisioned concurrently", slug)
		} else {
			out.Detail = fmt.Sprintf("create workspace: %v", err)
		}
		slog.Error("role workspaces: create failed", "template", tmpl.Key, "slug", slug, "error", err)
		return out
	}

	if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      owner,
		Role:        "owner",
	}); err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("add owner: %v", err)
		slog.Error("role workspaces: owner membership failed", "template", tmpl.Key, "error", err)
		return out
	}

	if status, msg := h.applyWorkspaceTemplate(ctx, qtx, tmpl, ws.ID, owner); status != 0 {
		out.Status = RoleWorkspaceError
		out.Detail = msg
		slog.Error("role workspaces: template composition failed", "template", tmpl.Key, "status", status, "detail", msg)
		return out
	}

	autopilots, err := provisionTemplateAutopilots(ctx, qtx, tmpl, ws.ID, owner)
	if err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("autopilots: %v", err)
		slog.Error("role workspaces: autopilot provisioning failed", "template", tmpl.Key, "error", err)
		return out
	}

	if _, err := qtx.InsertAdminAudit(ctx, roleWorkspaceAuditParams(adminAuditActionRoleWorkspaceProvisioned, tmpl.Key, uuidToString(ws.ID))); err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("journal: %v", err)
		slog.Error("role workspaces: audit insert failed", "template", tmpl.Key, "error", err)
		return out
	}

	if err := tx.Commit(ctx); err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("commit: %v", err)
		return out
	}

	out.Status = RoleWorkspaceCreated
	out.WorkspaceID = uuidToString(ws.ID)
	out.Autopilots = autopilots
	slog.Info("role workspaces: provisioned", "template", tmpl.Key, "slug", slug,
		"workspace_id", out.WorkspaceID, "autopilots", autopilots)
	return out
}

func pickRoleWorkspaceSlug(ctx context.Context, queries *db.Queries, templateKey string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(templateKey))
	for _, candidate := range []string{base, base + "-role"} {
		if !workspaceSlugPattern.MatchString(candidate) || isReservedSlug(candidate) {
			continue
		}
		_, err := queries.GetWorkspaceBySlug(ctx, candidate)
		if isNotFound(err) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("slug lookup for %q failed: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("no free workspace slug for template %q (tried %q and %q)", templateKey, base, base+"-role")
}

func roleWorkspaceAuditParams(action, templateKey, workspaceID string) db.InsertAdminAuditParams {
	return db.InsertAdminAuditParams{
		ActorUserID: pgtype.UUID{},
		Action:      action,
		TargetType:  "workspace_template",
		TargetID:    pgtype.Text{String: templateKey, Valid: true},
		RequestID:   pgtype.Text{String: workspaceID, Valid: workspaceID != ""},
	}
}

func roleWorkspaceSkipped(ctx context.Context, queries *db.Queries, templateKey string, workspaceID pgtype.UUID) RoleWorkspaceOutcome {
	out := RoleWorkspaceOutcome{
		TemplateKey: templateKey,
		Status:      RoleWorkspaceSkipped,
		WorkspaceID: uuidToString(workspaceID),
		Detail:      "already provisioned",
	}
	writeRoleWorkspaceAudit(ctx, queries, adminAuditActionRoleWorkspaceSkipped, templateKey, out.WorkspaceID)
	return out
}

func roleWorkspaceExisting(ctx context.Context, txs txStarter, queries *db.Queries, tmpl db.WorkspaceTemplate, workspaceID, owner pgtype.UUID) RoleWorkspaceOutcome {
	out := roleWorkspaceSkipped(ctx, queries, tmpl.Key, workspaceID)

	created, err := topUpTemplateAutopilots(ctx, txs, queries, tmpl, workspaceID, owner)
	if err != nil {
		out.Status = RoleWorkspaceError
		out.Detail = fmt.Sprintf("autopilots: %v", err)
		slog.Error("role workspaces: autopilot top-up failed", "template", tmpl.Key, "error", err)
		return out
	}
	out.Autopilots = created
	if created > 0 {
		out.Detail = fmt.Sprintf("already provisioned, %d new autopilot(s)", created)
		slog.Info("role workspaces: autopilots added to an existing role",
			"template", tmpl.Key, "workspace_id", out.WorkspaceID, "autopilots", created)
	}
	return out
}

func topUpTemplateAutopilots(ctx context.Context, txs txStarter, queries *db.Queries, tmpl db.WorkspaceTemplate, workspaceID, owner pgtype.UUID) (int, error) {
	tx, err := txs.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	if err := qtx.LockConfigLayerKey(ctx, roleWorkspaceLockKey(tmpl.Key)); err != nil {
		return 0, fmt.Errorf("lock: %w", err)
	}
	created, err := provisionTemplateAutopilots(ctx, qtx, tmpl, workspaceID, owner)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return created, nil
}

func writeRoleWorkspaceAudit(ctx context.Context, queries *db.Queries, action, templateKey, workspaceID string) {
	if _, err := queries.InsertAdminAudit(ctx, roleWorkspaceAuditParams(action, templateKey, workspaceID)); err != nil {
		slog.Error("role workspaces: audit insert failed", "action", action, "template", templateKey, "error", err)
	}
}

func MCPSecretBoxFromEnv() *secretbox.Box {
	box, err := secretbox.FromEnv("GOOSAR_MCP_SECRET_KEY")
	if err != nil {

		if strings.TrimSpace(os.Getenv("GOOSAR_MCP_SECRET_KEY")) != "" {
			slog.Error("GOOSAR_MCP_SECRET_KEY is set but unusable", "error", err)
		}
		return nil
	}
	return box
}
