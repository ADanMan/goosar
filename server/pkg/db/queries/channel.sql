

-- name: UpsertChannelInstallation :one
INSERT INTO channel_installation (
    workspace_id, agent_id, channel_type, config, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (workspace_id, agent_id, channel_type) DO UPDATE SET
    channel_type      = EXCLUDED.channel_type,
    config            = EXCLUDED.config,
    installer_user_id = EXCLUDED.installer_user_id,
    status            = 'active',
    installed_at      = now(),
    updated_at        = now()
RETURNING *;

-- name: UpsertChannelInstallationByAppID :one
INSERT INTO channel_installation (
    workspace_id, agent_id, channel_type, config, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (channel_type, (config ->> 'app_id')) DO UPDATE SET
    agent_id          = EXCLUDED.agent_id,
    config            = EXCLUDED.config,
    installer_user_id = EXCLUDED.installer_user_id,
    status            = 'active',
    installed_at      = now(),
    updated_at        = now()
WHERE channel_installation.workspace_id = EXCLUDED.workspace_id
RETURNING *;

-- name: GetChannelInstallation :one
SELECT * FROM channel_installation
WHERE id = sqlc.arg('id') AND channel_type = sqlc.arg('channel_type');

-- name: GetChannelInstallationInWorkspace :one
SELECT * FROM channel_installation
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: GetChannelInstallationByAppID :one
SELECT * FROM channel_installation
WHERE channel_type = sqlc.arg('channel_type')
  AND config ->> 'app_id' = sqlc.arg('app_id')::text;

-- name: GetChannelInstallationOwnerByAppID :one
SELECT ci.workspace_id, ci.agent_id, a.archived_at AS agent_archived_at
FROM channel_installation ci
JOIN agent a ON a.id = ci.agent_id
WHERE ci.channel_type = sqlc.arg('channel_type')
  AND ci.config ->> 'app_id' = sqlc.arg('app_id')::text;

-- name: ReclaimDeadChannelInstallationByAppID :one
WITH dead AS (
    DELETE FROM channel_installation ci
    WHERE ci.channel_type = sqlc.arg('channel_type')
      AND ci.config ->> 'app_id' = sqlc.arg('app_id')::text
      AND (
            (ci.status = 'revoked'
                AND NOT (ci.workspace_id = sqlc.arg('workspace_id')
                         AND ci.agent_id = sqlc.arg('agent_id')))
         OR NOT EXISTS (SELECT 1 FROM workspace w WHERE w.id = ci.workspace_id)
         OR NOT EXISTS (SELECT 1 FROM agent a WHERE a.id = ci.agent_id)
      )
    RETURNING ci.id
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM dead)
    RETURNING chat_session_id
),
cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token
    WHERE installation_id IN (SELECT id FROM dead)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM dead)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM dead)
),
detached_audit AS (
    UPDATE channel_inbound_audit SET installation_id = NULL
    WHERE installation_id IN (SELECT id FROM dead)
)
SELECT id FROM dead;

-- name: DeleteChannelInstallationsByArchivedRuntimeAgents :exec
WITH doomed AS (
    SELECT id FROM channel_installation
    WHERE agent_id IN (
        SELECT id FROM agent WHERE runtime_id = sqlc.arg('runtime_id') AND archived_at IS NOT NULL
    )
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM doomed)
    RETURNING chat_session_id
),
cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE installation_id IN (SELECT id FROM doomed)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding WHERE installation_id IN (SELECT id FROM doomed)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM doomed)
),
cleared_audit AS (
    DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM doomed)
)
DELETE FROM channel_installation WHERE id IN (SELECT id FROM doomed);

-- name: ListChannelInstallationsByWorkspace :many
SELECT * FROM channel_installation
WHERE workspace_id = sqlc.arg('workspace_id')
  AND channel_type = sqlc.arg('channel_type')
ORDER BY created_at ASC;

-- name: ListActiveChannelInstallations :many
SELECT ci.* FROM channel_installation ci
JOIN workspace w ON w.id = ci.workspace_id
JOIN agent a ON a.id = ci.agent_id
WHERE ci.status = 'active'
  AND ci.channel_type = sqlc.arg('channel_type')
ORDER BY ci.created_at ASC;

-- name: ListAllActiveChannelInstallations :many
SELECT ci.* FROM channel_installation ci
JOIN workspace w ON w.id = ci.workspace_id
JOIN agent a ON a.id = ci.agent_id
WHERE ci.status = 'active'
ORDER BY ci.created_at ASC;

-- name: SetChannelInstallationStatus :exec
UPDATE channel_installation
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: SetChannelInstallationConfig :exec
UPDATE channel_installation
SET config = $2, updated_at = now()
WHERE id = $1;


-- name: AcquireChannelWSLease :one
UPDATE channel_installation
SET ws_lease_token       = sqlc.arg('new_token'),
    ws_lease_expires_at  = sqlc.arg('new_expires_at'),
    updated_at           = now()
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseChannelWSLease :exec
UPDATE channel_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- name: CreateChannelUserBinding :one
INSERT INTO channel_user_binding (
    workspace_id, goosar_user_id, installation_id,
    channel_type, channel_user_id, config
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (installation_id, channel_user_id) DO UPDATE SET
    config   = channel_user_binding.config || jsonb_strip_nulls(EXCLUDED.config),
    bound_at = now()
WHERE channel_user_binding.goosar_user_id = EXCLUDED.goosar_user_id
RETURNING *;

-- name: GetChannelUserBindingByUserID :one
SELECT * FROM channel_user_binding
WHERE installation_id = $1 AND channel_user_id = $2;

-- name: FindReusableChannelUserBinding :one
SELECT b.* FROM channel_user_binding b
JOIN channel_installation ci ON ci.id = b.installation_id
WHERE b.workspace_id = sqlc.arg('workspace_id')
  AND b.channel_type = sqlc.arg('channel_type')
  AND b.channel_user_id = sqlc.arg('channel_user_id')
  AND ci.config ->> 'team_id' = sqlc.arg('team_id')::text
ORDER BY b.bound_at DESC
LIMIT 1;

-- name: DeleteChannelUserBindingsByWorkspaceMember :exec
DELETE FROM channel_user_binding
WHERE workspace_id = $1 AND goosar_user_id = $2;

-- name: DeleteChannelUserBindingsByInstallation :exec
DELETE FROM channel_user_binding
WHERE installation_id = $1;

-- name: CreateChannelChatSessionBinding :one
INSERT INTO channel_chat_session_binding (
    chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetChannelChatSessionBinding :one
SELECT * FROM channel_chat_session_binding
WHERE installation_id = $1 AND channel_chat_id = $2;

-- name: GetChannelChatSessionBindingBySession :one
SELECT * FROM channel_chat_session_binding
WHERE chat_session_id = sqlc.arg('chat_session_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: UpdateChannelChatSessionBindingReplyTarget :exec
UPDATE channel_chat_session_binding
SET last_message_id = sqlc.narg('last_message_id'),
    last_thread_id  = sqlc.narg('last_thread_id')
WHERE chat_session_id = $1;

-- name: DeleteChannelChatSessionBindingBySession :exec
DELETE FROM channel_chat_session_binding
WHERE chat_session_id = $1;

-- name: DeleteChannelChatSessionBindingsByInstallation :exec
DELETE FROM channel_chat_session_binding
WHERE installation_id = $1 AND channel_type = $2;

-- name: ClaimChannelInboundDedup :one
INSERT INTO channel_inbound_message_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (installation_id, message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE channel_inbound_message_dedup.processed_at IS NULL
      AND channel_inbound_message_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING installation_id, message_id, received_at, processed_at, claim_token;

-- name: MarkChannelInboundDedupProcessed :execrows
UPDATE channel_inbound_message_dedup
SET processed_at = now()
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: ReleaseChannelInboundDedup :execrows
DELETE FROM channel_inbound_message_dedup
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: PurgeChannelInboundDedup :exec
DELETE FROM channel_inbound_message_dedup
WHERE received_at < $1;

-- name: RecordChannelInboundDrop :exec
INSERT INTO channel_inbound_audit (
    installation_id, channel_type, channel_chat_id, event_type,
    channel_event_id, channel_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'),
    $1,
    sqlc.narg('channel_chat_id'),
    $2,
    sqlc.narg('channel_event_id'),
    sqlc.narg('channel_message_id'),
    $3
);

-- name: ListChannelInboundAuditByInstallation :many
SELECT * FROM channel_inbound_audit
WHERE installation_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- name: NullChannelInboundAuditInstallationID :exec
UPDATE channel_inbound_audit
SET installation_id = NULL
WHERE installation_id = $1;

-- name: CreateChannelOutboundCardMessage :one
INSERT INTO channel_outbound_card_message (
    chat_session_id, task_id, channel_type, channel_chat_id,
    channel_card_message_id, status
) VALUES (
    $1, sqlc.narg('task_id'), $2, $3, $4, $5
)
RETURNING *;

-- name: GetChannelOutboundCardByTask :one
SELECT * FROM channel_outbound_card_message
WHERE task_id = sqlc.arg('task_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: UpdateChannelOutboundCardStatus :exec
UPDATE channel_outbound_card_message
SET status = $2,
    last_patched_at = now()
WHERE id = $1;

-- name: DeleteChannelOutboundCardMessagesBySession :exec
DELETE FROM channel_outbound_card_message
WHERE chat_session_id = $1;

-- name: CreateChannelBindingToken :one
INSERT INTO channel_binding_token (
    token_hash, workspace_id, installation_id, channel_type,
    channel_user_id, expires_at
) VALUES (
    $1, $2, $3, $4, $5,
    LEAST(sqlc.arg('expires_at')::timestamptz, now() + INTERVAL '15 minutes')
)
RETURNING *;

-- name: ConsumeChannelBindingToken :one
UPDATE channel_binding_token
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: PurgeExpiredChannelBindingTokens :exec
DELETE FROM channel_binding_token
WHERE expires_at < $1;

-- name: DeleteChannelBindingTokensByInstallation :exec
DELETE FROM channel_binding_token
WHERE installation_id = $1;

-- name: RecordChannelMediaPendingObject :one
INSERT INTO channel_media_pending_object (
    storage_key, workspace_id, chat_message_id, storage_url, installation_id
)
VALUES ($1, $2, $3, $4, sqlc.narg(installation_id))
ON CONFLICT (storage_key) DO UPDATE
SET created_at = now(), next_attempt_at = now(),
    chat_message_id = EXCLUDED.chat_message_id,
    storage_url = EXCLUDED.storage_url
WHERE channel_media_pending_object.state = 'pending'
  AND channel_media_pending_object.workspace_id = EXCLUDED.workspace_id
RETURNING storage_key;

-- name: ClaimChannelMediaPendingObjectsForBind :many
DELETE FROM channel_media_pending_object
WHERE storage_key = ANY(@storage_keys::text[])
  AND workspace_id = @workspace_id
  AND state = 'pending'
RETURNING storage_key;

-- name: ClaimNextChannelMediaPendingObjectForReconcile :one
UPDATE channel_media_pending_object AS obj
SET state = CASE WHEN obj.state = 'tombstoned' THEN 'tombstoned' ELSE 'deleting' END,
    lease_token = @lease_token,
    lease_expires_at = now() + @lease::interval,
    attempt = obj.attempt + 1
FROM (
    SELECT cand.storage_key FROM channel_media_pending_object AS cand
    WHERE cand.next_attempt_at <= now()
      AND (
          (cand.state = 'pending' AND cand.created_at <= now() - @settle_delay::interval)
          OR (cand.state = 'deleting' AND (cand.lease_expires_at IS NULL OR cand.lease_expires_at <= now()))
          OR (cand.state = 'tombstoned' AND (cand.lease_expires_at IS NULL OR cand.lease_expires_at <= now()))
      )
    ORDER BY cand.next_attempt_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
) AS due
WHERE obj.storage_key = due.storage_key
RETURNING obj.*;

-- name: ReleaseChannelMediaPendingObject :exec
UPDATE channel_media_pending_object
SET lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now() + @backoff::interval,
    last_error = @last_error
WHERE storage_key = @storage_key
  AND workspace_id = @workspace_id
  AND lease_token = @lease_token;

-- name: TombstoneChannelMediaPendingObject :execrows
UPDATE channel_media_pending_object
SET state = 'tombstoned',
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now() + @redelete_delay::interval,
    tombstone_pass = @tombstone_pass,
    last_error = NULL
WHERE storage_key = @storage_key
  AND workspace_id = @workspace_id
  AND lease_token = @lease_token;

-- name: DeleteChannelMediaPendingObject :execrows
DELETE FROM channel_media_pending_object
WHERE storage_key = @storage_key
  AND workspace_id = @workspace_id
  AND lease_token = @lease_token;

-- name: ChannelMediaObjectIsReferenced :one
SELECT EXISTS (
    SELECT 1 FROM attachment
    WHERE chat_message_id = @chat_message_id
      AND workspace_id = @workspace_id
      AND url = @storage_url
) AS referenced;

-- name: CountChannelMediaPendingObjects :one
SELECT
    count(*) FILTER (WHERE state <> 'tombstoned') AS pending_objects,
    count(*) FILTER (WHERE state = 'tombstoned') AS tombstoned_objects
FROM channel_media_pending_object;
