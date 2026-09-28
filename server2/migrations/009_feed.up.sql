-- 009_feed: shared attachments, the notification inbox and per-member preferences.

-- assets: schemas.Attachment. Each nullable parent column is a real FK to exactly
-- one owning table (issue/comment/chat/chat-message/agent-task), same shape the
-- contract itself uses -- not a polymorphic single item_type/item_id pair.
CREATE TABLE assets (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id           uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    ticket_id              uuid REFERENCES tickets(id) ON DELETE CASCADE,
    ticket_note_id         uuid REFERENCES ticket_notes(id) ON DELETE CASCADE,
    convo_id               uuid REFERENCES convos(id) ON DELETE CASCADE,
    convo_message_id       uuid REFERENCES convo_messages(id) ON DELETE CASCADE,
    dispatch_job_id        uuid REFERENCES dispatch_jobs(id) ON DELETE CASCADE,
    as_uploader_type       text NOT NULL CHECK (as_uploader_type IN ('member','agent')),
    as_uploader_id         uuid NOT NULL,
    as_filename            text NOT NULL,
    as_storage_uri         text NOT NULL,
    as_download_path       text NOT NULL,
    as_markdown_ref        text NOT NULL,
    as_download_ticket_uri text,
    as_content_type        text NOT NULL,
    as_size_bytes          bigint NOT NULL,
    created_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assets_workspace_ix ON assets (workspace_id);
CREATE INDEX assets_ticket_ix ON assets (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE INDEX assets_note_ix ON assets (ticket_note_id) WHERE ticket_note_id IS NOT NULL;
CREATE INDEX assets_convo_message_ix ON assets (convo_message_id) WHERE convo_message_id IS NOT NULL;
CREATE INDEX assets_dispatch_job_ix ON assets (dispatch_job_id) WHERE dispatch_job_id IS NOT NULL;

-- alerts: schemas.InboxItem
CREATE TABLE alerts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    al_recipient_type text NOT NULL CHECK (al_recipient_type IN ('member','agent')),
    al_recipient_id   uuid NOT NULL,
    al_kind           text NOT NULL,
    al_severity       text NOT NULL DEFAULT 'info' CHECK (al_severity IN ('info','action_required','attention')),
    ticket_id         uuid REFERENCES tickets(id) ON DELETE CASCADE,
    al_headline       text NOT NULL,
    al_body           text,
    al_read_at        timestamptz,
    al_archived_at    timestamptz,
    al_actor_type     text CHECK (al_actor_type IN ('member','agent','system')),
    al_actor_id       uuid,
    al_details        jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alerts_recipient_ix ON alerts (workspace_id, al_recipient_type, al_recipient_id, created_at DESC)
    WHERE al_archived_at IS NULL;

-- notification_prefs: schemas.NotificationPreferences (one row per member per space)
CREATE TABLE notification_prefs (
    workspace_id uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    np_groups    jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, account_id)
);
