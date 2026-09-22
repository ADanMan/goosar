// Библиотека MCP-серверов деплоя: CRUD над общей библиотекой у
// администратора и отдельная ручка, которой рабочее пространство включает
// запись себе. Авторство и охват разделены: запись в библиотеке сама по
// себе никому ничего не даёт, включение пространством — тоже, а доступ
// агенту выдаётся только обычным путём через agent_mcp_server. Конфигурация
// запечатана и недоступна на чтение никому, включая создавшего её админа.
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

const (
	adminAuditActionDeploymentMcpCreate = "deployment_mcp_server.create"
	adminAuditActionDeploymentMcpUpdate = "deployment_mcp_server.update"
	adminAuditActionDeploymentMcpDelete = "deployment_mcp_server.delete"
)

const (
	mcpSourceWorkspace  = "workspace"
	mcpSourceDeployment = "deployment"
)

type DeploymentMcpServerResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Transport string `json:"transport"`

	CredentialSchema  []McpCredentialField `json:"credential_schema"`
	EnabledWorkspaces *int64               `json:"enabled_workspaces,omitempty"`
	Enabled           *bool                `json:"enabled,omitempty"`
	CreatedAt         string               `json:"created_at"`
	UpdatedAt         string               `json:"updated_at"`
}

type DeploymentMcpServerRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`

	CredentialSchema []McpCredentialField `json:"credential_schema"`
}

func (h *Handler) ListDeploymentMcpServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireDeploymentAdmin(w, r); !ok {
		return
	}
	rows, err := h.Queries.ListDeploymentMcpServers(r.Context())
	if err != nil {
		slog.Error("deployment mcp: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list deployment MCP servers")
		return
	}
	resp := make([]DeploymentMcpServerResponse, 0, len(rows))
	for _, row := range rows {
		count := row.EnabledWorkspaces
		item := deploymentMcpResponse(row.ID, row.Name, row.Transport, row.CredentialSchema, row.CreatedAt, row.UpdatedAt)
		item.EnabledWorkspaces = &count
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func decodeDeploymentMcpRequest(w http.ResponseWriter, r *http.Request, requireName bool) (DeploymentMcpServerRequest, bool) {
	var req DeploymentMcpServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, false
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" && !requireName {
		return req, true
	}
	if err := validateWorkspaceMcpServerName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return req, false
	}
	return req, true
}

func (h *Handler) CreateDeploymentMcpServer(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	req, ok := decodeDeploymentMcpRequest(w, r, true)
	if !ok {
		return
	}
	sealed, transport, ok := h.sealWorkspaceMcpEntry(w, req.Config)
	if !ok {
		return
	}
	if !requireMcpCredentialTransport(w, transport, req.CredentialSchema) {
		return
	}
	schema, ok := marshalMcpCredentialSchemaOrBadRequest(w, req.CredentialSchema)
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

	server, err := qtx.CreateDeploymentMcpServer(r.Context(), db.CreateDeploymentMcpServerParams{
		Name:             req.Name,
		Config:           sealed,
		Transport:        transport,
		CredentialSchema: schema,
		CreatedBy:        actorUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a deployment MCP server with this name already exists")
			return
		}
		slog.Error("deployment mcp: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}
	if !h.auditDeploymentMcp(w, r, qtx, actorUUID, adminAuditActionDeploymentMcpCreate, server.ID, req.Config) {
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the MCP server")
		return
	}

	slog.Info("deployment mcp server created", append(logger.RequestAttrs(r),
		"server_id", uuidToString(server.ID), "name", server.Name)...)
	writeJSON(w, http.StatusCreated, deploymentMcpResponse(server.ID, server.Name, server.Transport, server.CredentialSchema, server.CreatedAt, server.UpdatedAt))
}

func (h *Handler) UpdateDeploymentMcpServer(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	req, ok := decodeDeploymentMcpRequest(w, r, false)
	if !ok {
		return
	}
	params := db.UpdateDeploymentMcpServerParams{ID: serverUUID}
	if req.Name != "" {
		params.Name = pgtype.Text{String: req.Name, Valid: true}
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

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	current, err := qtx.LockDeploymentMcpServerForUpdate(r.Context(), serverUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		slog.Error("deployment mcp: lock failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}

	if req.CredentialSchema != nil || params.Transport.Valid {
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
	server, err := qtx.UpdateDeploymentMcpServer(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a deployment MCP server with this name already exists")
			return
		}

		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		slog.Error("deployment mcp: update failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	if !h.auditDeploymentMcp(w, r, qtx, actorUUID, adminAuditActionDeploymentMcpUpdate, server.ID, req.Config) {
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	slog.Info("deployment mcp server updated", append(logger.RequestAttrs(r),
		"server_id", uuidToString(server.ID), "name", server.Name)...)
	writeJSON(w, http.StatusOK, deploymentMcpResponse(server.ID, server.Name, server.Transport, server.CredentialSchema, server.CreatedAt, server.UpdatedAt))
}

func (h *Handler) DeleteDeploymentMcpServer(w http.ResponseWriter, r *http.Request) {
	actorUUID, ok := h.requireDeploymentAdmin(w, r)
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

	if _, err := qtx.LockDeploymentMcpServerForUpdate(r.Context(), serverUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		slog.Error("deployment mcp: lock failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	rows, err := qtx.DeleteDeploymentMcpServer(r.Context(), serverUUID)
	if err != nil {
		slog.Error("deployment mcp: delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err := qtx.DeleteWorkspaceDeploymentMcpServersByServer(r.Context(), serverUUID); err != nil {
		slog.Error("deployment mcp: sweep enablements failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}

	if err := qtx.DeleteAgentMcpServersByServer(r.Context(), serverUUID); err != nil {
		slog.Error("deployment mcp: sweep assignments failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}

	if err := qtx.DeleteWorkspaceMcpUserCredentialsByServer(r.Context(), serverUUID); err != nil {
		slog.Error("deployment mcp: sweep user credentials failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	if !h.auditDeploymentMcp(w, r, qtx, actorUUID, adminAuditActionDeploymentMcpDelete, serverUUID, nil) {
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the MCP server")
		return
	}
	slog.Info("deployment mcp server deleted", append(logger.RequestAttrs(r),
		"server_id", uuidToString(serverUUID))...)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) auditDeploymentMcp(w http.ResponseWriter, r *http.Request, qtx *db.Queries, actorUUID pgtype.UUID, action string, serverID pgtype.UUID, plaintext json.RawMessage) bool {
	after := deploymentPolicyHash(plaintext)
	if _, err := qtx.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: actorUUID,
		Action:      action,
		TargetType:  "deployment_mcp_server",
		TargetID:    pgtype.Text{String: uuidToString(serverID), Valid: true},
		AfterHash:   pgtype.Text{String: after, Valid: after != ""},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {
		slog.Error("deployment mcp: audit insert failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record the deployment audit entry")
		return false
	}
	return true
}

func deploymentMcpResponse(id pgtype.UUID, name, transport string, credentialSchema []byte, createdAt, updatedAt pgtype.Timestamptz) DeploymentMcpServerResponse {
	schema := parseMcpCredentialSchema(credentialSchema)
	if schema == nil {
		schema = []McpCredentialField{}
	}
	return DeploymentMcpServerResponse{
		ID:               uuidToString(id),
		Name:             name,
		Transport:        transport,
		CredentialSchema: schema,
		CreatedAt:        timestampToString(createdAt),
		UpdatedAt:        timestampToString(updatedAt),
	}
}

func (h *Handler) ListWorkspaceDeploymentMcpServers(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, ok := h.requireWorkspaceMcpWriter(w, r)
	if !ok {
		return
	}
	callerUUID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListDeploymentMcpServersForWorkspace(r.Context(), db.ListDeploymentMcpServersForWorkspaceParams{
		WorkspaceID: wsUUID,
		UserID:      callerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deployment MCP servers")
		return
	}
	resp := make([]DeploymentMcpServerResponse, 0, len(rows))
	for _, row := range rows {
		enabled := row.Enabled
		item := deploymentMcpResponse(row.ID, row.Name, row.Transport, row.CredentialSchema, row.CreatedAt, row.UpdatedAt)
		item.Enabled = &enabled
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

type SetWorkspaceDeploymentMcpServerEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h *Handler) SetWorkspaceDeploymentMcpServerEnabled(w http.ResponseWriter, r *http.Request) {
	workspaceID, wsUUID, ok := h.requireWorkspaceMcpWriter(w, r)
	if !ok {
		return
	}
	serverUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "serverId"), "server id")
	if !ok {
		return
	}
	var req SetWorkspaceDeploymentMcpServerEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("deployment mcp enablement: workspace fence failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}

	if *req.Enabled {

		if _, err := qtx.LockDeploymentMcpServerForShare(r.Context(), serverUUID); err != nil {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		if err := qtx.EnableDeploymentMcpServerForWorkspace(r.Context(), db.EnableDeploymentMcpServerForWorkspaceParams{
			WorkspaceID: wsUUID,
			ServerID:    serverUUID,
			EnabledBy:   parseUUID(requestUserID(r)),
		}); err != nil {
			slog.Warn("deployment mcp enablement failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
			return
		}
	} else {
		if _, err := qtx.DisableDeploymentMcpServerForWorkspace(r.Context(), db.DisableDeploymentMcpServerForWorkspaceParams{
			WorkspaceID: wsUUID,
			ServerID:    serverUUID,
		}); err != nil {
			slog.Warn("deployment mcp disablement failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
			return
		}

		if err := qtx.DeleteAgentMcpServersByServerAndWorkspace(r.Context(), db.DeleteAgentMcpServersByServerAndWorkspaceParams{
			ServerID:    serverUUID,
			WorkspaceID: wsUUID,
		}); err != nil {
			slog.Warn("deployment mcp disablement sweep failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the MCP server")
		return
	}
	slog.Info("workspace deployment mcp server toggled", append(logger.RequestAttrs(r),
		"workspace_id", workspaceID, "server_id", uuidToString(serverUUID), "enabled", *req.Enabled)...)
	w.WriteHeader(http.StatusNoContent)
}
