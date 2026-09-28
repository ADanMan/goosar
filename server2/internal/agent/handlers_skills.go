package agent

import (
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

func (d *Deps) handleListAgentSkills(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	skills, err := d.Store.ListAgentSkills(r.Context(), a.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, skills)
}

func (d *Deps) requireManage(w http.ResponseWriter, r *http.Request, wsID string) (Agent, bool) {
	_, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return Agent{}, false
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return Agent{}, false
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "only the agent owner or workspace owner/admin can manage it")
		return Agent{}, false
	}
	return a, true
}

type skillIDsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

func (d *Deps) handleSetAgentSkills(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	var req skillIDsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	skills, err := d.Store.SetAgentSkills(r.Context(), wsID, a.ID, req.SkillIDs)
	if d.writeSkillsErr(w, err) {
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID, "skills": skills})
	httpapi.WriteJSON(w, http.StatusOK, skills)
}

func (d *Deps) handleAddAgentSkills(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	var req skillIDsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.SkillIDs) == 0 {
		httpapi.BadRequest(w, "skill_ids is required")
		return
	}
	skills, err := d.Store.AddAgentSkills(r.Context(), wsID, a.ID, req.SkillIDs)
	if d.writeSkillsErr(w, err) {
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID, "skills": skills})
	httpapi.WriteJSON(w, http.StatusOK, skills)
}

func (d *Deps) writeSkillsErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSkillNotInWorkspace) {
		httpapi.BadRequest(w, "one of skill_ids does not belong to this workspace")
		return true
	}
	httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
	return true
}

func (d *Deps) handleSetAgentSkillEnabled(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	skills, err := d.Store.SetAgentSkillEnabled(r.Context(), a.ID, r.PathValue("skillId"), req.Enabled)
	if errors.Is(err, ErrSkillNotAttached) {
		httpapi.NotFound(w, "skill is not attached to this agent")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID, "skills": skills})
	httpapi.WriteJSON(w, http.StatusOK, skills)
}

func (d *Deps) handleRemoveAgentSkill(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	skills, err := d.Store.RemoveAgentSkill(r.Context(), a.ID, r.PathValue("skillId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID, "skills": skills})
	httpapi.WriteJSON(w, http.StatusOK, skills)
}

// runtimeSkillProviderOf — сопоставление executors.ex_provider (движок
// агента) с provider для DisabledRuntimeSkill (`runtime-c`/`runtime-e`,
// contract §10.4). Контракт не документирует это отображение явно (запрос
// setAgentRuntimeSkillEnabled не несёт provider) — решение этой сессии (см.
// server2/docs/decisions.md, T-028): первый встреченный движок сохраняет
// "runtime-c", остальные — "runtime-e"; сопоставление обновляется по мере
// того, как реальные значения ex_provider становятся известны (T-028
// runtime/daemon, сосед A).
func runtimeSkillProviderOf(exProvider string) string {
	if exProvider == "" {
		return "runtime-c"
	}
	switch exProvider {
	case "codex":
		return "runtime-e"
	default:
		return "runtime-c"
	}
}

func (d *Deps) handleSetAgentRuntimeSkillEnabled(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	var req struct {
		RuntimeID string `json:"runtime_id"`
		Root      string `json:"root"`
		Key       string `json:"key"`
		Name      string `json:"name"`
		Plugin    string `json:"plugin"`
		Enabled   bool   `json:"enabled"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.RuntimeID == "" || req.Root == "" || req.Key == "" {
		httpapi.BadRequest(w, "runtime_id, root and key are required")
		return
	}
	rt, found, err := d.Store.getRuntime(r.Context(), wsID, req.RuntimeID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.NotFound(w, "runtime not found in this workspace")
		return
	}
	provider := runtimeSkillProviderOf(rt.Provider)
	err = d.Store.SetRuntimeSkillEnabled(r.Context(), a.ID, a.ExecutorID, req.RuntimeID, provider, req.Root, req.Key, req.Name, req.Plugin, req.Enabled)
	if errors.Is(err, ErrRuntimeMismatch) {
		httpapi.WriteError(w, http.StatusConflict, "agent is no longer bound to this runtime", "runtime_mismatch")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID})
	w.WriteHeader(http.StatusNoContent)
}
