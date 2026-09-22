-- name: ListChatPinnedAgents :many
SELECT * FROM chat_pinned_agent
WHERE workspace_id = $1 AND user_id = $2
ORDER BY position ASC, created_at ASC;

-- name: CreateChatPinnedAgent :one
INSERT INTO chat_pinned_agent (workspace_id, user_id, agent_id, position)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, agent_id)
DO UPDATE SET position = chat_pinned_agent.position
RETURNING *;

-- name: DeleteChatPinnedAgent :exec
DELETE FROM chat_pinned_agent
WHERE workspace_id = $1 AND user_id = $2 AND agent_id = $3;

-- name: DeleteChatPinnedAgentsByWorkspace :exec
DELETE FROM chat_pinned_agent
WHERE workspace_id = $1;

-- name: DeleteChatPinnedAgentsByArchivedRuntimeAgents :exec
DELETE FROM chat_pinned_agent
WHERE agent_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
);

-- name: GetMaxChatPinnedAgentPosition :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM chat_pinned_agent
WHERE workspace_id = $1 AND user_id = $2;
