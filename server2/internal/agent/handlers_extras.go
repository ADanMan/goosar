// handlers_extras.go — три небольших связанных группы маршрутов агента,
// собранные в один файл вместо трёх отдельных: переменные окружения
// (AgentEnv, contract §10.3), общие MCP-серверы воркспейса в контексте
// агента (AgentMcpServers, §10.5) и ресурсные метки агента (§3). Каждая
// группа — самостоятельный набор обработчиков, не разделяющих между собой
// ничего, кроме общего пролога d.resolveWorkspace/d.loadAgent.
package agent

import (
	"errors"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- переменные окружения (AgentEnv) ----------------------------------------

func (d *Deps) handleGetAgentEnv(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "no rights to view this agent's environment variables")
		return
	}
	reveal := vc.canSeeSecrets(a)
	var values map[string]string
	if reveal {
		var okRead bool
		values, okRead = d.Store.CustomEnvValues(a)
		if !okRead {
			values = map[string]string{}
		}
	} else {
		keys, _ := d.Store.CustomEnvKeys(a)
		values = map[string]string{}
		for _, k := range keys {
			values[k] = "****"
		}
	}
	if err := d.Store.AuditEnvRead(r.Context(), wsID, vc.ViewerID, a.ID, !reveal); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "could not record audit log", "audit_log_failed")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"agent_id": a.ID, "custom_env": values, "values_masked": !reveal,
	})
}

func (d *Deps) handleUpdateAgentEnv(w http.ResponseWriter, r *http.Request) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	if !vc.canManage(a) {
		httpapi.Forbidden(w, "no rights to change this agent's environment variables")
		return
	}
	var req struct {
		CustomEnv map[string]string `json:"custom_env"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	for k, v := range req.CustomEnv {
		if BrokenMaskValue(v) {
			httpapi.BadRequest(w, "value for "+k+" looks like a corrupted mask marker")
			return
		}
	}

	isOwner := vc.isOwner(a)
	if !isOwner {
		for _, v := range req.CustomEnv {
			if v != "****" {
				_ = d.Store.AuditEnvUpdateRefused(r.Context(), wsID, vc.ViewerID, a.ID)
				httpapi.Forbidden(w, "only the agent owner can set real environment values")
				return
			}
		}
	}

	current, _ := d.Store.CustomEnvValues(a)
	if current == nil {
		current = map[string]string{}
	}
	final, diff, err := d.Store.ApplyCustomEnv(r.Context(), wsID, a.ID, current, req.CustomEnv)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.AuditEnvUpdate(r.Context(), wsID, vc.ViewerID, a.ID, diff); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "could not record audit log", "audit_log_failed")
		return
	}

	reveal := isOwner || vc.canSeeSecrets(a)
	out := final
	if !reveal {
		out = map[string]string{}
		for k := range final {
			out[k] = "****"
		}
	}
	d.notify(wsID, "agent:status", map[string]any{"agent_id": a.ID})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"agent_id": a.ID, "custom_env": out, "values_masked": !reveal,
	})
}

// --- MCP-серверы воркспейса в контексте агента (AgentMcpServers) ------------

func (d *Deps) handleListAgentMcpServers(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	servers, err := d.Store.ListAgentMcpServers(r.Context(), a.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, servers)
}

// requireWorkspaceAdmin — POST/PUT/DELETE .../mcp-servers требуют owner/admin
// воркспейса, не владельца агента (contract §10.5: "не сами агенты и не
// рядовые участники").
func (d *Deps) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request) (string, Agent, bool) {
	wsID, vc, ok := d.resolveWorkspace(w, r)
	if !ok {
		return "", Agent{}, false
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return "", Agent{}, false
	}
	if !vc.IsHuman || !vc.isOwnerOrAdmin() {
		httpapi.Forbidden(w, "only the workspace owner/admin can manage agent MCP servers")
		return "", Agent{}, false
	}
	return wsID, a, true
}

func (d *Deps) handleAddAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	wsID, a, ok := d.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		ServerID string `json:"server_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.ServerID == "" {
		httpapi.BadRequest(w, "server_id is required")
		return
	}
	servers, err := d.Store.AddAgentMcpServer(r.Context(), wsID, a.ID, req.ServerID)
	if errors.Is(err, ErrMcpServerNotFound) {
		httpapi.NotFound(w, "mcp server not found in this workspace")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, servers)
}

func (d *Deps) handleSetAgentMcpServerEnabled(w http.ResponseWriter, r *http.Request) {
	_, a, ok := d.requireWorkspaceAdmin(w, r)
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
	servers, err := d.Store.SetAgentMcpServerEnabled(r.Context(), a.ID, r.PathValue("serverId"), req.Enabled)
	if errors.Is(err, ErrMcpServerNotAttached) {
		httpapi.NotFound(w, "this mcp server is not attached to the agent")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, servers)
}

func (d *Deps) handleRemoveAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	_, a, ok := d.requireWorkspaceAdmin(w, r)
	if !ok {
		return
	}
	servers, err := d.Store.RemoveAgentMcpServer(r.Context(), a.ID, r.PathValue("serverId"))
	if errors.Is(err, ErrMcpServerNotAttached) {
		httpapi.NotFound(w, "this mcp server is not attached to the agent")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, servers)
}

// --- ресурсные метки агента --------------------------------------------------

func (d *Deps) resourceLabelsAllowed(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	enabled, err := d.Store.ResourceLabelsEnabled(r.Context(), workspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return false
	}
	if !enabled {
		httpapi.WriteError(w, http.StatusNotFound, "resource labels are not enabled for this workspace", "resource_labels_disabled")
		return false
	}
	return true
}

func (d *Deps) handleListAgentLabels(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, wsID) {
		return
	}
	a, ok := d.loadAgent(w, r, wsID)
	if !ok {
		return
	}
	labels, err := d.Store.ListAgentLabels(r.Context(), a.ID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

func (d *Deps) handleAttachAgentLabel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, wsID) {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	var req struct {
		LabelID string `json:"label_id"`
	}
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	labels, err := d.Store.AttachAgentLabel(r.Context(), wsID, a.ID, req.LabelID)
	if errors.Is(err, ErrLabelNotFound) {
		httpapi.NotFound(w, "agent label not found in this workspace")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "label:updated", map[string]any{"label_id": req.LabelID})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

func (d *Deps) handleDetachAgentLabel(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	if !d.resourceLabelsAllowed(w, r, wsID) {
		return
	}
	a, ok := d.requireManage(w, r, wsID)
	if !ok {
		return
	}
	labelID := r.PathValue("labelId")
	labels, err := d.Store.DetachAgentLabel(r.Context(), a.ID, labelID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.notify(wsID, "label:updated", map[string]any{"label_id": labelID, "resource_type": "agent"})
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"labels": labels})
}
