-- 008_dispatch: the agent task queue. schemas.AgentTask -> dispatch_jobs,
-- schemas.TaskMessage -> dispatch_messages, schemas.TaskUsageEntry -> dispatch_usage.
--
-- Claiming uses SELECT ... FOR UPDATE SKIP LOCKED against dispatch_jobs_claim_ix
-- (see docs/51-data-model.md "Очередь задач агентов" for the exact query shape).
-- Read-only, point-in-time context that the daemon needs when it claims a job
-- (project title/description, requester name, autopilot title, workspace context
-- text, parent ticket identifier, ...) is assembled once into dj_context_snapshot
-- rather than re-derived from possibly-since-changed rows, so retries/resumes see
-- the context as it was when the job was queued.

CREATE TABLE dispatch_jobs (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    operative_id          uuid NOT NULL REFERENCES operatives(id),
    executor_id           uuid NOT NULL REFERENCES executors(id),
    ticket_id             uuid REFERENCES tickets(id) ON DELETE CASCADE,
    initiative_id         uuid REFERENCES initiatives(id) ON DELETE SET NULL,
    crew_id               uuid REFERENCES crews(id) ON DELETE SET NULL,
    convo_id              uuid REFERENCES convos(id) ON DELETE CASCADE,
    sentinel_run_id       uuid REFERENCES sentinel_runs(id) ON DELETE SET NULL,
    dj_kind               text NOT NULL CHECK (dj_kind IN ('issue','chat','autopilot','quick_create')),
    dj_status             text NOT NULL DEFAULT 'queued'
                          CHECK (dj_status IN ('queued','dispatched','waiting_local_directory','running','completed','failed','cancelled','deferred')),
    dj_priority           integer NOT NULL DEFAULT 0,
    dj_thread_title       text,
    dj_dispatched_at      timestamptz,
    dj_started_at         timestamptz,
    dj_completed_at       timestamptz,
    dj_result             jsonb,
    dj_error              text,
    dj_failure_reason     text,
    dj_attempt            integer NOT NULL DEFAULT 1,
    dj_max_attempts       integer NOT NULL DEFAULT 2,
    dj_parent_job_id      uuid REFERENCES dispatch_jobs(id),
    dj_is_leader          boolean NOT NULL DEFAULT false,
    dj_prior_session_id   text,
    dj_prior_work_dir     text,
    dj_resume_unavailable boolean NOT NULL DEFAULT false,
    dj_work_dir           text,
    dj_trigger_note_id    uuid REFERENCES ticket_notes(id) ON DELETE SET NULL,
    dj_coalesced_note_ids uuid[] NOT NULL DEFAULT '{}',
    dj_delivered_note_ids uuid[] NOT NULL DEFAULT '{}',
    dj_trigger_thread_id  uuid,
    dj_trigger_summary    text,
    dj_trigger_author_type text CHECK (dj_trigger_author_type IN ('member','agent','system')),
    dj_chat_channel_type  text,
    dj_chat_in_thread     boolean NOT NULL DEFAULT false,
    dj_chat_message       text,
    dj_chat_intro         boolean NOT NULL DEFAULT false,
    dj_quick_create_prompt text,
    dj_quick_create_priority text,
    dj_quick_create_due_date date,
    dj_handoff_note       text,
    dj_mcp_policy         jsonb,
    dj_initiator_type     text CHECK (dj_initiator_type IN ('member','agent','system')),
    dj_initiator_id       uuid,
    dj_attribution        jsonb NOT NULL DEFAULT '{}'::jsonb,
    dj_connected_apps     jsonb NOT NULL DEFAULT '[]'::jsonb,
    dj_repo_refs          jsonb NOT NULL DEFAULT '[]'::jsonb,
    dj_context_snapshot   jsonb NOT NULL DEFAULT '{}'::jsonb,
    dj_claim_secret_digest text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- claim query: WHERE executor_id = $1 AND dj_status = 'queued' ORDER BY dj_priority
-- DESC, created_at ASC FOR UPDATE SKIP LOCKED LIMIT 1.
CREATE INDEX dispatch_jobs_claim_ix ON dispatch_jobs (executor_id, dj_priority DESC, created_at)
    WHERE dj_status = 'queued';
CREATE INDEX dispatch_jobs_ticket_ix ON dispatch_jobs (ticket_id) WHERE ticket_id IS NOT NULL;
CREATE INDEX dispatch_jobs_convo_ix ON dispatch_jobs (convo_id) WHERE convo_id IS NOT NULL;
CREATE INDEX dispatch_jobs_workspace_status_ix ON dispatch_jobs (workspace_id, dj_status);
CREATE INDEX dispatch_jobs_operative_ix ON dispatch_jobs (operative_id, dj_status);

-- deferred FKs from tables created before dispatch_jobs existed
ALTER TABLE ticket_notes
    ADD CONSTRAINT ticket_notes_dispatch_job_fk
    FOREIGN KEY (tn_source_dispatch_job_id) REFERENCES dispatch_jobs(id) ON DELETE SET NULL;
ALTER TABLE sentinel_runs
    ADD CONSTRAINT sentinel_runs_dispatch_job_fk
    FOREIGN KEY (srun_dispatch_job_id) REFERENCES dispatch_jobs(id) ON DELETE SET NULL;
ALTER TABLE convo_messages
    ADD CONSTRAINT convo_messages_dispatch_job_fk
    FOREIGN KEY (dispatch_job_id) REFERENCES dispatch_jobs(id) ON DELETE SET NULL;
ALTER TABLE convo_drafts
    ADD CONSTRAINT convo_drafts_dispatch_job_fk
    FOREIGN KEY (dispatch_job_id) REFERENCES dispatch_jobs(id) ON DELETE SET NULL;

-- dispatch_messages: schemas.TaskMessage / TaskMessageInput (the run transcript)
CREATE TABLE dispatch_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispatch_job_id uuid NOT NULL REFERENCES dispatch_jobs(id) ON DELETE CASCADE,
    dm_seq          integer NOT NULL,
    dm_kind         text NOT NULL CHECK (dm_kind IN ('text','thinking','tool_use','tool_result','error')),
    dm_tool         text,
    dm_body         text,
    dm_input        jsonb,
    dm_output       text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dispatch_messages_uk UNIQUE (dispatch_job_id, dm_seq)
);
CREATE INDEX dispatch_messages_job_ix ON dispatch_messages (dispatch_job_id, dm_seq);

-- dispatch_usage: schemas.TaskUsageEntry (one row per model used within a job run)
CREATE TABLE dispatch_usage (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dispatch_job_id       uuid NOT NULL REFERENCES dispatch_jobs(id) ON DELETE CASCADE,
    du_provider           text,
    du_model              text NOT NULL,
    du_input_tokens       bigint NOT NULL DEFAULT 0,
    du_output_tokens      bigint NOT NULL DEFAULT 0,
    du_cache_read_tokens  bigint NOT NULL DEFAULT 0,
    du_cache_write_tokens bigint NOT NULL DEFAULT 0,
    du_cost_usd_ticks     bigint NOT NULL DEFAULT 0,
    created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dispatch_usage_job_ix ON dispatch_usage (dispatch_job_id);
