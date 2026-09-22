
-- name: ListDeploymentAdmins :many
SELECT
    da.user_id,
    da.granted_by,
    da.granted_at,
    u.email,
    u.name
FROM deployment_admin da
LEFT JOIN "user" u ON u.id = da.user_id
ORDER BY da.granted_at ASC, da.user_id ASC;

-- name: GetDeploymentAdmin :one
SELECT * FROM deployment_admin
WHERE user_id = $1;

-- name: InsertDeploymentAdmin :execrows
INSERT INTO deployment_admin (user_id, granted_by)
VALUES ($1, $2)
ON CONFLICT (user_id) DO NOTHING;

-- name: DeleteDeploymentAdmin :execrows
DELETE FROM deployment_admin
WHERE user_id = $1;

-- name: CountDeploymentAdmins :one
SELECT count(*) FROM deployment_admin;
