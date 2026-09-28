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

// --- space_mcp_servers (собственные серверы воркспейса + видимость библиотеки) ---

const workspaceServerColumns = `id, workspace_id, wmcp_name, wmcp_transport, wmcp_source, wmcp_enabled,
	wmcp_credential_schema, created_at, updated_at`

func scanWorkspaceServer(row pgx.Row) (WorkspaceMcpServer, error) {
	var s WorkspaceMcpServer
	var enabled bool
	var schemaRaw []byte
	if err := row.Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Transport, &s.Source, &enabled,
		&schemaRaw, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return WorkspaceMcpServer{}, err
	}
	_ = json.Unmarshal(schemaRaw, &s.CredentialSchema)
	if s.Source == "deployment" {
		s.Enabled = &enabled
	}
	return s, nil
}

func getWorkspaceServer(ctx context.Context, db *store.Store, workspaceID, id string) (WorkspaceMcpServer, error) {
	row := db.Pool.QueryRow(ctx, `SELECT `+workspaceServerColumns+` FROM space_mcp_servers WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	s, err := scanWorkspaceServer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceMcpServer{}, ErrNotFound
	}
	if err != nil {
		return WorkspaceMcpServer{}, fmt.Errorf("deployment: чтение MCP-сервера воркспейса: %w", err)
	}
	return s, nil
}

// credentialsFor заполняет provided/missing_credentials для callerID.
func (d *Deps) fillCredentials(ctx context.Context, s *WorkspaceMcpServer, callerID string) error {
	provided := map[string]bool{}
	if len(s.CredentialSchema) > 0 {
		rows, err := d.DB.Pool.Query(ctx, `
			SELECT wmcpc_field_key FROM space_mcp_credentials
			WHERE space_mcp_server_id = $1 AND account_id = $2`, s.ID, callerID)
		if err != nil {
			return fmt.Errorf("deployment: чтение кредов сервера: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			provided[k] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	s.ProvidedCredentials = []string{}
	s.MissingCredentials = []string{}
	for _, f := range s.CredentialSchema {
		if provided[f.Key] {
			s.ProvidedCredentials = append(s.ProvidedCredentials, f.Key)
		} else if f.Required {
			s.MissingCredentials = append(s.MissingCredentials, f.Key)
		}
	}
	return nil
}

func (d *Deps) handleListWorkspaceMcpServers(w http.ResponseWriter, r *http.Request) {
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
		SELECT `+workspaceServerColumns+` FROM space_mcp_servers
		WHERE workspace_id = $1 AND (wmcp_source = 'workspace' OR wmcp_enabled)
		ORDER BY wmcp_name`, workspaceID)
	if checkErr(w, err) {
		return
	}
	out := []WorkspaceMcpServer{}
	for rows.Next() {
		s, err := scanWorkspaceServer(rows)
		if err != nil {
			rows.Close()
			internalError(w)
			return
		}
		out = append(out, s)
	}
	rows.Close()
	for i := range out {
		if err := d.fillCredentials(r.Context(), &out[i], actor.UserID); err != nil {
			internalError(w)
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

// requireWorkspaceOwnerAdmin — общий пролог мутирующих ручек
// workspace-mcp-servers (не workspace-config: там своя, чуть иначе устроенная
// логика в config.go).
func (d *Deps) requireWorkspaceOwnerAdmin(w http.ResponseWriter, r *http.Request) (workspaceID string, actor *httpapi.Actor, ok bool) {
	a, aok := httpapi.RequireHuman(w, r)
	if !aok {
		return "", nil, false
	}
	wsID, wok := d.resolveWorkspaceID(r, a)
	if !wok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return "", nil, false
	}
	role, memberOK, err := d.Workspace.HTTPAPIMembership().MemberRole(r.Context(), wsID, a.UserID)
	if err != nil {
		internalError(w)
		return "", nil, false
	}
	if !memberOK || !httpapi.RoleAtLeast(role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "owner/admin required")
		return "", nil, false
	}
	return wsID, a, true
}

func (d *Deps) handleCreateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := d.requireWorkspaceOwnerAdmin(w, r)
	if !ok {
		return
	}
	if !seal.Available(d.Config.McpSecretKey) {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "GOOSAR_MCP_SECRET_KEY is not configured", "encryption_unavailable")
		return
	}
	var req WorkspaceMcpServerRequest
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
	if _, err := d.Workspace.GetWorkspace(r.Context(), workspaceID); err != nil {
		httpapi.NotFound(w, "workspace not found")
		return
	}
	schemaJSON, _ := json.Marshal(req.CredentialSchema)
	sealed, err := seal.SealJSON(d.Config.McpSecretKey, req.Config)
	if err != nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "encryption unavailable", "encryption_unavailable")
		return
	}
	var s WorkspaceMcpServer
	var outSchema []byte
	var enabledUnused bool
	err = d.DB.Pool.QueryRow(r.Context(), `
		INSERT INTO space_mcp_servers (workspace_id, wmcp_name, wmcp_transport, wmcp_source, wmcp_config_sealed, wmcp_credential_schema)
		VALUES ($1, $2, $3, 'workspace', $4, $5)
		RETURNING `+workspaceServerColumns, workspaceID, req.Name, requestTransport(req.Config), sealed, schemaJSON,
	).Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Transport, &s.Source, &enabledUnused, &outSchema, &s.CreatedAt, &s.UpdatedAt)
	if store.IsUniqueViolation(err) {
		httpapi.WriteError(w, http.StatusConflict, "name already in use in this workspace", "name_conflict")
		return
	}
	if checkErr(w, err) {
		return
	}
	_ = json.Unmarshal(outSchema, &s.CredentialSchema)
	s.ProvidedCredentials, s.MissingCredentials = []string{}, []string{}
	for _, f := range s.CredentialSchema {
		if f.Required {
			s.MissingCredentials = append(s.MissingCredentials, f.Key)
		}
	}
	httpapi.WriteJSON(w, http.StatusCreated, s)
}

func (d *Deps) handleUpdateWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := d.requireWorkspaceOwnerAdmin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("serverId")
	existing, err := getWorkspaceServer(r.Context(), d.DB, workspaceID, id)
	if errors.Is(err, ErrNotFound) || (err == nil && existing.Source != "workspace") {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	var req WorkspaceMcpServerRequest
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
	}
	schemaJSON, _ := json.Marshal(credSchema)
	var s WorkspaceMcpServer
	var outSchema []byte
	var enabledUnused bool
	err = d.DB.Pool.QueryRow(r.Context(), `
		UPDATE space_mcp_servers SET
			wmcp_name = $3, wmcp_transport = $4, wmcp_credential_schema = $5,
			wmcp_config_sealed = COALESCE($6, wmcp_config_sealed), updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+workspaceServerColumns, workspaceID, id, name, transport, schemaJSON, sealedConfig,
	).Scan(&s.ID, &s.WorkspaceID, &s.Name, &s.Transport, &s.Source, &enabledUnused, &outSchema, &s.CreatedAt, &s.UpdatedAt)
	if store.IsUniqueViolation(err) {
		httpapi.WriteError(w, http.StatusConflict, "name already in use by another server", "name_conflict")
		return
	}
	if checkErr(w, err) {
		return
	}
	_ = json.Unmarshal(outSchema, &s.CredentialSchema)
	if err := d.fillCredentials(r.Context(), &s, actorUserIDFromRequest(r)); err != nil {
		internalError(w)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, s)
}

func actorUserIDFromRequest(r *http.Request) string {
	if a, ok := httpapi.ActorFrom(r.Context()); ok {
		return a.UserID
	}
	return ""
}

func (d *Deps) handleDeleteWorkspaceMcpServer(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := d.requireWorkspaceOwnerAdmin(w, r)
	if !ok {
		return
	}
	id := r.PathValue("serverId")
	existing, err := getWorkspaceServer(r.Context(), d.DB, workspaceID, id)
	if errors.Is(err, ErrNotFound) || (err == nil && existing.Source != "workspace") {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	if _, err := d.DB.Pool.Exec(r.Context(), `DELETE FROM space_mcp_servers WHERE workspace_id = $1 AND id = $2`, workspaceID, id); err != nil {
		internalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- персональные значения credential-полей -----------------------------------

func (d *Deps) handleSetWorkspaceMcpCredentials(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	serverID := r.PathValue("serverId")
	server, err := getWorkspaceServer(r.Context(), d.DB, workspaceID, serverID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	if len(server.CredentialSchema) == 0 {
		httpapi.BadRequest(w, "server does not request credential fields")
		return
	}
	var req SetCredentialsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	allowed := map[string]bool{}
	for _, f := range server.CredentialSchema {
		allowed[f.Key] = true
	}
	for k, v := range req.Values {
		if !allowed[k] {
			httpapi.BadRequest(w, "unknown credential key: "+k)
			return
		}
		if len(v) > maxCredValueLen {
			httpapi.BadRequest(w, "credential value too long: "+k)
			return
		}
	}
	if !seal.Available(d.Config.McpSecretKey) {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "GOOSAR_MCP_SECRET_KEY is not configured", "encryption_unavailable")
		return
	}
	err = d.DB.WithTx(r.Context(), func(tx pgx.Tx) error {
		for k, v := range req.Values {
			if v == "" {
				if _, err := tx.Exec(r.Context(), `
					DELETE FROM space_mcp_credentials WHERE space_mcp_server_id = $1 AND account_id = $2 AND wmcpc_field_key = $3`,
					serverID, actor.UserID, k); err != nil {
					return err
				}
				continue
			}
			sealed, err := seal.Seal(d.Config.McpSecretKey, []byte(v))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO space_mcp_credentials (space_mcp_server_id, account_id, wmcpc_field_key, wmcpc_value_sealed)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (space_mcp_server_id, account_id, wmcpc_field_key)
				DO UPDATE SET wmcpc_value_sealed = EXCLUDED.wmcpc_value_sealed, updated_at = now()`,
				serverID, actor.UserID, k, sealed); err != nil {
				return err
			}
		}
		return nil
	})
	if checkErr(w, err) {
		return
	}
	if err := d.fillCredentials(r.Context(), &server, actor.UserID); err != nil {
		internalError(w)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, server)
}

func (d *Deps) handleDeleteWorkspaceMcpCredentials(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	workspaceID, ok := d.resolveWorkspaceID(r, actor)
	if !ok {
		httpapi.BadRequest(w, "workspace_id/slug is required")
		return
	}
	serverID := r.PathValue("serverId")
	server, err := getWorkspaceServer(r.Context(), d.DB, workspaceID, serverID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	if _, err := d.DB.Pool.Exec(r.Context(), `
		DELETE FROM space_mcp_credentials WHERE space_mcp_server_id = $1 AND account_id = $2`, serverID, actor.UserID); err != nil {
		internalError(w)
		return
	}
	if err := d.fillCredentials(r.Context(), &server, actor.UserID); err != nil {
		internalError(w)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, server)
}
