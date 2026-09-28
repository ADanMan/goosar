-- 340_authn_mfa_pending: short-lived token issued by verify-code/verify-link/
-- ldap-login/oidc-callback when mfa_factors.mfa_enabled_at is set (second
-- factor required before a real session is started). schemas.LoginResult.
-- mfa_token / schemas.MFAVerifyRequest.mfa_token (T-029).
CREATE TABLE mfa_pending_logins (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id       uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    mpl_token_digest text NOT NULL,
    mpl_valid_until  timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mfa_pending_logins_token_uk ON mfa_pending_logins (mpl_token_digest);
