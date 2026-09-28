package agent

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/agenttemplate"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/skill"
)

type createFromTemplateRequest struct {
	TemplateSlug       string             `json:"template_slug"`
	Name               string             `json:"name"`
	RuntimeID          string             `json:"runtime_id"`
	Model              string             `json:"model"`
	Visibility         string             `json:"visibility"`
	MaxConcurrentTasks int                `json:"max_concurrent_tasks"`
	PermissionMode     string             `json:"permission_mode"`
	InvocationTargets  []InvocationTarget `json:"invocation_targets"`
	Description        string             `json:"description"`
	Instructions       string             `json:"instructions"`
	AvatarURL          string             `json:"avatar_url"`
	ExtraSkillIDs      []string           `json:"extra_skill_ids"`
}

// handleCreateFromTemplate — createAgentFromTemplate (contract §10.6):
// разворачивает набор навыков шаблона (agenttemplate), переиспользуя
// существующие в воркспейсе по имени и импортируя недостающие по
// source_url (skill.EnsureByNameOrImport); если импорт хотя бы одного
// источника не удался — вся операция отменяется (422), агент не создаётся.
func (d *Deps) handleCreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req createFromTemplateRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.TemplateSlug == "" || req.Name == "" || req.RuntimeID == "" {
		httpapi.BadRequest(w, "template_slug, name and runtime_id are required")
		return
	}
	tmpl, ok := agenttemplate.Get(req.TemplateSlug)
	if !ok {
		httpapi.BadRequest(w, "unknown template_slug")
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

	templateSkillIDs := make([]string, 0, len(tmpl.Skills))
	var failedURLs []string
	for _, ref := range tmpl.Skills {
		name := ref.CachedName
		if name == "" {
			name = ref.SourceURL
		}
		id, err := skill.EnsureByNameOrImport(r.Context(), d.DB, wsID, vc.ViewerID, name, ref.SourceURL)
		if err != nil {
			failedURLs = append(failedURLs, ref.SourceURL)
			continue
		}
		templateSkillIDs = append(templateSkillIDs, id)
	}
	if len(failedURLs) > 0 {
		httpapi.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "one or more template skill sources are unavailable", "failed_urls": failedURLs,
		})
		return
	}
	skillIDs := append(append([]string{}, templateSkillIDs...), req.ExtraSkillIDs...)

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

	instructions := req.Instructions
	if instructions == "" {
		instructions = tmpl.Instructions
	}
	description := req.Description
	if description == "" {
		description = tmpl.Description
	}
	var avatar *string
	if req.AvatarURL != "" {
		avatar = &req.AvatarURL
	}
	maxConcurrent := req.MaxConcurrentTasks
	if maxConcurrent == 0 {
		maxConcurrent = 6
	}

	a, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID: wsID, ExecutorID: req.RuntimeID, Title: req.Name, Summary: description,
		Instructions: instructions, AvatarURI: avatar, RuntimeMode: "local",
		PermissionMode: permissionMode, InvocationTargets: targets, MaxConcurrentTasks: maxConcurrent,
		Model: req.Model, OwnerAccountID: pickOwner(vc),
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if len(skillIDs) > 0 {
		if _, err := d.Store.SetAgentSkills(r.Context(), wsID, a.ID, skillIDs); err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	importedIDs := templateSkillIDs
	if importedIDs == nil {
		importedIDs = []string{}
	}

	view, err := d.fullView(r, vc, a)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:created", map[string]any{"agent": view})
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"agent": view, "imported_skill_ids": importedIDs})
}

func pickOwner(vc viewCtx) string {
	if vc.IsHuman {
		return vc.ViewerID
	}
	return ""
}
