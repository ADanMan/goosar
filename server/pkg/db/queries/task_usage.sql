-- name: UpsertTaskUsage :exec
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, sqlc.narg('cost_usd_ticks'), now())
ON CONFLICT (task_id, provider, model)
DO UPDATE SET
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    cost_usd_ticks = EXCLUDED.cost_usd_ticks,
    updated_at = now();

-- name: GetTaskUsage :many
SELECT * FROM task_usage
WHERE task_id = $1
ORDER BY model;

-- name: GetIssueUsageSummary :one
SELECT
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS total_cost_usd_ticks,
    COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = $1;

-- name: ListDashboardUsageDaily :many
SELECT
    DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) AS date,
    LOWER(provider) AS provider,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text), LOWER(provider), model
ORDER BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) DESC, LOWER(provider), model;

-- name: ListDashboardUsageByAgent :many
SELECT
    agent_id,
    LOWER(provider) AS provider,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY agent_id, LOWER(provider), model
ORDER BY agent_id, LOWER(provider), model;

-- name: ListDashboardRunTimeDaily :many
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)
ORDER BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) DESC;

-- name: ListDashboardAgentRunTime :many
SELECT
    atq.agent_id,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id
ORDER BY total_seconds DESC;

-- name: ListDashboardFailuresDaily :many
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    CASE
        WHEN atq.status = 'failed'
            THEN COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified')
        ELSE ''
    END AS failure_reason,
    COUNT(*)::int AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY 1, 2
ORDER BY 1 DESC, 2;

-- name: ListDashboardFailuresByAgent :many
SELECT
    atq.agent_id,
    CASE
        WHEN atq.status = 'failed'
            THEN COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified')
        ELSE ''
    END AS failure_reason,
    COUNT(*)::int AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id, 2
ORDER BY atq.agent_id, 2;
