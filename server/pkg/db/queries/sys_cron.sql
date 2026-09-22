-- name: PurgeCronExecutions :execrows
WITH doomed AS (
    SELECT id
      FROM sys_cron_executions
     WHERE status IN ('SUCCESS', 'FAILED')
       AND finished_at < now() - make_interval(secs => sqlc.arg(retention_secs)::float8)
     ORDER BY finished_at
     LIMIT sqlc.arg(max_rows)
)
DELETE FROM sys_cron_executions e
 USING doomed d
 WHERE e.id = d.id;
