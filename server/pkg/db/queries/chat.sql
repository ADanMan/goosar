-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, is_agent_intro, project_id)
VALUES ($1, $2, $3, $4, (SELECT runtime_id FROM agent WHERE id = $2), $5, sqlc.narg('project_id'))
RETURNING *;

-- name: ClearChatSessionProjectByProject :exec
UPDATE chat_session
SET project_id = NULL
WHERE project_id = $1 AND workspace_id = $2;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: ListChatSessionsByCreator :many
SELECT cs.*,
       (SELECT count(*) FROM chat_message m
          WHERE m.chat_session_id = cs.id
            AND m.role = 'assistant'
            AND m.created_at > cs.last_read_at)::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2 AND cs.status = 'active'
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: ListAllChatSessionsByCreator :many
SELECT cs.*,
       CASE WHEN cs.status = 'archived' THEN 0
            ELSE (SELECT count(*) FROM chat_message m
                    WHERE m.chat_session_id = cs.id
                      AND m.role = 'assistant'
                      AND m.created_at > cs.last_read_at)
       END::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = $1 AND cs.creator_id = $2
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionProject :one
UPDATE chat_session
SET project_id = sqlc.narg('project_id')
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: UpdateChatSessionTitleIfCurrent :one
UPDATE chat_session SET title = @new_title, updated_at = now()
WHERE id = @id AND title = @expected_title
RETURNING *;

-- name: SetChatSessionPinned :one
UPDATE chat_session
SET pinned_at = CASE WHEN @pinned::bool THEN COALESCE(pinned_at, now()) ELSE NULL END
WHERE id = $1
RETURNING *;

-- name: SetChatSessionArchived :one
UPDATE chat_session
SET status = CASE WHEN @archived::bool THEN 'archived' ELSE 'active' END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionSession :exec
UPDATE chat_session
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: LockChatSessionForDelete :one
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: LockChatSessionForRuntimeBind :one
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: DeleteChatSession :exec
DELETE FROM chat_session WHERE id = $1 AND workspace_id = $2;

-- name: TouchChatSession :exec
UPDATE chat_session SET updated_at = now()
WHERE id = $1;

-- name: CreateChatMessage :one
INSERT INTO chat_message (
    chat_session_id, role, content, task_id, failure_reason, elapsed_ms,
    message_kind, channel_media_pending_until, channel_ingested
)
VALUES (
    $1, $2, $3, sqlc.narg(task_id), sqlc.narg(failure_reason), sqlc.narg(elapsed_ms),
    COALESCE(sqlc.narg(message_kind)::text, 'message'),
    CASE WHEN sqlc.narg(channel_media_pending_secs)::float8 IS NULL THEN NULL
         ELSE now() + make_interval(secs => sqlc.narg(channel_media_pending_secs)::float8) END,
    COALESCE(sqlc.narg(channel_ingested)::boolean, FALSE)
)
RETURNING *;

-- name: TaskHasChannelIngestedMessages :one
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE task_id = $1
      AND role = 'user'
      AND channel_ingested
) AS channel_ingested;

-- name: GetChannelMediaPendingUntil :one
SELECT channel_media_pending_until
FROM chat_message
WHERE chat_session_id = $1
  AND role = 'user'
  AND channel_media_pending_until > now()
ORDER BY channel_media_pending_until DESC
LIMIT 1;

-- name: ClearChatMessageChannelMediaPending :exec
UPDATE chat_message
SET channel_media_pending_until = NULL
WHERE id = $1 AND chat_session_id = $2;

-- name: LinkChatMessageToTask :exec
UPDATE chat_message
SET task_id = $2
WHERE id = $1 AND role = 'user';

-- name: LinkUnownedChannelChatMessagesToTask :exec
UPDATE chat_message AS message
SET task_id = @task_id
WHERE message.chat_session_id = @chat_session_id
  AND message.role = 'user'
  AND message.task_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS prior
      WHERE prior.chat_session_id = @chat_session_id
        AND prior.role != 'user'
        AND (prior.created_at, prior.id) > (message.created_at, message.id)
  );

-- name: DeferChatTaskForSealedPendingMedia :one
UPDATE agent_task_queue AS task
SET status = 'deferred', fire_at = pending.max_until
FROM (
    SELECT max(message.channel_media_pending_until) AS max_until
    FROM chat_message AS message
    WHERE message.task_id = @task_id
      AND message.role = 'user'
      AND message.channel_media_pending_until > now()
) AS pending
WHERE task.id = @task_id
  AND pending.max_until IS NOT NULL
  AND (task.fire_at IS NULL OR task.fire_at < pending.max_until)
RETURNING task.*;

-- name: DeleteUserChatMessageByTask :one
DELETE FROM chat_message
WHERE task_id = $1 AND role = 'user'
RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListChatInputMessages :many
SELECT * FROM chat_message
WHERE task_id = $1 AND role = 'user'
ORDER BY created_at ASC, id ASC;

-- name: ListChatMessagesPage :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateChatTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, chat_session_id,
    initiator_user_id, originator_user_id, accountable_user_id, force_fresh_session, runtime_mcp_overlay,
    runtime_connected_apps, originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    fire_at
)
VALUES (
    $1, $2, NULL,
    CASE WHEN sqlc.narg('fire_at')::timestamptz IS NULL THEN 'queued' ELSE 'deferred' END,
    $3, $4, $5,
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id),
    sqlc.narg('fire_at')::timestamptz
)
RETURNING *;

-- name: PromoteChannelChatTasksIfMediaReady :many
UPDATE agent_task_queue AS task
SET status = 'queued', fire_at = NULL
WHERE task.chat_session_id = @chat_session_id
  AND task.status = 'deferred'
  AND task.issue_id IS NULL
  AND task.parent_task_id IS NULL
  AND task.escalation_for_task_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS message
      WHERE message.chat_session_id = @chat_session_id
        AND message.role = 'user'
        AND message.channel_media_pending_until > now()
  )
RETURNING task.*;

-- name: SetChatTaskInputOwnerSelf :one
UPDATE agent_task_queue
SET chat_input_task_id = id
WHERE id = $1
RETURNING *;

-- name: GetLastChatTaskSession :one
SELECT session_id, work_dir, runtime_id FROM agent_task_queue
WHERE chat_session_id = $1
  AND (
    status IN ('completed', 'cancelled')
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
      AND NOT (COALESCE(error, '') ILIKE '%session/prompt%' AND COALESCE(error, '') ILIKE '%(code=-32603, data=BadRequestError)%')
    )
  )
  AND session_id IS NOT NULL
ORDER BY completed_at DESC
LIMIT 1;

-- name: GetPendingChatTask :one
SELECT id, status, created_at FROM agent_task_queue
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingChatTasksByCreator :many
SELECT atq.id AS task_id, atq.status, atq.chat_session_id, cs.agent_id
FROM agent_task_queue atq
JOIN chat_session cs ON cs.id = atq.chat_session_id
WHERE atq.chat_session_id IS NOT NULL
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
  AND cs.workspace_id = $1
  AND cs.creator_id = $2
ORDER BY atq.created_at DESC;

-- name: HasPendingChatTasksByCreator :one
SELECT EXISTS (
  SELECT 1
  FROM agent_task_queue atq
  JOIN chat_session cs ON cs.id = atq.chat_session_id
  WHERE atq.chat_session_id IS NOT NULL
    AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
    AND cs.workspace_id = sqlc.arg(workspace_id)
    AND cs.creator_id = sqlc.arg(creator_id)
    AND cs.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
) AS has_pending;

-- name: MarkChatSessionRead :exec
UPDATE chat_session SET last_read_at = now()
WHERE id = $1;

-- name: GetMostRecentUserChatMessage :one
SELECT * FROM chat_message
WHERE chat_session_id = $1 AND role = 'user'
ORDER BY created_at DESC
LIMIT 1;

-- name: ChatSessionHasUserMessage :one
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE chat_session_id = $1 AND role = 'user'
) AS has_user_message;

-- name: CreateChatDraftRestore :one
INSERT INTO chat_draft_restore (id, chat_session_id, task_id, content, attachment_ids)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListChatDraftRestoresBySession :many
SELECT * FROM chat_draft_restore
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: DeleteChatDraftRestore :execrows
DELETE FROM chat_draft_restore
WHERE id = $1 AND chat_session_id = $2;

-- name: DeleteChatDraftRestoresBySession :exec
DELETE FROM chat_draft_restore
WHERE chat_session_id = $1;

-- name: LockChatSessionForTask :one
SELECT cs.id
FROM agent_task_queue t
JOIN chat_session cs ON cs.id = t.chat_session_id
WHERE t.id = $1
FOR UPDATE OF cs;

-- name: LockChatSessionsByWorkspace :many
SELECT id FROM chat_session
WHERE workspace_id = $1
ORDER BY id
FOR UPDATE;

-- name: LockChatSessionsByArchivedRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: LockChatSessionsBySystemRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.kind = 'system'
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: DeleteChatDraftRestoresByArchivedRuntimeAgents :exec
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
);

-- name: DeleteChatDraftRestoresBySystemRuntimeAgents :exec
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
