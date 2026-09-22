
-- name: ListGitHubPRRowsByAddress :many
SELECT id, workspace_id, head_sha, state
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND pr_number = $4;

-- name: UpdateGitHubPRSnapshot :execrows
UPDATE github_pull_request
SET api_mergeable          = sqlc.narg('api_mergeable'),
    api_merge_state_status = sqlc.narg('api_merge_state_status'),
    checks_rollup_state    = sqlc.narg('checks_rollup_state'),
    snapshot_head_sha      = sqlc.arg('head_sha'),
    snapshot_fetched_at    = sqlc.arg('fetched_at'),
    updated_at             = now()
WHERE id = sqlc.arg('pr_id') AND head_sha = sqlc.arg('head_sha');

-- name: DeleteGitHubPRCheckRuns :exec
DELETE FROM github_pull_request_check_run WHERE pr_id = $1;

-- name: InsertGitHubPRCheckRun :exec
INSERT INTO github_pull_request_check_run (
    pr_id, head_sha, ordinal, name, status, conclusion, details_url, is_status_context
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('conclusion'), sqlc.narg('details_url'), $6
);

-- name: ListStaleUndecidedGitHubPRs :many
WITH candidates AS (
    SELECT installation_id, repo_owner, repo_name, pr_number
    FROM github_pull_request AS pr
    WHERE state IN ('open', 'draft')
      AND (snapshot_fetched_at IS NULL OR snapshot_fetched_at < sqlc.arg('older_than'))
      AND (
          snapshot_fetched_at IS NULL
          OR api_mergeable IS NULL
          OR api_mergeable = 'UNKNOWN'
          OR checks_rollup_state IN ('PENDING', 'EXPECTED')
          OR EXISTS (
              SELECT 1
              FROM github_pull_request_check_run AS cr
              WHERE cr.pr_id = pr.id AND cr.status <> 'completed'
          )
      )
    GROUP BY installation_id, repo_owner, repo_name, pr_number
)
SELECT installation_id, repo_owner, repo_name, pr_number
FROM candidates
ORDER BY (
    ROW(installation_id, repo_owner, repo_name, pr_number) >
    ROW(
        sqlc.arg('after_installation_id')::BIGINT,
        sqlc.arg('after_repo_owner')::TEXT,
        sqlc.arg('after_repo_name')::TEXT,
        sqlc.arg('after_pr_number')::INTEGER
    )
) DESC,
installation_id, repo_owner, repo_name, pr_number
LIMIT sqlc.arg('max_rows');

-- name: ListGitHubPRNumbersByHeadSHA :many
SELECT DISTINCT pr_number
FROM github_pull_request
WHERE installation_id = $1 AND repo_owner = $2 AND repo_name = $3 AND head_sha = $4;

-- name: GetGitHubPullRequestByID :one
SELECT * FROM github_pull_request WHERE id = $1;
