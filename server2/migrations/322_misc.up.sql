-- 322_misc: T-029, POST /api/feedback and POST /api/client-usage. contact-sales
-- already has contact_leads (001_identity.up.sql); assignee-frequency,
-- workspace-templates and /api/status are computed on read (see
-- docs/51-data-model.md, "Что сознательно не хранится").

-- member_feedback: schemas.Feedback (POST /api/feedback response only echoes
-- id/created_at — no dedicated read schema in the contract).
CREATE TABLE member_feedback (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    workspace_id   uuid REFERENCES spaces(id) ON DELETE SET NULL,
    fb_message     text NOT NULL,
    fb_url         text,
    fb_kind        text NOT NULL DEFAULT 'general',
    fb_client_meta jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX member_feedback_account_ix ON member_feedback (account_id, created_at DESC);

-- install_usage_pings: one upserted row per (account, install, day) — POST
-- /api/client-usage contract: "Апсертит суточную запись использования клиента".
CREATE TABLE install_usage_pings (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id         uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    cud_install_id     uuid NOT NULL,
    cud_date           date NOT NULL DEFAULT current_date,
    cud_platform       text NOT NULL CHECK (cud_platform IN ('web','desktop')),
    cud_client_version text,
    cud_client_os      text,
    cud_runtime_probe  jsonb,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT install_usage_pings_uk UNIQUE (account_id, cud_install_id, cud_date)
);
