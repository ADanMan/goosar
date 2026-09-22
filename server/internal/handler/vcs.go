package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/vcs"
	"github.com/adanman/goosar/server/internal/middleware"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type VCSConnectionResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Provider     string `json:"provider"`
	InstanceURL  string `json:"instance_url"`
	AccountLogin string `json:"account_login"`
	WebhookURL   string `json:"webhook_url"`
	WebhookPath  string `json:"webhook_path"`
	CreatedAt    string `json:"created_at"`
}

type VCSConnectResponse struct {
	VCSConnectionResponse
	WebhookSecret string `json:"webhook_secret"`
}

const vcsWebhookPathPrefix = "/api/webhooks/vcs/"

func (h *Handler) isVCSAvailable() bool { return h.cfg.VCSIntegrationEnabled }

func (h *Handler) isVCSConfigured() bool { return h.VCSSecretBox != nil }

func (h *Handler) vcsWebhookPath(connID string) string { return vcsWebhookPathPrefix + connID }

func (h *Handler) vcsWebhookURL(connID string) string {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		return ""
	}
	return base + h.vcsWebhookPath(connID)
}

func (h *Handler) vcsConnectionToResponse(c db.VcsConnection) VCSConnectionResponse {
	id := uuidToString(c.ID)
	return VCSConnectionResponse{
		ID:           id,
		WorkspaceID:  uuidToString(c.WorkspaceID),
		Provider:     c.Provider,
		InstanceURL:  c.InstanceUrl,
		AccountLogin: c.AccountLogin,
		WebhookURL:   h.vcsWebhookURL(id),
		WebhookPath:  h.vcsWebhookPath(id),
		CreatedAt:    timestampToString(c.CreatedAt),
	}
}

func (h *Handler) sealVCSSecret(plaintext string) (string, error) {
	sealed, err := h.VCSSecretBox.Seal([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openVCSSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plaintext, err := h.VCSSecretBox.Open(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (h *Handler) ListVCSConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	if !h.isVCSAvailable() {
		writeJSON(w, http.StatusOK, map[string]any{
			"connections": []VCSConnectionResponse{},
			"available":   false,
			"configured":  false,
			"can_manage":  false,
		})
		return
	}

	rows, err := h.Queries.ListVCSConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	out := make([]VCSConnectionResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.vcsConnectionToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connections": out,
		"available":   true,
		"configured":  h.isVCSConfigured(),
		"can_manage":  canManage,
	})
}

type connectVCSRequest struct {
	Provider    string `json:"provider"`
	InstanceURL string `json:"instance_url"`
	AccessToken string `json:"access_token"`
}

func (h *Handler) ConnectVCS(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.isVCSAvailable() {
		writeError(w, http.StatusNotFound, "vcs integration is not available on this deployment")
		return
	}
	if !h.isVCSConfigured() {
		writeError(w, http.StatusServiceUnavailable, "vcs integration not configured (GOOSAR_VCS_SECRET_KEY unset)")
		return
	}

	var req connectVCSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	provider, ok := vcs.For(req.Provider)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported provider")
		return
	}
	instanceURL := vcs.NormalizeInstanceURL(req.InstanceURL)
	token := strings.TrimSpace(req.AccessToken)
	if instanceURL == "" || token == "" {
		writeError(w, http.StatusBadRequest, "instance_url and access_token are required")
		return
	}
	parsed, err := url.Parse(instanceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		writeError(w, http.StatusBadRequest, "instance_url must be an absolute http(s) URL")
		return
	}

	account, err := provider.ValidateToken(r.Context(), instanceURL, token)
	if err != nil {
		if errors.Is(err, vcs.ErrUnauthorized) {
			writeError(w, http.StatusBadRequest, "the provider rejected the access token")
			return
		}
		writeError(w, http.StatusBadGateway, "could not reach the provider instance")
		return
	}

	webhookSecret, err := newVCSWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint webhook secret")
		return
	}
	tokenEnc, err := h.sealVCSSecret(token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}
	secretEnc, err := h.sealVCSSecret(webhookSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
		return
	}

	var connectedBy pgtype.UUID
	if member, ok := middleware.MemberFromContext(r.Context()); ok {
		connectedBy = member.UserID
	}

	conn, err := h.Queries.UpsertVCSConnection(r.Context(), db.UpsertVCSConnectionParams{
		WorkspaceID:            wsUUID,
		Provider:               string(provider.Kind()),
		InstanceUrl:            instanceURL,
		AccountLogin:           account.Login,
		AccessTokenEncrypted:   tokenEnc,
		WebhookSecretEncrypted: secretEnc,
		ConnectedByID:          connectedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save connection")
		return
	}

	resp := h.vcsConnectionToResponse(conn)
	h.publish(protocol.EventVCSConnectionCreated, workspaceID, "system", "", map[string]any{"id": resp.ID})
	writeJSON(w, http.StatusOK, VCSConnectResponse{
		VCSConnectionResponse: resp,
		WebhookSecret:         webhookSecret,
	})
}

func (h *Handler) DeleteVCSConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "connectionId"), "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteVCSConnection(r.Context(), db.DeleteVCSConnectionParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove connection")
		return
	}
	h.publish(protocol.EventVCSConnectionDeleted, workspaceID, "system", "", map[string]any{
		"id": chi.URLParam(r, "connectionId"),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RotateVCSConnectionWebhook(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connID := chi.URLParam(r, "connectionId")
	connUUID, ok := parseUUIDOrBadRequest(w, connID, "connection id")
	if !ok {
		return
	}
	if !h.isVCSAvailable() {
		writeError(w, http.StatusNotFound, "vcs integration is not available on this deployment")
		return
	}
	if !h.isVCSConfigured() {
		writeError(w, http.StatusServiceUnavailable, "vcs integration not configured (GOOSAR_VCS_SECRET_KEY unset)")
		return
	}

	conn, err := h.Queries.GetVCSConnectionByID(r.Context(), connUUID)
	if err != nil || uuidToString(conn.WorkspaceID) != uuidToString(wsUUID) {
		writeError(w, http.StatusNotFound, "vcs connection not found")
		return
	}

	webhookSecret, err := newVCSWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint webhook secret")
		return
	}
	secretEnc, err := h.sealVCSSecret(webhookSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt webhook secret")
		return
	}

	updated, err := h.Queries.RotateVCSConnectionWebhookSecret(r.Context(), db.RotateVCSConnectionWebhookSecretParams{
		ID:                     connUUID,
		WorkspaceID:            wsUUID,
		WebhookSecretEncrypted: secretEnc,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate webhook secret")
		return
	}

	resp := h.vcsConnectionToResponse(updated)
	h.publish(protocol.EventVCSConnectionCreated, workspaceID, "system", "", map[string]any{"id": resp.ID})
	writeJSON(w, http.StatusOK, VCSConnectResponse{
		VCSConnectionResponse: resp,
		WebhookSecret:         webhookSecret,
	})
}

func newVCSWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
