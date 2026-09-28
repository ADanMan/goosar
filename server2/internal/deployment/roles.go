package deployment

import (
	"context"
	"fmt"
	"strings"

	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// RoleTemplate — минимальный встроенный каталог "шаблонов ролевых
// воркспейсов" для CLI `provision-roles`/автопровижининга при старте
// (`GOOSAR_ROLE_WORKSPACES=auto`). Контракт (приложение CLI) описывает
// поведение команды ("создаёт ролевые воркспейсы деплоя из включённых
// шаблонов воркспейсов"), но нигде не перечисляет сам список шаблонов —
// docs/50-api-contract.md — решение T-026 уже отмечало это как пробел
// (`workspaceTemplatesList` — пустой каталог) применительно к
// самообслуживаемым шаблонам пространства; тот же пробел здесь. Решение
// зафиксировано в server2/docs/decisions.md, раздел T-029: минимальный
// встроенный набор из трёх ролей, покрывающих типичные самостоятельно
// пополняемые пространства деплоя (§7 "join-targets").
type RoleTemplate struct {
	Key         string
	Name        string
	Slug        string
	Description string
	IssuePrefix string
}

var builtinRoleTemplates = []RoleTemplate{
	{Key: "support", Name: "Support", Slug: "support", Description: "Ролевое пространство поддержки пользователей", IssuePrefix: "SUP"},
	{Key: "sales", Name: "Sales", Slug: "sales", Description: "Ролевое пространство отдела продаж", IssuePrefix: "SLS"},
	{Key: "onboarding", Name: "Onboarding", Slug: "onboarding", Description: "Ролевое пространство для онбординга новых сотрудников", IssuePrefix: "ONB"},
}

// EnabledRoleTemplates разбирает значение GOOSAR_ROLE_WORKSPACES: "auto" —
// весь встроенный каталог, "" — ничего, иначе — CSV список ключей.
func EnabledRoleTemplates(spec string) []RoleTemplate {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	if strings.EqualFold(spec, "auto") {
		return builtinRoleTemplates
	}
	want := map[string]bool{}
	for _, k := range strings.Split(spec, ",") {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	out := []RoleTemplate{}
	for _, t := range builtinRoleTemplates {
		if want[t.Key] {
			out = append(out, t)
		}
	}
	return out
}

// ProvisionResult — одна строка отчёта CLI `provision-roles`.
type ProvisionResult struct {
	Key         string
	Action      string // "created" | "skipped"
	WorkspaceID string
	Slug        string
	Reason      string
}

// firstOwnerAccount — кому принадлежит вновь созданное ролевое пространство:
// контракт не определяет владельца автоматически создаваемых системой
// пространств. Решение — первый держатель роли deployment-admin
// (pa_granted_at asc); если такого ещё нет (совсем свежий деплой без единого
// admin), провижининг этой роли пропускается с явной причиной — создавать
// пространство без владельца было бы противоречием инварианту "у
// пространства всегда есть owner" (см. docs/51-data-model.md).
func firstOwnerAccount(ctx context.Context, db *store.Store) (string, bool, error) {
	var id string
	err := db.Pool.QueryRow(ctx, `
		SELECT account_id FROM platform_admins ORDER BY pa_granted_at ASC LIMIT 1`).Scan(&id)
	if store.IsNoRows(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("deployment: поиск владельца для provision-roles: %w", err)
	}
	return id, true, nil
}

// ProvisionRoleWorkspaces создаёт по одному воркспейсу на каждый включённый
// шаблон, если такого ещё нет (идемпотентно по ws_template_key). Тот же
// вызов используется CLI `provision-roles` и (когда GOOSAR_ROLE_WORKSPACES=auto)
// стартом cmd/server.
func ProvisionRoleWorkspaces(ctx context.Context, db *store.Store, ws *workspace.Store, templates []RoleTemplate) ([]ProvisionResult, error) {
	out := make([]ProvisionResult, 0, len(templates))
	if len(templates) == 0 {
		return out, nil
	}
	ownerID, hasOwner, err := firstOwnerAccount(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		var existingID string
		err := db.Pool.QueryRow(ctx, `SELECT id FROM spaces WHERE ws_template_key = $1`, t.Key).Scan(&existingID)
		if err == nil {
			out = append(out, ProvisionResult{Key: t.Key, Action: "skipped", WorkspaceID: existingID, Reason: "already provisioned"})
			continue
		}
		if !store.IsNoRows(err) {
			return nil, fmt.Errorf("deployment: проверка существующего провижининга роли %s: %w", t.Key, err)
		}
		if !hasOwner {
			out = append(out, ProvisionResult{Key: t.Key, Action: "skipped", Reason: "no deployment-admin exists yet to own the workspace"})
			continue
		}
		slug := t.Slug
		if taken, err := ws.SlugTaken(ctx, slug); err == nil && taken {
			slug = fmt.Sprintf("%s-role", t.Slug)
		}
		created, err := ws.CreateWorkspace(ctx, workspace.CreateWorkspaceParams{
			Name: t.Name, Slug: slug, Description: ptr(t.Description), IssuePrefix: t.IssuePrefix, OwnerID: ownerID,
		})
		if err != nil {
			return nil, fmt.Errorf("deployment: создание ролевого воркспейса %s: %w", t.Key, err)
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE spaces SET ws_template_key = $2, ws_open_join = true WHERE id = $1`, created.ID, t.Key); err != nil {
			return nil, fmt.Errorf("deployment: пометка ролевого воркспейса %s: %w", t.Key, err)
		}
		out = append(out, ProvisionResult{Key: t.Key, Action: "created", WorkspaceID: created.ID, Slug: created.Slug})
	}
	return out, nil
}
