package integration

import (
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

type composioConnectInitRequest struct {
	ToolkitSlug string `json:"toolkit_slug"`
}

// handleComposioConnectInit — POST /api/integrations/composio/connect/init.
func (d *Deps) handleComposioConnectInit(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if !d.Composio.Configured() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "Composio-интеграция или её MCP-приложения не включены", "composio_not_configured")
		return
	}
	var req composioConnectInitRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.ToolkitSlug == "" {
		httpapi.BadRequest(w, "toolkit_slug is required")
		return
	}
	// Composio-подключение не привязано к конкретному воркспейсу в
	// контракте (x-roles: any-authenticated, без {id} в пути) — решение
	// T-029: строка ведётся per-account, workspace_id берётся из заголовка
	// контекста воркспейса, если он передан, иначе пусто (не критично для
	// самого OAuth-потока, только для группировки в листинге по воркспейсу).
	workspaceID, _, _ := httpapi.ResolveWorkspaceRef(r, actor)
	state, err := signState(d.stateSecret(), map[string]any{
		"account_id": actor.UserID, "workspace_id": workspaceID, "toolkit_slug": req.ToolkitSlug,
	}, 15*time.Minute)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	redirectURI := d.publicURL(r) + "/api/integrations/composio/callback"
	redirectURL, err := d.Composio.ConnectInit(r.Context(), req.ToolkitSlug, redirectURI, state)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "сбой на стороне Composio", "composio_upstream_error")
		return
	}
	if _, err := d.Store.UpsertPendingComposioConnection(r.Context(), workspaceID, actor.UserID, req.ToolkitSlug); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"redirect_url": redirectURL})
}

// handleComposioCallback — GET /api/integrations/composio/callback (public).
func (d *Deps) handleComposioCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	payload, err := verifyState(d.stateSecret(), state)
	target := d.Cfg.FrontendOrigin + "/settings/integrations"
	if err != nil {
		http.Redirect(w, r, target+"?composio_error=invalid_state", http.StatusFound)
		return
	}
	toolkitSlug, _ := payload["toolkit_slug"].(string)
	status := r.URL.Query().Get("status")
	if status != "success" && status != "" {
		http.Redirect(w, r, target+"?composio_toolkit="+toolkitSlug+"&composio_error="+status, http.StatusFound)
		return
	}
	connectedAccountID := r.URL.Query().Get("connected_account_id")
	accountID, _ := payload["account_id"].(string)
	workspaceID, _ := payload["workspace_id"].(string)
	if accountID != "" && toolkitSlug != "" {
		conn, err := d.Store.UpsertPendingComposioConnection(r.Context(), workspaceID, accountID, toolkitSlug)
		if err == nil {
			_ = conn
			if err := d.markComposioConnected(r, conn.ID, connectedAccountID); err != nil {
				d.Logger.Warn("integration: не удалось завершить composio-подключение", "err", err)
			}
		}
	}
	http.Redirect(w, r, target+"?composio_toolkit="+toolkitSlug+"&composio_connected=1", http.StatusFound)
}

func (d *Deps) markComposioConnected(r *http.Request, connectionID, externalID string) error {
	_, err := d.Store.db.Pool.Exec(r.Context(), `
		UPDATE composio_connections SET cx_status = 'active', cx_external_connection_id = $2, cx_connected_at = now()
		WHERE id = $1`, connectionID, externalID)
	return err
}

// handleListComposioToolkits — GET /api/integrations/composio/toolkits.
func (d *Deps) handleListComposioToolkits(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpapi.RequireActor(w, r); !ok {
		return
	}
	if !d.Composio.Configured() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "интеграция не включена", "composio_not_configured")
		return
	}
	items, err := d.Composio.ListToolkits(r.Context())
	if err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "сбой Composio", "composio_upstream_error")
		return
	}
	if items == nil {
		items = []composioToolkit{}
	}
	httpapi.WriteJSON(w, http.StatusOK, items)
}

// handleListComposioConnections — GET /api/integrations/composio/connections.
func (d *Deps) handleListComposioConnections(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	rows, err := d.Store.ListComposioConnections(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		out = append(out, map[string]any{
			"id": c.ID, "toolkit_slug": c.ToolkitSlug, "status": c.Status,
			"connected_at": c.ConnectedAt, "last_used_at": c.LastUsedAt,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// handleDeleteComposioConnection — DELETE /api/integrations/composio/connections/{id}.
func (d *Deps) handleDeleteComposioConnection(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	rows, err := d.Store.ListComposioConnections(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	var external string
	found := false
	for _, c := range rows {
		if c.ID == id {
			found = true
			external = c.ExternalID
			break
		}
	}
	if !found {
		httpapi.NotFound(w, "composio connection not found")
		return
	}
	if err := d.Composio.Disconnect(r.Context(), external); err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "сбой Composio", "composio_upstream_error")
		return
	}
	if _, err := d.Store.DeleteComposioConnection(r.Context(), actor.UserID, id); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
