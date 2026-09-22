
-- name: ListAttachmentTombstones :many
SELECT attachment_id, url FROM attachment_tombstone
WHERE deleted_at < now() - make_interval(secs => sqlc.arg(grace_secs)::float8)
ORDER BY deleted_at
LIMIT sqlc.arg(max_rows);

-- name: DeleteAttachmentTombstones :execrows
DELETE FROM attachment_tombstone t
WHERE t.attachment_id = ANY(sqlc.arg(attachment_ids)::uuid[])
  AND NOT EXISTS (SELECT 1 FROM attachment a WHERE a.url = t.url);

-- name: ListRevivedAttachmentTombstones :many
SELECT attachment_id FROM attachment_tombstone t
WHERE t.attachment_id = ANY(sqlc.arg(attachment_ids)::uuid[])
  AND EXISTS (SELECT 1 FROM attachment a WHERE a.url = t.url);

-- name: DropAttachmentTombstones :execrows
DELETE FROM attachment_tombstone
WHERE attachment_id = ANY(sqlc.arg(attachment_ids)::uuid[]);

-- name: PurgeChatMessages :execrows
DELETE FROM chat_session
WHERE id IN (
    SELECT id FROM chat_session
    WHERE updated_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
    ORDER BY updated_at
    LIMIT sqlc.arg(max_rows)
);

-- name: PurgeCompletedTasks :execrows
DELETE FROM agent_task_queue
WHERE id IN (
    SELECT id FROM agent_task_queue
    WHERE status IN ('completed', 'failed', 'cancelled')
      AND completed_at IS NOT NULL
      AND completed_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
    ORDER BY completed_at
    LIMIT sqlc.arg(max_rows)
);

-- name: PurgeClosedIssues :execrows
DELETE FROM issue
WHERE id IN (
    SELECT id FROM issue
    WHERE status = 'done'
      AND updated_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
    ORDER BY updated_at
    LIMIT sqlc.arg(max_rows)
);

-- name: PurgeActivityLog :execrows
DELETE FROM activity_log
WHERE id IN (
    SELECT id FROM activity_log
    WHERE created_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
    ORDER BY created_at
    LIMIT sqlc.arg(max_rows)
);

-- name: CountChatSessionsOlderThan :one
SELECT count(*) FROM chat_session
WHERE updated_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8);

-- name: CountCompletedTasksOlderThan :one
SELECT count(*) FROM agent_task_queue
WHERE status IN ('completed', 'failed', 'cancelled')
  AND completed_at IS NOT NULL
  AND completed_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8);

-- name: CountClosedIssuesOlderThan :one
SELECT count(*) FROM issue
WHERE status = 'done'
  AND updated_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8);

-- name: CountActivityLogOlderThan :one
SELECT count(*) FROM activity_log
WHERE created_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8);

-- name: CountExpiredVerificationCodes :one
SELECT count(*) FROM verification_code WHERE expires_at < now();
