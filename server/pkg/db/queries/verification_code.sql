-- name: CreateVerificationCode :one
INSERT INTO verification_code (email, code, expires_at, link_token_hash)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetLatestVerificationCode :one
SELECT * FROM verification_code
WHERE email = $1
  AND used = FALSE
  AND expires_at > now()
  AND attempts < 5
ORDER BY created_at DESC
LIMIT 1;

-- name: GetVerificationCodeByLinkTokenHash :one
SELECT * FROM verification_code
WHERE link_token_hash = $1
  AND used = FALSE
  AND expires_at > now()
LIMIT 1;

-- name: ConsumeVerificationCode :execrows
UPDATE verification_code
SET used = TRUE
WHERE id = $1
  AND used = FALSE;

-- name: MarkVerificationCodeUsed :exec
UPDATE verification_code
SET used = TRUE
WHERE id = $1;

-- name: IncrementVerificationCodeAttempts :exec
UPDATE verification_code
SET attempts = attempts + 1
WHERE id = $1;

-- name: GetLatestCodeByEmail :one
SELECT * FROM verification_code
WHERE email = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteExpiredVerificationCodes :exec
DELETE FROM verification_code
WHERE expires_at < now() - interval '1 hour';

-- name: InvalidatePriorVerificationCodes :exec
UPDATE verification_code
SET used = TRUE
WHERE email = $1
  AND used = FALSE;
