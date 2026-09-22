
-- name: InsertAuthAudit :one
INSERT INTO auth_audit (
    actor_type,
    actor_id,
    actor_role,
    action,
    target_type,
    target_id,
    outcome,
    reason,
    workspace_id,
    request_id,
    client_ip,
    user_agent
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListAuthAuditRecent :many
SELECT * FROM auth_audit
WHERE (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('actor')::text IS NULL OR actor_id = sqlc.narg('actor')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at > sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at <= sqlc.narg('until')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim');

-- name: ListAuthAuditForward :many
SELECT * FROM auth_audit
WHERE (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('actor')::text IS NULL OR actor_id = sqlc.narg('actor')::text)
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at <= sqlc.narg('until')::timestamptz)
  AND (created_at, id) > (sqlc.arg('cursor_at')::timestamptz, sqlc.arg('cursor_id')::uuid)
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('lim');

-- name: PurgeAuthAuditBefore :execrows
DELETE FROM auth_audit WHERE created_at < sqlc.arg('cutoff');
