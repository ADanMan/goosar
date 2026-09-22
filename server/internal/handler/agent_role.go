package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	roleAgentSystemKeyPrefix = "goosar_role_"

	roleAgentMaxConcurrentTasks = helperAgentMaxConcurrentTasks
)

var roleAgentNamePrefixByLang = map[string]string{
	"ru": "Агент ",
	"en": "Agent ",
	"zh": "Agent ",
	"ko": "Agent ",
	"ja": "Agent ",
}

var roleAgentDescriptionByLang = map[string]string{
	"ru": "Общий агент роли: доступен всем участникам этого пространства.",
	"en": "The role's shared agent: available to every member of this workspace.",
	"zh": "角色的共享 agent:此工作区的所有成员都可以使用。",
	"ko": "역할의 공용 agent입니다. 이 워크스페이스의 모든 구성원이 사용할 수 있습니다.",
	"ja": "ロールの共有 agent です。このワークスペースの全メンバーが利用できます。",
}

var roleAgentMethodByLang = map[string]string{
	"ru": `## Как работает общий агент роли

Ты общий агент этой роли, а не персональный помощник одного человека. С тобой работают все участники пространства, и всё, что ты пишешь, видят все.

- Читай всё, что нужно роли: ишью, документы, почту, поиск, справочники и отчёты подключённых систем.
- Пиши только документы и комментарии к ишью. Ни одну внешнюю систему ты не изменяешь сам.
- Когда нужно изменение во внешней системе — создай ишью системному специалисту, который владеет записью в неё, и опиши в нём, что и зачем менять. Специалист проводит изменение через подтверждение человеком.
- Не храни личные учётные данные участников и не проси их прислать в чат: у каждого участника для этого есть свой персональный Helper.`,
	"en": `## How a shared role agent works

You are this role's shared agent, not one person's private assistant. Every member of the workspace works with you, and everything you write is visible to all of them.

- Read whatever the role needs: issues, documents, mail, search, reference data and reports from the connected systems.
- Write only documents and issue comments. You never change an external system yourself.
- When an external system has to change, file an issue for the specialist agent that owns the write path for it and describe what has to change and why. That specialist takes the change through human approval.
- Do not keep members' personal credentials and do not ask for them in chat: every member has their own personal Helper for that.`,
}

func roleAgentLockKey(workspaceID pgtype.UUID) string {
	return "workspace-role-agent:" + uuidToString(workspaceID)
}

func roleAgentSystemKey(templateKey string) string {
	return roleAgentSystemKeyPrefix + templateKey
}

func isRoleAgent(agent db.Agent) bool {
	return agent.SystemKey.Valid && strings.HasPrefix(agent.SystemKey.String, roleAgentSystemKeyPrefix)
}

func hasLiveRoleAgent(agents []db.Agent) bool {
	for _, a := range agents {
		if !a.ArchivedAt.Valid && isRoleAgent(a) {
			return true
		}
	}
	return false
}

func (h *Handler) roleAgentProvisionable(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) (string, bool) {
	allowed := h.cfg.AllowedProviders.AllowedList()
	state, err := q.RoleAgentProvisionable(ctx, db.RoleAgentProvisionableParams{
		WorkspaceID:       workspaceID,
		RestrictProviders: allowed != nil,
		AllowedProviders:  allowed,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("role agent: provisionable probe failed",
				"workspace_id", uuidToString(workspaceID), "error", err)
		}
		return "", false
	}
	if !state.TemplateKey.Valid || state.TemplateKey.String == "" {
		return "", false
	}
	return state.TemplateKey.String, state.SlotFree && state.HasPublicRuntime
}

func (h *Handler) ensureRoleAgent(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) (helperProvisionResult, error) {
	var none helperProvisionResult
	if err := q.LockWorkspaceHelperKey(ctx, roleAgentLockKey(workspaceID)); err != nil {
		return none, err
	}

	templateKey, ok := h.roleAgentProvisionable(ctx, q, workspaceID)
	if !ok {
		return none, nil
	}

	tmpl, err := q.GetEnabledWorkspaceTemplate(ctx, templateKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return none, nil
		}
		return none, err
	}

	runtime, ok, err := h.pickRoleAgentRuntime(ctx, q, workspaceID)
	if err != nil {
		return none, err
	}
	if !ok {
		return none, nil
	}

	lang := helperDefaultContentLang
	helper := parseWorkspaceTemplateHelper(tmpl.Helper)
	displayName := workspaceTemplateText(parseWorkspaceTemplateLangMap(tmpl.DisplayName), lang)

	takenNames, err := q.ListWorkspaceAgentNames(ctx, workspaceID)
	if err != nil {
		return none, err
	}
	name, ok := roleAgentNameFor(helper, displayName, tmpl.Key, lang, takenNames)
	if !ok {
		slog.Warn("role agent: no free name available",
			"workspace_id", uuidToString(workspaceID), "template", tmpl.Key)
		return none, nil
	}

	created, err := q.CreateRoleAgent(ctx, db.CreateRoleAgentParams{
		WorkspaceID:        workspaceID,
		Name:               name,
		Description:        roleAgentDescriptionByLang[lang],
		AvatarUrl:          pgtype.Text{String: helperAgentAvatarURL, Valid: true},
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeID:          runtime.ID,
		MaxConcurrentTasks: roleAgentMaxConcurrentTasks,
		Instructions:       roleAgentInstructions(lang, helper),
		SystemKey:          pgtype.Text{String: roleAgentSystemKey(tmpl.Key), Valid: true},
	})
	if err != nil {
		return none, err
	}

	if err := replaceInvocationTargetsWithQueries(ctx, q, created.ID, pgtype.UUID{}, []targetSpec{
		{targetType: invocationTargetWorkspace, targetID: workspaceID},
	}); err != nil {
		return none, err
	}

	if _, err := reconcileTemplateAutopilots(ctx, q, workspaceID, created.ID); err != nil {
		return none, err
	}

	return helperProvisionResult{Agent: created, Created: true, IsFirstAgent: len(takenNames) == 0}, nil
}

func (h *Handler) provisionRoleAgent(ctx context.Context, workspaceID pgtype.UUID) bool {
	wsID := uuidToString(workspaceID)

	if _, ok := h.roleAgentProvisionable(ctx, h.Queries, workspaceID); !ok {
		return false
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("role agent: begin failed", "workspace_id", wsID, "error", err)
		return false
	}
	defer tx.Rollback(ctx)

	result, err := h.ensureRoleAgent(ctx, h.Queries.WithTx(tx), workspaceID)
	if err != nil {
		slog.Warn("role agent: provisioning failed", "workspace_id", wsID, "error", err)
		return false
	}
	if !result.Created {
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("role agent: commit failed", "workspace_id", wsID, "error", err)
		return false
	}

	slog.Info("role agent provisioned",
		"workspace_id", wsID,
		"system_key", result.Agent.SystemKey.String,
		"agent_id", uuidToString(result.Agent.ID))

	h.announceHelperCreated(ctx, wsID, result.Agent, result.IsFirstAgent, helperAnnounceQuiet)
	return true
}

func (h *Handler) pickRoleAgentRuntime(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) (db.AgentRuntime, bool, error) {
	runtimes, err := q.ListAgentRuntimes(ctx, workspaceID)
	if err != nil {
		return db.AgentRuntime{}, false, err
	}

	usable := func(rt db.AgentRuntime) bool {
		return rt.Status == "online" &&
			rt.Visibility == "public" &&
			h.cfg.AllowedProviders.Allows(rt.Provider)
	}

	ranks := []func(db.AgentRuntime) bool{
		func(rt db.AgentRuntime) bool { return rt.Provider == helperHermesProvider },
		func(db.AgentRuntime) bool { return true },
	}
	for _, matches := range ranks {
		for _, rt := range runtimes {
			if matches(rt) && usable(rt) {
				return rt, true, nil
			}
		}
	}
	return db.AgentRuntime{}, false, nil
}

func parseWorkspaceTemplateHelper(raw []byte) workspaceTemplateHelper {
	var helper workspaceTemplateHelper
	if len(raw) == 0 || isJSONNull(raw) {
		return helper
	}
	if err := json.Unmarshal(raw, &helper); err != nil {
		return workspaceTemplateHelper{}
	}
	return helper
}

func roleAgentInstructions(lang string, helper workspaceTemplateHelper) string {
	parts := []string{helperInstructionsByLang[lang]}
	if method := roleAgentMethodByLang[lang]; method != "" {
		parts = append(parts, method)
	} else {
		parts = append(parts, roleAgentMethodByLang[helperDefaultContentLang])
	}
	if extra := workspaceTemplateText(helper.ExtraInstructions, lang); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, "\n\n")
}

func roleAgentNameFor(helper workspaceTemplateHelper, displayName, templateKey, lang string, takenNames []string) (string, bool) {
	base := workspaceTemplateText(helper.Name, lang)
	if base == "" {
		label := displayName
		if label == "" {
			label = templateKey
		}
		prefix, ok := roleAgentNamePrefixByLang[lang]
		if !ok {
			prefix = roleAgentNamePrefixByLang[helperDefaultContentLang]
		}
		base = prefix + label
	}

	taken := make(map[string]struct{}, len(takenNames))
	for _, name := range takenNames {
		taken[name] = struct{}{}
	}
	for n := 1; n <= helperNameNumberedAttempts; n++ {
		candidate := base
		if n > 1 {
			candidate = base + " " + strconv.Itoa(n)
		}
		if _, clash := taken[candidate]; !clash {
			return candidate, true
		}
	}
	return "", false
}

func (h *Handler) denyRoleAgentToNonDeploymentAdmin(w http.ResponseWriter, r *http.Request, agent db.Agent) bool {
	if !isRoleAgent(agent) {
		return false
	}
	if h.callerIsDeploymentAdmin(r, requestUserID(r)) {
		return false
	}
	writeError(w, http.StatusForbidden, "only a deployment administrator can manage the shared role agent")
	return true
}
