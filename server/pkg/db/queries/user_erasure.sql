
-- name: AnonymizeUser :one
UPDATE "user"
SET email = 'deleted-' || id::text || '@deleted.invalid',
    name = sqlc.arg(tombstone_name),
    avatar_url = NULL,
    profile_description = '',
    onboarding_questionnaire = '{}'::jsonb,
    cloud_waitlist_email = NULL,
    cloud_waitlist_reason = NULL,
    timezone = NULL,
    deactivated_at = COALESCE(deactivated_at, now()),
    token_version = token_version + 1,
    updated_at = now()
WHERE id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteUserChannelBindings :exec
DELETE FROM channel_user_binding WHERE goosar_user_id = sqlc.arg(user_id);

-- name: DeleteUserComposioConnections :exec
DELETE FROM user_composio_connection WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserMcpCredentials :exec
DELETE FROM workspace_mcp_user_credential WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserNotificationPreferences :exec
DELETE FROM notification_preference WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserInboxItems :exec
DELETE FROM inbox_item WHERE recipient_type = 'member' AND recipient_id = sqlc.arg(user_id);

-- name: DeleteUserMemberships :execrows
DELETE FROM member WHERE user_id = sqlc.arg(user_id);

-- name: DeleteUserVerificationCodes :exec
DELETE FROM verification_code WHERE email = sqlc.arg(email);

-- name: DeleteUserInvitations :exec
DELETE FROM workspace_invitation
WHERE invitee_user_id = sqlc.arg(user_id) OR invitee_email = sqlc.arg(email);

-- name: DeleteUserFeedback :exec
DELETE FROM feedback WHERE user_id = sqlc.arg(user_id);

-- name: ListMemberWorkspaceIDs :many
SELECT workspace_id FROM member WHERE user_id = sqlc.arg(user_id);
