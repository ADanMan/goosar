
-- name: ListEnabledWorkspaceTemplates :many
SELECT * FROM workspace_template
WHERE enabled = true
ORDER BY position, key;

-- name: GetEnabledWorkspaceTemplate :one
SELECT * FROM workspace_template
WHERE key = $1 AND enabled = true;

-- name: CreateWorkspaceHelperDefault :one
INSERT INTO workspace_helper_default (
    workspace_id,
    template_key,
    helper_name,
    helper_extra_instructions
) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWorkspaceHelperDefault :one
SELECT * FROM workspace_helper_default
WHERE workspace_id = $1;

-- name: GetWorkspaceByTemplateKey :one
SELECT * FROM workspace
WHERE template_key = $1;

-- name: CreateRoleWorkspace :one
INSERT INTO workspace (name, slug, description, context, issue_prefix, template_key, open_join)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;
