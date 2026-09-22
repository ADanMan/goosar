
-- name: ListDeploymentMcpServers :many
SELECT s.id, s.name, s.transport, s.credential_schema, s.created_by, s.created_at, s.updated_at,
       (SELECT count(*) FROM workspace_deployment_mcp_server w WHERE w.server_id = s.id) AS enabled_workspaces
FROM deployment_mcp_server s
ORDER BY s.name ASC;

-- name: CreateDeploymentMcpServer :one
INSERT INTO deployment_mcp_server (name, config, transport, credential_schema, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, transport, credential_schema, created_by, created_at, updated_at;

-- name: UpdateDeploymentMcpServer :one
UPDATE deployment_mcp_server SET
    name = COALESCE(sqlc.narg('name'), name),
    config = COALESCE(sqlc.narg('config'), config),
    transport = COALESCE(sqlc.narg('transport'), transport),
    credential_schema = COALESCE(sqlc.narg('credential_schema'), credential_schema),
    updated_at = now()
WHERE id = $1
RETURNING id, name, transport, credential_schema, created_by, created_at, updated_at;

-- name: DeleteDeploymentMcpServer :execrows
DELETE FROM deployment_mcp_server WHERE id = $1;

-- name: LockDeploymentMcpServerForUpdate :one
SELECT id, transport, credential_schema FROM deployment_mcp_server WHERE id = $1 FOR UPDATE;

-- name: LockDeploymentMcpServerForShare :one
SELECT id FROM deployment_mcp_server WHERE id = $1 FOR SHARE;

-- name: DeleteWorkspaceDeploymentMcpServersByServer :exec
DELETE FROM workspace_deployment_mcp_server WHERE server_id = $1;

-- name: ListDeploymentMcpServersForWorkspace :many
SELECT s.id, s.name, s.transport, s.credential_schema, s.created_at, s.updated_at,
       (w.server_id IS NOT NULL)::boolean AS enabled,
       COALESCE(c.value_keys, ARRAY[]::TEXT[])::TEXT[] AS provided_keys
FROM deployment_mcp_server s
LEFT JOIN workspace_deployment_mcp_server w
       ON w.server_id = s.id AND w.workspace_id = $1
LEFT JOIN workspace_mcp_user_credential c
       ON c.server_id = s.id AND c.user_id = $2
ORDER BY s.name ASC;

-- name: EnableDeploymentMcpServerForWorkspace :exec
INSERT INTO workspace_deployment_mcp_server (workspace_id, server_id, enabled_by)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: DisableDeploymentMcpServerForWorkspace :execrows
DELETE FROM workspace_deployment_mcp_server
WHERE workspace_id = $1 AND server_id = $2;

-- name: GetEnabledDeploymentMcpServerForWorkspace :one
SELECT s.id, s.name, s.transport, s.credential_schema, s.created_at, s.updated_at
FROM deployment_mcp_server s
JOIN workspace_deployment_mcp_server w ON w.server_id = s.id
WHERE s.id = $1 AND w.workspace_id = $2
FOR SHARE OF s;

-- name: DeleteAgentMcpServersByServerAndWorkspace :exec
DELETE FROM agent_mcp_server
WHERE server_id = $1
  AND agent_id IN (SELECT id FROM agent WHERE workspace_id = $2);

-- name: GetDeploymentMcpServerByName :one
SELECT id, name, config, transport, credential_schema, created_by, created_at, updated_at
FROM deployment_mcp_server
WHERE name = $1;

-- name: ListEnabledDeploymentMcpServerConfigsForUser :many
SELECT DISTINCT s.id, s.name, s.config, s.transport
FROM deployment_mcp_server s
JOIN workspace_deployment_mcp_server w ON w.server_id = s.id
JOIN member m ON m.workspace_id = w.workspace_id
WHERE m.user_id = $1
ORDER BY s.name ASC;
