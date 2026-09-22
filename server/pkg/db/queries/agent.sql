-- name: ListAgents :many
SELECT * FROM agent
WHERE workspace_id = $1 AND archived_at IS NULL AND kind = 'user'
ORDER BY created_at ASC;

-- name: ListAllAgents :many
SELECT * FROM agent
WHERE workspace_id = $1 AND kind = 'user'
ORDER BY created_at ASC;

-- name: GetAgent :one
SELECT * FROM agent
WHERE id = $1;

-- name: GetAgentForUpdate :one
SELECT * FROM agent
WHERE id = $1
FOR UPDATE;

-- name: GetAgentInWorkspace :one
SELECT * FROM agent
WHERE id = $1 AND workspace_id = $2 AND kind = 'user';

-- name: CreateAgent :one
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode,
    runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id,
    instructions, custom_env, custom_args, mcp_config, model, thinking_level,
    service_tier,
    composio_toolkit_allowlist, permission_mode
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16,
    $17,
    sqlc.narg('composio_toolkit_allowlist')::text[],
    COALESCE(sqlc.narg('permission_mode'), 'private')
)
RETURNING *;

-- name: LockWorkspaceHelperKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: MemberHelperProvisionable :one
SELECT
    EXISTS (
        SELECT 1 FROM member m
        WHERE m.workspace_id = @workspace_id AND m.user_id = @member_user_id
    ) AS is_member,
    NOT EXISTS (
        SELECT 1 FROM agent a
        WHERE a.workspace_id = @workspace_id
          AND a.owner_id = @member_user_id
          AND a.kind = 'user'
          AND (a.archived_at IS NULL OR a.system_key = @helper_system_key)
    ) AS slot_free,
    EXISTS (
        SELECT 1 FROM agent_runtime rt
        WHERE rt.workspace_id = @workspace_id
          AND rt.status = 'online'
          AND rt.owner_id = @member_user_id
          AND (
              NOT @restrict_providers::boolean
              OR lower(rt.provider) = ANY(@allowed_providers::text[])
          )
    ) AS has_usable_runtime;

-- name: ReleaseHelperIdentityByAgentIDs :exec
UPDATE agent
SET system_key = NULL, updated_at = now()
WHERE id = ANY(@agent_ids::uuid[]) AND system_key = @helper_system_key;

-- name: ListWorkspaceAgentNames :many
SELECT name FROM agent WHERE workspace_id = $1;

-- name: CreateHelperAgent :one
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode, runtime_config,
    runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
    instructions, custom_env, custom_args, kind, system_key
) VALUES (
    @workspace_id, @name, @description, @avatar_url, @runtime_mode, '{}'::jsonb,
    @runtime_id, @visibility, @permission_mode, @max_concurrent_tasks, @owner_id,
    @instructions, '{}'::jsonb, '[]'::jsonb, 'user', 'goosar_helper'
)
RETURNING *;

-- name: CreateAgentBuilder :one
INSERT INTO agent (
    workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
    visibility, permission_mode, max_concurrent_tasks, owner_id, instructions,
    custom_env, custom_args, model, kind, system_key
) VALUES (
    @workspace_id, @name, '', @runtime_mode, '{}'::jsonb, @runtime_id,
    'private', 'private', 1, @owner_id, @instructions,
    '{}'::jsonb, '[]'::jsonb, sqlc.narg('model'), 'system', @system_key
)
RETURNING *;

-- name: DeleteSystemAgentByID :exec
DELETE FROM agent
WHERE id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%';

-- name: RebindAgentBuilderRuntime :one
UPDATE agent
SET runtime_id = @runtime_id,
    runtime_mode = @runtime_mode,
    model = sqlc.narg('model'),
    updated_at = now()
WHERE id = @id AND kind = 'system' AND system_key LIKE 'agent_builder:%'
RETURNING *;

-- name: UpdateAgent :one
UPDATE agent SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    runtime_config = COALESCE(sqlc.narg('runtime_config'), runtime_config),
    runtime_mode = COALESCE(sqlc.narg('runtime_mode'), runtime_mode),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    permission_mode = COALESCE(sqlc.narg('permission_mode'), permission_mode),
    status = COALESCE(sqlc.narg('status'), status),
    max_concurrent_tasks = COALESCE(sqlc.narg('max_concurrent_tasks'), max_concurrent_tasks),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    custom_env = COALESCE(sqlc.narg('custom_env'), custom_env),
    custom_args = COALESCE(sqlc.narg('custom_args'), custom_args),
    mcp_config = COALESCE(sqlc.narg('mcp_config'), mcp_config),
    model = COALESCE(sqlc.narg('model'), model),
    thinking_level = COALESCE(sqlc.narg('thinking_level'), thinking_level),
    service_tier = COALESCE(sqlc.narg('service_tier'), service_tier),
    composio_toolkit_allowlist = COALESCE(sqlc.narg('composio_toolkit_allowlist')::text[], composio_toolkit_allowlist),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentComposioToolkitAllowlist :one
UPDATE agent SET composio_toolkit_allowlist = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentThinkingLevel :one
UPDATE agent SET thinking_level = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentServiceTier :one
UPDATE agent SET service_tier = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentMcpConfig :one
UPDATE agent SET mcp_config = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAgentMcpConfigsForBackfill :many
SELECT id, mcp_config FROM agent
WHERE mcp_config IS NOT NULL;

-- name: SealAgentMcpConfigForBackfill :execrows
UPDATE agent SET mcp_config = sqlc.arg('sealed')
WHERE id = sqlc.arg('id') AND mcp_config = sqlc.arg('previous');

-- name: UpdateAgentCustomEnv :one
UPDATE agent
SET custom_env = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateAgentDisabledRuntimeSkills :one
UPDATE agent
SET disabled_runtime_skills = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgent :one
UPDATE agent SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgentsByRuntime :many
UPDATE agent
SET archived_at = now(), archived_by = @archived_by, updated_at = now()
WHERE runtime_id = ANY(@runtime_ids::uuid[]) AND archived_at IS NULL
RETURNING *;

-- name: ArchiveAgentsByIDs :many
UPDATE agent
SET archived_at = now(), archived_by = @archived_by, updated_at = now()
WHERE id = ANY(@agent_ids::uuid[]) AND archived_at IS NULL
RETURNING *;

-- name: ListActiveAgentsByRuntime :many
SELECT * FROM agent
WHERE runtime_id = $1 AND archived_at IS NULL AND kind = 'user'
ORDER BY name ASC;

-- name: ListActiveAgentsByRuntimeInWorkspace :many
SELECT * FROM agent
WHERE runtime_id = $1 AND workspace_id = $2 AND archived_at IS NULL AND kind = 'user'
ORDER BY name ASC;

-- name: ListActiveAgentsByRuntimeForUpdate :many
SELECT * FROM agent
WHERE runtime_id = $1 AND archived_at IS NULL AND kind = 'user'
ORDER BY name ASC
FOR UPDATE;

-- name: RestoreAgent :one
UPDATE agent SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAgentTasks :many
SELECT * FROM agent_task_queue
WHERE agent_id = $1
ORDER BY created_at DESC;

-- name: CreateAgentTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
    coalesced_comment_ids, trigger_summary, force_fresh_session, is_leader_task, handoff_note,
    squad_id, context, originator_user_id, accountable_user_id, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, delegated_from_task_id, rule_version_id, rerun_of_task_id, trigger_evidence_kind, trigger_evidence_ref_id
)
VALUES (
    $1, $2, $3, 'queued', $4, sqlc.narg(trigger_comment_id),
    COALESCE(sqlc.narg(coalesced_comment_ids)::uuid[], '{}'),
    sqlc.narg(trigger_summary),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    COALESCE(sqlc.narg('is_leader_task')::boolean, FALSE),
    sqlc.narg(handoff_note),
    sqlc.narg(squad_id),
    CASE
        WHEN COALESCE(sqlc.narg('head_sha')::text, '') <> ''
        THEN jsonb_build_object('head_sha', sqlc.narg('head_sha')::text)
        ELSE NULL
    END,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(delegated_from_task_id),
    sqlc.narg(rule_version_id),
    sqlc.narg(rerun_of_task_id),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id)
)
RETURNING *;

-- name: CreateQuickCreateTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, context, originator_user_id,
    accountable_user_id, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, trigger_evidence_kind, trigger_evidence_ref_id
)
VALUES (
    $1, $2, NULL, 'queued', $3, $4,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id)
)
RETURNING *;

-- name: CreateDeferredAgentTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, trigger_comment_id,
    trigger_summary, is_leader_task, squad_id, escalation_for_task_id, fire_at,
    originator_user_id, accountable_user_id, originator_source,
    delegated_from_task_id, trigger_evidence_kind, trigger_evidence_ref_id
)
VALUES (
    @agent_id, @runtime_id, @issue_id, 'deferred', @priority,
    sqlc.narg(trigger_comment_id),
    sqlc.narg(trigger_summary),
    COALESCE(sqlc.narg('is_leader_task')::boolean, FALSE),
    sqlc.narg(squad_id),
    @escalation_for_task_id,
    @fire_at,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(originator_source),
    sqlc.narg(delegated_from_task_id),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id)
)
RETURNING *;

-- name: LinkTaskToIssue :exec
UPDATE agent_task_queue
SET issue_id = $2
WHERE id = $1 AND issue_id IS NULL;

-- name: CreateRetryTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, chat_session_id, autopilot_run_id,
    status, priority, trigger_comment_id, coalesced_comment_ids, trigger_summary, context,
    session_id, work_dir,
    attempt, max_attempts, parent_task_id, force_fresh_session, is_leader_task,
    squad_id, originator_user_id, accountable_user_id, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, delegated_from_task_id, rule_version_id,
    trigger_evidence_kind, trigger_evidence_ref_id, retry_of_task_id,
    chat_input_task_id, fire_at
)
SELECT
    p.agent_id, p.runtime_id, p.issue_id, p.chat_session_id, p.autopilot_run_id,
    CASE WHEN sqlc.narg(fire_at)::timestamptz IS NOT NULL THEN 'deferred' ELSE 'queued' END,
    CASE WHEN p.chat_session_id IS NOT NULL THEN GREATEST(p.priority, 3) ELSE p.priority END,
    p.trigger_comment_id, p.coalesced_comment_ids, p.trigger_summary, p.context,
    CASE WHEN p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity' THEN NULL ELSE p.session_id END,
    CASE WHEN p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity' THEN NULL ELSE p.work_dir END,
    p.attempt + 1, COALESCE(sqlc.narg(max_attempts)::int, p.max_attempts), p.id,
    p.failure_reason IS NOT DISTINCT FROM 'codex_semantic_inactivity',
    p.is_leader_task,
    p.squad_id,
    p.originator_user_id,
    p.accountable_user_id,
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    p.originator_source, p.delegated_from_task_id, p.rule_version_id,
    p.trigger_evidence_kind, p.trigger_evidence_ref_id, p.id,
    p.chat_input_task_id, sqlc.narg(fire_at)
FROM agent_task_queue p
WHERE p.id = $1
RETURNING *;

-- name: CancelAgentTasksByIssue :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CancelAgentTasksByIssueAndAgent :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CancelAgentTasksByAgent :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CancelAgentTasksByTriggerComment :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE (trigger_comment_id = $1 OR $1 = ANY(coalesced_comment_ids))
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: CancelAgentTasksByChatSession :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: GetAgentTask :one
SELECT * FROM agent_task_queue
WHERE id = $1;

-- name: GetAgentTaskInWorkspace :one
SELECT atq.* FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE atq.id = $1 AND a.workspace_id = $2;

-- name: ClaimAgentTask :one
UPDATE agent_task_queue
SET status = 'dispatched',
    dispatched_at = now(),
    prepare_lease_expires_at = now() + make_interval(secs => @prepare_lease_secs::double precision)
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.agent_id = $1 AND atq.status = 'queued'
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue active
          WHERE active.agent_id = atq.agent_id
            AND active.status IN ('dispatched', 'running', 'waiting_local_directory')
            AND (
              (atq.issue_id IS NOT NULL AND active.issue_id = atq.issue_id)
              OR (atq.chat_session_id IS NOT NULL AND active.chat_session_id = atq.chat_session_id)
              OR (
                atq.issue_id IS NULL
                AND atq.chat_session_id IS NULL
                AND atq.autopilot_run_id IS NULL
                AND active.issue_id IS NULL
                AND active.chat_session_id IS NULL
                AND active.autopilot_run_id IS NULL
              )
            )
      )
    ORDER BY atq.priority DESC, atq.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: SetTaskDeliveredCommentIDs :one
UPDATE agent_task_queue
SET delivered_comment_ids = @delivered_comment_ids::uuid[]
WHERE id = @task_id
  AND runtime_id = @runtime_id
  AND status = 'dispatched'
  AND started_at IS NULL
  AND dispatched_at = @dispatched_at
  AND trigger_comment_id IS NOT DISTINCT FROM sqlc.narg(expected_trigger_comment_id)::uuid
  AND NOT EXISTS (
      SELECT 1
      FROM unnest(@delivered_comment_ids::uuid[]) AS delivered(id)
      WHERE delivered.id IS NULL
         OR (
             delivered.id IS DISTINCT FROM trigger_comment_id
             AND NOT (delivered.id = ANY(coalesced_comment_ids))
         )
  )
RETURNING delivered_comment_ids;

-- name: RequeueAgentTaskAfterClaimFailure :one
UPDATE agent_task_queue
SET status = 'queued',
    dispatched_at = NULL,
    prepare_lease_expires_at = NULL,
    delivered_comment_ids = '{}'
WHERE id = @task_id
  AND runtime_id = @runtime_id
  AND status = 'dispatched'
  AND started_at IS NULL
  AND dispatched_at = @dispatched_at
RETURNING *;

-- name: ReclaimStaleDispatchedTaskForRuntime :one
UPDATE agent_task_queue
SET dispatched_at = now(),
    prepare_lease_expires_at = now() + make_interval(secs => @prepare_lease_secs::double precision)
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.runtime_id = $1
      AND atq.status = 'dispatched'
      AND atq.started_at IS NULL
      AND atq.dispatched_at < now() - make_interval(secs => @claim_recovery_secs::double precision)
      AND (atq.prepare_lease_expires_at IS NULL OR atq.prepare_lease_expires_at < now())
    ORDER BY atq.priority DESC, atq.dispatched_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ReclaimStaleDispatchedTasksForRuntimes :many
UPDATE agent_task_queue
SET dispatched_at = now(),
    prepare_lease_expires_at = now() + make_interval(secs => @prepare_lease_secs::double precision)
WHERE id IN (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.runtime_id = ANY(@runtime_ids::uuid[])
      AND atq.status = 'dispatched'
      AND atq.started_at IS NULL
      AND atq.dispatched_at < now() - make_interval(secs => @claim_recovery_secs::double precision)
      AND (atq.prepare_lease_expires_at IS NULL OR atq.prepare_lease_expires_at < now())
    ORDER BY atq.priority DESC, atq.dispatched_at ASC
    LIMIT @max_tasks::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ExtendAgentTaskPrepareLease :one
UPDATE agent_task_queue
SET prepare_lease_expires_at = now() + make_interval(secs => @lease_secs::double precision)
WHERE id = $1
  AND runtime_id = $2
  AND status IN ('dispatched', 'waiting_local_directory')
  AND started_at IS NULL
RETURNING *;

-- name: StartAgentTask :one
UPDATE agent_task_queue
SET status = 'running',
    started_at = now(),
    wait_reason = NULL,
    prepare_lease_expires_at = NULL
WHERE id = $1 AND status IN ('dispatched', 'waiting_local_directory')
RETURNING *;

-- name: MarkAgentTaskWaitingLocalDirectory :one
UPDATE agent_task_queue
SET status = 'waiting_local_directory',
    wait_reason = $2,
    prepare_lease_expires_at = now() + make_interval(secs => @prepare_lease_secs::double precision)
WHERE id = $1 AND status = 'dispatched'
RETURNING *;

-- name: CompleteAgentTask :one
UPDATE agent_task_queue
SET status = 'completed', completed_at = now(), result = $2,
    session_id = CASE WHEN sqlc.arg('session_rollout_missing') THEN NULL ELSE $3 END,
    work_dir = $4,
    session_rollout_missing = sqlc.arg('session_rollout_missing'),
    prepare_lease_expires_at = NULL
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: GetLastTaskSession :one
WITH latest_per_session AS (
    SELECT DISTINCT ON (session_id)
        session_id, work_dir, runtime_id, status, failure_reason, error,
        COALESCE(completed_at, started_at, dispatched_at, created_at) AS terminal_at
    FROM agent_task_queue
    WHERE agent_id = $1 AND issue_id = $2
      AND session_id IS NOT NULL
      AND status IN ('completed', 'failed')
    ORDER BY session_id, COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
)
SELECT session_id, work_dir, runtime_id FROM latest_per_session
WHERE (
    status = 'completed'
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
      AND NOT (COALESCE(error, '') ILIKE '%image dimensions exceed max allowed size%' AND COALESCE(error, '') ILIKE '%image.source.base64.data%')
      AND NOT (COALESCE(error, '') ILIKE '%session/prompt%' AND COALESCE(error, '') ILIKE '%(code=-32603, data=BadRequestError)%')
    )
  )
ORDER BY terminal_at DESC
LIMIT 1;

-- name: GetLatestTaskRolloutMissing :one
SELECT COALESCE(session_rollout_missing, FALSE) FROM agent_task_queue
WHERE agent_id = $1 AND issue_id = $2
  AND status IN ('completed', 'failed')
  AND started_at IS NOT NULL
ORDER BY COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
LIMIT 1;

-- name: GetLatestChatTaskRolloutMissing :one
SELECT COALESCE(session_rollout_missing, FALSE) FROM agent_task_queue
WHERE chat_session_id = $1
  AND status IN ('completed', 'failed')
  AND started_at IS NOT NULL
ORDER BY COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
LIMIT 1;

-- name: GetLastTaskStartedAtForIssueAndAgent :one
SELECT started_at FROM agent_task_queue
WHERE agent_id = $1 AND issue_id = $2 AND started_at IS NOT NULL
ORDER BY started_at DESC
LIMIT 1;

-- name: FailAgentTask :one
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = $2,
    failure_reason = COALESCE(sqlc.narg('failure_reason'), 'agent_error'),
    session_id = CASE WHEN sqlc.arg('session_rollout_missing') THEN NULL ELSE COALESCE(sqlc.narg('session_id'), session_id) END,
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir),
    session_rollout_missing = sqlc.arg('session_rollout_missing'),
    prepare_lease_expires_at = NULL
WHERE id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: UpdateAgentTaskRuntimeMCPOverlay :one
UPDATE agent_task_queue
SET runtime_mcp_overlay = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAgentTaskSession :exec
UPDATE agent_task_queue
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir  = COALESCE(sqlc.narg('work_dir'), work_dir)
WHERE id = $1
  AND (status IN ('dispatched', 'running')
       OR (status = 'cancelled' AND chat_session_id IS NOT NULL));

-- name: RecoverOrphanedTasksForRuntime :many
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = 'daemon restarted while task was in flight',
    failure_reason = 'runtime_recovery',
    wait_reason = NULL,
    prepare_lease_expires_at = NULL
WHERE runtime_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: FailStaleTasks :many
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'task timed out',
    failure_reason = 'timeout',
    prepare_lease_expires_at = NULL
WHERE (
    status = 'dispatched'
    AND dispatched_at < now() - make_interval(secs => @dispatch_timeout_secs::double precision)
    AND (prepare_lease_expires_at IS NULL OR prepare_lease_expires_at < now())
    AND (
      runtime_id IS NULL
      OR NOT EXISTS (
        SELECT 1 FROM agent_runtime r
        WHERE r.id = agent_task_queue.runtime_id
      )
      OR EXISTS (
        SELECT 1 FROM agent_runtime r
        WHERE r.id = agent_task_queue.runtime_id
          AND (
            (
              r.status = 'online'
              AND COALESCE(r.last_seen_at, r.updated_at) >=
                  now() - make_interval(secs => @runtime_stale_secs::double precision)
            )
            OR COALESCE(r.last_seen_at, r.updated_at) <
               now() - make_interval(secs => @runtime_reconnect_grace_secs::double precision)
          )
      )
    )
  )
   OR (
    status = 'running'
    AND started_at < now() - make_interval(secs => @running_timeout_secs::double precision)
    AND (
      runtime_id IS NULL
      OR NOT EXISTS (
        SELECT 1 FROM agent_runtime r
        WHERE r.id = agent_task_queue.runtime_id
          AND COALESCE(r.last_seen_at, r.updated_at) >=
              now() - make_interval(secs => @runtime_reconnect_grace_secs::double precision)
      )
    )
  )
RETURNING *;

-- name: ExpireStaleQueuedTasks :many
WITH victims AS (
    SELECT id FROM agent_task_queue
    WHERE status = 'queued'
      AND created_at < now() - make_interval(secs => @ttl_secs::double precision)
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue retry_parent
          WHERE retry_parent.id = agent_task_queue.parent_task_id
            AND retry_parent.failure_reason = 'runtime_offline'
      )
    ORDER BY created_at ASC
    LIMIT @max_per_tick::int
    FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue t
SET status = 'failed',
    completed_at = now(),
    error = 'task expired in queue',
    failure_reason = 'queued_expired',
    prepare_lease_expires_at = NULL
FROM victims v
WHERE t.id = v.id
  AND t.status = 'queued'
  AND t.created_at < now() - make_interval(secs => @ttl_secs::double precision)
  AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue retry_parent
      WHERE retry_parent.id = t.parent_task_id
        AND retry_parent.failure_reason = 'runtime_offline'
  )
RETURNING t.*;

-- name: FailExpiredRuntimeReconnectRetries :many
WITH victims AS (
    SELECT retry.id
    FROM agent_task_queue retry
    JOIN agent_task_queue parent ON parent.id = retry.parent_task_id
    WHERE retry.status = 'deferred'
      AND retry.fire_at < now() - make_interval(secs => @reconnect_grace_secs::double precision)
      AND parent.failure_reason = 'runtime_offline'
      AND NOT EXISTS (
          SELECT 1 FROM agent_runtime runtime
          WHERE runtime.id = retry.runtime_id
            AND runtime.status = 'online'
            AND COALESCE(runtime.last_seen_at, runtime.updated_at) >=
                now() - make_interval(secs => @runtime_stale_secs::double precision)
      )
    ORDER BY retry.fire_at, retry.created_at
    LIMIT @max_per_tick::int
    FOR UPDATE OF retry SKIP LOCKED
)
UPDATE agent_task_queue AS retry
SET status = 'failed',
    completed_at = now(),
    error = 'runtime did not reconnect within the configured grace period',
    failure_reason = 'runtime_reconnect_timeout',
    wait_reason = NULL,
    prepare_lease_expires_at = NULL
FROM victims
WHERE retry.id = victims.id
  AND retry.status = 'deferred'
RETURNING retry.*;

-- name: CancelAgentTask :one
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
RETURNING *;

-- name: MarkChatFinalizeDeferred :one
UPDATE agent_task_queue
SET chat_finalize_deferred_at = now()
WHERE id = $1
RETURNING *;

-- name: ClaimChatFinalizeDeferred :one
UPDATE agent_task_queue
SET chat_finalize_deferred_at = NULL
WHERE id = $1 AND chat_finalize_deferred_at IS NOT NULL
RETURNING *;

-- name: ListChatFinalizeDeferredExpired :many
SELECT * FROM agent_task_queue
WHERE chat_finalize_deferred_at IS NOT NULL
  AND chat_finalize_deferred_at < now() - make_interval(secs => @grace_secs::double precision)
ORDER BY chat_finalize_deferred_at
LIMIT @max_per_tick::int;

-- name: CountRunningTasks :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 AND status IN ('dispatched', 'running', 'waiting_local_directory');

-- name: GetAgentForClaimUpdate :one
SELECT * FROM agent
WHERE id = $1
FOR UPDATE;

-- name: HasActiveTaskForIssue :one
SELECT count(*) > 0 AS has_active FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');

-- name: HasPendingTaskForIssue :one
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched');

-- name: HasPendingTaskForIssueAndAgent :one
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
  AND (
    COALESCE(sqlc.narg('head_sha')::text, '') = ''
    OR context->>'head_sha' = sqlc.narg('head_sha')::text
  );

-- name: HasPendingTaskForIssueAndAgentExcludingTriggerComment :one
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = @issue_id
  AND agent_id = @agent_id
  AND status IN ('queued', 'dispatched')
  AND trigger_comment_id IS DISTINCT FROM @exclude_trigger_comment_id::uuid
  AND (
    COALESCE(sqlc.narg('head_sha')::text, '') = ''
    OR context->>'head_sha' = sqlc.narg('head_sha')::text
  );

-- name: MergeCommentIntoPendingTask :one
UPDATE agent_task_queue
SET coalesced_comment_ids = (
        SELECT COALESCE(array_agg(DISTINCT e), '{}')
        FROM unnest(array_append(coalesced_comment_ids, trigger_comment_id)) AS e
        WHERE e IS NOT NULL AND e <> @new_trigger_comment_id::uuid
    ),
    trigger_comment_id = @new_trigger_comment_id::uuid,
    trigger_summary = COALESCE(sqlc.narg('new_trigger_summary'), trigger_summary),
    originator_user_id = sqlc.narg('new_originator_user_id')::uuid,
    accountable_user_id = sqlc.narg('new_accountable_user_id')::uuid,
    originator_source = sqlc.narg('new_originator_source'),
    delegated_from_task_id = sqlc.narg('new_delegated_from_task_id')::uuid,
    rule_version_id = sqlc.narg('new_rule_version_id')::uuid,
    trigger_evidence_kind = sqlc.narg('new_trigger_evidence_kind'),
    trigger_evidence_ref_id = sqlc.narg('new_trigger_evidence_ref_id')::uuid,
    runtime_mcp_overlay = sqlc.narg('new_runtime_mcp_overlay'),
    runtime_connected_apps = sqlc.narg('new_runtime_connected_apps')
WHERE id = (
    SELECT t.id FROM agent_task_queue t
    WHERE t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND t.status = 'queued'
      AND (
          COALESCE(sqlc.narg('head_sha')::text, '') = ''
          OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
      )
    ORDER BY t.created_at DESC
    LIMIT 1
)
RETURNING id, coalesced_comment_ids;

-- name: RegisterPlannedCommentForActiveTask :one
UPDATE agent_task_queue
SET coalesced_comment_ids = (
        SELECT COALESCE(array_agg(DISTINCT e), '{}')
        FROM unnest(array_append(coalesced_comment_ids, @comment_id::uuid)) AS e
        WHERE e IS NOT NULL
    )
WHERE id = (
    SELECT t.id FROM agent_task_queue t
    WHERE t.issue_id = @issue_id
      AND t.agent_id = @agent_id
      AND t.status IN ('dispatched', 'running', 'waiting_local_directory')
      AND (
          COALESCE(sqlc.narg('head_sha')::text, '') = ''
          OR t.context->>'head_sha' = sqlc.narg('head_sha')::text
      )
    ORDER BY t.created_at DESC
    LIMIT 1
)
RETURNING id, coalesced_comment_ids;

-- name: HasActiveTaskForIssueAndAgent :one
SELECT count(*) > 0 AS has_active FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');

-- name: GetLatestTaskRoleForIssueAndAgent :one
SELECT is_leader_task, squad_id FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingTasksByRuntime :many
SELECT * FROM agent_task_queue
WHERE runtime_id = $1 AND status IN ('queued', 'dispatched')
ORDER BY priority DESC, created_at ASC;

-- name: ListQueuedClaimCandidatesByRuntime :many
SELECT * FROM agent_task_queue
WHERE runtime_id = $1 AND status = 'queued'
ORDER BY priority DESC, created_at ASC;

-- name: PromoteDueDeferredTasksForRuntime :many
UPDATE agent_task_queue
SET status = 'queued'
WHERE runtime_id = @runtime_id
  AND status = 'deferred'
  AND fire_at <= now()
  AND EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.id = agent_task_queue.runtime_id
      AND r.status = 'online'
      AND COALESCE(r.last_seen_at, r.updated_at) >=
          now() - make_interval(secs => @runtime_stale_secs::double precision)
  )
RETURNING *;

-- name: ListQueuedClaimCandidatesByRuntimes :many
SELECT * FROM agent_task_queue
WHERE runtime_id = ANY(@runtime_ids::uuid[]) AND status = 'queued'
ORDER BY priority DESC, created_at ASC;

-- name: PromoteDueDeferredTasksForRuntimes :many
UPDATE agent_task_queue
SET status = 'queued'
WHERE runtime_id = ANY(@runtime_ids::uuid[])
  AND status = 'deferred'
  AND fire_at <= now()
  AND EXISTS (
    SELECT 1 FROM agent_runtime r
    WHERE r.id = agent_task_queue.runtime_id
      AND r.status = 'online'
      AND COALESCE(r.last_seen_at, r.updated_at) >=
          now() - make_interval(secs => @runtime_stale_secs::double precision)
  )
RETURNING *;

-- name: CancelDeferredEscalationsForTask :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
WHERE escalation_for_task_id = $1
  AND status IN ('deferred', 'queued', 'dispatched', 'waiting_local_directory')
RETURNING *;

-- name: CancelDeferredEscalationsForIssueAgent :many
WITH cancelled AS (
    UPDATE agent_task_queue fallback
    SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL
    FROM agent_task_queue primary_task
    WHERE fallback.escalation_for_task_id = primary_task.id
      AND fallback.status IN ('deferred', 'queued', 'dispatched', 'waiting_local_directory')
      AND primary_task.issue_id = @issue_id
      AND primary_task.agent_id = @agent_id
    RETURNING fallback.*
)
SELECT * FROM cancelled;

-- name: ListActiveTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY created_at DESC;

-- name: GetWorkspaceAgentRunCounts :many
SELECT
    atq.agent_id,
    COUNT(*)::int AS run_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.created_at > now() - INTERVAL '30 days'
GROUP BY atq.agent_id;

-- name: GetWorkspaceAgentActivity30d :many
SELECT
    atq.agent_id,
    DATE_TRUNC('day', atq.completed_at)::timestamptz AS bucket,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at > now() - INTERVAL '30 days'
GROUP BY atq.agent_id, bucket
ORDER BY atq.agent_id, bucket;

-- name: ListWorkspaceAgentTaskSnapshot :many
SELECT atq.* FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')

UNION ALL

SELECT t.* FROM (
  SELECT DISTINCT ON (atq.agent_id) atq.*
  FROM agent_task_queue atq
  JOIN agent a ON a.id = atq.agent_id
  WHERE a.workspace_id = $1
    AND atq.status IN ('completed', 'failed')
  ORDER BY atq.agent_id, atq.completed_at DESC NULLS LAST
) t;

-- name: ListWorkspaceWorkingAgents :many
SELECT
  a.id,
  a.name,
  a.avatar_url,
  COUNT(*)::int AS running_task_count,
  COALESCE(
    ARRAY_AGG(DISTINCT atq.issue_id ORDER BY atq.issue_id)
      FILTER (WHERE atq.issue_id IS NOT NULL),
    ARRAY[]::uuid[]
  )::uuid[] AS issue_ids
FROM agent a
JOIN agent_task_queue atq ON atq.agent_id = a.id
WHERE a.workspace_id = $1
  AND a.kind = 'user'
  AND a.archived_at IS NULL
  AND atq.status = 'running'
  AND (
    @work_type::text = ''
    OR (@work_type::text = 'chat' AND atq.chat_session_id IS NOT NULL)
    OR (
      @work_type::text = 'autopilot'
      AND atq.chat_session_id IS NULL
      AND atq.autopilot_run_id IS NOT NULL
    )
    OR (
      @work_type::text = 'issue'
      AND atq.chat_session_id IS NULL
      AND atq.autopilot_run_id IS NULL
      AND atq.issue_id IS NOT NULL
    )
  )
  AND (
    @mine_relation::text = ''
    OR EXISTS (
      SELECT 1
      FROM issue i
      WHERE i.id = atq.issue_id
        AND i.workspace_id = a.workspace_id
        AND (
          (
            @mine_relation::text IN ('assigned', 'any')
            AND i.assignee_type = 'member'
            AND i.assignee_id = @member_id::uuid
          )
          OR (
            @mine_relation::text IN ('created', 'any')
            AND i.creator_type = 'member'
            AND i.creator_id = @member_id::uuid
          )
          OR (
            @mine_relation::text IN ('involved', 'any')
            AND (
              (
                i.assignee_type = 'agent'
                AND EXISTS (
                  SELECT 1
                  FROM agent owned_agent
                  WHERE owned_agent.id = i.assignee_id
                    AND owned_agent.workspace_id = a.workspace_id
                    AND owned_agent.owner_id = @member_id::uuid
                )
              )
              OR (
                i.assignee_type = 'squad'
                AND EXISTS (
                  SELECT 1
                  FROM squad s
                  WHERE s.id = i.assignee_id
                    AND s.workspace_id = a.workspace_id
                    AND (
                      EXISTS (
                        SELECT 1
                        FROM squad_member sm
                        WHERE sm.squad_id = s.id
                          AND sm.member_type = 'member'
                          AND sm.member_id = @member_id::uuid
                      )
                      OR EXISTS (
                        SELECT 1
                        FROM agent leader
                        WHERE leader.id = s.leader_id
                          AND leader.workspace_id = a.workspace_id
                          AND leader.owner_id = @member_id::uuid
                      )
                      OR EXISTS (
                        SELECT 1
                        FROM squad_member sm
                        JOIN agent owned_member ON owned_member.id = sm.member_id
                        WHERE sm.squad_id = s.id
                          AND sm.member_type = 'agent'
                          AND owned_member.workspace_id = a.workspace_id
                          AND owned_member.owner_id = @member_id::uuid
                      )
                    )
                )
              )
            )
          )
        )
    )
  )
  AND (
    @parent_issue_id::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM issue child
      WHERE child.id = atq.issue_id
        AND child.workspace_id = a.workspace_id
        AND child.parent_issue_id = @parent_issue_id::uuid
    )
  )
GROUP BY a.id, a.name, a.avatar_url, a.created_at
ORDER BY a.created_at ASC;

-- name: ListTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: UpdateAgentStatus :one
UPDATE agent SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RefreshAgentStatusFromTasks :one
UPDATE agent AS a
SET status = CASE WHEN EXISTS (
    SELECT 1 FROM agent_task_queue q
    WHERE q.agent_id = a.id AND q.status IN ('dispatched', 'running', 'waiting_local_directory')
) THEN 'working' ELSE 'idle' END,
    updated_at = now()
WHERE a.id = $1
RETURNING *;

-- name: RoleAgentProvisionable :one
SELECT
    w.template_key,
    NOT EXISTS (
        SELECT 1 FROM agent a
        WHERE a.workspace_id = w.id
          AND a.system_key = 'goosar_role_' || w.template_key
    ) AS slot_free,
    EXISTS (
        SELECT 1 FROM agent_runtime rt
        WHERE rt.workspace_id = w.id
          AND rt.status = 'online'
          AND rt.visibility = 'public'
          AND (
              NOT @restrict_providers::boolean
              OR lower(rt.provider) = ANY(@allowed_providers::text[])
          )
    ) AS has_public_runtime
FROM workspace w
WHERE w.id = @workspace_id AND w.template_key IS NOT NULL;

-- name: CreateRoleAgent :one
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode, runtime_config,
    runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
    instructions, custom_env, custom_args, kind, system_key
) VALUES (
    @workspace_id, @name, @description, @avatar_url, @runtime_mode, '{}'::jsonb,
    @runtime_id, 'workspace', 'public_to', @max_concurrent_tasks, NULL,
    @instructions, '{}'::jsonb, '[]'::jsonb, 'user', @system_key
)
RETURNING *;

-- name: ReleaseRoleAgentIdentityByAgentIDs :exec
UPDATE agent
SET system_key = NULL, updated_at = now()
WHERE id = ANY(@agent_ids::uuid[])
  AND system_key LIKE 'goosar_role_%';

-- name: GetRoleAgent :one
SELECT * FROM agent
WHERE workspace_id = @workspace_id AND system_key = @system_key AND archived_at IS NULL;
