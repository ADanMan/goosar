package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/seal"
	"github.com/adanman/goosar/server2/internal/store"
)

// --- platform_mcp_servers (библиотека MCP-серверов деплоя) --------------------

const platformServerColumns = `id, pmcp_name, pmcp_transport, pmcp_credential_schema, created_at, updated_at`

// listPlatformMcpServersAdmin — GET /api/deployment/mcp-servers: с числом
// пространств, включивших сервер (enabled_workspaces); без enabled — это
// поле только в контексте конкретного пространства (см.
// handleListWorkspaceDeploymentMcpServers).
func listPlatformMcpServersAdmin(ctx context.Context, db *store.Store) ([]PlatformMcpServer, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT p.id, p.pmcp_name, p.pmcp_transport, p.pmcp_credential_schema, p.created_at, p.updated_at,
			(SELECT count(*) FROM space_mcp_servers w WHERE w.wmcp_platform_server_id = p.id AND w.wmcp_enabled)
		FROM platform_mcp_servers p ORDER BY p.created_at`)
	if err != nil {
		return nil, fmt.Errorf("deployment: список MCP-серверов деплоя: %w", err)
	}
	defer rows.Close()
	out := []PlatformMcpServer{}
	for rows.Next() {
		var s PlatformMcpServer
		var schemaRaw []byte
		var count int
		if err := rows.Scan(&s.ID, &s.Name, &s.Transport, &schemaRaw, &s.CreatedAt, &s.UpdatedAt, &count); err != nil {
			return nil, err
		}
		s.CredentialSchema = unmarshalCredentialSchema(schemaRaw)
		s.EnabledWorkspaces = &count
		out = append(out, s)
	}
	return out, rows.Err()
}

func getPlatformServer(ctx context.Context, db *store.Store, id string) (PlatformMcpServer, error) {
	var s PlatformMcpServer
	var schemaRaw []byte
	err := db.Pool.QueryRow(ctx, `SELECT `+platformServerColumns+` FROM platform_mcp_servers WHERE id = $1`, id).
		Scan(&s.ID, &s.Name, &s.Transport, &schemaRaw, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlatformMcpServer{}, ErrNotFound
	}
	if err != nil {
		return PlatformMcpServer{}, fmt.Errorf("deployment: чтение MCP-сервера деплоя: %w", err)
	}
	s.CredentialSchema = unmarshalCredentialSchema(schemaRaw)
	return s, nil
}

func (d *Deps) handleListPlatformMcpServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	list, err := listPlatformMcpServersAdmin(r.Context(), d.DB)
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) handleCreatePlatformMcpServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	if !seal.Available(d.Config.McpSecretKey) {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "GOOSAR_MCP_SECRET_KEY is not configured", "encryption_unavailable")
		return
	}
	var req PlatformMcpServerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if !validMcpName(req.Name) {
		httpapi.BadRequest(w, "name must be letters/digits/dash/underscore")
		return
	}
	if msg, ok := validateMcpConfig(req.Config, len(req.CredentialSchema) > 0); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	if msg, ok := validateCredentialSchema(req.CredentialSchema); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	schemaJSON, _ := json.Marshal(req.CredentialSchema)
	sealed, err := seal.SealJSON(d.Config.McpSecretKey, req.Config)
	if err != nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "encryption unavailable", "encryption_unavailable")
		return
	}
	var s PlatformMcpServer
	var outSchema []byte
	err = d.DB.Pool.QueryRow(r.Context(), `
		INSERT INTO platform_mcp_servers (pmcp_name, pmcp_transport, pmcp_config_sealed, pmcp_credential_schema)
		VALUES ($1, $2, $3, $4)
		RETURNING `+platformServerColumns, req.Name, requestTransport(req.Config), sealed, schemaJSON,
	).Scan(&s.ID, &s.Name, &s.Transport, &outSchema, &s.CreatedAt, &s.UpdatedAt)
	if store.IsUniqueViolation(err) {
		httpapi.WriteError(w, http.StatusConflict, "name already in use", "name_conflict")
		return
	}
	if checkErr(w, err) {
		return
	}
	s.CredentialSchema = unmarshalCredentialSchema(outSchema)
	audit := httpAudit(r, actor, "deployment_mcp_server.create")
	audit.TargetType, audit.TargetID = ptr("platform_mcp_server"), ptr(s.ID)
	audit.AfterHash = ptr(seal.HashJSON(req.Config))
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusCreated, s)
}

func (d *Deps) handleUpdatePlatformMcpServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("serverId")
	existing, err := getPlatformServer(r.Context(), d.DB, id)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	var req PlatformMcpServerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	name := existing.Name
	if req.Name != "" {
		if !validMcpName(req.Name) {
			httpapi.BadRequest(w, "name must be letters/digits/dash/underscore")
			return
		}
		name = req.Name
	}
	credSchema := existing.CredentialSchema
	if req.CredentialSchema != nil {
		credSchema = req.CredentialSchema
	}
	if msg, ok := validateCredentialSchema(credSchema); !ok {
		httpapi.BadRequest(w, msg)
		return
	}
	transport := existing.Transport
	var sealedConfig []byte
	var afterHash string
	if req.Config != nil {
		if msg, ok := validateMcpConfig(req.Config, len(credSchema) > 0); !ok {
			httpapi.BadRequest(w, msg)
			return
		}
		if !seal.Available(d.Config.McpSecretKey) {
			httpapi.WriteError(w, http.StatusServiceUnavailable, "GOOSAR_MCP_SECRET_KEY is not configured", "encryption_unavailable")
			return
		}
		transport = requestTransport(req.Config)
		sealed, err := seal.SealJSON(d.Config.McpSecretKey, req.Config)
		if err != nil {
			httpapi.WriteError(w, http.StatusServiceUnavailable, "encryption unavailable", "encryption_unavailable")
			return
		}
		sealedConfig = sealed
		afterHash = seal.HashJSON(req.Config)
	}
	schemaJSON, _ := json.Marshal(credSchema)
	var s PlatformMcpServer
	var outSchema []byte
	err = d.DB.Pool.QueryRow(r.Context(), `
		UPDATE platform_mcp_servers SET
			pmcp_name = $2, pmcp_transport = $3, pmcp_credential_schema = $4,
			pmcp_config_sealed = COALESCE($5, pmcp_config_sealed), updated_at = now()
		WHERE id = $1
		RETURNING `+platformServerColumns, id, name, transport, schemaJSON, sealedConfig,
	).Scan(&s.ID, &s.Name, &s.Transport, &outSchema, &s.CreatedAt, &s.UpdatedAt)
	if store.IsUniqueViolation(err) {
		httpapi.WriteError(w, http.StatusConflict, "name already in use by another server", "name_conflict")
		return
	}
	if checkErr(w, err) {
		return
	}
	s.CredentialSchema = unmarshalCredentialSchema(outSchema)
	audit := httpAudit(r, actor, "deployment_mcp_server.update")
	audit.TargetType, audit.TargetID = ptr("platform_mcp_server"), ptr(id)
	if afterHash != "" {
		audit.AfterHash = ptr(afterHash)
	}
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, s)
}

func (d *Deps) handleDeletePlatformMcpServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("serverId")
	if _, err := getPlatformServer(r.Context(), d.DB, id); errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	} else if err != nil {
		internalError(w)
		return
	}
	err := d.DB.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM space_mcp_servers WHERE wmcp_platform_server_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `DELETE FROM platform_mcp_servers WHERE id = $1`, id)
		return err
	})
	if checkErr(w, err) {
		return
	}
	audit := httpAudit(r, actor, "deployment_mcp_server.delete")
	audit.TargetType, audit.TargetID = ptr("platform_mcp_server"), ptr(id)
	_ = WriteAudit(r.Context(), d.DB, audit)
	w.WriteHeader(http.StatusNoContent)
}

// --- /api/deployment-mcp-servers (вид и включение из воркспейса) -------------

func (d *Deps) handleListWorkspaceDeploymentMcpServers(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT p.id, p.pmcp_name, p.pmcp_transport, p.pmcp_credential_schema, p.created_at, p.updated_at,
			COALESCE(w.wmcp_enabled, false)
		FROM platform_mcp_servers p
		LEFT JOIN space_mcp_servers w ON w.wmcp_platform_server_id = p.id AND w.workspace_id = $1
		ORDER BY p.pmcp_name`, workspaceID)
	if checkErr(w, err) {
		return
	}
	defer rows.Close()
	out := []PlatformMcpServer{}
	for rows.Next() {
		var s PlatformMcpServer
		var schemaRaw []byte
		var enabled bool
		if err := rows.Scan(&s.ID, &s.Name, &s.Transport, &schemaRaw, &s.CreatedAt, &s.UpdatedAt, &enabled); err != nil {
			internalError(w)
			return
		}
		s.CredentialSchema = unmarshalCredentialSchema(schemaRaw)
		s.Enabled = &enabled
		out = append(out, s)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleSetWorkspaceDeploymentMcpServerEnabled(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	role, memberOK, err := d.Workspace.HTTPAPIMembership().MemberRole(r.Context(), workspaceID, actor.UserID)
	if checkErr(w, err) {
		return
	}
	if !memberOK || !httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "owner/admin required")
		return
	}
	serverID := r.PathValue("serverId")
	platform, err := getPlatformServer(r.Context(), d.DB, serverID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil || body.Enabled == nil {
		httpapi.BadRequest(w, "enabled is required")
		return
	}
	schemaJSON, _ := json.Marshal(platform.CredentialSchema)
	err = d.DB.WithTx(r.Context(), func(tx pgx.Tx) error {
		var rowID string
		scanErr := tx.QueryRow(r.Context(), `
			INSERT INTO space_mcp_servers (workspace_id, wmcp_name, wmcp_transport, wmcp_source,
				wmcp_platform_server_id, wmcp_credential_schema, wmcp_enabled)
			VALUES ($1, $2, $3, 'deployment', $4, $5, $6)
			ON CONFLICT (workspace_id, wmcp_name) DO UPDATE SET
				wmcp_enabled = EXCLUDED.wmcp_enabled, wmcp_platform_server_id = EXCLUDED.wmcp_platform_server_id,
				wmcp_credential_schema = EXCLUDED.wmcp_credential_schema, updated_at = now()
			RETURNING id`, workspaceID, platform.Name, platform.Transport, serverID, schemaJSON, *body.Enabled,
		).Scan(&rowID)
		if scanErr != nil {
			return scanErr
		}
		if !*body.Enabled {
			if _, err := tx.Exec(r.Context(), `DELETE FROM operative_mcp_links WHERE space_mcp_server_id = $1`, rowID); err != nil {
				return err
			}
		}
		return nil
	})
	if checkErr(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
