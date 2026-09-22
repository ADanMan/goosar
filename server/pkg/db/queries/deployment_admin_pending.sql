
-- name: InsertDeploymentAdminPending :one
INSERT INTO deployment_admin_pending (action, target_user_id, requested_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDeploymentAdminPending :one
SELECT * FROM deployment_admin_pending
WHERE id = $1;

-- name: GetDeploymentAdminPendingByActionTarget :one
SELECT * FROM deployment_admin_pending
WHERE action = $1 AND target_user_id = $2
ORDER BY requested_at ASC, id ASC
LIMIT 1;

-- name: ListDeploymentAdminPending :many
SELECT
    p.id,
    p.action,
    p.target_user_id,
    p.requested_by,
    p.requested_at,
    u.email AS target_email,
    u.name AS target_name
FROM deployment_admin_pending p
LEFT JOIN "user" u ON u.id = p.target_user_id
ORDER BY p.requested_at ASC, p.id ASC;

-- name: DeleteDeploymentAdminPending :execrows
DELETE FROM deployment_admin_pending
WHERE id = $1;
