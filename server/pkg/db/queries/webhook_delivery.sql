
-- name: CreateWebhookDelivery :one
INSERT INTO webhook_delivery (
    workspace_id, autopilot_id, trigger_id, provider, event,
    dedupe_key, dedupe_source, signature_status, status,
    selected_headers, content_type, raw_body,
    replayed_from_delivery_id
) VALUES (
    $1, $2, $3, $4, $5,
    sqlc.narg('dedupe_key'), sqlc.narg('dedupe_source'), $6, $7,
    $8, sqlc.narg('content_type'), sqlc.narg('raw_body'),
    sqlc.narg('replayed_from_delivery_id')
) RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_delivery
WHERE id = $1;

-- name: GetWebhookDeliveryInWorkspace :one
SELECT * FROM webhook_delivery
WHERE id = $1 AND workspace_id = $2;

-- name: GetWebhookDeliveryByTriggerAndDedupe :one
SELECT * FROM webhook_delivery
WHERE trigger_id = $1
  AND dedupe_key = $2
ORDER BY (status IN ('rejected', 'failed')), created_at DESC
LIMIT 1;

-- name: BumpWebhookDeliveryAttempt :one
UPDATE webhook_delivery
SET attempt_count = attempt_count + 1,
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: AcknowledgeWebhookDelivery :one
UPDATE webhook_delivery
SET response_status = $2,
    response_body = $3,
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: ClaimQueuedWebhookDelivery :one
WITH candidate AS (
    SELECT id
    FROM webhook_delivery
    WHERE status = 'queued'
      AND available_at <= now()
      AND (lease_expires_at IS NULL OR lease_expires_at <= now())
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE webhook_delivery AS d
SET lease_token = gen_random_uuid(),
    lease_expires_at = now() + interval '2 minutes'
FROM candidate
WHERE d.id = candidate.id
RETURNING d.*;

-- name: DeferClaimedWebhookDelivery :one
UPDATE webhook_delivery
SET available_at = $3,
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: RetryClaimedWebhookDelivery :one
UPDATE webhook_delivery
SET available_at = $3,
    dispatch_attempts = dispatch_attempts + 1,
    error = $4,
    lease_token = NULL,
    lease_expires_at = NULL,
    last_attempt_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: CompleteClaimedWebhookDelivery :one
UPDATE webhook_delivery
SET status = $3,
    autopilot_run_id = sqlc.narg('autopilot_run_id'),
    dispatch_attempts = dispatch_attempts + 1,
    error = sqlc.narg('error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    last_attempt_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: UpdateWebhookDeliveryDispatched :one
UPDATE webhook_delivery
SET status = $2,
    autopilot_run_id = sqlc.narg('autopilot_run_id'),
    response_status = sqlc.narg('response_status'),
    response_body = sqlc.narg('response_body'),
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWebhookDeliveryTerminal :one
UPDATE webhook_delivery
SET status = $2,
    error = sqlc.narg('error'),
    response_status = sqlc.narg('response_status'),
    response_body = sqlc.narg('response_body'),
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: ListWebhookDeliveriesByAutopilot :many
SELECT
    d.id, d.workspace_id, d.autopilot_id, d.trigger_id, d.provider, d.event,
    d.dedupe_key, d.dedupe_source, d.signature_status, d.status,
    d.attempt_count, d.content_type, d.response_status,
    d.autopilot_run_id, d.replayed_from_delivery_id, d.error,
    d.received_at, d.last_attempt_at, d.created_at,
    d.available_at, d.dispatch_attempts
FROM webhook_delivery d
JOIN autopilot a ON a.id = d.autopilot_id
WHERE d.autopilot_id = $1
  AND a.workspace_id = $2
ORDER BY d.created_at DESC
LIMIT $3 OFFSET $4;
