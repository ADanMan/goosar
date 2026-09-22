
-- name: ListAutopilots :many
SELECT
  sqlc.embed(a),
  (
    SELECT array_agg(DISTINCT t.kind ORDER BY t.kind)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled
  )::text[] AS trigger_kinds,
  (
    SELECT min(t.next_run_at)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled AND t.kind = 'schedule'
  )::timestamptz AS next_run_at,
  COALESCE((
    SELECT r.status
    FROM autopilot_run r
    WHERE r.autopilot_id = a.id
    ORDER BY r.triggered_at DESC
    LIMIT 1
  ), '')::text AS last_run_status
FROM autopilot a
WHERE a.workspace_id = $1
  AND (
    (sqlc.narg('status')::text IS NULL AND a.status <> 'archived')
    OR a.status = sqlc.narg('status')
  )
ORDER BY a.created_at DESC;

-- name: GetAutopilot :one
SELECT * FROM autopilot
WHERE id = $1;

-- name: GetAutopilotInWorkspace :one
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAutopilot :one
INSERT INTO autopilot (
    workspace_id, title, description, assignee_type, assignee_id,
    status, execution_mode, issue_title_template, project_id,
    created_by_type, created_by_id, external_key
) VALUES (
    $1, $2, sqlc.narg('description'), $3, $4,
    $5, $6, sqlc.narg('issue_title_template'), sqlc.narg('project_id'),
    $7, $8, sqlc.narg('external_key')
) RETURNING *;

-- name: UpdateAutopilot :one
UPDATE autopilot SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    assignee_type = COALESCE(sqlc.narg('assignee_type'), assignee_type),
    assignee_id = COALESCE(sqlc.narg('assignee_id')::uuid, assignee_id),
    status = COALESCE(sqlc.narg('status'), status),
    execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    issue_title_template = sqlc.narg('issue_title_template'),
    project_id = sqlc.narg('project_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAutopilot :exec
UPDATE autopilot
SET status = 'archived', updated_at = now()
WHERE id = $1;

-- name: UpdateAutopilotLastRunAt :exec
UPDATE autopilot SET last_run_at = now(), updated_at = now()
WHERE id = $1;

-- name: CreateAutopilotRuleVersion :one
INSERT INTO autopilot_rule_version (
    autopilot_id, workspace_id, published_by_type, published_by_id, config_summary
)
VALUES (
    @autopilot_id, @workspace_id, @published_by_type, sqlc.narg(published_by_id),
    COALESCE(sqlc.narg(config_summary), '{}'::jsonb)
)
RETURNING *;

-- name: GetActiveAutopilotRuleVersion :one
SELECT * FROM autopilot_rule_version
WHERE workspace_id = $1 AND autopilot_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: ListAutopilotTriggers :many
SELECT * FROM autopilot_trigger
WHERE autopilot_id = $1
ORDER BY created_at ASC;

-- name: GetAutopilotTrigger :one
SELECT * FROM autopilot_trigger
WHERE id = $1;

-- name: CreateAutopilotTrigger :one
INSERT INTO autopilot_trigger (
    autopilot_id, kind, enabled, cron_expression, timezone,
    next_run_at, webhook_token, label, provider, event_filters,
    published_by_type, published_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('cron_expression'), sqlc.narg('timezone'),
    sqlc.narg('next_run_at'), sqlc.narg('webhook_token'), sqlc.narg('label'),
    COALESCE(sqlc.narg('provider')::text, 'generic'),
    sqlc.narg('event_filters'),
    sqlc.narg('published_by_type'), sqlc.narg('published_by_id')
) RETURNING *;

-- name: SetAutopilotTriggerPublisher :exec
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE id = $1;

-- name: SetAutopilotTriggerPublishersByAutopilot :exec
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE autopilot_id = $1;

-- name: UpdateAutopilotTrigger :one
UPDATE autopilot_trigger SET
    enabled = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    next_run_at = sqlc.narg('next_run_at'),
    label = COALESCE(sqlc.narg('label'), label),
    event_filters = COALESCE(sqlc.narg('event_filters'), event_filters),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAutopilotTrigger :exec
DELETE FROM autopilot_trigger WHERE id = $1;

-- name: AdvanceTriggerNextRun :exec
UPDATE autopilot_trigger
SET next_run_at = sqlc.narg('next_run_at'),
    last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: GetWebhookTriggerByToken :one
SELECT t.*, a.workspace_id AS autopilot_workspace_id
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'webhook'
  AND t.webhook_token = $1;

-- name: TouchAutopilotTriggerFiredAt :exec
UPDATE autopilot_trigger
SET last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: RotateAutopilotTriggerWebhookToken :one
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- name: SetAutopilotTriggerWebhookToken :one
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAutopilotTriggerSigningSecret :one
UPDATE autopilot_trigger
SET signing_secret = sqlc.narg('signing_secret'),
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- name: CreateAutopilotRun :one
INSERT INTO autopilot_run (
    autopilot_id, trigger_id, source, status, trigger_payload, squad_id, planned_at,
    webhook_delivery_id
) VALUES (
    $1, sqlc.narg('trigger_id'), $2, $3, sqlc.narg('trigger_payload'),
    sqlc.narg('squad_id'), sqlc.narg('planned_at'),
    sqlc.narg('webhook_delivery_id')
) RETURNING *;

-- name: GetAutopilotRunByTriggerAndPlanned :one
SELECT * FROM autopilot_run
WHERE trigger_id = $1
  AND planned_at = $2
LIMIT 1;

-- name: GetAutopilotRunByWebhookDelivery :one
SELECT * FROM autopilot_run
WHERE webhook_delivery_id = $1
LIMIT 1;

-- name: RecoverPartialAutopilotRun :exec
UPDATE autopilot_run
SET status = 'failed',
    completed_at = now(),
    failure_reason = 'recovered partial dispatch (crashed before downstream creation)',
    planned_at = NULL
WHERE id = $1;

-- name: GetAutopilotRun :one
SELECT * FROM autopilot_run
WHERE id = $1;

-- name: ListAutopilotRuns :many
SELECT * FROM autopilot_run
WHERE autopilot_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateAutopilotRunIssueCreated :one
UPDATE autopilot_run
SET status = 'issue_created', issue_id = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunRunning :one
UPDATE autopilot_run
SET status = 'running', task_id = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunCompleted :one
UPDATE autopilot_run
SET status = 'completed', completed_at = now(), result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunFailed :one
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkipped :one
UPDATE autopilot_run
SET status = 'skipped', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkippedWithResult :one
UPDATE autopilot_run
SET status = 'skipped',
    completed_at = now(),
    failure_reason = $2,
    result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- name: ListSchedulableAutopilotTriggers :many
SELECT t.id, t.autopilot_id, t.cron_expression, t.timezone, t.created_at, t.last_fired_at
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'schedule'
  AND t.enabled = TRUE
  AND a.status = 'active'
  AND t.cron_expression IS NOT NULL
  AND t.cron_expression <> ''
ORDER BY t.id;

-- name: CreateAutopilotTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, status, priority, autopilot_run_id, trigger_summary,
    originator_user_id, accountable_user_id, rule_version_id,
    originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    runtime_mcp_overlay, runtime_connected_apps
)
VALUES (
    $1, $2, NULL, 'queued', $3, $4, sqlc.narg(trigger_summary),
    sqlc.narg(originator_user_id),
    sqlc.narg(accountable_user_id),
    sqlc.narg(rule_version_id),
    sqlc.narg(originator_source),
    sqlc.narg(trigger_evidence_kind),
    sqlc.narg(trigger_evidence_ref_id),
    sqlc.narg(runtime_mcp_overlay),
    sqlc.narg(runtime_connected_apps)
)
RETURNING *;

-- name: GetAutopilotTaskByRun :one
SELECT * FROM agent_task_queue
WHERE autopilot_run_id = $1
ORDER BY created_at
LIMIT 1;

-- name: GetAutopilotRunByIssue :one
SELECT * FROM autopilot_run
WHERE issue_id = $1 AND status IN ('issue_created', 'running')
LIMIT 1;

-- name: FailAutopilotRunsByIssue :exec
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = 'linked issue was deleted'
WHERE issue_id = $1
  AND status IN ('issue_created', 'running');

-- name: SelectAutopilotsExceedingFailureThreshold :many
WITH stats AS (
    SELECT autopilot_id,
           count(*) FILTER (WHERE status IN ('completed', 'failed')) AS total,
           count(*) FILTER (WHERE status = 'failed') AS failed
    FROM autopilot_run
    WHERE created_at >= sqlc.arg('since')::timestamptz
    GROUP BY autopilot_id
)
SELECT a.id, a.workspace_id, a.title, a.assignee_id,
       a.created_by_type, a.created_by_id,
       s.total::bigint  AS total_runs,
       s.failed::bigint AS failed_runs
FROM autopilot a
JOIN stats s ON s.autopilot_id = a.id
WHERE a.status = 'active'
  AND s.total >= sqlc.arg('min_runs')::bigint
  AND s.failed::float8 / NULLIF(s.total, 0)::float8 >= sqlc.arg('fail_ratio_threshold')::float8
ORDER BY s.failed DESC, a.id ASC;

-- name: SystemPauseAutopilot :one
UPDATE autopilot
SET status = 'paused', updated_at = now()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- name: ListAutopilotSubscribers :many
SELECT * FROM autopilot_subscriber
WHERE autopilot_id = $1
ORDER BY created_at ASC, user_id ASC;

-- name: AddAutopilotSubscriber :exec
INSERT INTO autopilot_subscriber (autopilot_id, user_type, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (autopilot_id, user_type, user_id) DO NOTHING;

-- name: DeleteAutopilotSubscribersForAutopilot :exec
DELETE FROM autopilot_subscriber
WHERE autopilot_id = $1;

-- name: ListAutopilotCollaborators :many
SELECT * FROM autopilot_collaborator
WHERE autopilot_id = $1
ORDER BY created_at ASC, user_id ASC;

-- name: AddAutopilotCollaborator :one
INSERT INTO autopilot_collaborator (autopilot_id, user_type, user_id, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (autopilot_id, user_type, user_id)
    DO UPDATE SET granted_by = EXCLUDED.granted_by
RETURNING *;

-- name: DeleteAutopilotCollaborator :exec
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1 AND user_type = $2 AND user_id = $3;

-- name: DeleteAutopilotCollaboratorsForAutopilot :exec
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1;

-- name: IsAutopilotCollaborator :one
SELECT EXISTS (
    SELECT 1 FROM autopilot_collaborator
    WHERE autopilot_id = $1 AND user_type = 'member' AND user_id = $2
) AS is_collaborator;

-- name: ListAutopilotIDsForCollaborator :many
SELECT autopilot_id FROM autopilot_collaborator
WHERE user_type = 'member' AND user_id = $1;

-- name: TemplateAutopilotExists :one
SELECT EXISTS (
    SELECT 1 FROM autopilot
    WHERE workspace_id = $1 AND external_key = $2
);

-- name: AssignRoleAgentToTemplateAutopilots :many
UPDATE autopilot SET
    assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE workspace_id = $1
  AND assignee_id IS NULL
  AND status = 'paused'
  AND external_key LIKE 'template:%'
RETURNING id;
