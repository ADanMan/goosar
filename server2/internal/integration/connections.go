package integration

// connections.go — обработчики source-control-подключений воркспейса: GitHub
// App (installations/connect/setup-callback/repositories) и self-hosted VCS
// (GitLab/Gitea/подобные, connections/rotate-webhook) — оба варианта одного
// и того же раздела контракта «§1 Рабочие пространства, интеграции», держатся
// рядом, потому что делят одну и ту же роль-модель (owner/admin управляют
// подключением, member только читает список).

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleListGitHubInstallations — GET /api/workspaces/{id}/github/installations.
func (d *Deps) handleListGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin, httpapi.RoleMember)
	if !ok {
		return
	}
	rows, err := d.Store.ListGitHubInstallations(r.Context(), c.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	canManage := httpapi.RoleAtLeast(c.Role, httpapi.RoleOwner, httpapi.RoleAdmin)
	installations := make([]map[string]any, 0, len(rows))
	for _, g := range rows {
		installations = append(installations, githubInstallationJSON(g, canManage))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"installations":                installations,
		"configured":                   d.GitHub.Configured(),
		"repository_browse_configured": d.GitHub.Configured(),
		"can_manage":                   canManage,
	})
}

func githubInstallationJSON(g GitHubInstallation, canManage bool) map[string]any {
	var installationID any
	if canManage && g.InstallationID != nil {
		installationID = *g.InstallationID
	}
	return map[string]any{
		"id":                 g.ID,
		"workspace_id":       g.WorkspaceID,
		"installation_id":    installationID,
		"account_login":      g.AccountLogin,
		"account_type":       g.AccountType,
		"account_avatar_url": g.AccountAvatar,
		"created_at":         g.CreatedAt,
	}
}

// handleGetGitHubConnectURL — GET /api/workspaces/{id}/github/connect.
func (d *Deps) handleGetGitHubConnectURL(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "github"
	}
	if returnTo != "github" && returnTo != "repositories" {
		httpapi.BadRequest(w, "return_to must be github or repositories")
		return
	}
	if d.Cfg.GitHubAppSlug == "" {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"url": "", "configured": false})
		return
	}
	state, err := signState(d.stateSecret(), map[string]any{
		"workspace_id": c.WorkspaceID, "return_to": returnTo,
	}, 15*time.Minute)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	installURL := "https://github.com/apps/" + d.Cfg.GitHubAppSlug + "/installations/new?state=" + url.QueryEscape(state)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"url": installURL, "configured": true})
}

func (d *Deps) stateSecret() string {
	if d.Cfg.JWTSecret != "" {
		return d.Cfg.JWTSecret
	}
	return "dev-insecure-secret-change-me"
}

// handleGitHubSetupCallback — GET /api/github/setup (public, подписанный state).
func (d *Deps) handleGitHubSetupCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	payload, err := verifyState(d.stateSecret(), state)
	if err != nil {
		d.redirectSettings(w, r, "", "invalid_state")
		return
	}
	workspaceID, _ := payload["workspace_id"].(string)
	returnTo, _ := payload["return_to"].(string)

	var ghInstallationID *int64
	if raw := r.URL.Query().Get("installation_id"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ghInstallationID = &n
		}
	}

	accountLogin, accountType, avatar := "unknown", "Organization", (*string)(nil)
	if ghInstallationID != nil && d.GitHub.Configured() {
		if acc, err := d.GitHub.InstallationAccount(r.Context(), *ghInstallationID); err == nil {
			accountLogin, accountType, avatar = acc.Login, acc.Type, acc.Avatar
		}
	}

	installation, err := d.Store.UpsertGitHubInstallation(r.Context(), workspaceID, ghInstallationID, accountLogin, accountType, avatar)
	if err != nil {
		d.redirectSettings(w, r, returnTo, "internal_error")
		return
	}
	d.publish(workspaceID, "github_installation:created", map[string]any{"installation": githubInstallationJSON(installation, true)})
	d.redirectSettings(w, r, returnTo, "")
}

func (d *Deps) redirectSettings(w http.ResponseWriter, r *http.Request, returnTo, errCode string) {
	target := d.Cfg.FrontendOrigin + "/settings/integrations"
	if errCode != "" {
		target += "?github_error=" + url.QueryEscape(errCode)
	} else {
		target += "?github_connected=1"
		if returnTo != "" {
			target += "&return_to=" + url.QueryEscape(returnTo)
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleListGitHubInstallationRepositories — GET
// /api/workspaces/{id}/github/installations/{installationId}/repositories.
func (d *Deps) handleListGitHubInstallationRepositories(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	installation, found, err := d.Store.GitHubInstallationByID(r.Context(), c.WorkspaceID, r.PathValue("installationId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusNotFound, "установка не найдена в этом пространстве", "not_found")
		return
	}
	if !d.GitHub.Configured() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "просмотр репозиториев не настроен на деплое", "github_not_configured")
		return
	}
	if installation.InstallationID == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "просмотр репозиториев не настроен на деплое", "github_not_configured")
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 100)
	repos, total, err := d.GitHub.ListRepositories(r.Context(), *installation.InstallationID, page, perPage)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadGateway, "сбой при обращении к GitHub API", "github_upstream_error")
		return
	}
	var nextPage any
	if len(repos) == perPage {
		nextPage = page + 1
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
		"total_count":  total,
		"next_page":    nextPage,
	})
}

// handleDeleteGitHubInstallation — DELETE
// /api/workspaces/{id}/github/installations/{installationId}.
func (d *Deps) handleDeleteGitHubInstallation(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	affected, err := d.Store.DeleteGitHubInstallation(r.Context(), c.WorkspaceID, r.PathValue("installationId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if affected > 0 {
		d.publish(c.WorkspaceID, "github_installation:deleted", map[string]any{"installation_id": r.PathValue("installationId")})
	}
	w.WriteHeader(http.StatusNoContent)
}

func queryInt(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	return n
}

// --- self-hosted VCS (GitLab/Bitbucket/etc, distinct from the GitHub App flow above) ---

// handleListVCSConnections — GET /api/workspaces/{id}/vcs/connections.
func (d *Deps) handleListVCSConnections(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin, httpapi.RoleMember)
	if !ok {
		return
	}
	if !d.vcsAvailable() {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"connections": []any{}, "available": false, "configured": false,
			"can_manage": httpapi.RoleAtLeast(c.Role, httpapi.RoleOwner, httpapi.RoleAdmin),
		})
		return
	}
	rows, err := d.Store.ListVCSConnections(r.Context(), c.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	conns := make([]map[string]any, 0, len(rows))
	for _, cc := range rows {
		conns = append(conns, vcsConnectionJSON(cc))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"connections": conns, "available": true, "configured": true,
		"can_manage": httpapi.RoleAtLeast(c.Role, httpapi.RoleOwner, httpapi.RoleAdmin),
	})
}

func (d *Deps) vcsAvailable() bool {
	return d.Cfg.VCSIntegrationEnabled && d.Cfg.VCSSecretKey != ""
}

func vcsConnectionJSON(c VCSConnection) map[string]any {
	return map[string]any{
		"id": c.ID, "workspace_id": c.WorkspaceID, "provider": c.Provider,
		"instance_url": c.InstanceURL, "account_login": c.AccountLogin,
		"webhook_url": c.WebhookURL, "webhook_path": c.WebhookPath, "created_at": c.CreatedAt,
	}
}

type connectVCSRequest struct {
	Provider    string `json:"provider"`
	InstanceURL string `json:"instance_url"`
	AccessToken string `json:"access_token"`
}

// handleConnectVCS — POST /api/workspaces/{id}/vcs/connections.
func (d *Deps) handleConnectVCS(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	if !d.Cfg.VCSIntegrationEnabled {
		httpapi.WriteError(w, http.StatusNotFound, "VCS-интеграция выключена на этом деплое", "vcs_disabled")
		return
	}
	if !d.vcsAvailable() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "VCS-интеграция не настроена на деплое (нет ключа шифрования)", "vcs_not_configured")
		return
	}
	var req connectVCSRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	instanceURL, ok2 := normalizeAbsoluteURL(req.InstanceURL)
	if !ok2 || req.Provider == "" || req.AccessToken == "" {
		httpapi.BadRequest(w, "provider, instance_url (absolute http/https) and access_token are required")
		return
	}
	accountLogin, err := d.VCS.ValidateToken(r.Context(), req.Provider, instanceURL, req.AccessToken)
	if errors.Is(err, errUnsupportedProvider) {
		httpapi.BadRequest(w, "unsupported VCS provider")
		return
	}
	if err != nil {
		if isAuthRejection(err) {
			httpapi.BadRequest(w, "provider rejected the access token")
			return
		}
		httpapi.WriteError(w, http.StatusBadGateway, "инстанс провайдера недоступен", "vcs_upstream_error")
		return
	}
	tokenSealed, err := seal(d.Cfg.VCSSecretKey, []byte(req.AccessToken))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	webhookSecret := randomHex(32)
	webhookSecretSealed, err := seal(d.Cfg.VCSSecretKey, []byte(webhookSecret))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	var loginPtr *string
	if accountLogin != "" {
		loginPtr = &accountLogin
	}
	conn, err := d.Store.UpsertVCSConnection(r.Context(), c.WorkspaceID, req.Provider, instanceURL, loginPtr,
		tokenSealed, webhookSecretSealed, "", "")
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	webhookPath := "/api/webhooks/vcs/" + conn.ID
	webhookURL := d.publicURL(r) + webhookPath
	if err := d.Store.SetVCSWebhookURL(r.Context(), conn.ID, webhookURL, webhookPath); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	conn.WebhookURL, conn.WebhookPath = &webhookURL, &webhookPath

	resp := vcsConnectionJSON(conn)
	resp["webhook_secret"] = webhookSecret
	d.publish(c.WorkspaceID, "vcs.connection.created", map[string]any{"connection": vcsConnectionJSON(conn)})
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

// handleDeleteVCSConnection — DELETE /api/workspaces/{id}/vcs/connections/{connectionId}.
func (d *Deps) handleDeleteVCSConnection(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	id := r.PathValue("connectionId")
	affected, err := d.Store.DeleteVCSConnection(r.Context(), c.WorkspaceID, id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if affected > 0 {
		d.publish(c.WorkspaceID, "vcs.connection.deleted", map[string]any{"connection_id": id})
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRotateVCSWebhook — POST
// /api/workspaces/{id}/vcs/connections/{connectionId}/rotate-webhook.
func (d *Deps) handleRotateVCSWebhook(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	if !d.vcsAvailable() {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "VCS-интеграция не настроена", "vcs_not_configured")
		return
	}
	newSecret := randomHex(32)
	sealed, err := seal(d.Cfg.VCSSecretKey, []byte(newSecret))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	conn, found, err := d.Store.RotateVCSWebhookSecret(r.Context(), c.WorkspaceID, r.PathValue("connectionId"), sealed)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusNotFound, "подключение не найдено в этом пространстве", "not_found")
		return
	}
	resp := vcsConnectionJSON(conn)
	resp["webhook_secret"] = newSecret
	d.publish(c.WorkspaceID, "vcs.connection.created", map[string]any{"connection": vcsConnectionJSON(conn)})
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (d *Deps) publicURL(r *http.Request) string {
	if d.Cfg.PublicURL != "" {
		return d.Cfg.PublicURL
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func normalizeAbsoluteURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), true
}

// isAuthRejection — err пришёл из doUsernameRequest на 401/403 у провайдера
// (contract: "ошибка аутентификации на стороне VCS-провайдера → 400, сетевой
// сбой → 502"), отличается от сетевой ошибки обёрткой errProviderRejected.
func isAuthRejection(err error) bool {
	return errors.Is(err, errProviderRejected)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
