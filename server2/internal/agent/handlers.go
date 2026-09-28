package agent

import (
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// resolveWorkspace — общий пролог обработчиков /api/agents/** (воркспейс
// только в заголовке/query, contract §1.4), тот же приём, что internal/chat
// и internal/project используют через httpapi.RequireWorkspaceMember.
func (d *Deps) resolveWorkspace(w http.ResponseWriter, r *http.Request) (workspaceID string, vc viewCtx, ok bool) {
	wsID, role, actor, rok := httpapi.RequireWorkspaceMember(w, r, d.Workspace)
	if !rok {
		return "", viewCtx{}, false
	}
	alwaysReveal, err := d.Store.alwaysRevealSecrets(r.Context(), wsID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return "", viewCtx{}, false
	}
	composioEnabled, err := d.Store.composioEnabled(r.Context(), wsID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return "", viewCtx{}, false
	}
	return wsID, viewCtx{
		ViewerID: actor.UserID, IsHuman: actor.IsHuman, Role: role,
		AlwaysReveal: alwaysReveal, ComposioEnabled: composioEnabled,
	}, true
}

// fullView собирает Agent + связанные списки в JSON components/schemas/Agent.
func (d *Deps) fullView(r *http.Request, vc viewCtx, a Agent) (map[string]any, error) {
	targets, err := d.Store.InvocationTargets(r.Context(), a.ID)
	if err != nil {
		return nil, err
	}
	skills, err := d.Store.ListAgentSkills(r.Context(), a.ID)
	if err != nil {
		return nil, err
	}
	disabled, err := d.Store.ListDisabledRuntimeSkills(r.Context(), a.ID)
	if err != nil {
		return nil, err
	}
	return d.View(vc, a, targets, skills, disabled), nil
}

func (d *Deps) handleListAgents(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	agents, err := d.Store.List(r.Context(), wsID, includeArchived)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		visible, err := d.Store.CanView(r.Context(), a, vc.ViewerID, vc.IsHuman, vc.Role)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if a.PermissionMode == "private" && !visible {
			continue
		}
		view, err := d.fullView(r, vc, a)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		out = append(out, view)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

type createAgentRequest struct {
	Name                     string             `json:"name"`
	Description              string             `json:"description"`
	Instructions             string             `json:"instructions"`
	AvatarURL                *string            `json:"avatar_url"`
	RuntimeID                string             `json:"runtime_id"`
	RuntimeConfig            map[string]any     `json:"runtime_config"`
	CustomEnv                map[string]string  `json:"custom_env"`
	CustomArgs               []string           `json:"custom_args"`
	McpConfig                map[string]any     `json:"mcp_config"`
	Visibility               string             `json:"visibility"`
	PermissionMode           string             `json:"permission_mode"`
	InvocationTargets        []InvocationTarget `json:"invocation_targets"`
	MaxConcurrentTasks       int                `json:"max_concurrent_tasks"`
	Model                    string             `json:"model"`
	ThinkingLevel            string             `json:"thinking_level"`
	ServiceTier              string             `json:"service_tier"`
	ComposioToolkitAllowlist []string           `json:"composio_toolkit_allowlist"`
	SkillIDs                 []string           `json:"skill_ids"`
}

// permissionFromRequest resuelve permission_mode/invocation_targets либо
// visibility (устаревший алиас) — если оба переданы, побеждает
// permission_mode/invocation_targets (contract §10.1).
func permissionFromRequest(permissionMode, visibility string, targets []InvocationTarget) (string, []InvocationTarget) {
	if permissionMode != "" {
		return permissionMode, targets
	}
	if visibility == "workspace" {
		return "public_to", []InvocationTarget{{TargetType: "workspace"}}
	}
	return "private", nil
}

func (d *Deps) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req createAgentRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.Name == "" || req.RuntimeID == "" {
		httpapi.BadRequest(w, "name and runtime_id are required")
		return
	}
	rt, found, err := d.Store.getRuntime(r.Context(), wsID, req.RuntimeID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.BadRequest(w, "runtime_id does not belong to this workspace")
		return
	}
	if !runtimeAccessible(rt, vc.ViewerID, vc.isOwnerOrAdmin()) {
		httpapi.Forbidden(w, "runtime is private and not accessible to the caller")
		return
	}
	permissionMode, targets := permissionFromRequest(req.PermissionMode, req.Visibility, req.InvocationTargets)

	taken, err := d.Store.NameTaken(r.Context(), wsID, req.Name, "")
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if taken {
		httpapi.WriteError(w, http.StatusConflict, "an agent with this name already exists", "agent_name_taken")
		return
	}

	ownerID := ""
	if vc.IsHuman {
		ownerID = vc.ViewerID
	}
	a, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID: wsID, ExecutorID: req.RuntimeID, Title: req.Name, Summary: req.Description,
		Instructions: req.Instructions, AvatarURI: req.AvatarURL, RuntimeMode: "local",
		RuntimeConfig: req.RuntimeConfig, CustomArgs: req.CustomArgs, McpConfig: req.McpConfig,
		CustomEnv: req.CustomEnv, PermissionMode: permissionMode, InvocationTargets: targets,
		MaxConcurrentTasks: req.MaxConcurrentTasks, Model: req.Model, ThinkingLevel: req.ThinkingLevel,
		ServiceTier: req.ServiceTier, ComposioAllowlist: req.ComposioToolkitAllowlist, OwnerAccountID: ownerID,
	})
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "an agent with this name already exists", "agent_name_taken")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if len(req.SkillIDs) > 0 {
		if _, err := d.Store.SetAgentSkills(r.Context(), wsID, a.ID, req.SkillIDs); err != nil {
			if errors.Is(err, ErrSkillNotInWorkspace) {
				httpapi.BadRequest(w, "one of skill_ids does not belong to this workspace")
				return
			}
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	view, err := d.fullView(r, vc, a)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:created", map[string]any{"agent": view})
	httpapi.WriteJSON(w, http.StatusCreated, view)
}

func (d *Deps) loadAgent(w http.ResponseWriter, r *http.Request, wsID string) (Agent, bool) {
	a, err := d.Store.Get(r.Context(), wsID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "agent not found")
		return Agent{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Agent{}, false
	}
	return a, true
}

func (d *Deps) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if a.PermissionMode == "private" {
		visible, err := d.Store.CanView(r.Context(), a, vc.ViewerID, vc.IsHuman, vc.Role)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if !visible {
			httpapi.Forbidden(w, "no access to this private agent")
			return
		}
	}
	view, err := d.fullView(r, vc, a)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
}

func (d *Deps) handleArchiveAgent(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "only the agent owner or workspace owner/admin can archive it")
		return
	}
	archived, err := d.Store.Archive(r.Context(), wsID, a.ID, vc.ViewerID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !archived {
		httpapi.WriteError(w, http.StatusConflict, "agent is already archived", "agent_already_archived")
		return
	}
	if d.Dispatch != nil {
		if _, err := d.Dispatch.CancelActiveForOperative(r.Context(), d.DB.Pool, wsID, a.ID); err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	a, ok = d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	view, err := d.fullView(r, vc, a)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:archived", map[string]any{"agent": view})
	httpapi.WriteJSON(w, http.StatusOK, view)
}

func (d *Deps) handleRestoreAgent(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "only the agent owner or workspace owner/admin can restore it")
		return
	}
	restored, err := d.Store.Restore(r.Context(), wsID, a.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !restored {
		httpapi.WriteError(w, http.StatusConflict, "agent is not archived", "agent_not_archived")
		return
	}
	a, ok = d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	view, err := d.fullView(r, vc, a)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:restored", map[string]any{"agent": view})
	httpapi.WriteJSON(w, http.StatusOK, view)
}

func (d *Deps) handleCancelAgentTasks(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "only the agent owner or workspace owner/admin can cancel its tasks")
		return
	}
	cancelled := 0
	if d.Dispatch != nil {
		jobs, err := d.Dispatch.CancelActiveForOperative(r.Context(), d.DB.Pool, wsID, a.ID)
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		cancelled = len(jobs)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}
