package handler

import (
	"log/slog"
	"net/http"
	"sort"
)

type EffectiveConfigViewLLM struct {
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	HasAPIKey bool   `json:"has_api_key"`
	Origin    string `json:"origin"`
	Locked    bool   `json:"locked"`
}

type EffectiveConfigViewMCP struct {
	Enabled bool `json:"enabled"`

	HasEnv bool `json:"has_env"`

	EnvKeys []string `json:"env_keys,omitempty"`
	Origin  string   `json:"origin"`
	Locked  bool     `json:"locked,omitempty"`
}

func maskEffectiveMCPEntry(entry EffectiveMCPServer) EffectiveConfigViewMCP {
	view := EffectiveConfigViewMCP{
		Enabled: entry.Enabled,
		HasEnv:  len(entry.Env) > 0,
		Origin:  entry.Origin,
		Locked:  entry.Locked,
	}
	if len(entry.Env) > 0 {
		view.EnvKeys = make([]string, 0, len(entry.Env))
		for name := range entry.Env {
			view.EnvKeys = append(view.EnvKeys, name)
		}
		sort.Strings(view.EnvKeys)
	}
	return view
}

type EffectiveConfigViewResponse struct {
	SchemaVersion   int                               `json:"schema_version"`
	LLM             *EffectiveConfigViewLLM           `json:"llm,omitempty"`
	MCP             map[string]EffectiveConfigViewMCP `json:"mcp,omitempty"`
	RevokedPackages []string                          `json:"revoked_packages"`
}

func (h *Handler) GetEffectiveConfigView(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	wsUUID, wsOK := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !wsOK {
		return
	}
	userUUID, userOK := parseUUIDOrBadRequest(w, userID, "user_id")
	if !userOK {
		return
	}

	eff, err := h.ResolveEffectiveConfig(r.Context(), wsUUID, userUUID)
	if err != nil {
		slog.Warn("resolve effective config view failed",
			"workspace_id", workspaceID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve effective configuration")
		return
	}

	view := EffectiveConfigViewResponse{
		SchemaVersion:   eff.SchemaVersion,
		RevokedPackages: eff.RevokedPackages,
	}
	if eff.LLM != nil {
		view.LLM = &EffectiveConfigViewLLM{
			BaseURL:   eff.LLM.BaseURL,
			Model:     eff.LLM.Model,
			HasAPIKey: eff.LLM.APIKey != "",
			Origin:    eff.LLM.Origin,
			Locked:    eff.LLM.Locked,
		}
	}
	if len(eff.MCP) > 0 {
		view.MCP = make(map[string]EffectiveConfigViewMCP, len(eff.MCP))
		for name, entry := range eff.MCP {
			view.MCP[name] = maskEffectiveMCPEntry(entry)
		}
	}
	writeJSON(w, http.StatusOK, view)
}
