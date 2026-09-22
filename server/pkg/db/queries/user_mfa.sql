
-- name: GetUserMFA :one
SELECT * FROM user_mfa WHERE user_id = $1;

-- name: UpsertUserMFASecret :one
INSERT INTO user_mfa (user_id, totp_secret_sealed, enabled_at, last_used_step)
VALUES ($1, $2, NULL, 0)
ON CONFLICT (user_id) DO UPDATE SET
    totp_secret_sealed = EXCLUDED.totp_secret_sealed,
    enabled_at = NULL,
    last_used_step = 0,
    updated_at = now()
RETURNING *;

-- name: ConfirmUserMFA :execrows
UPDATE user_mfa
SET enabled_at = COALESCE(enabled_at, now()),
    last_used_step = $2,
    updated_at = now()
WHERE user_id = $1 AND last_used_step < $2;

-- name: ClaimUserMFAStep :execrows
UPDATE user_mfa
SET last_used_step = $2, updated_at = now()
WHERE user_id = $1 AND enabled_at IS NOT NULL AND last_used_step < $2;

-- name: DeleteUserMFA :exec
DELETE FROM user_mfa WHERE user_id = $1;

-- name: ListUserMFASecretsForRotation :many
SELECT user_id, totp_secret_sealed FROM user_mfa ORDER BY created_at;

-- name: ResealUserMFASecret :execrows
UPDATE user_mfa
SET totp_secret_sealed = sqlc.arg(sealed), updated_at = now()
WHERE user_id = sqlc.arg(user_id) AND totp_secret_sealed = sqlc.arg(previous);

-- name: DeleteUserMFARecoveryCodes :exec
DELETE FROM user_mfa_recovery_code WHERE user_id = $1;

-- name: InsertUserMFARecoveryCode :exec
INSERT INTO user_mfa_recovery_code (user_id, code_hash) VALUES ($1, $2);

-- name: ConsumeUserMFARecoveryCode :execrows
UPDATE user_mfa_recovery_code
SET used_at = now()
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountUserMFARecoveryCodesUnused :one
SELECT COUNT(*) FROM user_mfa_recovery_code
WHERE user_id = $1 AND used_at IS NULL;
