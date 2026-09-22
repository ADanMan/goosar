package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/featureflags"
	composio "github.com/adanman/goosar/server/internal/integrations/composio"
)

type ComposioConnectInitRequest struct {
	ToolkitSlug string `json:"toolkit_slug"`
}

type ComposioConnectInitResponse struct {
	RedirectURL string `json:"redirect_url"`
}

type ComposioConnectionResponse struct {
	ID          string  `json:"id"`
	ToolkitSlug string  `json:"toolkit_slug"`
	Status      string  `json:"status"`
	ConnectedAt string  `json:"connected_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

type ComposioToolkitResponse struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Logo        string `json:"logo,omitempty"`
	Category    string `json:"category,omitempty"`
	Connectable bool   `json:"connectable"`
}

func (h *Handler) composioMCPAppsEnabled(ctx context.Context) bool {
	return featureflags.ComposioMCPAppsEnabled(ctx, h.FeatureFlags)
}

func (h *Handler) ComposioConnectInit(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req ComposioConnectInitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ToolkitSlug) == "" {
		writeError(w, http.StatusBadRequest, "toolkit_slug is required")
		return
	}

	redirectURL, err := h.Composio.BeginConnect(r.Context(), userUUID, req.ToolkitSlug)
	if err != nil {
		if errors.Is(err, composio.ErrToolkitNotSupported) {
			writeError(w, http.StatusBadRequest, "toolkit not supported")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to start composio connect")
		return
	}
	writeJSON(w, http.StatusOK, ComposioConnectInitResponse{RedirectURL: redirectURL})
}

func (h *Handler) ComposioCallback(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	status := q.Get("status")
	connectedAccountID := q.Get("connected_account_id")

	slug, err := h.Composio.CompleteCallback(r.Context(), state, status, connectedAccountID)
	if err != nil {

		http.Redirect(w, r, h.Composio.CallbackRedirect(slug, false), http.StatusFound)
		return
	}
	http.Redirect(w, r, h.Composio.CallbackRedirect(slug, true), http.StatusFound)
}

func (h *Handler) ListComposioConnections(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	conns, err := h.Composio.ListConnections(r.Context(), userUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list composio connections")
		return
	}
	out := make([]ComposioConnectionResponse, 0, len(conns))
	for _, c := range conns {
		out = append(out, ComposioConnectionResponse{
			ID:          c.ID,
			ToolkitSlug: c.ToolkitSlug,
			Status:      c.Status,
			ConnectedAt: c.ConnectedAt,
			LastUsedAt:  c.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) ListComposioToolkits(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	toolkits, err := h.Composio.ListToolkits(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list composio toolkits")
		return
	}
	out := make([]ComposioToolkitResponse, 0, len(toolkits))
	for _, tk := range toolkits {
		out = append(out, ComposioToolkitResponse{
			Slug:        tk.Slug,
			Name:        tk.Name,
			Logo:        tk.LogoURL,
			Category:    tk.Category,
			Connectable: tk.Connectable,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteComposioConnection(w http.ResponseWriter, r *http.Request) {
	if h.Composio == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeError(w, http.StatusServiceUnavailable, "composio integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	connUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "connection id")
	if !ok {
		return
	}
	if err := h.Composio.Disconnect(r.Context(), userUUID, connUUID); err != nil {
		if errors.Is(err, composio.ErrConnectionNotFound) {
			writeError(w, http.StatusNotFound, "composio connection not found")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to disconnect composio connection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
