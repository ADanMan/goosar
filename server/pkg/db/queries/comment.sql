-- name: ListCommentsForIssue :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC
LIMIT $3;

-- name: ListCommentsSinceForIssue :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2 AND created_at > $3
ORDER BY created_at ASC, id ASC
LIMIT $4;

-- name: ListRootCommentsForIssue :many
WITH RECURSIVE selected_roots AS (
    SELECT c.id, c.created_at
    FROM comment c
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
      AND c.parent_id IS NULL
    ORDER BY c.created_at ASC, c.id ASC
    LIMIT @row_limit
),
membership(id, root_id, comment_created_at) AS (
    SELECT sr.id, sr.id AS root_id, sr.created_at
    FROM selected_roots sr
    UNION ALL
    SELECT c.id, m.root_id, c.created_at
    FROM comment c
    JOIN membership m ON c.parent_id = m.id
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
),
thread_stats AS (
    SELECT root_id,
           (COUNT(*) - 1)::int AS reply_count,
           MAX(comment_created_at)::timestamptz AS last_activity_at
    FROM membership
    GROUP BY root_id
)
SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
       c.created_at, c.updated_at, c.parent_id, c.workspace_id,
       c.resolved_at, c.resolved_by_type, c.resolved_by_id,
       ts.reply_count AS reply_count,
       ts.last_activity_at AS last_activity_at
FROM selected_roots sr
JOIN comment c ON c.id = sr.id
JOIN thread_stats ts ON ts.root_id = sr.id
ORDER BY c.created_at ASC, c.id ASC;

-- name: ListRootCommentsSinceForIssue :many
WITH RECURSIVE selected_roots AS (
    SELECT c.id, c.created_at
    FROM comment c
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
      AND c.parent_id IS NULL
      AND c.created_at > @since
    ORDER BY c.created_at ASC, c.id ASC
    LIMIT @row_limit
),
membership(id, root_id, comment_created_at) AS (
    SELECT sr.id, sr.id AS root_id, sr.created_at
    FROM selected_roots sr
    UNION ALL
    SELECT c.id, m.root_id, c.created_at
    FROM comment c
    JOIN membership m ON c.parent_id = m.id
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
),
thread_stats AS (
    SELECT root_id,
           (COUNT(*) - 1)::int AS reply_count,
           MAX(comment_created_at)::timestamptz AS last_activity_at
    FROM membership
    GROUP BY root_id
)
SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
       c.created_at, c.updated_at, c.parent_id, c.workspace_id,
       c.resolved_at, c.resolved_by_type, c.resolved_by_id,
       ts.reply_count AS reply_count,
       ts.last_activity_at AS last_activity_at
FROM selected_roots sr
JOIN comment c ON c.id = sr.id
JOIN thread_stats ts ON ts.root_id = sr.id
ORDER BY c.created_at ASC, c.id ASC;

-- name: ListThreadCommentsForIssue :many
WITH RECURSIVE root_of AS (
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @anchor_id AND c.issue_id = @issue_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
),
thread_root AS (
    SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1
),
descendants AS (
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN thread_root tr ON c.id = tr.id
    UNION
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.issue_id = @issue_id AND c.workspace_id = @workspace_id
)
SELECT id, issue_id, author_type, author_id, content, type,
       created_at, updated_at, parent_id, workspace_id,
       resolved_at, resolved_by_type, resolved_by_id
FROM descendants
ORDER BY created_at ASC, id ASC
LIMIT @row_limit;

-- name: ListThreadCommentsForIssuePaged :many
WITH RECURSIVE root_of AS (
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @anchor_id AND c.issue_id = @issue_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
),
thread_root AS (
    SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1
),
descendants AS (
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN thread_root tr ON c.id = tr.id
    UNION
    SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
           c.created_at, c.updated_at, c.parent_id, c.workspace_id,
           c.resolved_at, c.resolved_by_type, c.resolved_by_id
    FROM comment c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.issue_id = @issue_id AND c.workspace_id = @workspace_id
),
reply_page AS (
    SELECT d.id, d.issue_id, d.author_type, d.author_id, d.content, d.type,
           d.created_at, d.updated_at, d.parent_id, d.workspace_id,
           d.resolved_at, d.resolved_by_type, d.resolved_by_id
    FROM descendants d
    WHERE d.id NOT IN (SELECT id FROM thread_root)
      AND (
          @has_cursor::boolean = FALSE
          OR (d.created_at, d.id) < (@before_at::timestamptz, @before_id::uuid)
      )
    ORDER BY d.created_at DESC, d.id DESC
    LIMIT @reply_limit
)
SELECT id, issue_id, author_type, author_id, content, type,
       created_at, updated_at, parent_id, workspace_id,
       resolved_at, resolved_by_type, resolved_by_id
FROM (
    SELECT d.id, d.issue_id, d.author_type, d.author_id, d.content, d.type,
           d.created_at, d.updated_at, d.parent_id, d.workspace_id,
           d.resolved_at, d.resolved_by_type, d.resolved_by_id
    FROM descendants d
    JOIN thread_root tr ON d.id = tr.id
    UNION ALL
    SELECT id, issue_id, author_type, author_id, content, type,
           created_at, updated_at, parent_id, workspace_id,
           resolved_at, resolved_by_type, resolved_by_id
    FROM reply_page
) combined
ORDER BY created_at ASC, id ASC;

-- name: ListRecentThreadCommentsForIssue :many
WITH RECURSIVE membership(id, root_id, comment_created_at) AS (
    SELECT c.id, c.id AS root_id, c.created_at
    FROM comment c
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
      AND c.parent_id IS NULL
    UNION ALL
    SELECT c.id, m.root_id, c.created_at
    FROM comment c
    JOIN membership m ON c.parent_id = m.id
    WHERE c.issue_id = @issue_id
      AND c.workspace_id = @workspace_id
),
thread_stats AS (
    SELECT root_id, MAX(comment_created_at)::timestamptz AS last_activity_at
    FROM membership
    GROUP BY root_id
),
picked AS (
    SELECT ts.root_id, ts.last_activity_at
    FROM thread_stats ts
    WHERE (
        @has_cursor::boolean = FALSE
        OR (ts.last_activity_at, ts.root_id) < (@before_at::timestamptz, @before_id::uuid)
    )
    ORDER BY ts.last_activity_at DESC, ts.root_id DESC
    LIMIT @thread_limit
)
SELECT c.id, c.issue_id, c.author_type, c.author_id, c.content, c.type,
       c.created_at, c.updated_at, c.parent_id, c.workspace_id,
       c.resolved_at, c.resolved_by_type, c.resolved_by_id,
       p.root_id AS thread_root_id,
       p.last_activity_at AS thread_last_activity_at
FROM picked p
JOIN membership m ON m.root_id = p.root_id
JOIN comment c ON c.id = m.id
ORDER BY p.last_activity_at ASC, p.root_id ASC, c.created_at ASC, c.id ASC;

-- name: CountComments :one
SELECT count(*) FROM comment
WHERE issue_id = $1 AND workspace_id = $2;

-- name: CountNewCommentsSince :one
SELECT count(*) FROM comment
WHERE issue_id = @issue_id
  AND workspace_id = @workspace_id
  AND created_at > @since
  AND id <> @anchor_id
  AND NOT (author_type = 'agent' AND author_id = @author_id);

-- name: GetLatestMemberCommentForIssueSince :one
SELECT * FROM comment
WHERE issue_id = @issue_id
  AND author_type = 'member'
  AND created_at > @since
ORDER BY created_at DESC
LIMIT 1;

-- name: ListReconcilableCommentsForIssueSince :many
SELECT * FROM comment
WHERE issue_id = @issue_id
  AND author_type IN ('member', 'agent')
  AND (
      created_at > @since
      OR id = ANY(@planned_comment_ids::uuid[])
  )
ORDER BY created_at ASC, id ASC;

-- name: GetComment :one
SELECT * FROM comment
WHERE id = $1;

-- name: GetCommentInWorkspace :one
SELECT * FROM comment
WHERE id = $1 AND workspace_id = $2;

-- name: GetThreadRoot :one
WITH RECURSIVE root_of AS (
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @comment_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
)
SELECT c.* FROM comment c
WHERE c.id = (SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1);

-- name: CreateComment :one
WITH touched_issue AS (
    UPDATE issue SET updated_at = now()
    WHERE issue.id = sqlc.arg(issue_id) AND issue.workspace_id = sqlc.arg(workspace_id)
    RETURNING issue.id, issue.workspace_id
)
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, source_task_id)
SELECT ti.id, ti.workspace_id, sqlc.arg(author_type), sqlc.arg(author_id), sqlc.arg(content), sqlc.arg(type), sqlc.narg(parent_id), sqlc.narg(source_task_id)
FROM touched_issue ti
RETURNING *;

-- name: UpdateComment :one
UPDATE comment SET
    content = $2,
    source_task_id = sqlc.narg(source_task_id),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: HasAgentCommentedSince :one
SELECT EXISTS (
    SELECT 1 FROM comment
    WHERE issue_id = @issue_id
      AND author_type = 'agent'
      AND author_id = @author_id
      AND created_at >= @since
) AS commented;

-- name: GetLatestAgentCommentIssueBySourceTask :one
SELECT c.issue_id, i.number AS issue_number, w.issue_prefix AS issue_prefix
FROM comment c
JOIN issue i ON i.id = c.issue_id
JOIN workspace w ON w.id = c.workspace_id
WHERE c.source_task_id = @task_id
  AND c.author_type = 'agent'
ORDER BY c.created_at DESC
LIMIT 1;

-- name: HasAgentRepliedInThread :one
SELECT count(*) > 0 AS has_replied FROM comment
WHERE parent_id = @parent_id AND author_type = 'agent' AND author_id = @agent_id;

-- name: DeleteComment :exec
DELETE FROM comment WHERE id = $1 AND workspace_id = $2;

-- name: ResolveComment :one
UPDATE comment SET
    resolved_at = COALESCE(resolved_at, now()),
    resolved_by_type = COALESCE(resolved_by_type, $2),
    resolved_by_id = COALESCE(resolved_by_id, $3),
    updated_at = CASE WHEN resolved_at IS NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;

-- name: ClearOtherThreadResolutions :many
WITH RECURSIVE root_of AS (
    SELECT c.id, c.parent_id
    FROM comment c
    WHERE c.id = @target_id AND c.issue_id = @issue_id AND c.workspace_id = @workspace_id
    UNION ALL
    SELECT p.id, p.parent_id
    FROM comment p
    JOIN root_of r ON p.id = r.parent_id
),
thread_root AS (
    SELECT id FROM root_of WHERE parent_id IS NULL LIMIT 1
),
descendants AS (
    SELECT c.id
    FROM comment c
    JOIN thread_root tr ON c.id = tr.id
    UNION
    SELECT c.id
    FROM comment c
    JOIN descendants d ON c.parent_id = d.id
    WHERE c.issue_id = @issue_id AND c.workspace_id = @workspace_id
)
UPDATE comment SET
    resolved_at = NULL,
    resolved_by_type = NULL,
    resolved_by_id = NULL,
    updated_at = now()
WHERE comment.id IN (SELECT id FROM descendants)
  AND comment.id <> @target_id
  AND comment.resolved_at IS NOT NULL
RETURNING *;

-- name: UnresolveComment :one
UPDATE comment SET
    resolved_at = NULL,
    resolved_by_type = NULL,
    resolved_by_id = NULL,
    updated_at = CASE WHEN resolved_at IS NOT NULL THEN now() ELSE updated_at END
WHERE id = $1
RETURNING *;
