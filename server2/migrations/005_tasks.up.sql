-- 005_tasks: projects (initiatives), the shared label/property taxonomy, and the
-- ticket tracker itself (schemas.Issue and everything hanging off it).

-- initiatives: schemas.Project
CREATE TABLE initiatives (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    init_title    text NOT NULL,
    init_summary  text,
    init_icon     text,
    init_status   text NOT NULL DEFAULT 'planned'
                  CHECK (init_status IN ('planned','in_progress','paused','completed','cancelled')),
    init_priority text NOT NULL DEFAULT 'none' CHECK (init_priority IN ('urgent','high','medium','low','none')),
    init_lead_type text CHECK (init_lead_type IN ('member','agent')),
    init_lead_id  uuid,
    init_start_date date,
    init_due_date   date,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX initiatives_workspace_ix ON initiatives (workspace_id);
CREATE INDEX initiatives_title_trgm_ix ON initiatives USING gin (init_title gin_trgm_ops);

-- initiative_resources: schemas.ProjectResource
CREATE TABLE initiative_resources (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    initiative_id  uuid NOT NULL REFERENCES initiatives(id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    ir_resource_type text NOT NULL CHECK (ir_resource_type IN ('github_repo','local_directory')),
    ir_resource_ref  jsonb NOT NULL DEFAULT '{}'::jsonb,
    ir_label       text,
    ir_position    integer NOT NULL DEFAULT 0,
    ir_created_by  uuid REFERENCES accounts(id),
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX initiative_resources_initiative_ix ON initiative_resources (initiative_id);

-- tags: schemas.Label (resource_type issue/agent/skill share one taxonomy)
CREATE TABLE tags (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    tag_resource_type text NOT NULL CHECK (tag_resource_type IN ('issue','agent','skill')),
    tag_label         text NOT NULL,
    tag_summary       text,
    tag_color         text NOT NULL,
    tag_usage_count   integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tags_uk UNIQUE (workspace_id, tag_resource_type, tag_label)
);

-- field_defs: schemas.Property (custom ticket fields)
CREATE TABLE field_defs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    fd_title       text NOT NULL,
    fd_type        text NOT NULL CHECK (fd_type IN ('text','number','select','multi_select','date','checkbox','url')),
    fd_summary     text,
    fd_icon        text,
    fd_config      jsonb NOT NULL DEFAULT '{}'::jsonb,
    fd_position    double precision NOT NULL DEFAULT 0,
    fd_archived_at timestamptz,
    fd_usage_count integer NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX field_defs_workspace_ix ON field_defs (workspace_id) WHERE fd_archived_at IS NULL;

-- tickets: schemas.Issue. tk_seq_number/tk_display_key implement the per-workspace
-- numbering scheme described in docs/51-data-model.md (spaces.ws_next_ticket_seq).
CREATE TABLE tickets (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    tk_seq_number         integer NOT NULL,
    tk_display_key        text NOT NULL,
    tk_headline           text NOT NULL,
    tk_narrative          text,
    tk_status             text NOT NULL DEFAULT 'backlog'
                          CHECK (tk_status IN ('backlog','todo','in_progress','in_review','done','blocked','cancelled')),
    tk_priority           text NOT NULL DEFAULT 'none' CHECK (tk_priority IN ('urgent','high','medium','low','none')),
    tk_assignee_type      text CHECK (tk_assignee_type IN ('member','agent','squad')),
    tk_assignee_id        uuid,
    tk_creator_type       text NOT NULL CHECK (tk_creator_type IN ('member','agent')),
    tk_creator_id         uuid NOT NULL,
    tk_parent_ticket_id   uuid REFERENCES tickets(id) ON DELETE SET NULL,
    initiative_id         uuid REFERENCES initiatives(id) ON DELETE SET NULL,
    tk_position           double precision,
    tk_stage              integer CHECK (tk_stage IS NULL OR tk_stage >= 1),
    tk_start_date         date,
    tk_due_date           date,
    tk_metadata           jsonb NOT NULL DEFAULT '{}'::jsonb,
    tk_custom_field_values jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tickets_seq_uk UNIQUE (workspace_id, tk_seq_number),
    CONSTRAINT tickets_key_uk UNIQUE (workspace_id, tk_display_key)
);
CREATE INDEX tickets_workspace_status_ix ON tickets (workspace_id, tk_status);
CREATE INDEX tickets_assignee_ix ON tickets (tk_assignee_type, tk_assignee_id) WHERE tk_assignee_id IS NOT NULL;
CREATE INDEX tickets_initiative_ix ON tickets (initiative_id) WHERE initiative_id IS NOT NULL;
CREATE INDEX tickets_parent_ix ON tickets (tk_parent_ticket_id) WHERE tk_parent_ticket_id IS NOT NULL;
CREATE INDEX tickets_headline_trgm_ix ON tickets USING gin (tk_headline gin_trgm_ops);
CREATE INDEX tickets_narrative_trgm_ix ON tickets USING gin (tk_narrative gin_trgm_ops) WHERE tk_narrative IS NOT NULL;

-- ticket_tag_links / operative_tag_links / capability_tag_links: schemas.Label joins
CREATE TABLE ticket_tag_links (
    ticket_id  uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tag_id     uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, tag_id)
);
CREATE TABLE operative_tag_links (
    operative_id uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    tag_id       uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operative_id, tag_id)
);
CREATE TABLE capability_tag_links (
    capability_id uuid NOT NULL REFERENCES capabilities(id) ON DELETE CASCADE,
    tag_id        uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (capability_id, tag_id)
);

-- ticket_notes: schemas.IssueComment
CREATE TABLE ticket_notes (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id                uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tn_author_type           text NOT NULL CHECK (tn_author_type IN ('member','agent','system')),
    tn_author_id             uuid NOT NULL,
    tn_body                  text NOT NULL,
    tn_kind                  text NOT NULL DEFAULT 'comment'
                             CHECK (tn_kind IN ('comment','status_change','progress_update','system')),
    tn_parent_note_id        uuid REFERENCES ticket_notes(id) ON DELETE CASCADE,
    tn_resolved_at           timestamptz,
    tn_resolved_by_type      text CHECK (tn_resolved_by_type IN ('member','agent')),
    tn_resolved_by_id        uuid,
    tn_source_dispatch_job_id uuid, -- soft reference to dispatch_jobs(id), created in 008_dispatch
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_notes_ticket_ix ON ticket_notes (ticket_id, created_at);
CREATE INDEX ticket_notes_parent_ix ON ticket_notes (tn_parent_note_id) WHERE tn_parent_note_id IS NOT NULL;

-- note_marks / ticket_marks: schemas.CommentReaction / IssueReaction
CREATE TABLE note_marks (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_note_id uuid NOT NULL REFERENCES ticket_notes(id) ON DELETE CASCADE,
    nm_actor_type  text NOT NULL CHECK (nm_actor_type IN ('member','agent')),
    nm_actor_id    uuid NOT NULL,
    nm_emoji       text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT note_marks_uk UNIQUE (ticket_note_id, nm_actor_type, nm_actor_id, nm_emoji)
);
CREATE TABLE ticket_marks (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id     uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tm_actor_type text NOT NULL CHECK (tm_actor_type IN ('member','agent')),
    tm_actor_id   uuid NOT NULL,
    tm_emoji      text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ticket_marks_uk UNIQUE (ticket_id, tm_actor_type, tm_actor_id, tm_emoji)
);

-- ticket_subscribers: schemas.IssueSubscriber
CREATE TABLE ticket_subscribers (
    ticket_id       uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tsub_watcher_type text NOT NULL CHECK (tsub_watcher_type IN ('member','agent')),
    tsub_watcher_id   uuid NOT NULL,
    tsub_reason       text NOT NULL CHECK (tsub_reason IN ('creator','assignee','commenter','mentioned','manual','autopilot')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, tsub_watcher_type, tsub_watcher_id)
);

-- ticket_pr_links: schemas.IssuePullRequestLink
CREATE TABLE ticket_pr_links (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tpr_provider text NOT NULL,
    tpr_url      text NOT NULL,
    tpr_number   integer NOT NULL,
    tpr_title    text,
    tpr_state    text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ticket_pr_links_uk UNIQUE (ticket_id, tpr_url)
);

-- ticket_activity: schemas.IssueTimelineEntry system entries (status/assignee
-- changes etc.), separate from human ticket_notes; the timeline endpoint merges both.
CREATE TABLE ticket_activity (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    ticket_id    uuid REFERENCES tickets(id) ON DELETE CASCADE,
    ta_actor_type text NOT NULL CHECK (ta_actor_type IN ('member','agent','system')),
    ta_actor_id   uuid,
    ta_action     text NOT NULL,
    ta_details    jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_activity_ticket_ix ON ticket_activity (ticket_id, created_at);

-- ticket_bookmarks / initiative_bookmarks: schemas.Pin (item_type issue/project),
-- split per target type instead of one polymorphic table (same style the contract
-- itself uses for issue_reaction vs comment_reaction).
CREATE TABLE ticket_bookmarks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id   uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ticket_id    uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    bm_position  double precision NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ticket_bookmarks_uk UNIQUE (account_id, ticket_id)
);
CREATE TABLE initiative_bookmarks (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id    uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    initiative_id uuid NOT NULL REFERENCES initiatives(id) ON DELETE CASCADE,
    bm_position   double precision NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT initiative_bookmarks_uk UNIQUE (account_id, initiative_id)
);
