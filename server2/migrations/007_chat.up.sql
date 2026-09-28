-- 007_chat: schemas.ChatSession / ChatMessage and related, renamed to convos.

CREATE TABLE convos (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    operative_id          uuid NOT NULL REFERENCES operatives(id),
    cv_creator_account_id uuid NOT NULL REFERENCES accounts(id),
    initiative_id         uuid REFERENCES initiatives(id) ON DELETE SET NULL,
    cv_title              text NOT NULL DEFAULT '',
    cv_status             text NOT NULL DEFAULT 'active' CHECK (cv_status IN ('active','archived')),
    cv_pinned             boolean NOT NULL DEFAULT false,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX convos_workspace_ix ON convos (workspace_id, updated_at DESC);
CREATE INDEX convos_operative_ix ON convos (operative_id);

-- convo_messages: schemas.ChatMessage. dispatch_job_id is a soft reference to
-- dispatch_jobs(id) (schemas.ChatMessage.task_id), added as a real FK once
-- dispatch_jobs exists (008_dispatch).
CREATE TABLE convo_messages (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    convo_id         uuid NOT NULL REFERENCES convos(id) ON DELETE CASCADE,
    cvm_role         text NOT NULL CHECK (cvm_role IN ('user','assistant','system')),
    cvm_body         text NOT NULL,
    dispatch_job_id  uuid,
    cvm_failure_reason text,
    cvm_elapsed_ms   integer,
    cvm_kind         text NOT NULL DEFAULT 'message' CHECK (cvm_kind IN ('message','no_response')),
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX convo_messages_convo_ix ON convo_messages (convo_id, created_at);

-- convo_drafts: schemas.ChatDraftRestore
CREATE TABLE convo_drafts (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    convo_id        uuid NOT NULL REFERENCES convos(id) ON DELETE CASCADE,
    dispatch_job_id uuid,
    cvd_body        text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX convo_drafts_convo_ix ON convo_drafts (convo_id);

-- convo_pinned_operatives: schemas.ChatPinnedAgent (per-account sidebar pins)
CREATE TABLE convo_pinned_operatives (
    account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    operative_id uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    cvp_position double precision NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, operative_id)
);

-- convo_channel_links: binds a convo to an external channel thread (e.g. Slack),
-- backing schemas.ChatChannelHistoryResponse.
CREATE TABLE convo_channel_links (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    convo_id                 uuid NOT NULL REFERENCES convos(id) ON DELETE CASCADE,
    cvc_channel_type         text NOT NULL,
    cvc_external_channel_id  text,
    cvc_external_thread_id   text,
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT convo_channel_links_uk UNIQUE (convo_id, cvc_channel_type)
);
