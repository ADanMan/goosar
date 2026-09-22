-- name: ListWorkspaceMcpServers :many
SELECT s.id, s.workspace_id, s.name, s.transport, s.credential_schema,
       s.created_by, s.created_at, s.updated_at,
       COALESCE(c.value_keys, ARRAY[]::TEXT[])::TEXT[] AS provided_keys
FROM workspace_mcp_server s
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = s.id AND c.user_id = $2
WHERE s.workspace_id = $1
ORDER BY s.name ASC;

-- name: LockWorkspaceMcpServerForShare :one
SELECT id FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: LockWorkspaceMcpServerForUpdate :one
SELECT id FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateWorkspaceMcpServer :one
INSERT INTO workspace_mcp_server (workspace_id, name, config, transport, credential_schema, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, name, transport, credential_schema, created_by, created_at, updated_at;

-- name: UpdateWorkspaceMcpServer :one
UPDATE workspace_mcp_server SET
    name = COALESCE(sqlc.narg('name'), name),
    config = COALESCE(sqlc.narg('config'), config),
    transport = COALESCE(sqlc.narg('transport'), transport),
    credential_schema = COALESCE(sqlc.narg('credential_schema'), credential_schema),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, transport, credential_schema, created_by, created_at, updated_at;

-- name: DeleteWorkspaceMcpServer :execrows
DELETE FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteAgentMcpServersByServer :exec
DELETE FROM agent_mcp_server WHERE server_id = $1;

-- name: DeleteWorkspaceMcpUserCredentialsByServer :exec
DELETE FROM workspace_mcp_user_credential WHERE server_id = $1;

-- name: ListAgentMcpServers :many
SELECT s.id, s.workspace_id, s.name, s.transport, s.credential_schema,
       s.created_at, s.updated_at, ams.enabled,
       'workspace'::text AS source,
       COALESCE(c.value_keys, ARRAY[]::TEXT[])::TEXT[] AS provided_keys
FROM workspace_mcp_server s
JOIN agent_mcp_server ams ON ams.server_id = s.id
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = s.id AND c.user_id = $2
WHERE ams.agent_id = $1
UNION ALL
SELECT d.id, sqlc.arg(workspace_id)::uuid AS workspace_id, d.name, d.transport, d.credential_schema,
       d.created_at, d.updated_at, ams.enabled,
       'deployment'::text AS source,
       COALESCE(c.value_keys, ARRAY[]::TEXT[])::TEXT[] AS provided_keys
FROM deployment_mcp_server d
JOIN agent_mcp_server ams ON ams.server_id = d.id
JOIN workspace_deployment_mcp_server w
     ON w.server_id = d.id AND w.workspace_id = sqlc.arg(workspace_id)
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = d.id AND c.user_id = $2
WHERE ams.agent_id = $1
ORDER BY name ASC;

-- name: ListEnabledAgentMcpServers :many
SELECT s.name, s.config, s.credential_schema, c.sealed_values, 'workspace'::text AS source
FROM workspace_mcp_server s
JOIN agent_mcp_server ams ON ams.server_id = s.id
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = s.id AND c.user_id = $2
WHERE ams.agent_id = $1 AND ams.enabled = TRUE
UNION ALL
SELECT d.name, d.config, d.credential_schema, c.sealed_values, 'deployment'::text AS source
FROM deployment_mcp_server d
JOIN agent_mcp_server ams ON ams.server_id = d.id
JOIN workspace_deployment_mcp_server w
     ON w.server_id = d.id AND w.workspace_id = sqlc.arg(workspace_id)
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = d.id AND c.user_id = $2
WHERE ams.agent_id = $1 AND ams.enabled = TRUE
ORDER BY name ASC, source DESC;

-- name: AddAgentMcpServer :exec
INSERT INTO agent_mcp_server (agent_id, server_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: SetAgentMcpServerEnabled :execrows
UPDATE agent_mcp_server
SET enabled = $3
WHERE agent_id = $1 AND server_id = $2;

-- name: RemoveAgentMcpServer :execrows
DELETE FROM agent_mcp_server
WHERE agent_id = $1 AND server_id = $2;

-- name: DeleteAgentMcpServersByArchivedRuntimeAgents :exec
DELETE FROM agent_mcp_server
WHERE agent_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
);

-- name: UpsertWorkspaceMcpUserCredential :exec
INSERT INTO workspace_mcp_user_credential (server_id, user_id, workspace_id, sealed_values, value_keys)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (server_id, user_id) DO UPDATE SET
    sealed_values = EXCLUDED.sealed_values,
    value_keys = EXCLUDED.value_keys,
    updated_at = now();

-- name: DeleteWorkspaceMcpUserCredential :execrows
DELETE FROM workspace_mcp_user_credential
WHERE server_id = $1 AND user_id = $2;

-- name: GetWorkspaceMcpServer :one
SELECT id, workspace_id, name, transport, credential_schema, created_by, created_at, updated_at
FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: GetWorkspaceMcpUserCredentialKeys :one
SELECT value_keys
FROM workspace_mcp_user_credential
WHERE server_id = $1 AND user_id = $2;

-- name: GetWorkspaceMcpUserCredentialValues :one
SELECT sealed_values
FROM workspace_mcp_user_credential
WHERE server_id = $1 AND user_id = $2;
