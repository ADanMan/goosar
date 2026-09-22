
-- name: CreateUserSession :one
INSERT INTO user_session (user_id, user_agent, ip_hash)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserSession :one
SELECT * FROM user_session WHERE id = $1;

-- name: TouchUserSession :exec
UPDATE user_session SET last_seen_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: ListUserSessions :many
SELECT * FROM user_session
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY last_seen_at DESC;

-- name: RevokeUserSession :execrows
UPDATE user_session SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :execrows
UPDATE user_session SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: RevokeOldestUserSessions :execrows
UPDATE user_session SET revoked_at = now()
WHERE revoked_at IS NULL AND id IN (
    SELECT keep.id FROM user_session AS keep
    WHERE keep.user_id = $1 AND keep.revoked_at IS NULL
    ORDER BY keep.created_at DESC
    OFFSET $2
);

-- name: DeleteUserSessions :exec
DELETE FROM user_session WHERE user_id = $1;
