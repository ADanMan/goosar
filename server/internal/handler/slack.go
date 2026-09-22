package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/adanman/goosar/server/internal/integrations/slack"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type SlackInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	TeamID          string `json:"team_id"`
	BotUserID       string `json:"bot_user_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func slackInstallationToResponse(row db.ChannelInstallation) SlackInstallationResponse {
	info := slack.DecodePublicConfig(row.Config)
	return SlackInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		TeamID:          info.TeamID,
		BotUserID:       info.BotUserID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) ListSlackInstallations(w http.ResponseWriter, r *http.Request) {
	if h.SlackInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []SlackInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.SlackInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list slack installations")
		return
	}
	out := make([]SlackInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, slackInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

type RegisterSlackBYORequest struct {
	BotToken string `json:"bot_token"`
	AppToken string `json:"app_token"`
}

func (h *Handler) RegisterSlackBYO(w http.ResponseWriter, r *http.Request) {
	if h.SlackInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "slack integration not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	agentIDStr := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentIDStr == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, agentIDStr, "agent_id")
	if !ok {
		return
	}

	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	initiatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var body RegisterSlackBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.SlackInstall.RegisterBYO(r.Context(), slack.RegisterBYOParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		BotToken:    body.BotToken,
		AppToken:    body.AppToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, slack.ErrInvalidBotToken), errors.Is(err, slack.ErrInvalidAppToken), errors.Is(err, slack.ErrTokenAppMismatch):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, slack.ErrTeamOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this Slack app is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, slack.ErrTeamOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this Slack app is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, slack.ErrTeamOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this Slack app is already connected to a different Goosar workspace — disconnect it there before connecting it here")
		default:

			writeError(w, http.StatusBadRequest, "could not verify the Slack tokens — check the bot token and app-level token, that the app is installed to your workspace, and that it has the users:read scope")
		}
		return
	}

	h.publishSlackInstallationCreated(row, userID)
	writeJSON(w, http.StatusOK, slackInstallationToResponse(row))
}

func (h *Handler) publishSlackInstallationCreated(row db.ChannelInstallation, actorID string) {
	h.publish(protocol.EventSlackInstallationCreated, uuidToString(row.WorkspaceID), "user", actorID, map[string]any{
		"id": uuidToString(row.ID),
	})
}

func (h *Handler) RevokeSlackInstallation(w http.ResponseWriter, r *http.Request) {
	if h.SlackInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "slack integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	instUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}

	if _, err := h.SlackInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, slack.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "slack installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.SlackInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventSlackInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

type RedeemSlackBindingTokenRequest struct {
	Token string `json:"token"`
}

type RedeemSlackBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	SlackUserID    string `json:"slack_user_id"`
}

func (h *Handler) RedeemSlackBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.SlackBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "slack integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemSlackBindingTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	redeemed, err := h.SlackBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, slack.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, slack.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Slack account is already bound to a different Goosar user")
		case errors.Is(err, slack.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemSlackBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		SlackUserID:    redeemed.SlackUserID,
	})
}
