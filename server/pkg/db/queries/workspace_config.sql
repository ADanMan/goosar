
-- name: GetWorkspaceConfig :one
SELECT * FROM workspace_config
WHERE workspace_id = $1;

-- name: UpsertWorkspaceConfig :one
INSERT INTO workspace_config (
    workspace_id,
    llm_base_url,
    llm_model,
    llm_api_key,
    mcp_defaults,
    updated_by
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id)
DO UPDATE SET
    llm_base_url = EXCLUDED.llm_base_url,
    llm_model    = EXCLUDED.llm_model,
    llm_api_key  = EXCLUDED.llm_api_key,
    mcp_defaults = EXCLUDED.mcp_defaults,
    updated_at   = now(),
    updated_by   = EXCLUDED.updated_by
RETURNING *;

-- name: GetUserConfigOverride :one
SELECT * FROM user_config_override
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertUserConfigOverride :one
INSERT INTO user_config_override (
    workspace_id,
    user_id,
    llm_base_url,
    llm_model,
    llm_api_key,
    mcp_overrides,
    updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET
    llm_base_url  = EXCLUDED.llm_base_url,
    llm_model     = EXCLUDED.llm_model,
    llm_api_key   = EXCLUDED.llm_api_key,
    mcp_overrides = EXCLUDED.mcp_overrides,
    updated_at    = now(),
    updated_by    = EXCLUDED.updated_by
RETURNING *;

-- name: DeleteUserConfigOverride :execrows
DELETE FROM user_config_override
WHERE workspace_id = $1 AND user_id = $2;

-- name: GetDeploymentPolicy :one
SELECT * FROM deployment_policy
WHERE singleton = true;

-- name: SetDeploymentPolicy :one
INSERT INTO deployment_policy (singleton, policy, updated_by)
VALUES (true, $1, $2)
ON CONFLICT (singleton)
DO UPDATE SET
    policy     = EXCLUDED.policy,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: LockConfigLayerKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: ListUserConfigOverrides :many
SELECT * FROM user_config_override
WHERE workspace_id = $1
ORDER BY user_id ASC;
