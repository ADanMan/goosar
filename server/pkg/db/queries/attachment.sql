-- name: CreateAttachment :one
INSERT INTO attachment (
  id, workspace_id, issue_id, comment_id, chat_session_id, task_id,
  uploader_type, uploader_id, filename, url, content_type, size_bytes
)
VALUES (
  $1, $2, sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id), sqlc.narg(task_id),
  $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListAttachmentsByIssue :many
SELECT * FROM attachment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = $1 AND workspace_id = $2;

-- name: GetAttachmentByIDOnly :one
SELECT * FROM attachment
WHERE id = $1;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentURLsByIssueOrComments :many
SELECT a.url FROM attachment a
WHERE a.issue_id = $1
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.issue_id = $1);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = $1;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = $1
WHERE issue_id = $2
  AND comment_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: ReplaceCommentAttachments :exec
UPDATE attachment
SET comment_id = CASE
  WHEN id = ANY(sqlc.arg(attachment_ids)::uuid[]) THEN $1
  ELSE NULL
END
WHERE issue_id = $2
  AND (
    comment_id = $1
    OR (comment_id IS NULL AND id = ANY(sqlc.arg(attachment_ids)::uuid[]))
  );

-- name: LinkAttachmentsToChatMessage :many
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id),
    chat_session_id = sqlc.arg(chat_session_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
  AND (
    chat_session_id IS NULL
    OR chat_session_id = sqlc.arg(chat_session_id)
  )
  AND uploader_type = sqlc.arg(uploader_type)
  AND uploader_id = sqlc.arg(uploader_id)
  AND id = ANY(sqlc.arg(attachment_ids)::uuid[])
RETURNING id;

-- name: DetachAttachmentsFromUserChatMessageByTask :many
UPDATE attachment
SET chat_message_id = NULL
WHERE chat_message_id IN (
  SELECT id FROM chat_message WHERE chat_message.task_id = $1 AND role = 'user'
)
RETURNING *;

-- name: CountUnboundChatAttachmentsForTask :one
SELECT COUNT(*) FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL;

-- name: BindChatAttachmentsToMessage :many
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND task_id = sqlc.arg(task_id)
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL
RETURNING id;

-- name: ListAttachmentsByChatMessage :many
SELECT * FROM attachment
WHERE chat_message_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByChatMessageIDs :many
SELECT * FROM attachment
WHERE chat_message_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: LinkAttachmentsToIssue :exec
UPDATE attachment
SET issue_id = $1
WHERE workspace_id = $2
  AND issue_id IS NULL
  AND id = ANY($3::uuid[]);

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = $1 AND workspace_id = $2;

-- name: ListAttachmentsByIDs :many
SELECT * FROM attachment
WHERE id = ANY(sqlc.arg(attachment_ids)::uuid[]) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC;

-- name: ListOrphanAttachments :many
SELECT a.id, a.url FROM attachment a
WHERE a.issue_id IS NULL
  AND a.comment_id IS NULL
  AND a.chat_message_id IS NULL
  AND a.created_at < now() - make_interval(secs => sqlc.arg(grace_secs)::float8)
  AND NOT EXISTS (SELECT 1 FROM "user" u WHERE u.avatar_url = a.url)
  AND NOT EXISTS (SELECT 1 FROM agent ag WHERE ag.avatar_url = a.url)
  AND NOT EXISTS (SELECT 1 FROM squad s WHERE s.avatar_url = a.url)
  AND NOT EXISTS (SELECT 1 FROM workspace w WHERE w.avatar_url = a.url)
  AND NOT EXISTS (SELECT 1 FROM chat_draft_restore r WHERE a.id = ANY(r.attachment_ids))
  AND NOT EXISTS (SELECT 1 FROM feedback f WHERE f.message LIKE '%' || a.id::text || '%')
ORDER BY a.created_at
LIMIT sqlc.arg(max_rows);

-- name: DeleteAttachmentsByIDs :execrows
DELETE FROM attachment
WHERE id = ANY(sqlc.arg(attachment_ids)::uuid[])
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL;
