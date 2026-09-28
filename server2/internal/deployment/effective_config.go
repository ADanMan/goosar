package deployment

import (
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// buildEffectiveConfig — наложение слоёв "политика деплоя → конфиг
// пространства → персональный override" (contract §13 "getEffectiveConfigView").
// Порядок приоритета сверху вниз: значение политики с locked=true побеждает
// всегда (origin=policy, locked=true); иначе персональный override, если
// задан (origin=user); иначе конфиг пространства (origin=workspace); иначе
// значение политики без locked, если оно вообще задано (origin=policy);
// иначе пусто (origin=default).
func (d *Deps) buildEffectiveConfig(r *http.Request, workspaceID, userID string) (EffectiveConfigView, error) {
	policy, err := d.readPolicy(r)
	if err != nil {
		return EffectiveConfigView{}, err
	}
	wsConfig, err := d.readConfigLayerRaw(r.Context(), workspaceID)
	if err != nil {
		return EffectiveConfigView{}, err
	}
	override, hasOverride, err := d.readConfigOverrideRaw(r.Context(), workspaceID, userID)
	if err != nil {
		return EffectiveConfigView{}, err
	}

	view := EffectiveConfigView{SchemaVersion: 1, MCP: map[string]EffectiveMcpEntry{}, RevokedPackages: []string{}}
	view.LLM = resolveEffectiveLLM(policy, wsConfig, override, hasOverride)
	view.MCP = resolveEffectiveMCP(policy, wsConfig, override, hasOverride)
	return view, nil
}

func resolveEffectiveLLM(policy PolicyDocument, ws rawConfigLayer, ov rawConfigLayer, hasOverride bool) EffectiveLLM {
	policyLLM, _ := policy.Policy.LLM["base_url"].(string)
	policyModel, _ := policy.Policy.LLM["model"].(string)
	policyLocked, _ := policy.Policy.LLM["locked"].(bool)

	if policyLocked {
		return EffectiveLLM{BaseURL: policyLLM, Model: policyModel, Origin: "policy", Locked: true}
	}
	if hasOverride && (ov.baseURL != nil || ov.model != nil || len(ov.keySealed) > 0) {
		out := EffectiveLLM{Origin: "user", HasAPIKey: len(ov.keySealed) > 0}
		if ov.baseURL != nil {
			out.BaseURL = *ov.baseURL
		}
		if ov.model != nil {
			out.Model = *ov.model
		}
		return out
	}
	if ws.baseURL != nil || ws.model != nil || len(ws.keySealed) > 0 {
		out := EffectiveLLM{Origin: "workspace", HasAPIKey: len(ws.keySealed) > 0}
		if ws.baseURL != nil {
			out.BaseURL = *ws.baseURL
		}
		if ws.model != nil {
			out.Model = *ws.model
		}
		return out
	}
	if policyLLM != "" || policyModel != "" {
		return EffectiveLLM{BaseURL: policyLLM, Model: policyModel, Origin: "policy"}
	}
	return EffectiveLLM{Origin: "default"}
}

func resolveEffectiveMCP(policy PolicyDocument, ws rawConfigLayer, ov rawConfigLayer, hasOverride bool) map[string]EffectiveMcpEntry {
	out := map[string]EffectiveMcpEntry{}
	names := map[string]bool{}
	for name := range ws.mcp {
		names[name] = true
	}
	if hasOverride {
		for name := range ov.mcp {
			names[name] = true
		}
	}
	for name := range policy.Policy.MCP {
		if name != "*" {
			names[name] = true
		}
	}
	globalPolicy, hasGlobalPolicy := policy.Policy.MCP["*"]

	for name := range names {
		entry := EffectiveMcpEntry{Origin: "default"}
		if wsEntry, has := asMap(ws.mcp[name]); has {
			if enabled, ok := wsEntry["enabled"].(bool); ok {
				entry.Enabled, entry.Origin = enabled, "workspace"
			}
			if env, ok := wsEntry["env"].(map[string]any); ok {
				entry.HasEnv = len(env) > 0
				for k := range env {
					entry.EnvKeys = append(entry.EnvKeys, k)
				}
			}
		}
		if hasOverride {
			if ovEntry, has := asMap(ov.mcp[name]); has {
				if enabled, ok := ovEntry["enabled"].(bool); ok {
					entry.Enabled, entry.Origin = enabled, "user"
				}
				if env, ok := ovEntry["env"].(map[string]any); ok {
					entry.HasEnv = entry.HasEnv || len(env) > 0
					for k := range env {
						entry.EnvKeys = append(entry.EnvKeys, k)
					}
				}
			}
		}
		if p, ok := asMapAny(policy.Policy.MCP[name]); ok {
			if enabled, ok := p["enabled"].(bool); ok {
				entry.Enabled, entry.Origin = enabled, "policy"
			}
			if locked, ok := p["locked"].(bool); ok && locked {
				entry.Locked = true
				entry.Origin = "policy"
			}
		}
		if hasGlobalPolicy {
			if enabled, ok := globalPolicy["enabled"].(bool); ok && !enabled {
				entry.Enabled, entry.Locked, entry.Origin = false, true, "policy"
			}
		}
		out[name] = entry
	}
	return out
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asMapAny(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func (d *Deps) handleGetEffectiveConfig(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	if !d.isWorkspaceMember(r, workspaceID, actor.UserID) {
		httpapi.Forbidden(w, "not a member of this workspace")
		return
	}
	view, err := d.buildEffectiveConfig(r, workspaceID, actor.UserID)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
}
