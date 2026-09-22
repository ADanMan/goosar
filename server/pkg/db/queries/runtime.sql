-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE id = ANY(@ids::uuid[]);

-- name: LockAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1
FOR UPDATE;

-- name: GetAgentRuntimeForWorkspace :one
SELECT * FROM agent_runtime
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertAgentRuntime :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (workspace_id, daemon_id, provider) WHERE profile_id IS NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpsertAgentRuntimeWithProfile :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    profile_id,
    last_seen_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    provider = EXCLUDED.provider,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpsertFailedAgentRuntimeProfile :one
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    profile_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    provider = EXCLUDED.provider,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpdateAgentRuntimeVisibility :one
UPDATE agent_runtime
SET visibility = @visibility, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomName :one
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomNameByDaemon :many
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND (@owner_id::uuid IS NULL OR owner_id = @owner_id)
RETURNING *;

-- name: ListDaemonCustomNames :many
SELECT custom_name FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND id <> @exclude_id;

-- name: TouchAgentRuntimeLastSeen :execrows
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = $1 AND status = 'online';

-- name: TouchAgentRuntimesLastSeenBatch :execrows
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'online';

-- name: MarkAgentRuntimeOnline :one
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: SelectStaleOnlineRuntimes :many
SELECT id, workspace_id, owner_id, daemon_id, provider FROM agent_runtime
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision);

-- name: MarkRuntimesOfflineByIDs :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND id = ANY(@ids::uuid[])
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: FailTasksForOfflineRuntimes :many
WITH victims AS (
  SELECT task.id
  FROM agent_task_queue task
  JOIN agent_runtime runtime ON runtime.id = task.runtime_id
  WHERE task.status IN ('dispatched', 'running', 'waiting_local_directory')
    AND runtime.status = 'offline'
    AND COALESCE(runtime.last_seen_at, runtime.updated_at) <
        now() - make_interval(secs => @reconnect_grace_secs::double precision)
  ORDER BY COALESCE(runtime.last_seen_at, runtime.updated_at), task.created_at
  LIMIT @max_per_tick::int
  FOR UPDATE OF task SKIP LOCKED
)
UPDATE agent_task_queue AS task
SET status = 'failed', completed_at = now(), error = 'runtime went offline',
    failure_reason = 'runtime_offline',
    wait_reason = NULL
FROM victims
WHERE task.id = victims.id
  AND task.status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING task.*;

-- name: ListAgentRuntimesByOwner :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY created_at ASC;

-- name: ForceOfflineRuntimesByIDs :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = ANY(@runtime_ids::uuid[]) AND status = 'online'
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: CancelAgentTasksByRuntimeOrAgent :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE (runtime_id = ANY(@runtime_ids::uuid[]) OR agent_id = ANY(@agent_ids::uuid[]))
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: DeleteAgentRuntime :exec
DELETE FROM agent_runtime WHERE id = $1;

-- name: DeleteSystemAgentsByRuntime :exec
DELETE FROM agent WHERE runtime_id = $1 AND kind = 'system';

-- name: CountActiveAgentsByRuntime :one
SELECT count(*) FROM agent WHERE runtime_id = $1 AND archived_at IS NULL;

-- name: CountActiveSquadsWithArchivedLeadersByRuntime :one
SELECT count(*)
FROM squad
WHERE archived_at IS NULL
  AND leader_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
  );

-- name: DeleteArchivedAgentsByRuntime :exec
DELETE FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL;

-- name: PauseAutopilotsByAgentAssignees :exec
UPDATE autopilot
SET status = 'paused', updated_at = now()
WHERE status = 'active'
  AND assignee_type = 'agent'
  AND assignee_id = ANY(@assignee_ids::uuid[]);

-- name: ListArchivedAgentIDsByRuntime :many
SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL;

-- name: DeleteSquadsByArchivedAgentsOnRuntime :exec
DELETE FROM squad
WHERE leader_id IN (
    SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NOT NULL
)
  AND archived_at IS NOT NULL;

-- name: FindLegacyRuntimesByDaemonID :many
SELECT * FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND provider = @provider
  AND LOWER(daemon_id) = LOWER(@daemon_id);

-- name: FindStaleOwnerRuntimesByHostname :many
SELECT * FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND provider = @provider
  AND profile_id IS NULL
  AND status = 'offline'
  AND owner_id = @owner_id
  AND LOWER(daemon_id) != LOWER(@daemon_id)
  AND (device_info = @hostname OR device_info LIKE @hostname || ' · %');

-- name: ReassignAgentsToRuntime :execrows
UPDATE agent
SET runtime_id = @new_runtime_id
WHERE runtime_id = @old_runtime_id;

-- name: ReassignTasksToRuntime :execrows
UPDATE agent_task_queue
SET runtime_id = @new_runtime_id
WHERE runtime_id = @old_runtime_id;

-- name: RecordRuntimeLegacyDaemonID :exec
UPDATE agent_runtime
SET legacy_daemon_id = COALESCE(legacy_daemon_id, $2)
WHERE id = $1;

-- name: DeleteStaleOfflineRuntimes :many
DELETE FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND id NOT IN (SELECT DISTINCT runtime_id FROM agent)
RETURNING id, workspace_id;

-- name: ForceOfflineRuntimesByOwner :many
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE owner_id = @owner_id AND status = 'online'
RETURNING id;

-- name: ListDeploymentFleetRuntimes :many
SELECT
    ar.id,
    ar.workspace_id,
    w.name AS workspace_name,
    w.slug AS workspace_slug,
    ar.daemon_id,
    COALESCE(NULLIF(btrim(ar.custom_name), ''), ar.name) AS runtime_name,
    ar.provider,
    ar.visibility,
    ar.status,
    ar.device_info,
    ar.last_seen_at,
    ar.metadata,
    ar.owner_id,
    COALESCE(u.email, '') AS owner_email,
    COALESCE(u.name, '') AS owner_name,
    COALESCE(t.running_tasks, 0)::bigint AS running_tasks,
    (CASE
        WHEN ar.status = 'online'
             AND ar.last_seen_at >= now() - make_interval(secs => @runtime_stale_seconds::double precision)
        THEN 0
        ELSE COALESCE(t.stuck_tasks, 0)
     END)::bigint AS stuck_tasks
FROM agent_runtime ar
JOIN workspace w ON w.id = ar.workspace_id
LEFT JOIN "user" u ON u.id = ar.owner_id
LEFT JOIN (
    SELECT
        q.runtime_id,
        count(*) AS running_tasks,
        count(*) FILTER (
            WHERE q.status = 'running'
              AND q.started_at IS NOT NULL
              AND q.started_at < now() - make_interval(secs => @stuck_seconds::double precision)
        ) AS stuck_tasks
    FROM agent_task_queue q
    WHERE q.status IN ('dispatched', 'running', 'waiting_local_directory')
    GROUP BY q.runtime_id
) t ON t.runtime_id = ar.id
ORDER BY w.name ASC, ar.workspace_id ASC,
         COALESCE(ar.daemon_id, '') ASC, runtime_name ASC, ar.id ASC;

-- name: ListDeploymentFleetAgents :many
SELECT
    a.id,
    a.runtime_id,
    a.name,
    COALESCE(a.system_key, '') AS system_key,
    a.status
FROM agent a
WHERE a.archived_at IS NULL
ORDER BY a.runtime_id ASC, a.name ASC, a.id ASC;
