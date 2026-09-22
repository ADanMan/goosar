-- name: CreateDaemonToken :one
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetDaemonTokenByHash :one
SELECT * FROM daemon_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteDaemonTokensByWorkspaceAndDaemons :many
DELETE FROM daemon_token
WHERE workspace_id = @workspace_id
  AND daemon_id = ANY(@daemon_ids::text[])
RETURNING token_hash;

-- name: DeleteExpiredDaemonTokens :exec
DELETE FROM daemon_token
WHERE expires_at <= now();

-- name: DeleteDaemonTokensByRuntimeOwner :many
DELETE FROM daemon_token
WHERE (workspace_id, daemon_id) IN (
    SELECT workspace_id, daemon_id FROM agent_runtime
    WHERE owner_id = @owner_id
      AND daemon_id IS NOT NULL
      AND daemon_id <> ''
)
RETURNING token_hash;
