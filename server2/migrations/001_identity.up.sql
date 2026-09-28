-- 001_identity: accounts, login methods, sessions, tokens, MFA, sales leads.
-- Naming: every table/column is renamed vs server/migrations/001_init.up.sql on purpose
-- (T-025); only id/created_at/updated_at/workspace_id are kept in common.
-- JSON field names from docs/50-api-contract.yaml are NOT renamed: the mapping from a
-- JSON field to its (renamed) column is documented in docs/51-data-model.md.

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

-- accounts: schemas.User
CREATE TABLE accounts (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    acct_email             text NOT NULL,
    acct_full_name         text NOT NULL,
    acct_avatar_uri        text,
    acct_locale            text,
    acct_tz                text,
    acct_onboarded_at      timestamptz,
    acct_onboarding_survey jsonb NOT NULL DEFAULT '{}'::jsonb,
    acct_starter_state     text,
    acct_bio               text NOT NULL DEFAULT '',
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT accounts_locale_chk CHECK (acct_locale IS NULL OR acct_locale IN ('en','zh-Hans','ko','ja','ru'))
);
CREATE UNIQUE INDEX accounts_email_uk ON accounts (lower(acct_email));

-- auth_bindings: one row per login method bound to an account (email/oidc/ldap).
-- schemas.AuthMethodsResponse enumerates the methods; this table backs it.
CREATE TABLE auth_bindings (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ab_method            text NOT NULL CHECK (ab_method IN ('email','oidc','ldap')),
    ab_external_subject  text,
    ab_external_email    text,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_bindings_account_method_uk UNIQUE (account_id, ab_method)
);
CREATE UNIQUE INDEX auth_bindings_subject_uk ON auth_bindings (ab_method, ab_external_subject)
    WHERE ab_external_subject IS NOT NULL;

-- mfa_factors: schemas.MFAStatusResponse / MFAEnrollResponse (one TOTP factor per account).
CREATE TABLE mfa_factors (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id          uuid NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    mfa_secret_sealed   bytea NOT NULL,
    mfa_pending_since   timestamptz NOT NULL DEFAULT now(),
    mfa_enabled_at      timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- mfa_recovery_codes: schemas.MFAConfirmResponse.recovery_codes, one-time use.
CREATE TABLE mfa_recovery_codes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    mrc_code_digest text NOT NULL,
    mrc_used_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT mfa_recovery_codes_uk UNIQUE (account_id, mrc_code_digest)
);

-- login_sessions: schemas.SessionResponse (browser/CLI sessions, cookie-backed).
CREATE TABLE login_sessions (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sess_secret_digest   text NOT NULL,
    sess_client_agent    text NOT NULL DEFAULT '',
    sess_origin_ip_digest text,
    sess_last_ping_at    timestamptz NOT NULL DEFAULT now(),
    sess_valid_until     timestamptz,
    sess_invalidated_at  timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX login_sessions_secret_uk ON login_sessions (sess_secret_digest);
CREATE INDEX login_sessions_account_ix ON login_sessions (account_id) WHERE sess_invalidated_at IS NULL;

-- access_keys: schemas.PersonalAccessToken.
CREATE TABLE access_keys (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id         uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ak_title           text NOT NULL,
    ak_secret_digest   text NOT NULL,
    ak_secret_prefix   text NOT NULL,
    ak_valid_until     timestamptz,
    ak_last_used_at    timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX access_keys_secret_uk ON access_keys (ak_secret_digest);
CREATE INDEX access_keys_account_ix ON access_keys (account_id);

-- login_codes: one-time email codes used to complete LoginResult (mfa_token exchange
-- is handled in-memory/short TTL here too, keyed by lc_purpose).
CREATE TABLE login_codes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lc_email        text NOT NULL,
    lc_code_digest  text NOT NULL,
    lc_purpose      text NOT NULL DEFAULT 'login',
    lc_consumed_at  timestamptz,
    lc_valid_until  timestamptz NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_codes_email_ix ON login_codes (lower(lc_email), lc_purpose);

-- contact_leads: schemas.ContactSalesRequest/ContactSalesResponse.
CREATE TABLE contact_leads (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lead_first_name        text NOT NULL,
    lead_last_name         text NOT NULL,
    lead_business_email    text NOT NULL,
    lead_company_name      text NOT NULL,
    lead_company_size      text NOT NULL,
    lead_country_region    text NOT NULL,
    lead_use_case          text NOT NULL,
    lead_goals             text,
    lead_source            text,
    lead_consent_outreach  boolean NOT NULL DEFAULT false,
    lead_consent_updates   boolean NOT NULL DEFAULT false,
    created_at             timestamptz NOT NULL DEFAULT now()
);
