-- 013_authn_token_epoch: T-026 gap-fill (see server2/docs/decisions.md).
-- authRevokeAllSessions must invalidate every session JWT issued before the
-- call "immediately" (contract: "bump their token version"); accounts has no
-- such counter in 001_identity, so it is added here as a narrow, additive
-- migration rather than a schema rewrite.

ALTER TABLE accounts ADD COLUMN acct_token_epoch integer NOT NULL DEFAULT 0;
