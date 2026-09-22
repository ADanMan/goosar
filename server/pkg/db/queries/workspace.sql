-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.issue_prefix, w.issue_counter, w.avatar_url, w.attribution_fail_closed,
       w.template_key, w.open_join
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: ListDaemonWorkspaces :many
SELECT w.id, w.name
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.id ASC;

-- name: GetDaemonWorkspace :one
SELECT id, name
FROM workspace
WHERE id = $1;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = $1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = $1;

-- name: GetWorkspaceAttributionFailClosed :one
SELECT attribution_fail_closed FROM workspace
WHERE id = $1;

-- name: ListAllWorkspacesWithMemberCount :many
SELECT w.id, w.name, w.slug,
       (SELECT count(*) FROM member m WHERE m.workspace_id = w.id) AS member_count
FROM workspace w
ORDER BY w.created_at ASC, w.id ASC;

-- name: CreateWorkspace :one
INSERT INTO workspace (name, slug, description, context, issue_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWorkspace :one
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: LockWorkspaceForDelete :one
SELECT id FROM workspace WHERE id = $1 FOR UPDATE;

-- name: LockWorkspaceForChatSessionCreate :one
SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE;

-- name: DeleteWorkspace :exec
WITH ws_installations AS (
    SELECT id FROM channel_installation WHERE workspace_id = $1
),
ws_agents AS (
    SELECT id FROM agent WHERE workspace_id = $1
),
ws_skills AS (
    SELECT id FROM skill WHERE workspace_id = $1
),
cleared_agent_label_assignments AS (
    DELETE FROM agent_to_label WHERE agent_id IN (SELECT id FROM ws_agents)
),
cleared_skill_label_assignments AS (
    DELETE FROM skill_to_label WHERE skill_id IN (SELECT id FROM ws_skills)
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM ws_installations)
    RETURNING chat_session_id
),
cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_draft_restores AS (
    DELETE FROM chat_draft_restore
    WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_audit AS (
    DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding WHERE workspace_id = $1
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE workspace_id = $1
),
cleared_installations AS (
    DELETE FROM channel_installation WHERE workspace_id = $1
),
cleared_issue_properties AS (
    DELETE FROM issue_property WHERE workspace_id = $1
),
deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
),
ws_github_prs AS (
    SELECT id FROM github_pull_request WHERE workspace_id = $1
),
cleared_github_pr_check_runs AS (
    DELETE FROM github_pull_request_check_run
    WHERE pr_id IN (SELECT id FROM ws_github_prs)
),
ws_vcs_prs AS (
    SELECT id FROM vcs_pull_request WHERE workspace_id = $1
),
ws_vcs_connections AS (
    SELECT id FROM vcs_connection WHERE workspace_id = $1
),
cleared_vcs_pr_links AS (
    DELETE FROM issue_vcs_pull_request
    WHERE pull_request_id IN (SELECT id FROM ws_vcs_prs)
),
cleared_vcs_commit_statuses AS (
    DELETE FROM vcs_commit_status
    WHERE connection_id IN (SELECT id FROM ws_vcs_connections)
),
cleared_vcs_prs AS (
    DELETE FROM vcs_pull_request WHERE workspace_id = $1
),
cleared_vcs_connections AS (
    DELETE FROM vcs_connection WHERE workspace_id = $1
),
ws_mcp_servers AS (
    SELECT id FROM workspace_mcp_server WHERE workspace_id = $1
),
cleared_agent_mcp_servers AS (
    DELETE FROM agent_mcp_server
    WHERE server_id IN (SELECT id FROM ws_mcp_servers)
),
cleared_workspace_mcp_user_credentials AS (
    DELETE FROM workspace_mcp_user_credential WHERE workspace_id = $1
),
cleared_workspace_mcp_servers AS (
    DELETE FROM workspace_mcp_server WHERE workspace_id = $1
),
cleared_agent_deployment_mcp_servers AS (
    DELETE FROM agent_mcp_server
    WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)
),
cleared_workspace_deployment_mcp_servers AS (
    DELETE FROM workspace_deployment_mcp_server WHERE workspace_id = $1
),
cleared_client_usage_workspace AS (
    UPDATE client_usage_daily SET workspace_id = NULL WHERE workspace_id = $1
)
DELETE FROM workspace WHERE workspace.id = $1;

-- name: ListOpenJoinTargets :many
SELECT w.id, w.name, w.slug, w.description, w.template_key,
       (SELECT count(*) FROM member m WHERE m.workspace_id = w.id) AS member_count
FROM workspace w
LEFT JOIN workspace_template t ON t.key = w.template_key
WHERE w.template_key IS NOT NULL
  AND w.open_join = true
  AND NOT EXISTS (
      SELECT 1 FROM member m WHERE m.workspace_id = w.id AND m.user_id = $1
  )
ORDER BY COALESCE(t.position, 2147483647) ASC, w.created_at ASC, w.id ASC;

-- name: GetOpenJoinTarget :one
SELECT id, name, slug, description, template_key
FROM workspace
WHERE id = $1 AND template_key IS NOT NULL AND open_join = true;

-- name: SetWorkspaceOpenJoin :one
UPDATE workspace SET open_join = $2, updated_at = now()
WHERE id = $1
RETURNING id, name, slug, open_join;
