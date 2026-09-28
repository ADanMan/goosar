package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrMcpServerNotFound — сервер не зарегистрирован в этом воркспейсе (404).
var ErrMcpServerNotFound = errors.New("agent: mcp-сервер не найден в воркспейсе")

// ErrMcpServerNotAttached — сервер не привязан к этому агенту (404).
var ErrMcpServerNotAttached = errors.New("agent: mcp-сервер не привязан к агенту")

const mcpServerColumns = `s.id, s.workspace_id, s.wmcp_name, s.wmcp_transport, s.wmcp_source,
	s.wmcp_credential_schema, l.opml_enabled, s.created_at, s.updated_at`

func scanMcpServer(row pgx.Row) (McpServer, error) {
	var m McpServer
	var cred []byte
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.Name, &m.Transport, &m.Source, &cred, &m.Enabled,
		&m.CreatedAt, &m.UpdatedAt); err != nil {
		return McpServer{}, err
	}
	m.CredentialSchema = rawOr(cred, emptyArray())
	m.ProvidedKeys = []string{}
	return m, nil
}

// ListAgentMcpServers — общие MCP-серверы воркспейса, привязанные к агенту.
func (s *Store) ListAgentMcpServers(ctx context.Context, agentID string) ([]McpServer, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+mcpServerColumns+`
		FROM operative_mcp_links l JOIN space_mcp_servers s ON s.id = l.space_mcp_server_id
		WHERE l.operative_id = $1 ORDER BY l.created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent: mcp-серверы агента: %w", err)
	}
	defer rows.Close()
	out := []McpServer{}
	for rows.Next() {
		m, err := scanMcpServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.fillProvidedKeys(ctx, agentID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillProvidedKeys — какие поля credential_schema уже имеют сохранённое
// значение для владельца агента (space_mcp_credentials.account_id); контракт
// не уточняет, "чьи" provided_keys показывать в контексте агента — решение:
// владельца агента (тот, чей ключ реально используется при запуске).
func (s *Store) fillProvidedKeys(ctx context.Context, agentID string, servers []McpServer) error {
	if len(servers) == 0 {
		return nil
	}
	var ownerID *string
	if err := s.db.Pool.QueryRow(ctx, `SELECT op_owner_account_id FROM operatives WHERE id = $1`, agentID).Scan(&ownerID); err != nil {
		return fmt.Errorf("agent: владелец агента для provided_keys: %w", err)
	}
	if ownerID == nil {
		return nil
	}
	for i := range servers {
		rows, err := s.db.Pool.Query(ctx, `SELECT wmcpc_field_key FROM space_mcp_credentials
			WHERE space_mcp_server_id = $1 AND account_id = $2`, servers[i].ID, *ownerID)
		if err != nil {
			return fmt.Errorf("agent: provided_keys: %w", err)
		}
		keys := []string{}
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, k)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		servers[i].ProvidedKeys = keys
	}
	return nil
}

// AddAgentMcpServer — привязать (addAgentMcpServer).
func (s *Store) AddAgentMcpServer(ctx context.Context, workspaceID, agentID, serverID string) ([]McpServer, error) {
	exists, err := s.db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM space_mcp_servers WHERE workspace_id = $1 AND id = $2)`,
		workspaceID, serverID)
	if err != nil {
		return nil, fmt.Errorf("agent: проверка mcp-сервера: %w", err)
	}
	if !exists {
		return nil, ErrMcpServerNotFound
	}
	if _, err := s.db.Pool.Exec(ctx, `INSERT INTO operative_mcp_links (operative_id, space_mcp_server_id, opml_enabled)
		VALUES ($1,$2,true) ON CONFLICT (operative_id, space_mcp_server_id) DO NOTHING`, agentID, serverID); err != nil {
		return nil, fmt.Errorf("agent: привязка mcp-сервера: %w", err)
	}
	return s.ListAgentMcpServers(ctx, agentID)
}

// SetAgentMcpServerEnabled — вкл/выкл привязку.
func (s *Store) SetAgentMcpServerEnabled(ctx context.Context, agentID, serverID string, enabled bool) ([]McpServer, error) {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE operative_mcp_links SET opml_enabled = $3, updated_at = now()
		WHERE operative_id = $1 AND space_mcp_server_id = $2`, agentID, serverID, enabled)
	if err != nil {
		return nil, fmt.Errorf("agent: переключение mcp-сервера: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrMcpServerNotAttached
	}
	return s.ListAgentMcpServers(ctx, agentID)
}

// RemoveAgentMcpServer — отвязать.
func (s *Store) RemoveAgentMcpServer(ctx context.Context, agentID, serverID string) ([]McpServer, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM operative_mcp_links WHERE operative_id = $1 AND space_mcp_server_id = $2`,
		agentID, serverID)
	if err != nil {
		return nil, fmt.Errorf("agent: отвязка mcp-сервера: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrMcpServerNotAttached
	}
	return s.ListAgentMcpServers(ctx, agentID)
}
