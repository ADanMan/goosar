-- name: ListIssueProperties :many
SELECT p.*,
    (
        SELECT COUNT(*) FROM issue i
        WHERE i.workspace_id = p.workspace_id
          AND i.properties ? p.id::text
    )::bigint AS usage_count
FROM issue_property p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR p.archived_at IS NULL)
ORDER BY p.position ASC, LOWER(p.name) ASC;

-- name: GetIssueProperty :one
SELECT * FROM issue_property
WHERE id = $1 AND workspace_id = $2;

-- name: CountActiveIssueProperties :one
SELECT COUNT(*) FROM issue_property
WHERE workspace_id = $1 AND archived_at IS NULL;

-- name: CreateIssueProperty :one
INSERT INTO issue_property (workspace_id, name, type, description, icon, config, position)
SELECT sqlc.arg('workspace_id')::uuid,
       sqlc.arg('name')::text,
       sqlc.arg('type')::text,
       sqlc.arg('description')::text,
       sqlc.arg('icon')::text,
       sqlc.arg('config')::jsonb,
       COALESCE((SELECT MAX(position) FROM issue_property WHERE workspace_id = sqlc.arg('workspace_id')::uuid), 0) + 1
RETURNING *;

-- name: UpdateIssueProperty :one
UPDATE issue_property SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    config = COALESCE(sqlc.narg('config'), config),
    archived_at = CASE WHEN sqlc.arg('archived_set')::bool THEN sqlc.narg('archived_at') ELSE archived_at END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetIssuePropertyValue :one
UPDATE issue
SET properties = jsonb_set(properties, ARRAY[sqlc.arg('key')::text], sqlc.arg('value')::jsonb, true),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: DeleteIssuePropertyValue :one
UPDATE issue
SET properties = properties - sqlc.arg('key')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: CountIssuesUsingPropertyOptions :many
SELECT opt::text AS option_id, COUNT(i.id) AS usage_count
FROM unnest(sqlc.arg('option_ids')::text[]) AS opt
LEFT JOIN issue i
  ON i.workspace_id = sqlc.arg('workspace_id')::uuid
 AND (i.properties -> sqlc.arg('property_key')::text) ? opt
GROUP BY opt
HAVING COUNT(i.id) > 0;
