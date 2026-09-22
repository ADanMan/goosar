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

	"github.com/adanman/goosar/server/internal/logger"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type WorkspaceMcpServerResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Transport   string `json:"transport"`
	Source      string `json:"source"`
	Enabled     *bool  `json:"enabled,omitempty"`

	CredentialSchema []McpCredentialField `json:"credential_schema"`

	ProvidedCredentials []string `json:"provided_credentials"`

	MissingCredentials []string `json:"missing_credentials"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

func newWorkspaceMcpServerResponse(
	id, workspaceID pgtype.UUID,
	name, transport, source string,
	credentialSchema []byte,
	providedKeys []string,
	createdAt, updatedAt pgtype.Timestamptz,
) WorkspaceMcpServerResponse {
	schema := parseMcpCredentialSchema(credentialSchema)
	if schema == nil {
		schema = []McpCredentialField{}
	}
	if providedKeys == nil {
		providedKeys = []string{}
	}
	missing := missingMcpCredentials(schema, providedKeys)
	if missing == nil {
		missing = []string{}
	}
	return WorkspaceMcpServerResponse{
		ID:                  uuidToString(id),
		WorkspaceID:         uuidToString(workspaceID),
		Name:                name,
		Transport:           transport,
		Source:              source,
		CredentialSchema:    schema,
		ProvidedCredentials: providedKeys,
		MissingCredentials:  missing,
		CreatedAt:           timestampToString(createdAt),
		UpdatedAt:           timestampToString(updatedAt),
	}
}

func (h *Handler) ListWorkspaceMcpServers(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	callerUUID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	servers, err := h.Queries.ListWorkspaceMcpServers(r.Context(), db.ListWorkspaceMcpServersParams{
		WorkspaceID: wsUUID,
		UserID:      callerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace MCP servers")
		return
	}
	resp := make([]WorkspaceMcpServerResponse, 0, len(servers))
	for _, server := range servers {
		resp = append(resp, newWorkspaceMcpServerResponse(
			server.ID, server.WorkspaceID, server.Name, server.Transport,
			mcpSourceWorkspace, server.CredentialSchema, server.ProvidedKeys,
			server.CreatedAt, server.UpdatedAt,
		))
	}

	offered, err := h.Queries.ListDeploymentMcpServersForWorkspace(r.Context(), db.ListDeploymentMcpServersForWorkspaceParams{
		WorkspaceID: wsUUID,
		UserID:      callerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace MCP servers")
		return
	}
	for _, server := range offered {
		if !server.Enabled {
			continue
		}
		resp = append(resp, newWorkspaceMcpServerResponse(
			server.ID, wsUUID, server.Name, server.Transport,
			mcpSourceDeployment, server.CredentialSchema, server.ProvidedKeys,
			server.CreatedAt, server.UpdatedAt,
		))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) requireWorkspaceMcpWriter(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return "", pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return "", pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify the workspace MCP servers")
		return "", pgtype.UUID{}, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return "", pgtype.UUID{}, false
	}
	return workspaceID, wsUUID, true
}

type WorkspaceMcpServerRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`

	CredentialSchema []McpCredentialField `json:"credential_schema"`
}

func (h *Handler) sealWorkspaceMcpEntry(w http.ResponseWriter, config json.RawMessage) (sealed []byte, transport string, ok bool) {
	if err := validateWorkspaceMcpServerEntry(config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, "", false
	}
	transport = mcpTransportOf(config)
	sealed, err := h.sealConfigDocument(config)
	if err != nil {
		if errors.Is(err, errConfigSecretKeyUnset) {
			writeError(w, http.StatusServiceUnavailable,
				"workspace MCP servers require GOOSAR_MCP_SECRET_KEY to be configured on the server")
			return nil, "", false
		}

		slog.Error("workspace mcp server: seal config failed")
		writeError(w, http.StatusInternalServerError, "failed to store the MCP server")
		return nil, "", false
	}
	return sealed, transport, true
}

func (h *Handler) CreateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, wsUUID, ok := h.requireWorkspaceMcpWriter(w, r)
	if !ok {
		return
	}

	var req WorkspaceMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := validateWorkspaceMcpServerName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, transport, ok := h.sealWorkspaceMcpEntry(w, req.Config)
	if !ok {
		return
	}
	if !requireMcpCredentialTransport(w, transport, req.CredentialSchema) {
		return
	}
	credentialSchema, ok := marshalMcpCredentialSchemaOrBadRequest(w, req.CredentialSchema)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("delete workspace mcp server: workspace fence failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}

	server, err := qtx.CreateWorkspaceMcpServer(r.Context(), db.CreateWorkspaceMcpServerParams{
		WorkspaceID:      wsUUID,
		Name:             name,
		Config:           sealed,
		Transport:        transport,
		CredentialSchema: credentialSchema,
		CreatedBy:        parseUUID(requestUserID(r)),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in the workspace")
			return
		}
		slog.Warn("create workspace mcp server failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}

	slog.Info("workspace mcp server created", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(server.ID), "name", server.Name)...)

	writeJSON(w, http.StatusCreated, newWorkspaceMcpServerResponse(
		server.ID, server.WorkspaceID, server.Name, server.Transport,
		mcpSourceWorkspace, server.CredentialSchema, nil,
		server.CreatedAt, server.UpdatedAt,
	))
}

func (h *Handler) UpdateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, wsUUID, ok := h.requireWorkspaceMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}

	var req WorkspaceMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateWorkspaceMcpServerParams{ID: serverUUID, WorkspaceID: wsUUID}
	if name := strings.TrimSpace(req.Name); name != "" {
		if err := validateWorkspaceMcpServerName(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if len(req.Config) > 0 {
		sealed, transport, ok := h.sealWorkspaceMcpEntry(w, req.Config)
		if !ok {
			return
		}
		params.Config = sealed
		params.Transport = pgtype.Text{String: transport, Valid: true}
	}
	if req.CredentialSchema != nil {
		schema, ok := marshalMcpCredentialSchemaOrBadRequest(w, req.CredentialSchema)
		if !ok {
			return
		}
		params.CredentialSchema = schema
	}

	if req.CredentialSchema != nil || params.Transport.Valid {
		current, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{
			ID:          serverUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "MCP server not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
			return
		}
		transport := current.Transport
		if params.Transport.Valid {
			transport = params.Transport.String
		}
		fields := req.CredentialSchema
		if fields == nil {
			fields = parseMcpCredentialSchema(current.CredentialSchema)
		}
		if !requireMcpCredentialTransport(w, transport, fields) {
			return
		}
	}

	server, err := h.Queries.UpdateWorkspaceMcpServer(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in the workspace")
			return
		}

		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		slog.Warn("update workspace mcp server failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	slog.Info("workspace mcp server updated", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(server.ID), "name", server.Name)...)

	writeJSON(w, http.StatusOK, newWorkspaceMcpServerResponse(
		server.ID, server.WorkspaceID, server.Name, server.Transport,
		mcpSourceWorkspace, server.CredentialSchema, h.mcpCredentialKeysForCaller(r, server.ID),
		server.CreatedAt, server.UpdatedAt,
	))
}

func (h *Handler) DeleteWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, wsUUID, ok := h.requireWorkspaceMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {

		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("delete workspace mcp server: workspace fence failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}

	if _, err := qtx.LockWorkspaceMcpServerForUpdate(r.Context(), db.LockWorkspaceMcpServerForUpdateParams{
		ID:          serverUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}

	rows, err := qtx.DeleteWorkspaceMcpServer(r.Context(), db.DeleteWorkspaceMcpServerParams{
		ID:          serverUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("delete workspace mcp server failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err := qtx.DeleteAgentMcpServersByServer(r.Context(), serverUUID); err != nil {
		slog.Warn("sweep agent mcp assignments failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}

	if err := qtx.DeleteWorkspaceMcpUserCredentialsByServer(r.Context(), serverUUID); err != nil {
		slog.Warn("sweep workspace mcp user credentials failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	slog.Info("workspace mcp server deleted", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(serverUUID))...)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAgentMcpServers(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.writeAgentMcpServers(w, r, agent.ID, agent.WorkspaceID, agent.OwnerID)
}

func (h *Handler) requireAgentMcpWriter(w http.ResponseWriter, r *http.Request) (db.Agent, bool) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Agent{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot modify MCP server assignments")
		return db.Agent{}, false
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin"); !ok {
		return db.Agent{}, false
	}

	if h.denyRoleAgentToNonDeploymentAdmin(w, r, agent) {
		return db.Agent{}, false
	}
	return agent, true
}

type AddAgentMcpServerRequest struct {
	ServerID string `json:"server_id"`
}

func (h *Handler) AddAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	var req AddAgentMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ServerID), "server_id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), agent.WorkspaceID); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	if _, err := qtx.LockWorkspaceMcpServerForShare(r.Context(), db.LockWorkspaceMcpServerForShareParams{
		ID:          serverUUID,
		WorkspaceID: agent.WorkspaceID,
	}); err != nil {

		if _, derr := qtx.GetEnabledDeploymentMcpServerForWorkspace(r.Context(), db.GetEnabledDeploymentMcpServerForWorkspaceParams{
			ID:          serverUUID,
			WorkspaceID: agent.WorkspaceID,
		}); derr != nil {
			writeError(w, http.StatusNotFound, "MCP server not found in this workspace")
			return
		}
	}
	if err := qtx.AddAgentMcpServer(r.Context(), db.AddAgentMcpServerParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
	}); err != nil {
		slog.Warn("add agent mcp server failed", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add the MCP server")
		return
	}
	slog.Info("agent mcp server added", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID))...)
	h.writeAgentMcpServers(w, r, agent.ID, agent.WorkspaceID, agent.OwnerID)
}

type SetAgentMcpServerEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h *Handler) SetAgentMcpServerEnabled(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	var req SetAgentMcpServerEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	rows, err := h.Queries.SetAgentMcpServerEnabled(r.Context(), db.SetAgentMcpServerEnabledParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
		Enabled:  *req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "this MCP server is not assigned to the agent")
		return
	}
	slog.Info("agent mcp server toggled", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID), "enabled", *req.Enabled)...)
	h.writeAgentMcpServers(w, r, agent.ID, agent.WorkspaceID, agent.OwnerID)
}

func (h *Handler) RemoveAgentMcpServer(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.requireAgentMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	rows, err := h.Queries.RemoveAgentMcpServer(r.Context(), db.RemoveAgentMcpServerParams{
		AgentID:  agent.ID,
		ServerID: serverUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "this MCP server is not assigned to the agent")
		return
	}
	slog.Info("agent mcp server removed", append(logger.RequestAttrs(r),
		"agent_id", uuidToString(agent.ID), "server_id", uuidToString(serverUUID))...)
	h.writeAgentMcpServers(w, r, agent.ID, agent.WorkspaceID, agent.OwnerID)
}

func (h *Handler) writeAgentMcpServers(w http.ResponseWriter, r *http.Request, agentID, workspaceID, ownerID pgtype.UUID) {
	rows, err := h.Queries.ListAgentMcpServers(r.Context(), db.ListAgentMcpServersParams{
		AgentID:     agentID,
		UserID:      ownerID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the agent's MCP servers")
		return
	}
	resp := make([]WorkspaceMcpServerResponse, 0, len(rows))
	for _, row := range rows {
		enabled := row.Enabled
		item := newWorkspaceMcpServerResponse(
			row.ID, row.WorkspaceID, row.Name, row.Transport,
			row.Source, row.CredentialSchema, row.ProvidedKeys,
			row.CreatedAt, row.UpdatedAt,
		)
		item.Enabled = &enabled
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}
