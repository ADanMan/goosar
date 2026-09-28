package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// handleListSlackInstallations — GET /api/workspaces/{id}/slack/installations.
func (d *Deps) handleListSlackInstallations(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin, httpapi.RoleMember)
	if !ok {
		return
	}
	if d.Cfg.SlackSecretKey == "" {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{
			"installations": []any{}, "configured": false, "install_supported": false,
		})
		return
	}
	rows, err := d.Store.ListSlackInstallations(r.Context(), c.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	installations := make([]map[string]any, 0, len(rows))
	for _, si := range rows {
		installations = append(installations, slackInstallationJSON(si))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"installations": installations, "configured": true, "install_supported": true,
	})
}

func slackInstallationJSON(si SlackInstallation) map[string]any {
	return map[string]any{
		"id": si.ID, "workspace_id": si.WorkspaceID, "agent_id": si.AgentID,
		"team_id": si.TeamID, "bot_user_id": si.BotUserID, "installer_user_id": si.InstallerUserID,
		"status": si.Status, "installed_at": si.InstalledAt, "created_at": si.CreatedAt, "updated_at": si.UpdatedAt,
	}
}

// handleDeleteSlackInstallation — DELETE
// /api/workspaces/{id}/slack/installations/{installationId}.
func (d *Deps) handleDeleteSlackInstallation(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	if d.Cfg.SlackSecretKey == "" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "Slack-интеграция не настроена", "slack_not_configured")
		return
	}
	id := r.PathValue("installationId")
	affected, err := d.Store.DeleteSlackInstallation(r.Context(), c.WorkspaceID, id)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if affected == 0 {
		httpapi.NotFound(w, "installation not found")
		return
	}
	d.publish(c.WorkspaceID, "slack.installation.revoked", map[string]any{"installation_id": id})
	w.WriteHeader(http.StatusNoContent)
}

type registerSlackBotRequest struct {
	BotToken string `json:"bot_token"`
	AppToken string `json:"app_token"`
}

// handleRegisterSlackBotBYO — POST /api/workspaces/{id}/slack/install/byo.
func (d *Deps) handleRegisterSlackBotBYO(w http.ResponseWriter, r *http.Request) {
	c, ok := d.requireRole(w, r, httpapi.RoleOwner, httpapi.RoleAdmin)
	if !ok {
		return
	}
	if d.Cfg.SlackSecretKey == "" {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "Slack-интеграция не настроена", "slack_not_configured")
		return
	}
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		httpapi.BadRequest(w, "agent_id query parameter is required")
		return
	}
	var req registerSlackBotRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.BotToken == "" || req.AppToken == "" {
		httpapi.BadRequest(w, "bot_token and app_token are required")
		return
	}
	agentExists, err := d.Store.db.RowExists(r.Context(), `SELECT EXISTS(SELECT 1 FROM operatives WHERE id = $1 AND workspace_id = $2)`, agentID, c.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !agentExists {
		httpapi.WriteError(w, http.StatusNotFound, "агент не найден в этом пространстве", "not_found")
		return
	}
	teamID, botUserID, err := d.Slack.AuthTest(r.Context(), req.BotToken)
	if err != nil {
		httpapi.BadRequest(w, "bot_token/app_token невалидны или не совпадают друг с другом")
		return
	}
	if err := d.Slack.ValidateAppToken(r.Context(), req.AppToken); err != nil {
		httpapi.BadRequest(w, "bot_token/app_token невалидны или не совпадают друг с другом")
		return
	}
	conflict, err := d.Store.SlackTeamConflict(r.Context(), c.WorkspaceID, agentID, teamID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	switch conflict {
	case TeamConflictOtherAgent:
		httpapi.WriteError(w, http.StatusConflict, "эта Slack-команда уже привязана к другому агенту в этом пространстве", "slack_team_bound_other_agent")
		return
	case TeamConflictArchivedAgent:
		httpapi.WriteError(w, http.StatusConflict, "эта Slack-команда уже привязана к архивному агенту", "slack_team_bound_archived_agent")
		return
	case TeamConflictOtherWorkspace:
		httpapi.WriteError(w, http.StatusConflict, "эта Slack-команда уже привязана в другом пространстве", "slack_team_bound_other_workspace")
		return
	}
	botTokenSealed, err := seal(d.Cfg.SlackSecretKey, []byte(req.BotToken))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	appTokenSealed, err := seal(d.Cfg.SlackSecretKey, []byte(req.AppToken))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	var botUserIDPtr *string
	if botUserID != "" {
		botUserIDPtr = &botUserID
	}
	installation, err := d.Store.UpsertSlackInstallation(r.Context(), c.WorkspaceID, agentID, teamID, botUserIDPtr,
		botTokenSealed, appTokenSealed, c.Actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(c.WorkspaceID, "slack.installation.created", map[string]any{"installation": slackInstallationJSON(installation)})
	httpapi.WriteJSON(w, http.StatusOK, slackInstallationJSON(installation))
}

type redeemSlackBindingRequest struct {
	Token string `json:"token"`
}

// handleRedeemSlackBinding — POST /api/slack/binding/redeem.
func (d *Deps) handleRedeemSlackBinding(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if d.Cfg.SlackSecretKey == "" {
		// docs/50-api-contract-changes.md п.6: та же форма, что у остальных
		// slack/*-ручек — 503, когда интеграция не настроена на деплое.
		httpapi.WriteError(w, http.StatusServiceUnavailable, "Slack-интеграция не настроена", "slack_not_configured")
		return
	}
	var req redeemSlackBindingRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.Token == "" {
		httpapi.BadRequest(w, "token is required")
		return
	}
	hash := sha256.Sum256([]byte(req.Token))
	binding, found, err := d.Store.RedeemSlackBindingToken(r.Context(), hex.EncodeToString(hash[:]))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.WriteError(w, http.StatusGone, "токен недействителен или истёк", "binding_token_invalid")
		return
	}
	if _, isMember := d.memberRole(r.Context(), binding.WorkspaceID, actor.UserID); !isMember {
		httpapi.WriteError(w, http.StatusForbidden, "вызывающий не участник пространства установки", "not_a_member")
		return
	}
	boundTo, alreadyBound, berr := d.Store.SlackAccountBoundTo(r.Context(), binding.InstallationID, binding.SlackUserID)
	if berr != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if alreadyBound && boundTo != actor.UserID {
		httpapi.WriteError(w, http.StatusConflict, "этот Slack-аккаунт уже привязан к другому пользователю", "slack_account_bound")
		return
	}
	if err := d.Store.BindSlackAccount(r.Context(), binding.InstallationID, binding.SlackUserID, actor.UserID); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"workspace_id": binding.WorkspaceID, "installation_id": binding.InstallationID, "slack_user_id": binding.SlackUserID,
	})
}
