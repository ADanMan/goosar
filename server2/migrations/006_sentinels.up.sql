-- 006_sentinels: schemas.Autopilot and everything around it, renamed to sentinels.

CREATE TABLE sentinels (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id            uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    sen_title               text NOT NULL,
    sen_summary             text,
    initiative_id           uuid REFERENCES initiatives(id) ON DELETE SET NULL,
    sen_assignee_type       text NOT NULL CHECK (sen_assignee_type IN ('agent','squad')),
    sen_assignee_id         uuid NOT NULL,
    sen_status              text NOT NULL DEFAULT 'active' CHECK (sen_status IN ('active','paused','archived')),
    sen_execution_mode      text NOT NULL CHECK (sen_execution_mode IN ('create_issue','run_only')),
    sen_issue_title_template text,
    sen_created_by_type     text NOT NULL CHECK (sen_created_by_type IN ('member','agent')),
    sen_created_by_id       uuid NOT NULL,
    sen_last_run_at         timestamptz,
    sen_is_template         boolean NOT NULL DEFAULT false,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sentinels_workspace_ix ON sentinels (workspace_id);

CREATE TABLE sentinel_triggers (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sentinel_id                 uuid NOT NULL REFERENCES sentinels(id) ON DELETE CASCADE,
    strig_kind                  text NOT NULL CHECK (strig_kind IN ('schedule','webhook','api')),
    strig_enabled                boolean NOT NULL DEFAULT true,
    strig_cron_expression        text,
    strig_timezone               text,
    strig_next_run_at            timestamptz,
    strig_webhook_token_digest   text,
    strig_webhook_path           text,
    strig_provider                text CHECK (strig_provider IN ('generic','github')),
    strig_signing_secret_sealed   bytea,
    strig_label                   text,
    strig_last_fired_at           timestamptz,
    strig_event_filters           jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at                    timestamptz NOT NULL DEFAULT now(),
    updated_at                    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sentinel_triggers_sentinel_ix ON sentinel_triggers (sentinel_id);
CREATE UNIQUE INDEX sentinel_triggers_webhook_path_uk ON sentinel_triggers (strig_webhook_path) WHERE strig_webhook_path IS NOT NULL;

CREATE TABLE sentinel_runs (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sentinel_id           uuid NOT NULL REFERENCES sentinels(id) ON DELETE CASCADE,
    sentinel_trigger_id   uuid REFERENCES sentinel_triggers(id) ON DELETE SET NULL,
    srun_source           text NOT NULL CHECK (srun_source IN ('schedule','manual','webhook','api')),
    srun_status           text NOT NULL DEFAULT 'running'
                          CHECK (srun_status IN ('issue_created','running','completed','failed','skipped')),
    ticket_id             uuid REFERENCES tickets(id) ON DELETE SET NULL,
    srun_dispatch_job_id  uuid, -- soft reference to dispatch_jobs(id), created in 008_dispatch
    srun_completed_at     timestamptz,
    srun_failure_reason   text,
    srun_reason_code      text,
    srun_trigger_payload  jsonb,
    srun_result           jsonb,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sentinel_runs_sentinel_ix ON sentinel_runs (sentinel_id, created_at DESC);

CREATE TABLE sentinel_subscribers (
    sentinel_id uuid NOT NULL REFERENCES sentinels(id) ON DELETE CASCADE,
    account_id  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sentinel_id, account_id)
);

CREATE TABLE sentinel_collaborators (
    sentinel_id       uuid NOT NULL REFERENCES sentinels(id) ON DELETE CASCADE,
    account_id        uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sencol_granted_by uuid NOT NULL REFERENCES accounts(id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sentinel_id, account_id)
);

-- webhook_events: schemas.WebhookDelivery
CREATE TABLE webhook_events (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    sentinel_id           uuid NOT NULL REFERENCES sentinels(id) ON DELETE CASCADE,
    sentinel_trigger_id   uuid NOT NULL REFERENCES sentinel_triggers(id) ON DELETE CASCADE,
    whe_provider          text NOT NULL,
    whe_event             text NOT NULL,
    whe_dedupe_key        text,
    whe_dedupe_source     text,
    whe_signature_status  text NOT NULL DEFAULT 'not_required'
                          CHECK (whe_signature_status IN ('not_required','valid','invalid','missing')),
    whe_status            text NOT NULL DEFAULT 'queued'
                          CHECK (whe_status IN ('queued','dispatched','rejected','ignored','failed')),
    whe_attempt_count     integer NOT NULL DEFAULT 0,
    whe_dispatch_attempts integer NOT NULL DEFAULT 0,
    whe_available_at      timestamptz NOT NULL DEFAULT now(),
    whe_content_type      text,
    whe_response_status   integer,
    sentinel_run_id       uuid REFERENCES sentinel_runs(id) ON DELETE SET NULL,
    whe_replayed_from_id  uuid REFERENCES webhook_events(id),
    whe_error             text,
    whe_selected_headers  jsonb,
    whe_raw_body          text,
    whe_response_body     text,
    whe_received_at       timestamptz NOT NULL DEFAULT now(),
    whe_last_attempt_at   timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_events_trigger_ix ON webhook_events (sentinel_trigger_id, created_at DESC);
CREATE INDEX webhook_events_dedupe_ix ON webhook_events (sentinel_trigger_id, whe_dedupe_key) WHERE whe_dedupe_key IS NOT NULL;
