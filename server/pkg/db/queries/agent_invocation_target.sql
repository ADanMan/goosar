
-- name: ListAgentInvocationTargets :many
SELECT * FROM agent_invocation_target
WHERE agent_id = $1
ORDER BY target_type ASC, created_at ASC;

-- name: ListAgentInvocationTargetsByAgentIDs :many
SELECT * FROM agent_invocation_target
WHERE agent_id = ANY(@agent_ids::uuid[])
ORDER BY agent_id, target_type ASC, created_at ASC;

-- name: CreateAgentInvocationTarget :exec
INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
VALUES ($1, $2, $3, sqlc.narg('created_by'))
ON CONFLICT (agent_id, target_type, target_id) DO UPDATE SET
    created_by = EXCLUDED.created_by,
    created_at = now();

-- name: DeleteAgentInvocationTargets :exec
DELETE FROM agent_invocation_target
WHERE agent_id = $1;

-- name: DeleteAgentInvocationTargetsByMember :exec
DELETE FROM agent_invocation_target ait
USING agent a
WHERE ait.agent_id = a.id
  AND a.workspace_id = @workspace_id
  AND ait.target_type = 'member'
  AND ait.target_id = @target_id;

-- name: DeleteAgentInvocationTargetsByArchivedRuntimeAgents :exec
DELETE FROM agent_invocation_target
WHERE agent_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
);
