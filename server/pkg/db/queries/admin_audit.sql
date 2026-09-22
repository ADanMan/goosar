
-- name: InsertAdminAudit :one
INSERT INTO admin_audit (
    actor_user_id,
    action,
    target_type,
    target_id,
    before_hash,
    after_hash,
    request_id
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListAdminAudit :many
SELECT * FROM admin_audit
ORDER BY created_at DESC, id DESC
LIMIT $1;

-- name: ListAdminAuditRecent :many
SELECT * FROM admin_audit
WHERE (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('actor')::uuid IS NULL OR actor_user_id = sqlc.narg('actor')::uuid)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at > sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at <= sqlc.narg('until')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim');

-- name: ListAdminAuditForward :many
SELECT * FROM admin_audit
WHERE (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('actor')::uuid IS NULL OR actor_user_id = sqlc.narg('actor')::uuid)
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at <= sqlc.narg('until')::timestamptz)
  AND (created_at, id) > (sqlc.arg('cursor_at')::timestamptz, sqlc.arg('cursor_id')::uuid)
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('lim');

-- name: PurgeAdminAuditBefore :execrows
DELETE FROM admin_audit WHERE created_at < sqlc.arg('cutoff');
