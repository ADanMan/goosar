package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/pkg/agent"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

type RuntimeProfileResponse struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Visibility     string   `json:"visibility"`
	CreatedBy      *string  `json:"created_by"`
	Enabled        bool     `json:"enabled"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func runtimeProfileToResponse(p db.RuntimeProfile) RuntimeProfileResponse {
	args := []string{}
	if len(p.FixedArgs) > 0 {
		_ = json.Unmarshal(p.FixedArgs, &args)
		if args == nil {
			args = []string{}
		}
	}
	return RuntimeProfileResponse{
		ID:             uuidToString(p.ID),
		WorkspaceID:    uuidToString(p.WorkspaceID),
		DisplayName:    p.DisplayName,
		ProtocolFamily: p.ProtocolFamily,
		CommandName:    p.CommandName,
		Description:    textToPtr(p.Description),
		FixedArgs:      args,
		Visibility:     p.Visibility,
		CreatedBy:      uuidToPtr(p.CreatedBy),
		Enabled:        p.Enabled,
		CreatedAt:      timestampToString(p.CreatedAt),
		UpdatedAt:      timestampToString(p.UpdatedAt),
	}
}

const runtimeProfileDefaultVisibility = "workspace"

func marshalFixedArgs(args []string) ([]byte, error) {
	if len(args) == 0 {
		return []byte("[]"), nil
	}
	clean := make([]string, 0, len(args))
	for _, a := range args {

		if strings.TrimSpace(a) == "" {
			return nil, errors.New("fixed_args entries must be non-empty")
		}
		if strings.ContainsRune(a, '\x00') {
			return nil, errors.New("fixed_args entries cannot contain NUL bytes")
		}
		clean = append(clean, a)
	}
	return json.Marshal(clean)
}

func validateRuntimeProfileCommandName(commandName string) error {
	if commandName == "" {
		return errors.New("command_name is required")
	}
	if strings.ContainsAny(commandName, " \t\r\n") {
		return errors.New("command_name must be a single executable token; put arguments in fixed_args")
	}
	if strings.ContainsRune(commandName, '\x00') {
		return errors.New("command_name cannot contain NUL bytes")
	}
	return nil
}

type createRuntimeProfileRequest struct {
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Enabled        *bool    `json:"enabled"`
}

func (h *Handler) CreateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}

	var req createRuntimeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.ProtocolFamily = strings.TrimSpace(req.ProtocolFamily)
	req.CommandName = strings.TrimSpace(req.CommandName)

	if req.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}
	if !agent.IsSupportedType(req.ProtocolFamily) {
		writeError(w, http.StatusBadRequest, "unsupported protocol_family: must be one of "+strings.Join(agent.SupportedTypes, ", "))
		return
	}
	if req.CommandName == "" {
		writeError(w, http.StatusBadRequest, "command_name is required")
		return
	}
	if err := validateRuntimeProfileCommandName(req.CommandName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fixedArgs, err := marshalFixedArgs(req.FixedArgs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	profile, err := h.Queries.CreateRuntimeProfile(r.Context(), db.CreateRuntimeProfileParams{
		WorkspaceID:    wsUUID,
		DisplayName:    req.DisplayName,
		ProtocolFamily: req.ProtocolFamily,
		CommandName:    req.CommandName,
		Description:    ptrToText(req.Description),
		FixedArgs:      fixedArgs,
		Visibility:     runtimeProfileDefaultVisibility,
		CreatedBy:      member.UserID,
		Enabled:        enabled,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a runtime profile with this display_name already exists")
			return
		}
		slog.Error("CreateRuntimeProfile failed", "error", err, "workspace_id", wsID)
		writeError(w, http.StatusInternalServerError, "failed to create runtime profile")
		return
	}

	profileID := uuidToString(profile.ID)
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"runtime_profile_id": profileID,
	})

	writeJSON(w, http.StatusCreated, runtimeProfileToResponse(profile))
}

func (h *Handler) ListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}

	profiles, err := h.Queries.ListRuntimeProfiles(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime profiles")
		return
	}
	resp := make([]RuntimeProfileResponse, len(profiles))
	for i, p := range profiles {
		resp[i] = runtimeProfileToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime_profiles": resp})
}

func (h *Handler) GetRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	profile, err := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
		ID:          profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime profile not found")
		return
	}
	writeJSON(w, http.StatusOK, runtimeProfileToResponse(profile))
}

type updateRuntimeProfileRequest struct {
	DisplayName *string   `json:"display_name"`
	CommandName *string   `json:"command_name"`
	Description *string   `json:"description"`
	FixedArgs   *[]string `json:"fixed_args"`
	Enabled     *bool     `json:"enabled"`
}

func (h *Handler) UpdateRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	var req updateRuntimeProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateRuntimeProfileParams{ID: profileUUID, WorkspaceID: wsUUID}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			writeError(w, http.StatusBadRequest, "display_name cannot be empty")
			return
		}
		params.DisplayName = strToText(name)
	}
	if req.CommandName != nil {
		cmd := strings.TrimSpace(*req.CommandName)
		if err := validateRuntimeProfileCommandName(cmd); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.CommandName = strToText(cmd)
	}
	if req.Description != nil {
		params.Description = ptrToText(req.Description)
	}
	if req.FixedArgs != nil {
		fixedArgs, err := marshalFixedArgs(*req.FixedArgs)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.FixedArgs = fixedArgs
	}
	if req.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	profile, err := h.Queries.UpdateRuntimeProfile(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "runtime profile not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a runtime profile with this display_name already exists")
			return
		}
		slog.Error("UpdateRuntimeProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
		writeError(w, http.StatusInternalServerError, "failed to update runtime profile")
		return
	}

	profileID := uuidToString(profile.ID)
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"runtime_profile_id": profileID,
	})

	writeJSON(w, http.StatusOK, runtimeProfileToResponse(profile))
}

func (h *Handler) DeleteRuntimeProfile(w http.ResponseWriter, r *http.Request) {
	wsID := strings.TrimSpace(chi.URLParam(r, "id"))
	member, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	profileUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "profileId"), "profile id")
	if !ok {
		return
	}

	_, profileErr := h.Queries.GetRuntimeProfileForWorkspace(r.Context(), db.GetRuntimeProfileForWorkspaceParams{
		ID:          profileUUID,
		WorkspaceID: wsUUID,
	})
	profileMissing := errors.Is(profileErr, pgx.ErrNoRows)
	if profileErr != nil && !profileMissing {
		writeError(w, http.StatusInternalServerError, "failed to load runtime profile")
		return
	}

	runtimeIDs, err := h.Queries.ListAgentRuntimeIDsByProfile(r.Context(), db.ListAgentRuntimeIDsByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enumerate profile runtimes")
		return
	}
	if profileMissing && len(runtimeIDs) == 0 {
		writeError(w, http.StatusNotFound, "runtime profile not found")
		return
	}

	agentCount, err := h.Queries.CountAgentsByProfile(r.Context(), db.CountAgentsByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check profile usage")
		return
	}
	if agentCount > 0 {
		writeError(w, http.StatusConflict, "cannot delete runtime profile: active agents are still bound to its runtimes")
		return
	}

	for _, rid := range runtimeIDs {
		activeSquadCount, err := h.Queries.CountActiveSquadsWithArchivedLeadersByRuntime(r.Context(), rid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check runtime squad dependencies")
			return
		}
		if activeSquadCount > 0 {
			writeError(w, http.StatusConflict, "cannot delete runtime profile: a runtime has active squads led by archived agents. Archive those squads or assign them a new leader first.")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	for _, rid := range runtimeIDs {
		archivedAgentIDs, err := qtx.ListArchivedAgentIDsByRuntime(r.Context(), rid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enumerate archived agents")
			return
		}
		if len(archivedAgentIDs) > 0 {
			if err := qtx.PauseAutopilotsByAgentAssignees(r.Context(), archivedAgentIDs); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to pause autopilots")
				return
			}
		}
		if err := qtx.DeleteSquadsByArchivedAgentsOnRuntime(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up squads referencing archived agents")
			return
		}
		if err := qtx.DeleteAgentInvocationTargetsByArchivedRuntimeAgents(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up agent invocation targets")
			return
		}

		if err := qtx.DeleteChannelInstallationsByArchivedRuntimeAgents(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up channel installations")
			return
		}

		if err := qtx.DeleteAgentLabelAssignmentsByRuntime(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up agent label assignments")
			return
		}

		if err := pruneRuntimeAgentChatDraftRestores(r.Context(), qtx, rid, false); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up chat draft restores")
			return
		}

		if err := qtx.DeleteAgentMcpServersByArchivedRuntimeAgents(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up agent MCP assignments")
			return
		}
		if err := qtx.DeleteArchivedAgentsByRuntime(r.Context(), rid); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clean up archived agents")
			return
		}
	}

	if _, err := qtx.DeleteAgentRuntimesByProfile(r.Context(), db.DeleteAgentRuntimesByProfileParams{
		ProfileID:   profileUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("DeleteAgentRuntimesByProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
		writeError(w, http.StatusInternalServerError, "failed to clean up runtime instances")
		return
	}
	if !profileMissing {
		if err := qtx.DeleteRuntimeProfile(r.Context(), db.DeleteRuntimeProfileParams{
			ID:          profileUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.Error("DeleteRuntimeProfile failed", "error", err, "profile_id", uuidToString(profileUUID))
			writeError(w, http.StatusInternalServerError, "failed to delete runtime profile")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	profileID := uuidToString(profileUUID)
	h.requestDaemonRuntimeProfileRefresh(wsID, profileID)
	h.publish(protocol.EventDaemonRegister, wsID, "member", uuidToString(member.UserID), map[string]any{
		"deleted_runtime_profile_id": profileID,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DaemonListRuntimeProfiles(w http.ResponseWriter, r *http.Request) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	profiles, err := h.Queries.ListEnabledRuntimeProfilesForWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list runtime profiles")
		return
	}
	resp := make([]RuntimeProfileResponse, len(profiles))
	for i, p := range profiles {
		resp[i] = runtimeProfileToResponse(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id":     workspaceID,
		"runtime_profiles": resp,
	})
}

func (h *Handler) requestDaemonRuntimeProfileRefresh(workspaceID, profileID string) {
	if h.DaemonProfileRefresh == nil {
		return
	}
	h.DaemonProfileRefresh.NotifyRuntimeProfilesChanged(workspaceID, profileID)
}
