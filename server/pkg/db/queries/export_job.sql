-- name: CreateExportJob :one
INSERT INTO export_job (workspace_id, requested_by)
VALUES ($1, $2)
RETURNING *;

-- name: GetExportJob :one
SELECT * FROM export_job WHERE id = $1;

-- name: GetExportJobForWorkspace :one
SELECT * FROM export_job WHERE id = $1 AND workspace_id = $2;

-- name: StartExportJob :one
UPDATE export_job
SET status = 'running', started_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CompleteExportJob :one
UPDATE export_job
SET status = 'completed', completed_at = now(),
    file_path = $2, size_bytes = $3, manifest = $4, error = NULL
WHERE id = $1
RETURNING *;

-- name: FailExportJob :one
UPDATE export_job
SET status = 'failed', completed_at = now(), error = $2
WHERE id = $1
RETURNING *;

-- name: ReapStaleExportJobs :many
UPDATE export_job
SET status = 'failed', completed_at = now(),
    error = 'export did not finish before the timeout (server restart or timeout)'
WHERE status IN ('pending', 'running')
  AND created_at < now() - make_interval(secs => sqlc.arg(timeout_secs)::float8)
RETURNING id, workspace_id, file_path;

-- name: ListExpiredExportArtifacts :many
SELECT id, file_path FROM export_job
WHERE status = 'completed' AND file_path IS NOT NULL
  AND completed_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
ORDER BY completed_at
LIMIT sqlc.arg(max_rows);

-- name: ForgetExportArtifact :exec
UPDATE export_job SET file_path = NULL, size_bytes = 0 WHERE id = $1;
