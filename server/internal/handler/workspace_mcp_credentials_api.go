package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/logger"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

func marshalMcpCredentialSchemaOrBadRequest(w http.ResponseWriter, fields []McpCredentialField) ([]byte, bool) {
	if err := validateMcpCredentialSchema(fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	encoded, err := json.Marshal(normalizeMcpCredentialSchema(fields))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the credential schema")
		return nil, false
	}
	return encoded, true
}

func requireMcpCredentialTransport(w http.ResponseWriter, transport string, fields []McpCredentialField) bool {
	if len(fields) == 0 || mcpCredentialTransportSupported(transport) {
		return true
	}
	writeError(w, http.StatusBadRequest,
		"an MCP server reached over "+transport+" takes its credentials in headers, not in env, "+
			"so it cannot ask its users for per-user credentials; use a stdio entry for that")
	return false
}

func (h *Handler) openOwnMcpCredentialValues(r *http.Request, serverID, userID pgtype.UUID) map[string]string {
	sealed, err := h.Queries.GetWorkspaceMcpUserCredentialValues(r.Context(), db.GetWorkspaceMcpUserCredentialValuesParams{
		ServerID: serverID,
		UserID:   userID,
	})
	if err != nil || len(sealed) == 0 {
		return nil
	}
	opened, err := h.openConfigDocument(sealed)
	if err != nil {
		slog.Warn("workspace mcp credentials: stored values could not be opened; this write replaces them",
			append(logger.RequestAttrs(r), "server_id", uuidToString(serverID))...)
		return nil
	}
	values := map[string]string{}
	if err := json.Unmarshal(opened, &values); err != nil {
		return nil
	}
	return values
}

func (h *Handler) mcpCredentialKeysForCaller(r *http.Request, serverID pgtype.UUID) []string {
	callerUUID, err := util.ParseUUID(requestUserID(r))
	if err != nil {
		return nil
	}
	keys, err := h.Queries.GetWorkspaceMcpUserCredentialKeys(r.Context(), db.GetWorkspaceMcpUserCredentialKeysParams{
		ServerID: serverID,
		UserID:   callerUUID,
	})
	if err != nil {
		return nil
	}
	return keys
}

type SetWorkspaceMcpCredentialsRequest struct {
	Values map[string]string `json:"values"`
}

type mcpCredentialTarget struct {
	ID               pgtype.UUID
	WorkspaceID      pgtype.UUID
	Name             string
	Transport        string
	Source           string
	CredentialSchema []byte
	CreatedAt        pgtype.Timestamptz
	UpdatedAt        pgtype.Timestamptz
}

func (h *Handler) requireWorkspaceMcpCredentialCaller(w http.ResponseWriter, r *http.Request) (server mcpCredentialTarget, caller pgtype.UUID, ok bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage MCP credentials")
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}
	caller, ok = parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}

	row, err := h.Queries.GetWorkspaceMcpServer(r.Context(), db.GetWorkspaceMcpServerParams{
		ID:          serverUUID,
		WorkspaceID: wsUUID,
	})
	if err == nil {
		return mcpCredentialTarget{
			ID:               row.ID,
			WorkspaceID:      row.WorkspaceID,
			Name:             row.Name,
			Transport:        row.Transport,
			Source:           mcpSourceWorkspace,
			CredentialSchema: row.CredentialSchema,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		}, caller, true
	}

	deployment, derr := h.Queries.GetEnabledDeploymentMcpServerForWorkspace(r.Context(), db.GetEnabledDeploymentMcpServerForWorkspaceParams{
		ID:          serverUUID,
		WorkspaceID: wsUUID,
	})
	if derr != nil {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return mcpCredentialTarget{}, pgtype.UUID{}, false
	}

	return mcpCredentialTarget{
		ID:               deployment.ID,
		WorkspaceID:      wsUUID,
		Name:             deployment.Name,
		Transport:        deployment.Transport,
		Source:           mcpSourceDeployment,
		CredentialSchema: deployment.CredentialSchema,
		CreatedAt:        deployment.CreatedAt,
		UpdatedAt:        deployment.UpdatedAt,
	}, caller, true
}

func (h *Handler) SetWorkspaceMcpCredentials(w http.ResponseWriter, r *http.Request) {
	server, caller, ok := h.requireWorkspaceMcpCredentialCaller(w, r)
	if !ok {
		return
	}
	var req SetWorkspaceMcpCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	schema := parseMcpCredentialSchema(server.CredentialSchema)
	if len(schema) == 0 {
		writeError(w, http.StatusBadRequest, "this MCP server does not ask its users for any credentials")
		return
	}
	if err := validateMcpCredentialValues(schema, req.Values); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	values := mergeMcpCredentialValues(schema, h.openOwnMcpCredentialValues(r, server.ID, caller), req.Values)

	keys := mcpCredentialKeys(values)
	plaintext, err := json.Marshal(values)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store the credentials")
		return
	}
	sealed, err := h.sealConfigDocument(plaintext)
	if err != nil {

		if errors.Is(err, errConfigSecretKeyUnset) {
			writeError(w, http.StatusServiceUnavailable,
				"MCP credentials require GOOSAR_MCP_SECRET_KEY to be configured on the server")
			return
		}
		slog.Error("workspace mcp credentials: seal failed")
		writeError(w, http.StatusInternalServerError, "failed to store the credentials")
		return
	}
	if err := h.Queries.UpsertWorkspaceMcpUserCredential(r.Context(), db.UpsertWorkspaceMcpUserCredentialParams{
		ServerID:     server.ID,
		UserID:       caller,
		WorkspaceID:  server.WorkspaceID,
		SealedValues: sealed,
		ValueKeys:    keys,
	}); err != nil {
		slog.Warn("set workspace mcp credentials failed", append(logger.RequestAttrs(r),
			"server_id", uuidToString(server.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to store the credentials")
		return
	}

	slog.Info("workspace mcp credentials set", append(logger.RequestAttrs(r),
		"server_id", uuidToString(server.ID), "fields", keys)...)
	writeJSON(w, http.StatusOK, newWorkspaceMcpServerResponse(
		server.ID, server.WorkspaceID, server.Name, server.Transport,
		server.Source, server.CredentialSchema, keys,
		server.CreatedAt, server.UpdatedAt,
	))
}

func (h *Handler) DeleteWorkspaceMcpCredentials(w http.ResponseWriter, r *http.Request) {
	server, caller, ok := h.requireWorkspaceMcpCredentialCaller(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteWorkspaceMcpUserCredential(r.Context(), db.DeleteWorkspaceMcpUserCredentialParams{
		ServerID: server.ID,
		UserID:   caller,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear the credentials")
		return
	}
	slog.Info("workspace mcp credentials cleared", append(logger.RequestAttrs(r),
		"server_id", uuidToString(server.ID))...)
	writeJSON(w, http.StatusOK, newWorkspaceMcpServerResponse(
		server.ID, server.WorkspaceID, server.Name, server.Transport,
		server.Source, server.CredentialSchema, nil,
		server.CreatedAt, server.UpdatedAt,
	))
}
