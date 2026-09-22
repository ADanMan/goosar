
-- name: GetUserIdentity :one
SELECT * FROM user_identity
WHERE provider = $1 AND subject = $2;

-- name: LinkUserIdentity :one
INSERT INTO user_identity (user_id, provider, subject, email_at_link)
VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, subject) DO UPDATE
SET email_at_link = EXCLUDED.email_at_link,
    last_login_at = now()
RETURNING *;

-- name: ListUserIdentitiesForUser :many
SELECT * FROM user_identity
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: DropUserIdentitiesForUser :exec
DELETE FROM user_identity
WHERE user_id = $1;
