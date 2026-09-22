-- Начальная схема Goosar: полное состояние БД.
SET check_function_bodies = false;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;
CREATE FUNCTION public.attachment_tombstone_on_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- ON CONFLICT DO NOTHING: an id can only be deleted once, but a restored
    -- dump or a re-created row with the same id must not fail the delete.
    INSERT INTO attachment_tombstone (attachment_id, workspace_id, url, deleted_at)
    VALUES (OLD.id, OLD.workspace_id, OLD.url, now())
    ON CONFLICT (attachment_id) DO NOTHING;
RETURN OLD;
END;
$$;
CREATE FUNCTION public.clear_runtime_mcp_overlay_on_terminal_state() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.status IN ('completed', 'failed', 'cancelled')
       AND OLD.status IS DISTINCT FROM NEW.status
       AND (NEW.runtime_mcp_overlay IS NOT NULL OR NEW.runtime_connected_apps IS NOT NULL) THEN
        NEW.runtime_mcp_overlay := NULL;
NEW.runtime_connected_apps := NULL;
END IF;
RETURN NEW;
END;
$$;
CREATE FUNCTION public.enqueue_task_usage_hourly_dirty_for_atq() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id
           OR OLD.issue_id IS DISTINCT FROM NEW.issue_id THEN
            -- OLD side. NULL runtime_id rows are not aggregated (no
            -- runtime → no bucket); skip those.
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    OLD.runtime_id,
                    OLD.agent_id,
                    i_old.project_id,
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = OLD.agent_id
                  LEFT JOIN issue i_old ON i_old.id = OLD.issue_id
                 WHERE tu.task_id = OLD.id
                ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
END IF;
IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    NEW.runtime_id,
                    NEW.agent_id,
                    i_new.project_id,
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = NEW.agent_id
                  LEFT JOIN issue i_new ON i_new.id = NEW.issue_id
                 WHERE tu.task_id = NEW.id
                ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
END IF;
END IF;
RETURN NEW;
ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO task_usage_hourly_dirty (
                bucket_hour, workspace_id, runtime_id, agent_id,
                project_id, provider, model
            )
            SELECT DISTINCT
                task_usage_hour_bucket(tu.created_at),
                a.workspace_id,
                OLD.runtime_id,
                OLD.agent_id,
                i.project_id,
                tu.provider,
                tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = OLD.agent_id
              LEFT JOIN issue i ON i.id = OLD.issue_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
END IF;
RETURN OLD;
END IF;
RETURN NULL;
END;
$$;
CREATE FUNCTION public.enqueue_task_usage_hourly_dirty_for_issue_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT DISTINCT
        task_usage_hour_bucket(tu.created_at),
        OLD.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        OLD.project_id,
        tu.provider,
        tu.model
      FROM agent_task_queue atq
      JOIN task_usage tu ON tu.task_id = atq.id
     WHERE atq.issue_id = OLD.id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
RETURN OLD;
END;
$$;
CREATE FUNCTION public.enqueue_task_usage_hourly_dirty_for_issue_project() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            OLD.project_id,
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
-- NEW project buckets.
        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            NEW.project_id,
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
END IF;
RETURN NEW;
END;
$$;
CREATE FUNCTION public.enqueue_task_usage_hourly_dirty_for_tu() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT
        task_usage_hour_bucket(OLD.created_at),
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        OLD.provider,
        OLD.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
      LEFT JOIN issue i ON i.id = atq.issue_id
     WHERE atq.id = OLD.task_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
RETURN OLD;
END;
$$;
CREATE FUNCTION public.prune_task_usage_hourly_dirty(p_retention interval DEFAULT '7 days'::interval) RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    DELETE FROM task_usage_hourly_dirty
     WHERE enqueued_at < now() - p_retention;
GET DIAGNOSTICS v_rows = ROW_COUNT;
RETURN v_rows;
END;
$$;
CREATE FUNCTION public.rollup_task_usage_hourly() RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_lock_ok BOOLEAN;
v_from    TIMESTAMPTZ;
v_to      TIMESTAMPTZ;
v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4246) INTO v_lock_ok;
IF NOT v_lock_ok THEN
        RETURN 0;
END IF;
BEGIN
        UPDATE task_usage_hourly_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;
-- Cap each tick at a one-day window. In steady state v_from is
        -- recent, so LEAST picks `now() - 5 min` and nothing changes. But
        -- if the worker was paused (incident, migration freeze) the
        -- watermark can fall far behind; without the cap a single catch-up
        -- tick would recompute a multi-week window in one statement while
        -- holding lock 4246, blocking every other tick. Capped, catch-up
        -- advances in bounded one-day steps over successive ticks.
        v_to := LEAST(now() - INTERVAL '5 minutes', v_from + INTERVAL '1 day');
IF v_from < v_to THEN
            v_rows := rollup_task_usage_hourly_window(v_from, v_to);
UPDATE task_usage_hourly_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
ELSE
            UPDATE task_usage_hourly_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
END IF;
PERFORM pg_advisory_unlock(4246);
EXCEPTION WHEN OTHERS THEN
        UPDATE task_usage_hourly_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
PERFORM pg_advisory_unlock(4246);
RAISE;
END;
-- TTL prune. Runs AFTER the advisory lock is released: on a large
    -- stale backlog the prune can be slow, and holding lock 4246 through
    -- it would serialise every concurrent cron tick. It is a plain
    -- bounded DELETE — idempotent and safe to run unlocked.
    PERFORM prune_task_usage_hourly_dirty();
RETURN v_rows;
END;
$$;
CREATE FUNCTION public.rollup_task_usage_hourly_window(p_from timestamp with time zone, p_to timestamp with time zone) RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    IF p_from >= p_to THEN
        RETURN 0;
END IF;
WITH
    dirty_from_updates AS (
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at) AS bucket_hour,
            a.workspace_id                        AS workspace_id,
            atq.runtime_id                        AS runtime_id,
            atq.agent_id                          AS agent_id,
            i.project_id                          AS project_id,
            tu.provider                           AS provider,
            tu.model                              AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN issue       i   ON i.id        = atq.issue_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                -- Legacy updated_at-NULL rows; partial index from 078.
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_from_queue AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, provider, model
          FROM task_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.runtime_id,
            dk.agent_id,
            dk.project_id,
            dk.provider,
            dk.model,
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            -- Authoritative half: only rows the provider priced.
            COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
            -- Estimated half: tokens from rows the provider did not price.
            COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
            COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
            COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
            COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
            COUNT(DISTINCT tu.task_id)::bigint AS task_count,
            COUNT(*)::bigint                   AS event_count
          FROM dirty_keys dk
          JOIN agent_task_queue atq ON atq.runtime_id  = dk.runtime_id
                                    AND atq.agent_id    = dk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.provider    = dk.provider
                                    AND tu.model       = dk.model
                                    AND task_usage_hour_bucket(tu.created_at) = dk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
         GROUP BY 1, 2, 3, 4, 5, 6, 7
    ),
    upserted AS (
        INSERT INTO task_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
            SET input_tokens                = EXCLUDED.input_tokens,
                output_tokens               = EXCLUDED.output_tokens,
                cache_read_tokens           = EXCLUDED.cache_read_tokens,
                cache_write_tokens          = EXCLUDED.cache_write_tokens,
                cost_usd_ticks              = EXCLUDED.cost_usd_ticks,
                uncosted_input_tokens       = EXCLUDED.uncosted_input_tokens,
                uncosted_output_tokens      = EXCLUDED.uncosted_output_tokens,
                uncosted_cache_read_tokens  = EXCLUDED.uncosted_cache_read_tokens,
                uncosted_cache_write_tokens = EXCLUDED.uncosted_cache_write_tokens,
                task_count                  = EXCLUDED.task_count,
                event_count                 = EXCLUDED.event_count,
                updated_at                  = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM task_usage_hourly d
         USING dirty_keys dk
         WHERE d.bucket_hour  = dk.bucket_hour
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id IS NOT DISTINCT FROM dk.project_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = dk.bucket_hour
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id IS NOT DISTINCT FROM dk.project_id
                  AND r.provider     = dk.provider
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;
DELETE FROM task_usage_hourly_dirty WHERE enqueued_at < p_to;
RETURN v_rows;
END;
$$;
CREATE FUNCTION public.task_usage_hour_bucket(ts timestamp with time zone) RETURNS timestamp with time zone
    LANGUAGE sql IMMUTABLE
    AS $$
    SELECT (date_trunc('hour', ts AT TIME ZONE 'UTC')) AT TIME ZONE 'UTC';
$$;
CREATE FUNCTION public.task_usage_hourly_rollup_lag_seconds() RETURNS double precision
    LANGUAGE sql STABLE
    AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM task_usage_hourly_rollup_state
     WHERE id = 1;
$$;
CREATE TABLE public.activity_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid,
    actor_type text,
    actor_id uuid,
    action text NOT NULL,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT activity_log_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text, 'system'::text])))
);
CREATE TABLE public.admin_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    actor_user_id uuid,
    action text NOT NULL,
    target_type text NOT NULL,
    target_id text,
    before_hash text,
    after_hash text,
    request_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.agent (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    avatar_url text,
    runtime_mode text NOT NULL,
    runtime_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    visibility text DEFAULT 'private'::text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    max_concurrent_tasks integer DEFAULT 6 NOT NULL,
    owner_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    runtime_id uuid NOT NULL,
    instructions text DEFAULT ''::text NOT NULL,
    archived_at timestamp with time zone,
    archived_by uuid,
    custom_env jsonb DEFAULT '{}'::jsonb NOT NULL,
    custom_args jsonb DEFAULT '[]'::jsonb NOT NULL,
    mcp_config jsonb,
    model text,
    thinking_level text,
    composio_toolkit_allowlist text[],
    permission_mode text DEFAULT 'private'::text NOT NULL,
    kind text DEFAULT 'user'::text NOT NULL,
    system_key text,
    disabled_runtime_skills jsonb DEFAULT '[]'::jsonb NOT NULL,
    service_tier text,
    CONSTRAINT agent_description_length CHECK ((char_length(description) <= 255)),
    CONSTRAINT agent_kind_check CHECK ((kind = ANY (ARRAY['user'::text, 'system'::text]))),
    CONSTRAINT agent_permission_mode_check CHECK ((permission_mode = ANY (ARRAY['private'::text, 'public_to'::text]))),
    CONSTRAINT agent_runtime_mode_check CHECK ((runtime_mode = ANY (ARRAY['local'::text, 'cloud'::text]))),
    CONSTRAINT agent_status_check CHECK ((status = ANY (ARRAY['idle'::text, 'working'::text, 'blocked'::text, 'error'::text, 'offline'::text]))),
    CONSTRAINT agent_visibility_check CHECK ((visibility = ANY (ARRAY['workspace'::text, 'private'::text])))
);
CREATE TABLE public.agent_invocation_target (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT agent_invocation_target_target_type_check CHECK ((target_type = ANY (ARRAY['workspace'::text, 'member'::text, 'team'::text])))
);
CREATE TABLE public.agent_mcp_server (
    agent_id uuid NOT NULL,
    server_id uuid NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.agent_runtime (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    daemon_id text,
    name text NOT NULL,
    runtime_mode text NOT NULL,
    provider text NOT NULL,
    status text DEFAULT 'offline'::text NOT NULL,
    device_info text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    owner_id uuid,
    legacy_daemon_id text,
    visibility text DEFAULT 'private'::text NOT NULL,
    profile_id uuid,
    custom_name text,
    CONSTRAINT agent_runtime_runtime_mode_check CHECK ((runtime_mode = ANY (ARRAY['local'::text, 'cloud'::text]))),
    CONSTRAINT agent_runtime_status_check CHECK ((status = ANY (ARRAY['online'::text, 'offline'::text]))),
    CONSTRAINT agent_runtime_visibility_check CHECK ((visibility = ANY (ARRAY['private'::text, 'public'::text])))
);
CREATE TABLE public.agent_skill (
    agent_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    enabled boolean DEFAULT true NOT NULL
);
CREATE TABLE public.agent_task_queue (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    issue_id uuid,
    status text DEFAULT 'queued'::text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    dispatched_at timestamp with time zone,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    result jsonb,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    context jsonb,
    runtime_id uuid NOT NULL,
    session_id text,
    work_dir text,
    trigger_comment_id uuid,
    chat_session_id uuid,
    autopilot_run_id uuid,
    attempt integer DEFAULT 1 NOT NULL,
    max_attempts integer DEFAULT 2 NOT NULL,
    parent_task_id uuid,
    failure_reason text,
    trigger_summary text,
    force_fresh_session boolean DEFAULT false NOT NULL,
    is_leader_task boolean DEFAULT false NOT NULL,
    wait_reason text,
    initiator_user_id uuid,
    handoff_note text,
    prepare_lease_expires_at timestamp with time zone,
    squad_id uuid,
    runtime_mcp_overlay jsonb,
    escalation_for_task_id uuid,
    fire_at timestamp with time zone,
    originator_user_id uuid,
    runtime_connected_apps jsonb,
    coalesced_comment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    delivered_comment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    chat_input_task_id uuid,
    chat_finalize_deferred_at timestamp with time zone,
    originator_source text,
    delegated_from_task_id uuid,
    retry_of_task_id uuid,
    rerun_of_task_id uuid,
    rule_version_id uuid,
    trigger_evidence_kind text,
    trigger_evidence_ref_id uuid,
    accountable_user_id uuid,
    session_rollout_missing boolean DEFAULT false NOT NULL,
    CONSTRAINT agent_task_queue_accountable_matches_originator CHECK (((originator_user_id IS NULL) OR ((accountable_user_id IS NOT NULL) AND (accountable_user_id = originator_user_id)))),
    CONSTRAINT agent_task_queue_chat_issue_exclusive CHECK (((chat_session_id IS NULL) OR (issue_id IS NULL))),
    CONSTRAINT agent_task_queue_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'dispatched'::text, 'running'::text, 'completed'::text, 'failed'::text, 'cancelled'::text, 'waiting_local_directory'::text, 'deferred'::text])))
);
CREATE TABLE public.agent_to_label (
    agent_id uuid NOT NULL,
    label_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.attachment (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    issue_id uuid,
    comment_id uuid,
    uploader_type text NOT NULL,
    uploader_id uuid NOT NULL,
    filename text NOT NULL,
    url text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    chat_session_id uuid,
    chat_message_id uuid,
    task_id uuid,
    CONSTRAINT attachment_uploader_type_check CHECK ((uploader_type = ANY (ARRAY['member'::text, 'agent'::text])))
);
CREATE TABLE public.attachment_tombstone (
    attachment_id uuid NOT NULL,
    workspace_id uuid,
    url text NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.auth_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    actor_type text NOT NULL,
    actor_id text,
    actor_role text,
    action text NOT NULL,
    target_type text NOT NULL,
    target_id text,
    outcome text NOT NULL,
    reason text,
    workspace_id uuid,
    request_id text,
    client_ip text,
    user_agent text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.autopilot (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    assignee_id uuid,
    status text DEFAULT 'active'::text NOT NULL,
    execution_mode text DEFAULT 'create_issue'::text NOT NULL,
    issue_title_template text,
    created_by_type text NOT NULL,
    created_by_id uuid NOT NULL,
    last_run_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    assignee_type text DEFAULT 'agent'::text NOT NULL,
    project_id uuid,
    external_key text,
    CONSTRAINT autopilot_assignee_type_check CHECK ((assignee_type = ANY (ARRAY['agent'::text, 'squad'::text]))),
    CONSTRAINT autopilot_created_by_type_check CHECK ((created_by_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT autopilot_execution_mode_check CHECK ((execution_mode = ANY (ARRAY['create_issue'::text, 'run_only'::text]))),
    CONSTRAINT autopilot_status_check CHECK ((status = ANY (ARRAY['active'::text, 'paused'::text, 'archived'::text])))
);
CREATE TABLE public.autopilot_collaborator (
    autopilot_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    granted_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT autopilot_collaborator_user_type_check CHECK ((user_type = 'member'::text))
);
CREATE TABLE public.autopilot_rule_version (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    published_by_type text NOT NULL,
    published_by_id uuid,
    config_summary jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.autopilot_run (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    trigger_id uuid,
    source text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    issue_id uuid,
    task_id uuid,
    triggered_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    failure_reason text,
    trigger_payload jsonb,
    result jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    squad_id uuid,
    planned_at timestamp with time zone,
    webhook_delivery_id uuid,
    CONSTRAINT autopilot_run_source_check CHECK ((source = ANY (ARRAY['schedule'::text, 'manual'::text, 'webhook'::text, 'api'::text]))),
    CONSTRAINT autopilot_run_status_check CHECK ((status = ANY (ARRAY['issue_created'::text, 'running'::text, 'completed'::text, 'failed'::text, 'skipped'::text])))
);
CREATE TABLE public.autopilot_subscriber (
    autopilot_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT autopilot_subscriber_user_type_check CHECK ((user_type = 'member'::text))
);
CREATE TABLE public.autopilot_trigger (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    autopilot_id uuid NOT NULL,
    kind text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    cron_expression text,
    timezone text DEFAULT 'UTC'::text,
    next_run_at timestamp with time zone,
    webhook_token text,
    label text,
    last_fired_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider text DEFAULT 'generic'::text NOT NULL,
    signing_secret text,
    event_filters jsonb,
    published_by_type text,
    published_by_id uuid,
    CONSTRAINT autopilot_trigger_kind_check CHECK ((kind = ANY (ARRAY['schedule'::text, 'webhook'::text, 'api'::text]))),
    CONSTRAINT autopilot_trigger_provider_check CHECK ((provider = ANY (ARRAY['generic'::text, 'github'::text])))
);
CREATE TABLE public.channel_binding_token (
    token_hash text NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_user_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_binding_token_ttl_cap CHECK ((expires_at <= (created_at + '00:15:00'::interval)))
);
CREATE TABLE public.channel_chat_session_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_chat_id text NOT NULL,
    chat_type text NOT NULL,
    last_message_id text,
    last_thread_id text,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_chat_session_binding_chat_type_check CHECK ((chat_type = ANY (ARRAY['p2p'::text, 'group'::text])))
);
CREATE TABLE public.channel_inbound_audit (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    installation_id uuid,
    channel_type text NOT NULL,
    channel_chat_id text,
    event_type text NOT NULL,
    channel_event_id text,
    channel_message_id text,
    drop_reason text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.channel_inbound_message_dedup (
    installation_id uuid NOT NULL,
    message_id text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    claim_token uuid DEFAULT gen_random_uuid() NOT NULL
);
CREATE TABLE public.channel_installation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    channel_type text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    ws_lease_token text,
    ws_lease_expires_at timestamp with time zone,
    installer_user_id uuid NOT NULL,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_installation_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text])))
);
CREATE TABLE public.channel_media_pending_object (
    storage_key text NOT NULL,
    workspace_id uuid NOT NULL,
    chat_message_id uuid NOT NULL,
    storage_url text NOT NULL,
    installation_id uuid,
    state text DEFAULT 'pending'::text NOT NULL,
    lease_token uuid,
    lease_expires_at timestamp with time zone,
    attempt integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    tombstone_pass integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_media_pending_object_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'deleting'::text, 'tombstoned'::text])))
);
CREATE TABLE public.channel_outbound_card_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    task_id uuid,
    channel_type text NOT NULL,
    channel_chat_id text NOT NULL,
    channel_card_message_id text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    last_patched_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT channel_outbound_card_message_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'streaming'::text, 'final'::text, 'error'::text])))
);
CREATE TABLE public.channel_user_binding (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    goosar_user_id uuid NOT NULL,
    installation_id uuid NOT NULL,
    channel_type text NOT NULL,
    channel_user_id text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    bound_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.chat_draft_restore (
    id uuid NOT NULL,
    chat_session_id uuid NOT NULL,
    task_id uuid NOT NULL,
    content text NOT NULL,
    attachment_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.chat_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_session_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    task_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    failure_reason text,
    elapsed_ms bigint,
    message_kind text DEFAULT 'message'::text NOT NULL,
    channel_media_pending_until timestamp with time zone,
    channel_ingested boolean DEFAULT false NOT NULL,
    CONSTRAINT chat_message_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text])))
);
CREATE TABLE public.chat_pinned_agent (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.chat_session (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    creator_id uuid NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    session_id text,
    work_dir text,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    unread_since timestamp with time zone,
    runtime_id uuid,
    last_read_at timestamp with time zone DEFAULT now() NOT NULL,
    is_agent_intro boolean DEFAULT false NOT NULL,
    pinned_at timestamp with time zone,
    project_id uuid,
    CONSTRAINT chat_session_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);
CREATE TABLE public.client_usage_daily (
    user_id uuid NOT NULL,
    client_type text NOT NULL,
    install_id uuid NOT NULL,
    activity_date date NOT NULL,
    workspace_id uuid,
    client_version text NOT NULL,
    os text NOT NULL,
    first_active_at timestamp with time zone NOT NULL,
    last_active_at timestamp with time zone NOT NULL,
    runtime_probed_at timestamp with time zone,
    probe_result text,
    runtime_count integer,
    provider_summary jsonb,
    online_count integer,
    offline_count integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT client_usage_daily_check CHECK ((first_active_at <= last_active_at)),
    CONSTRAINT client_usage_daily_check1 CHECK ((((runtime_probed_at IS NULL) AND (probe_result IS NULL) AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'error'::text) AND (runtime_count IS NULL) AND (provider_summary IS NULL) AND (online_count IS NULL) AND (offline_count IS NULL)) OR ((runtime_probed_at IS NOT NULL) AND (probe_result = 'success'::text) AND (runtime_count IS NOT NULL) AND (provider_summary IS NOT NULL) AND (online_count IS NOT NULL) AND (offline_count IS NOT NULL) AND ((online_count + offline_count) = runtime_count) AND (jsonb_typeof(provider_summary) = 'object'::text)))),
    CONSTRAINT client_usage_daily_client_type_check CHECK ((client_type = ANY (ARRAY['web'::text, 'desktop'::text]))),
    CONSTRAINT client_usage_daily_offline_count_check CHECK ((offline_count >= 0)),
    CONSTRAINT client_usage_daily_online_count_check CHECK ((online_count >= 0)),
    CONSTRAINT client_usage_daily_probe_result_check CHECK ((probe_result = ANY (ARRAY['success'::text, 'error'::text]))),
    CONSTRAINT client_usage_daily_runtime_count_check CHECK ((runtime_count >= 0))
);
CREATE TABLE public.comment (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    issue_id uuid NOT NULL,
    author_type text NOT NULL,
    author_id uuid NOT NULL,
    content text NOT NULL,
    type text DEFAULT 'comment'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    parent_id uuid,
    workspace_id uuid NOT NULL,
    resolved_at timestamp with time zone,
    resolved_by_type text,
    resolved_by_id uuid,
    source_task_id uuid,
    CONSTRAINT comment_author_type_check CHECK ((author_type = ANY (ARRAY['member'::text, 'agent'::text, 'system'::text]))),
    CONSTRAINT comment_resolved_consistency CHECK ((((resolved_at IS NULL) AND (resolved_by_type IS NULL) AND (resolved_by_id IS NULL)) OR ((resolved_at IS NOT NULL) AND (resolved_by_type IS NOT NULL) AND (resolved_by_id IS NOT NULL)))),
    CONSTRAINT comment_type_check CHECK ((type = ANY (ARRAY['comment'::text, 'status_change'::text, 'progress_update'::text, 'system'::text])))
);
CREATE TABLE public.comment_reaction (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    comment_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT comment_reaction_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text])))
);
CREATE TABLE public.contact_sales_inquiry (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    first_name text NOT NULL,
    last_name text NOT NULL,
    business_email text NOT NULL,
    company_name text NOT NULL,
    company_size text NOT NULL,
    country_region text NOT NULL,
    use_case text NOT NULL,
    goals text DEFAULT ''::text NOT NULL,
    consent_outreach boolean DEFAULT false NOT NULL,
    consent_updates boolean DEFAULT false NOT NULL,
    submitter_ip inet,
    user_agent text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.daemon_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    agent_id uuid NOT NULL,
    daemon_id text NOT NULL,
    status text DEFAULT 'disconnected'::text NOT NULL,
    last_heartbeat_at timestamp with time zone,
    runtime_info jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT daemon_connection_status_check CHECK ((status = ANY (ARRAY['connected'::text, 'disconnected'::text])))
);
CREATE TABLE public.daemon_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    token_hash text NOT NULL,
    workspace_id uuid NOT NULL,
    daemon_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.deployment_admin (
    user_id uuid NOT NULL,
    granted_by uuid,
    granted_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.deployment_admin_pending (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    action text NOT NULL,
    target_user_id uuid NOT NULL,
    requested_by uuid NOT NULL,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT deployment_admin_pending_action_check CHECK ((action = ANY (ARRAY['grant'::text, 'revoke'::text])))
);
CREATE TABLE public.deployment_mcp_server (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    config jsonb NOT NULL,
    transport text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    credential_schema jsonb DEFAULT '[]'::jsonb NOT NULL
);
CREATE TABLE public.deployment_policy (
    singleton boolean DEFAULT true NOT NULL,
    policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT deployment_policy_singleton_check CHECK (singleton)
);
CREATE TABLE public.export_job (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    requested_by uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    error text,
    file_path text,
    size_bytes bigint DEFAULT 0 NOT NULL,
    manifest jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone
);
CREATE TABLE public.feedback (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    workspace_id uuid,
    message text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.github_installation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    account_avatar_url text,
    connected_by_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT github_installation_account_type_check CHECK ((account_type = ANY (ARRAY['User'::text, 'Organization'::text])))
);
CREATE TABLE public.github_pending_check_suite (
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    suite_id bigint NOT NULL,
    head_sha text NOT NULL,
    app_id bigint NOT NULL,
    conclusion text,
    status text NOT NULL,
    suite_updated_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.github_pending_installation (
    installation_id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    account_avatar_url text,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT github_pending_installation_account_login_check CHECK ((account_login <> ''::text)),
    CONSTRAINT github_pending_installation_account_type_check CHECK ((account_type = ANY (ARRAY['User'::text, 'Organization'::text])))
);
CREATE TABLE public.github_pull_request (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    installation_id bigint NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    title text NOT NULL,
    state text NOT NULL,
    html_url text NOT NULL,
    branch text,
    author_login text,
    author_avatar_url text,
    merged_at timestamp with time zone,
    closed_at timestamp with time zone,
    pr_created_at timestamp with time zone NOT NULL,
    pr_updated_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    head_sha text DEFAULT ''::text NOT NULL,
    mergeable_state text,
    additions integer DEFAULT 0 NOT NULL,
    deletions integer DEFAULT 0 NOT NULL,
    changed_files integer DEFAULT 0 NOT NULL,
    api_mergeable text,
    api_merge_state_status text,
    checks_rollup_state text,
    snapshot_head_sha text DEFAULT ''::text NOT NULL,
    snapshot_fetched_at timestamp with time zone,
    CONSTRAINT github_pull_request_state_check CHECK ((state = ANY (ARRAY['open'::text, 'closed'::text, 'merged'::text, 'draft'::text])))
);
CREATE TABLE public.github_pull_request_check_run (
    pr_id uuid NOT NULL,
    head_sha text NOT NULL,
    ordinal integer NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    conclusion text,
    details_url text,
    is_status_context boolean DEFAULT false NOT NULL
);
CREATE TABLE public.github_pull_request_check_suite (
    pr_id uuid NOT NULL,
    suite_id bigint NOT NULL,
    head_sha text NOT NULL,
    app_id bigint NOT NULL,
    conclusion text,
    status text NOT NULL,
    updated_at timestamp with time zone NOT NULL
);
CREATE TABLE public.inbox_item (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    recipient_type text NOT NULL,
    recipient_id uuid NOT NULL,
    type text NOT NULL,
    severity text DEFAULT 'info'::text NOT NULL,
    issue_id uuid,
    title text NOT NULL,
    body text,
    read boolean DEFAULT false NOT NULL,
    archived boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_type text,
    actor_id uuid,
    details jsonb DEFAULT '{}'::jsonb,
    CONSTRAINT inbox_item_recipient_type_check CHECK ((recipient_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT inbox_item_severity_check CHECK ((severity = ANY (ARRAY['action_required'::text, 'attention'::text, 'info'::text])))
);
CREATE TABLE public.issue (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    status text DEFAULT 'backlog'::text NOT NULL,
    priority text DEFAULT 'none'::text NOT NULL,
    assignee_type text,
    assignee_id uuid,
    creator_type text NOT NULL,
    creator_id uuid NOT NULL,
    parent_issue_id uuid,
    acceptance_criteria jsonb DEFAULT '[]'::jsonb NOT NULL,
    context_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    due_date date,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    number integer DEFAULT 0 NOT NULL,
    project_id uuid,
    origin_type text,
    origin_id uuid,
    first_executed_at timestamp with time zone,
    start_date date,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    stage integer,
    properties jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT issue_assignee_type_check CHECK ((assignee_type = ANY (ARRAY['member'::text, 'agent'::text, 'squad'::text]))),
    CONSTRAINT issue_creator_type_check CHECK ((creator_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT issue_metadata_is_object CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT issue_metadata_size_limit CHECK ((pg_column_size(metadata) <= 8192)),
    CONSTRAINT issue_origin_type_check CHECK ((origin_type = ANY (ARRAY['autopilot'::text, 'quick_create'::text, 'lark_chat'::text, 'slack_chat'::text, 'agent_create'::text]))),
    CONSTRAINT issue_priority_check CHECK ((priority = ANY (ARRAY['urgent'::text, 'high'::text, 'medium'::text, 'low'::text, 'none'::text]))),
    CONSTRAINT issue_properties_is_object CHECK ((jsonb_typeof(properties) = 'object'::text)),
    CONSTRAINT issue_properties_size_limit CHECK ((pg_column_size(properties) <= 16384)),
    CONSTRAINT issue_stage_check CHECK (((stage IS NULL) OR (stage >= 1))),
    CONSTRAINT issue_status_check CHECK ((status = ANY (ARRAY['backlog'::text, 'todo'::text, 'in_progress'::text, 'in_review'::text, 'done'::text, 'blocked'::text, 'cancelled'::text])))
);
CREATE TABLE public.issue_dependency (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    issue_id uuid NOT NULL,
    depends_on_issue_id uuid NOT NULL,
    type text NOT NULL,
    CONSTRAINT issue_dependency_type_check CHECK ((type = ANY (ARRAY['blocks'::text, 'blocked_by'::text, 'related'::text])))
);
CREATE TABLE public.issue_label (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    color text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resource_type text DEFAULT 'issue'::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    CONSTRAINT issue_label_resource_type_check CHECK ((resource_type = ANY (ARRAY['issue'::text, 'agent'::text, 'skill'::text])))
);
CREATE TABLE public.issue_property (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    type text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    icon text DEFAULT ''::text NOT NULL,
    CONSTRAINT issue_property_config_check CHECK ((jsonb_typeof(config) = 'object'::text)),
    CONSTRAINT issue_property_type_check CHECK ((type = ANY (ARRAY['text'::text, 'number'::text, 'select'::text, 'multi_select'::text, 'date'::text, 'checkbox'::text, 'url'::text])))
);
CREATE TABLE public.issue_pull_request (
    issue_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    linked_by_type text,
    linked_by_id uuid,
    linked_at timestamp with time zone DEFAULT now() NOT NULL,
    close_intent boolean DEFAULT false NOT NULL,
    reference_only boolean DEFAULT false NOT NULL
);
CREATE TABLE public.issue_reaction (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    issue_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    emoji text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_reaction_actor_type_check CHECK ((actor_type = ANY (ARRAY['member'::text, 'agent'::text])))
);
CREATE TABLE public.issue_subscriber (
    issue_id uuid NOT NULL,
    user_type text NOT NULL,
    user_id uuid NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT issue_subscriber_reason_check CHECK ((reason = ANY (ARRAY['creator'::text, 'assignee'::text, 'commenter'::text, 'mentioned'::text, 'manual'::text, 'autopilot'::text]))),
    CONSTRAINT issue_subscriber_user_type_check CHECK ((user_type = ANY (ARRAY['member'::text, 'agent'::text])))
);
CREATE TABLE public.issue_to_label (
    issue_id uuid NOT NULL,
    label_id uuid NOT NULL
);
CREATE TABLE public.issue_vcs_pull_request (
    issue_id uuid NOT NULL,
    pull_request_id uuid NOT NULL,
    close_intent boolean DEFAULT false NOT NULL,
    reference_only boolean DEFAULT false NOT NULL,
    linked_by_type text,
    linked_by_id uuid,
    linked_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.member (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    perimeter_access boolean DEFAULT false NOT NULL,
    CONSTRAINT member_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text])))
);
CREATE TABLE public.notification_preference (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    preferences jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.personal_access_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    revoked boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.pinned_item (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    item_type text NOT NULL,
    item_id uuid NOT NULL,
    "position" double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT pinned_item_item_type_check CHECK ((item_type = ANY (ARRAY['issue'::text, 'project'::text])))
);
CREATE TABLE public.project (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    title text NOT NULL,
    description text,
    icon text,
    status text DEFAULT 'planned'::text NOT NULL,
    lead_type text,
    lead_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    priority text DEFAULT 'none'::text NOT NULL,
    start_date date,
    due_date date,
    CONSTRAINT project_lead_type_check CHECK ((lead_type = ANY (ARRAY['member'::text, 'agent'::text]))),
    CONSTRAINT project_priority_check CHECK ((priority = ANY (ARRAY['urgent'::text, 'high'::text, 'medium'::text, 'low'::text, 'none'::text]))),
    CONSTRAINT project_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'in_progress'::text, 'paused'::text, 'completed'::text, 'cancelled'::text])))
);
CREATE TABLE public.project_resource (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    resource_type text NOT NULL,
    resource_ref jsonb NOT NULL,
    label text,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);
CREATE TABLE public.provisioning_delivered_package (
    workspace_id uuid NOT NULL,
    package_name text NOT NULL,
    package_type text NOT NULL,
    version text NOT NULL,
    first_delivered_at timestamp with time zone DEFAULT now() NOT NULL,
    last_delivered_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provisioning_delivered_package_package_type_check CHECK ((package_type = ANY (ARRAY['skill'::text, 'mcp-server'::text, 'runtime'::text])))
);
CREATE TABLE public.provisioning_pin (
    workspace_id uuid NOT NULL,
    package_name text NOT NULL,
    package_type text NOT NULL,
    version text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT provisioning_pin_package_type_check CHECK ((package_type = ANY (ARRAY['skill'::text, 'mcp-server'::text, 'runtime'::text])))
);
CREATE TABLE public.runtime_profile (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    display_name text NOT NULL,
    protocol_family text NOT NULL,
    command_name text NOT NULL,
    description text,
    fixed_args jsonb DEFAULT '[]'::jsonb NOT NULL,
    visibility text DEFAULT 'workspace'::text NOT NULL,
    created_by uuid,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT runtime_profile_visibility_check CHECK ((visibility = ANY (ARRAY['workspace'::text, 'private'::text])))
);
CREATE TABLE public.skill (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.skill_file (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    skill_id uuid NOT NULL,
    path text NOT NULL,
    content text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.skill_to_label (
    skill_id uuid NOT NULL,
    label_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.squad (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    leader_id uuid NOT NULL,
    creator_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    archived_at timestamp with time zone,
    archived_by uuid,
    avatar_url text,
    instructions text DEFAULT ''::text NOT NULL
);
CREATE TABLE public.squad_member (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    squad_id uuid NOT NULL,
    member_type text NOT NULL,
    member_id uuid NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT squad_member_member_type_check CHECK ((member_type = ANY (ARRAY['agent'::text, 'member'::text])))
);
CREATE TABLE public.sys_cron_executions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    job_name text NOT NULL,
    scope_kind text DEFAULT 'global'::text NOT NULL,
    scope_id text DEFAULT 'global'::text NOT NULL,
    plan_time timestamp with time zone NOT NULL,
    status text NOT NULL,
    attempt integer DEFAULT 1 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    next_retry_at timestamp with time zone,
    runner_id text,
    lease_token uuid DEFAULT gen_random_uuid() NOT NULL,
    heartbeat_at timestamp with time zone,
    stale_after timestamp with time zone,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    duration_ms integer,
    rows_affected bigint,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_code text,
    error_msg text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_sys_cron_attempt CHECK (((attempt >= 1) AND (max_attempts >= attempt))),
    CONSTRAINT chk_sys_cron_duration CHECK (((duration_ms IS NULL) OR (duration_ms >= 0))),
    CONSTRAINT chk_sys_cron_status CHECK ((status = ANY (ARRAY['RUNNING'::text, 'SUCCESS'::text, 'FAILED'::text])))
);
CREATE TABLE public.task_message (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    seq integer NOT NULL,
    type text NOT NULL,
    tool text,
    content text,
    input jsonb,
    output text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.task_token (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    token_hash text NOT NULL,
    task_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.task_usage (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    provider text DEFAULT ''::text NOT NULL,
    model text NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_tokens bigint DEFAULT 0 NOT NULL,
    cache_write_tokens bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now(),
    cost_usd_ticks bigint
);
CREATE TABLE public.task_usage_hourly (
    bucket_hour timestamp with time zone NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    project_id uuid,
    provider text NOT NULL,
    model text NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_tokens bigint DEFAULT 0 NOT NULL,
    cache_write_tokens bigint DEFAULT 0 NOT NULL,
    task_count bigint DEFAULT 0 NOT NULL,
    event_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    cost_usd_ticks bigint DEFAULT 0 NOT NULL,
    uncosted_input_tokens bigint,
    uncosted_output_tokens bigint,
    uncosted_cache_read_tokens bigint,
    uncosted_cache_write_tokens bigint
);
CREATE TABLE public.task_usage_hourly_dirty (
    bucket_hour timestamp with time zone NOT NULL,
    workspace_id uuid NOT NULL,
    runtime_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    project_id uuid,
    provider text NOT NULL,
    model text NOT NULL,
    enqueued_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.task_usage_hourly_rollup_state (
    id smallint DEFAULT 1 NOT NULL,
    watermark_at timestamp with time zone DEFAULT '1970-01-01 00:00:00+00'::timestamp with time zone NOT NULL,
    last_run_started_at timestamp with time zone,
    last_run_finished_at timestamp with time zone,
    last_run_rows bigint DEFAULT 0 NOT NULL,
    last_error text,
    CONSTRAINT task_usage_hourly_rollup_state_id_check CHECK ((id = 1))
);
CREATE TABLE public."user" (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    avatar_url text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    onboarded_at timestamp with time zone,
    onboarding_questionnaire jsonb DEFAULT '{}'::jsonb NOT NULL,
    cloud_waitlist_email character varying(254),
    cloud_waitlist_reason text,
    starter_content_state text,
    language character varying(20) DEFAULT NULL::character varying,
    profile_description text DEFAULT ''::text NOT NULL,
    timezone text,
    deactivated_at timestamp with time zone,
    token_version integer DEFAULT 0 NOT NULL
);
CREATE TABLE public.user_composio_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    toolkit_slug text NOT NULL,
    auth_config_id text NOT NULL,
    connected_account_id text NOT NULL,
    composio_user_id text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    connected_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.user_config_override (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    llm_base_url text,
    llm_model text,
    llm_api_key bytea,
    mcp_overrides jsonb,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);
CREATE TABLE public.user_identity (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    subject text NOT NULL,
    email_at_link text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_login_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.user_mfa (
    user_id uuid NOT NULL,
    totp_secret_sealed bytea NOT NULL,
    enabled_at timestamp with time zone,
    last_used_step bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.user_mfa_recovery_code (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    code_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    used_at timestamp with time zone
);
CREATE TABLE public.user_session (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    ip_hash text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone
);
CREATE TABLE public.vcs_commit_status (
    connection_id uuid NOT NULL,
    sha text NOT NULL,
    context text NOT NULL,
    state text NOT NULL,
    target_url text,
    description text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.vcs_connection (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    provider text DEFAULT 'forgejo'::text NOT NULL,
    instance_url text NOT NULL,
    account_login text NOT NULL,
    access_token_encrypted text NOT NULL,
    webhook_secret_encrypted text NOT NULL,
    connected_by_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vcs_connection_provider_check CHECK ((provider = ANY (ARRAY['forgejo'::text, 'gitea'::text, 'gitlab'::text])))
);
CREATE TABLE public.vcs_pull_request (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    connection_id uuid NOT NULL,
    provider text DEFAULT 'forgejo'::text NOT NULL,
    repo_owner text NOT NULL,
    repo_name text NOT NULL,
    pr_number integer NOT NULL,
    title text NOT NULL,
    state text NOT NULL,
    html_url text NOT NULL,
    branch text,
    head_sha text DEFAULT ''::text NOT NULL,
    author_login text,
    author_avatar_url text,
    merged_at timestamp with time zone,
    closed_at timestamp with time zone,
    pr_created_at timestamp with time zone NOT NULL,
    pr_updated_at timestamp with time zone NOT NULL,
    additions integer DEFAULT 0 NOT NULL,
    deletions integer DEFAULT 0 NOT NULL,
    changed_files integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vcs_pull_request_provider_check CHECK ((provider = ANY (ARRAY['forgejo'::text, 'gitea'::text, 'gitlab'::text]))),
    CONSTRAINT vcs_pull_request_state_check CHECK ((state = ANY (ARRAY['open'::text, 'closed'::text, 'merged'::text, 'draft'::text])))
);
CREATE TABLE public.verification_code (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    code text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    link_token_hash text
);
CREATE TABLE public.webhook_delivery (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    autopilot_id uuid NOT NULL,
    trigger_id uuid NOT NULL,
    provider text NOT NULL,
    event text DEFAULT 'webhook.received'::text NOT NULL,
    dedupe_key text,
    dedupe_source text,
    signature_status text DEFAULT 'not_required'::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempt_count integer DEFAULT 1 NOT NULL,
    selected_headers jsonb DEFAULT '{}'::jsonb NOT NULL,
    content_type text,
    raw_body bytea,
    response_status integer,
    response_body text,
    autopilot_run_id uuid,
    replayed_from_delivery_id uuid,
    error text,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    last_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_token uuid,
    lease_expires_at timestamp with time zone,
    dispatch_attempts integer DEFAULT 0 NOT NULL,
    CONSTRAINT webhook_delivery_provider_check CHECK ((provider = ANY (ARRAY['generic'::text, 'github'::text]))),
    CONSTRAINT webhook_delivery_signature_status_check CHECK ((signature_status = ANY (ARRAY['not_required'::text, 'valid'::text, 'invalid'::text, 'missing'::text]))),
    CONSTRAINT webhook_delivery_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'dispatched'::text, 'rejected'::text, 'ignored'::text, 'failed'::text])))
);
CREATE TABLE public.workspace (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    context text,
    repos jsonb DEFAULT '[]'::jsonb NOT NULL,
    issue_prefix text DEFAULT ''::text NOT NULL,
    issue_counter integer DEFAULT 0 NOT NULL,
    avatar_url text,
    attribution_fail_closed boolean DEFAULT false NOT NULL,
    template_key text,
    open_join boolean DEFAULT false NOT NULL
);
CREATE TABLE public.workspace_config (
    workspace_id uuid NOT NULL,
    llm_base_url text,
    llm_model text,
    llm_api_key bytea,
    mcp_defaults jsonb,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);
CREATE TABLE public.workspace_deployment_mcp_server (
    workspace_id uuid NOT NULL,
    server_id uuid NOT NULL,
    enabled_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.workspace_helper_default (
    workspace_id uuid NOT NULL,
    template_key text NOT NULL,
    helper_name jsonb DEFAULT '{}'::jsonb NOT NULL,
    helper_extra_instructions jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.workspace_invitation (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    inviter_id uuid NOT NULL,
    invitee_email text NOT NULL,
    invitee_user_id uuid,
    role text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '7 days'::interval) NOT NULL,
    CONSTRAINT workspace_invitation_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text]))),
    CONSTRAINT workspace_invitation_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'accepted'::text, 'declined'::text, 'expired'::text])))
);
CREATE TABLE public.workspace_mcp_server (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    workspace_id uuid NOT NULL,
    name text NOT NULL,
    config jsonb NOT NULL,
    transport text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    credential_schema jsonb DEFAULT '[]'::jsonb NOT NULL
);
CREATE TABLE public.workspace_mcp_user_credential (
    server_id uuid NOT NULL,
    user_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    sealed_values jsonb NOT NULL,
    value_keys text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
CREATE TABLE public.workspace_template (
    key text NOT NULL,
    display_name jsonb DEFAULT '{}'::jsonb NOT NULL,
    description jsonb DEFAULT '{}'::jsonb NOT NULL,
    pins jsonb DEFAULT '[]'::jsonb NOT NULL,
    mcp_defaults jsonb DEFAULT '{}'::jsonb NOT NULL,
    helper jsonb DEFAULT '{}'::jsonb NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    capabilities jsonb DEFAULT '[]'::jsonb NOT NULL,
    sample_tasks jsonb DEFAULT '[]'::jsonb NOT NULL,
    autopilots jsonb DEFAULT '[]'::jsonb NOT NULL
);
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT activity_log_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.admin_audit
    ADD CONSTRAINT admin_audit_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_invocation_target
    ADD CONSTRAINT agent_invocation_target_agent_id_target_type_target_id_key UNIQUE (agent_id, target_type, target_id);
ALTER TABLE ONLY public.agent_invocation_target
    ADD CONSTRAINT agent_invocation_target_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_mcp_server
    ADD CONSTRAINT agent_mcp_server_pkey PRIMARY KEY (agent_id, server_id);
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_runtime
    ADD CONSTRAINT agent_runtime_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_skill
    ADD CONSTRAINT agent_skill_pkey PRIMARY KEY (agent_id, skill_id);
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.agent_to_label
    ADD CONSTRAINT agent_to_label_pkey PRIMARY KEY (agent_id, label_id);
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_workspace_name_unique UNIQUE (workspace_id, name);
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.attachment_tombstone
    ADD CONSTRAINT attachment_tombstone_pkey PRIMARY KEY (attachment_id);
ALTER TABLE ONLY public.auth_audit
    ADD CONSTRAINT auth_audit_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.autopilot_collaborator
    ADD CONSTRAINT autopilot_collaborator_pkey PRIMARY KEY (autopilot_id, user_type, user_id);
ALTER TABLE ONLY public.autopilot
    ADD CONSTRAINT autopilot_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.autopilot_rule_version
    ADD CONSTRAINT autopilot_rule_version_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.autopilot_subscriber
    ADD CONSTRAINT autopilot_subscriber_pkey PRIMARY KEY (autopilot_id, user_type, user_id);
ALTER TABLE ONLY public.autopilot_trigger
    ADD CONSTRAINT autopilot_trigger_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.channel_binding_token
    ADD CONSTRAINT channel_binding_token_pkey PRIMARY KEY (token_hash);
ALTER TABLE ONLY public.channel_chat_session_binding
    ADD CONSTRAINT channel_chat_session_binding_chat_session_id_key UNIQUE (chat_session_id);
ALTER TABLE ONLY public.channel_chat_session_binding
    ADD CONSTRAINT channel_chat_session_binding_installation_id_channel_chat_i_key UNIQUE (installation_id, channel_chat_id);
ALTER TABLE ONLY public.channel_chat_session_binding
    ADD CONSTRAINT channel_chat_session_binding_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.channel_inbound_audit
    ADD CONSTRAINT channel_inbound_audit_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.channel_inbound_message_dedup
    ADD CONSTRAINT channel_inbound_message_dedup_pkey PRIMARY KEY (installation_id, message_id);
ALTER TABLE ONLY public.channel_installation
    ADD CONSTRAINT channel_installation_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.channel_installation
    ADD CONSTRAINT channel_installation_workspace_id_agent_id_channel_type_key UNIQUE (workspace_id, agent_id, channel_type);
ALTER TABLE ONLY public.channel_media_pending_object
    ADD CONSTRAINT channel_media_pending_object_pkey PRIMARY KEY (storage_key);
ALTER TABLE ONLY public.channel_outbound_card_message
    ADD CONSTRAINT channel_outbound_card_message_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.channel_user_binding
    ADD CONSTRAINT channel_user_binding_installation_id_channel_user_id_key UNIQUE (installation_id, channel_user_id);
ALTER TABLE ONLY public.channel_user_binding
    ADD CONSTRAINT channel_user_binding_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.chat_draft_restore
    ADD CONSTRAINT chat_draft_restore_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.chat_message
    ADD CONSTRAINT chat_message_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.chat_pinned_agent
    ADD CONSTRAINT chat_pinned_agent_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.chat_pinned_agent
    ADD CONSTRAINT chat_pinned_agent_workspace_id_user_id_agent_id_key UNIQUE (workspace_id, user_id, agent_id);
ALTER TABLE ONLY public.chat_session
    ADD CONSTRAINT chat_session_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.client_usage_daily
    ADD CONSTRAINT client_usage_daily_pkey PRIMARY KEY (user_id, client_type, install_id, activity_date);
ALTER TABLE ONLY public.comment
    ADD CONSTRAINT comment_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.comment_reaction
    ADD CONSTRAINT comment_reaction_comment_id_actor_type_actor_id_emoji_key UNIQUE (comment_id, actor_type, actor_id, emoji);
ALTER TABLE ONLY public.comment_reaction
    ADD CONSTRAINT comment_reaction_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.contact_sales_inquiry
    ADD CONSTRAINT contact_sales_inquiry_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.daemon_connection
    ADD CONSTRAINT daemon_connection_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.daemon_token
    ADD CONSTRAINT daemon_token_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.deployment_admin_pending
    ADD CONSTRAINT deployment_admin_pending_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.deployment_admin
    ADD CONSTRAINT deployment_admin_pkey PRIMARY KEY (user_id);
ALTER TABLE ONLY public.deployment_mcp_server
    ADD CONSTRAINT deployment_mcp_server_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.deployment_policy
    ADD CONSTRAINT deployment_policy_pkey PRIMARY KEY (singleton);
ALTER TABLE ONLY public.export_job
    ADD CONSTRAINT export_job_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.feedback
    ADD CONSTRAINT feedback_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.github_installation
    ADD CONSTRAINT github_installation_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.github_installation
    ADD CONSTRAINT github_installation_workspace_id_installation_id_key UNIQUE (workspace_id, installation_id);
ALTER TABLE ONLY public.github_pending_check_suite
    ADD CONSTRAINT github_pending_check_suite_pkey PRIMARY KEY (workspace_id, repo_owner, repo_name, pr_number, suite_id);
ALTER TABLE ONLY public.github_pending_installation
    ADD CONSTRAINT github_pending_installation_pkey PRIMARY KEY (installation_id);
ALTER TABLE ONLY public.github_pull_request_check_suite
    ADD CONSTRAINT github_pull_request_check_suite_pkey PRIMARY KEY (pr_id, suite_id);
ALTER TABLE ONLY public.github_pull_request
    ADD CONSTRAINT github_pull_request_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.github_pull_request
    ADD CONSTRAINT github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key UNIQUE (workspace_id, repo_owner, repo_name, pr_number);
ALTER TABLE ONLY public.inbox_item
    ADD CONSTRAINT inbox_item_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue_dependency
    ADD CONSTRAINT issue_dependency_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue_label
    ADD CONSTRAINT issue_label_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue
    ADD CONSTRAINT issue_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue_property
    ADD CONSTRAINT issue_property_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue_pull_request
    ADD CONSTRAINT issue_pull_request_pkey PRIMARY KEY (issue_id, pull_request_id);
ALTER TABLE ONLY public.issue_reaction
    ADD CONSTRAINT issue_reaction_issue_id_actor_type_actor_id_emoji_key UNIQUE (issue_id, actor_type, actor_id, emoji);
ALTER TABLE ONLY public.issue_reaction
    ADD CONSTRAINT issue_reaction_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.issue_subscriber
    ADD CONSTRAINT issue_subscriber_pkey PRIMARY KEY (issue_id, user_type, user_id);
ALTER TABLE ONLY public.issue_to_label
    ADD CONSTRAINT issue_to_label_pkey PRIMARY KEY (issue_id, label_id);
ALTER TABLE ONLY public.issue_vcs_pull_request
    ADD CONSTRAINT issue_vcs_pull_request_pkey PRIMARY KEY (issue_id, pull_request_id);
ALTER TABLE ONLY public.member
    ADD CONSTRAINT member_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.member
    ADD CONSTRAINT member_workspace_id_user_id_key UNIQUE (workspace_id, user_id);
ALTER TABLE ONLY public.notification_preference
    ADD CONSTRAINT notification_preference_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.notification_preference
    ADD CONSTRAINT notification_preference_workspace_id_user_id_key UNIQUE (workspace_id, user_id);
ALTER TABLE ONLY public.personal_access_token
    ADD CONSTRAINT personal_access_token_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.pinned_item
    ADD CONSTRAINT pinned_item_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.pinned_item
    ADD CONSTRAINT pinned_item_workspace_id_user_id_item_type_item_id_key UNIQUE (workspace_id, user_id, item_type, item_id);
ALTER TABLE ONLY public.project
    ADD CONSTRAINT project_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.project_resource
    ADD CONSTRAINT project_resource_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.project_resource
    ADD CONSTRAINT project_resource_project_id_resource_type_resource_ref_key UNIQUE (project_id, resource_type, resource_ref);
ALTER TABLE ONLY public.provisioning_delivered_package
    ADD CONSTRAINT provisioning_delivered_package_pkey PRIMARY KEY (workspace_id, package_name, package_type, version);
ALTER TABLE ONLY public.provisioning_pin
    ADD CONSTRAINT provisioning_pin_pkey PRIMARY KEY (workspace_id, package_name, package_type);
ALTER TABLE ONLY public.runtime_profile
    ADD CONSTRAINT runtime_profile_pkey PRIMARY KEY (id);
ALTER TABLE public.runtime_profile
    ADD CONSTRAINT runtime_profile_protocol_family_check CHECK ((protocol_family = ANY (ARRAY['runtime-b'::text, 'runtime-c'::text, 'runtime-d'::text, 'runtime-e'::text, 'runtime-f'::text, 'runtime-m'::text, 'runtime-n'::text, 'runtime-j'::text, 'runtime-o'::text, 'runtime-g'::text, 'runtime-k'::text, 'runtime-l'::text, 'runtime-a'::text, 'runtime-p'::text, 'runtime-r'::text, 'runtime-h'::text, 'runtime-i'::text, 'runtime-q'::text]))) NOT VALID;
ALTER TABLE ONLY public.runtime_profile
    ADD CONSTRAINT runtime_profile_workspace_id_display_name_key UNIQUE (workspace_id, display_name);
ALTER TABLE ONLY public.skill_file
    ADD CONSTRAINT skill_file_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.skill_file
    ADD CONSTRAINT skill_file_skill_id_path_key UNIQUE (skill_id, path);
ALTER TABLE ONLY public.skill
    ADD CONSTRAINT skill_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.skill_to_label
    ADD CONSTRAINT skill_to_label_pkey PRIMARY KEY (skill_id, label_id);
ALTER TABLE ONLY public.skill
    ADD CONSTRAINT skill_workspace_id_name_key UNIQUE (workspace_id, name);
ALTER TABLE ONLY public.squad_member
    ADD CONSTRAINT squad_member_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.squad_member
    ADD CONSTRAINT squad_member_squad_id_member_type_member_id_key UNIQUE (squad_id, member_type, member_id);
ALTER TABLE ONLY public.squad
    ADD CONSTRAINT squad_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.sys_cron_executions
    ADD CONSTRAINT sys_cron_executions_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.task_message
    ADD CONSTRAINT task_message_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.task_token
    ADD CONSTRAINT task_token_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.task_usage_hourly_rollup_state
    ADD CONSTRAINT task_usage_hourly_rollup_state_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.task_usage
    ADD CONSTRAINT task_usage_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.task_usage
    ADD CONSTRAINT task_usage_task_id_provider_model_key UNIQUE (task_id, provider, model);
ALTER TABLE ONLY public.daemon_connection
    ADD CONSTRAINT uq_daemon_agent UNIQUE (agent_id, daemon_id);
ALTER TABLE ONLY public.issue
    ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
ALTER TABLE ONLY public.sys_cron_executions
    ADD CONSTRAINT uq_sys_cron_execution UNIQUE (job_name, scope_kind, scope_id, plan_time);
ALTER TABLE ONLY public.task_usage_hourly_dirty
    ADD CONSTRAINT uq_task_usage_hourly_dirty_key UNIQUE NULLS NOT DISTINCT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model);
ALTER TABLE ONLY public.task_usage_hourly
    ADD CONSTRAINT uq_task_usage_hourly_key UNIQUE NULLS NOT DISTINCT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model);
ALTER TABLE ONLY public.user_composio_connection
    ADD CONSTRAINT user_composio_connection_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.user_composio_connection
    ADD CONSTRAINT user_composio_connection_user_id_connected_account_id_key UNIQUE (user_id, connected_account_id);
ALTER TABLE ONLY public.user_config_override
    ADD CONSTRAINT user_config_override_pkey PRIMARY KEY (workspace_id, user_id);
ALTER TABLE ONLY public."user"
    ADD CONSTRAINT user_email_key UNIQUE (email);
ALTER TABLE ONLY public.user_identity
    ADD CONSTRAINT user_identity_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.user_mfa
    ADD CONSTRAINT user_mfa_pkey PRIMARY KEY (user_id);
ALTER TABLE ONLY public.user_mfa_recovery_code
    ADD CONSTRAINT user_mfa_recovery_code_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public."user"
    ADD CONSTRAINT user_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.user_session
    ADD CONSTRAINT user_session_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.vcs_commit_status
    ADD CONSTRAINT vcs_commit_status_pkey PRIMARY KEY (connection_id, sha, context);
ALTER TABLE ONLY public.vcs_connection
    ADD CONSTRAINT vcs_connection_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.vcs_connection
    ADD CONSTRAINT vcs_connection_workspace_id_instance_url_key UNIQUE (workspace_id, instance_url);
ALTER TABLE ONLY public.vcs_pull_request
    ADD CONSTRAINT vcs_pull_request_connection_id_repo_owner_repo_name_pr_numb_key UNIQUE (connection_id, repo_owner, repo_name, pr_number);
ALTER TABLE ONLY public.vcs_pull_request
    ADD CONSTRAINT vcs_pull_request_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.verification_code
    ADD CONSTRAINT verification_code_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.workspace_config
    ADD CONSTRAINT workspace_config_pkey PRIMARY KEY (workspace_id);
ALTER TABLE ONLY public.workspace_deployment_mcp_server
    ADD CONSTRAINT workspace_deployment_mcp_server_pkey PRIMARY KEY (workspace_id, server_id);
ALTER TABLE ONLY public.workspace_helper_default
    ADD CONSTRAINT workspace_helper_default_pkey PRIMARY KEY (workspace_id);
ALTER TABLE ONLY public.workspace_invitation
    ADD CONSTRAINT workspace_invitation_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.workspace_mcp_server
    ADD CONSTRAINT workspace_mcp_server_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.workspace_mcp_user_credential
    ADD CONSTRAINT workspace_mcp_user_credential_pkey PRIMARY KEY (server_id, user_id);
ALTER TABLE ONLY public.workspace
    ADD CONSTRAINT workspace_pkey PRIMARY KEY (id);
ALTER TABLE ONLY public.workspace
    ADD CONSTRAINT workspace_slug_key UNIQUE (slug);
ALTER TABLE ONLY public.workspace_template
    ADD CONSTRAINT workspace_template_pkey PRIMARY KEY (key);
CREATE INDEX agent_invocation_target_agent_id_idx ON public.agent_invocation_target USING btree (agent_id);
CREATE INDEX agent_invocation_target_target_idx ON public.agent_invocation_target USING btree (target_type, target_id);
CREATE UNIQUE INDEX agent_runtime_workspace_daemon_profile_key ON public.agent_runtime USING btree (workspace_id, daemon_id, profile_id) WHERE (profile_id IS NOT NULL);
CREATE UNIQUE INDEX agent_runtime_workspace_daemon_provider_key ON public.agent_runtime USING btree (workspace_id, daemon_id, provider) WHERE (profile_id IS NULL);
CREATE UNIQUE INDEX agent_system_identity_unique ON public.agent USING btree (workspace_id, owner_id, runtime_id, system_key) WHERE (system_key IS NOT NULL);
CREATE INDEX agent_task_queue_originator_user_id_idx ON public.agent_task_queue USING btree (originator_user_id) WHERE (originator_user_id IS NOT NULL);
CREATE INDEX agent_task_queue_squad_id_idx ON public.agent_task_queue USING btree (squad_id) WHERE (squad_id IS NOT NULL);
CREATE INDEX agent_to_label_label_idx ON public.agent_to_label USING btree (label_id);
CREATE UNIQUE INDEX agent_workspace_member_helper_unique ON public.agent USING btree (workspace_id, owner_id) WHERE ((system_key = 'goosar_helper'::text) AND (archived_at IS NULL));
CREATE UNIQUE INDEX agent_workspace_role_agent_unique ON public.agent USING btree (workspace_id, system_key) WHERE ((system_key ~~ 'goosar_role_%'::text) AND (archived_at IS NULL));
CREATE INDEX client_usage_daily_activity_client_user_idx ON public.client_usage_daily USING btree (activity_date, client_type, user_id);
CREATE INDEX client_usage_daily_workspace_idx ON public.client_usage_daily USING btree (workspace_id) WHERE (workspace_id IS NOT NULL);
CREATE INDEX comment_issue_resolved_at_idx ON public.comment USING btree (issue_id, resolved_at);
CREATE UNIQUE INDEX github_pull_request_check_run_pr_ordinal_idx ON public.github_pull_request_check_run USING btree (pr_id, ordinal);
CREATE INDEX idx_activity_log_issue_keyset ON public.activity_log USING btree (issue_id, created_at DESC, id DESC);
CREATE INDEX idx_activity_log_squad_no_action_task ON public.activity_log USING btree (issue_id, actor_id, ((details ->> 'task_id'::text))) WHERE ((actor_type = 'agent'::text) AND (action = 'squad_leader_evaluated'::text) AND ((details ->> 'outcome'::text) = 'no_action'::text));
CREATE INDEX idx_admin_audit_created_at ON public.admin_audit USING btree (created_at DESC, id DESC);
CREATE INDEX idx_agent_mcp_server_server ON public.agent_mcp_server USING btree (server_id);
CREATE INDEX idx_agent_runtime_last_seen_at ON public.agent_runtime USING btree (last_seen_at);
CREATE INDEX idx_agent_runtime_status ON public.agent_runtime USING btree (workspace_id, status);
CREATE INDEX idx_agent_runtime_workspace ON public.agent_runtime USING btree (workspace_id);
CREATE INDEX idx_agent_skill_agent ON public.agent_skill USING btree (agent_id);
CREATE INDEX idx_agent_skill_skill ON public.agent_skill USING btree (skill_id);
CREATE INDEX idx_agent_task_queue_agent ON public.agent_task_queue USING btree (agent_id, status);
CREATE INDEX idx_agent_task_queue_chat_pending_v2 ON public.agent_task_queue USING btree (chat_session_id, created_at DESC) WHERE ((chat_session_id IS NOT NULL) AND (status = ANY (ARRAY['queued'::text, 'dispatched'::text, 'running'::text, 'waiting_local_directory'::text])));
CREATE INDEX idx_agent_task_queue_claim_candidates ON public.agent_task_queue USING btree (runtime_id, priority DESC, created_at) WHERE (status = 'queued'::text);
CREATE INDEX idx_agent_task_queue_deferred_fire ON public.agent_task_queue USING btree (runtime_id, fire_at) WHERE (status = 'deferred'::text);
CREATE INDEX idx_agent_task_queue_dispatched_prepare ON public.agent_task_queue USING btree (runtime_id, priority DESC, dispatched_at) WHERE ((status = 'dispatched'::text) AND (started_at IS NULL));
CREATE INDEX idx_agent_task_queue_escalation_for ON public.agent_task_queue USING btree (escalation_for_task_id) WHERE (escalation_for_task_id IS NOT NULL);
CREATE INDEX idx_agent_task_queue_issue_id ON public.agent_task_queue USING btree (issue_id);
CREATE INDEX idx_agent_task_queue_parent ON public.agent_task_queue USING btree (parent_task_id);
CREATE INDEX idx_agent_task_queue_pending ON public.agent_task_queue USING btree (agent_id, priority DESC, created_at) WHERE (status = ANY (ARRAY['queued'::text, 'dispatched'::text]));
CREATE INDEX idx_agent_task_queue_queued_created_at ON public.agent_task_queue USING btree (created_at) WHERE (status = 'queued'::text);
CREATE INDEX idx_agent_task_queue_running_started_at ON public.agent_task_queue USING btree (started_at) WHERE (status = 'running'::text);
CREATE INDEX idx_agent_task_queue_runtime_pending ON public.agent_task_queue USING btree (runtime_id, priority DESC, created_at) WHERE (status = ANY (ARRAY['queued'::text, 'dispatched'::text]));
CREATE INDEX idx_agent_task_queue_terminal_completed_at ON public.agent_task_queue USING btree (completed_at) WHERE (status = ANY (ARRAY['completed'::text, 'failed'::text]));
CREATE INDEX idx_agent_workspace ON public.agent USING btree (workspace_id);
CREATE INDEX idx_attachment_chat_message ON public.attachment USING btree (chat_message_id) WHERE (chat_message_id IS NOT NULL);
CREATE INDEX idx_attachment_chat_session ON public.attachment USING btree (chat_session_id) WHERE (chat_session_id IS NOT NULL);
CREATE INDEX idx_attachment_comment ON public.attachment USING btree (comment_id) WHERE (comment_id IS NOT NULL);
CREATE INDEX idx_attachment_issue ON public.attachment USING btree (issue_id) WHERE (issue_id IS NOT NULL);
CREATE INDEX idx_attachment_task ON public.attachment USING btree (task_id) WHERE (task_id IS NOT NULL);
CREATE INDEX idx_attachment_tombstone_deleted_at ON public.attachment_tombstone USING btree (deleted_at);
CREATE INDEX idx_attachment_workspace ON public.attachment USING btree (workspace_id);
CREATE INDEX idx_auth_audit_action_created_at ON public.auth_audit USING btree (action, created_at DESC, id DESC);
CREATE INDEX idx_auth_audit_created_at ON public.auth_audit USING btree (created_at DESC, id DESC);
CREATE INDEX idx_autopilot_assignee ON public.autopilot USING btree (assignee_id);
CREATE INDEX idx_autopilot_assignee_type_id ON public.autopilot USING btree (assignee_type, assignee_id);
CREATE INDEX idx_autopilot_collaborator_user ON public.autopilot_collaborator USING btree (user_type, user_id);
CREATE INDEX idx_autopilot_project ON public.autopilot USING btree (project_id);
CREATE INDEX idx_autopilot_rule_version_active ON public.autopilot_rule_version USING btree (workspace_id, autopilot_id, created_at DESC);
CREATE INDEX idx_autopilot_run_autopilot ON public.autopilot_run USING btree (autopilot_id, created_at DESC);
CREATE INDEX idx_autopilot_run_issue ON public.autopilot_run USING btree (issue_id) WHERE (issue_id IS NOT NULL);
CREATE INDEX idx_autopilot_run_squad_id ON public.autopilot_run USING btree (squad_id) WHERE (squad_id IS NOT NULL);
CREATE INDEX idx_autopilot_run_status ON public.autopilot_run USING btree (autopilot_id, status) WHERE (status = ANY (ARRAY['issue_created'::text, 'running'::text]));
CREATE INDEX idx_autopilot_subscriber_user ON public.autopilot_subscriber USING btree (user_type, user_id);
CREATE INDEX idx_autopilot_trigger_autopilot ON public.autopilot_trigger USING btree (autopilot_id);
CREATE INDEX idx_autopilot_trigger_next_run ON public.autopilot_trigger USING btree (next_run_at) WHERE ((enabled = true) AND (kind = 'schedule'::text));
CREATE UNIQUE INDEX idx_autopilot_trigger_webhook_token ON public.autopilot_trigger USING btree (webhook_token) WHERE ((kind = 'webhook'::text) AND (webhook_token IS NOT NULL));
CREATE INDEX idx_autopilot_workspace ON public.autopilot USING btree (workspace_id);
CREATE UNIQUE INDEX idx_autopilot_workspace_external_key ON public.autopilot USING btree (workspace_id, external_key) WHERE ((external_key IS NOT NULL) AND (status <> 'archived'::text));
CREATE INDEX idx_channel_binding_token_installation ON public.channel_binding_token USING btree (installation_id, expires_at);
CREATE INDEX idx_channel_chat_session_binding_session ON public.channel_chat_session_binding USING btree (chat_session_id);
CREATE INDEX idx_channel_inbound_audit_installation ON public.channel_inbound_audit USING btree (installation_id, received_at DESC);
CREATE INDEX idx_channel_inbound_audit_reason ON public.channel_inbound_audit USING btree (drop_reason, received_at DESC);
CREATE INDEX idx_channel_inbound_dedup_received ON public.channel_inbound_message_dedup USING btree (received_at);
CREATE INDEX idx_channel_installation_agent ON public.channel_installation USING btree (agent_id);
CREATE INDEX idx_channel_installation_lease ON public.channel_installation USING btree (ws_lease_expires_at) WHERE (status = 'active'::text);
CREATE UNIQUE INDEX idx_channel_installation_type_appid ON public.channel_installation USING btree (channel_type, ((config ->> 'app_id'::text)));
CREATE INDEX idx_channel_installation_workspace ON public.channel_installation USING btree (workspace_id);
CREATE INDEX idx_channel_media_pending_object_claim ON public.channel_media_pending_object USING btree (state, next_attempt_at);
CREATE INDEX idx_channel_media_pending_object_due ON public.channel_media_pending_object USING btree (next_attempt_at);
CREATE INDEX idx_channel_outbound_card_session ON public.channel_outbound_card_message USING btree (chat_session_id, created_at DESC);
CREATE UNIQUE INDEX idx_channel_outbound_card_task ON public.channel_outbound_card_message USING btree (task_id) WHERE (task_id IS NOT NULL);
CREATE INDEX idx_channel_user_binding_user ON public.channel_user_binding USING btree (goosar_user_id, workspace_id);
CREATE INDEX idx_channel_user_binding_workspace_user ON public.channel_user_binding USING btree (workspace_id, channel_user_id);
CREATE INDEX idx_chat_draft_restore_session ON public.chat_draft_restore USING btree (chat_session_id);
CREATE INDEX idx_chat_message_input_owner ON public.chat_message USING btree (task_id, created_at) WHERE (role = 'user'::text);
CREATE INDEX idx_chat_message_session ON public.chat_message USING btree (chat_session_id, created_at);
CREATE INDEX idx_chat_pinned_agent_user_ws ON public.chat_pinned_agent USING btree (workspace_id, user_id, "position");
CREATE INDEX idx_chat_session_creator ON public.chat_session USING btree (creator_id, workspace_id);
CREATE INDEX idx_chat_session_pinned ON public.chat_session USING btree (creator_id, workspace_id, pinned_at DESC) WHERE (pinned_at IS NOT NULL);
CREATE INDEX idx_chat_session_project ON public.chat_session USING btree (project_id) WHERE (project_id IS NOT NULL);
CREATE INDEX idx_chat_session_workspace ON public.chat_session USING btree (workspace_id);
CREATE INDEX idx_comment_content_trgm ON public.comment USING gin (lower(content) public.gin_trgm_ops);
CREATE INDEX idx_comment_issue_keyset ON public.comment USING btree (issue_id, created_at DESC, id DESC);
CREATE INDEX idx_comment_reaction_comment_id ON public.comment_reaction USING btree (comment_id);
CREATE INDEX idx_comment_source_task ON public.comment USING btree (source_task_id) WHERE (source_task_id IS NOT NULL);
CREATE INDEX idx_comment_workspace ON public.comment USING btree (workspace_id);
CREATE INDEX idx_contact_sales_inquiry_created ON public.contact_sales_inquiry USING btree (created_at DESC);
CREATE INDEX idx_contact_sales_inquiry_email_created ON public.contact_sales_inquiry USING btree (business_email, created_at DESC);
CREATE UNIQUE INDEX idx_daemon_token_hash ON public.daemon_token USING btree (token_hash);
CREATE INDEX idx_daemon_token_workspace_daemon ON public.daemon_token USING btree (workspace_id, daemon_id);
CREATE UNIQUE INDEX idx_deployment_mcp_server_name ON public.deployment_mcp_server USING btree (name);
CREATE UNIQUE INDEX idx_export_job_active_per_workspace ON public.export_job USING btree (workspace_id) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));
CREATE INDEX idx_feedback_user_created ON public.feedback USING btree (user_id, created_at DESC);
CREATE INDEX idx_github_installation_installation_id ON public.github_installation USING btree (installation_id);
CREATE INDEX idx_github_installation_workspace ON public.github_installation USING btree (workspace_id);
CREATE INDEX idx_github_pending_check_suite_received_at ON public.github_pending_check_suite USING btree (received_at);
CREATE INDEX idx_github_pr_check_suite_aggregate ON public.github_pull_request_check_suite USING btree (pr_id, head_sha, app_id, updated_at DESC);
CREATE INDEX idx_github_pull_request_workspace ON public.github_pull_request USING btree (workspace_id);
CREATE INDEX idx_inbox_active_by_issue ON public.inbox_item USING btree (workspace_id, recipient_type, recipient_id, issue_id) WHERE (archived = false);
CREATE INDEX idx_inbox_recipient ON public.inbox_item USING btree (recipient_type, recipient_id, read);
CREATE INDEX idx_inbox_recipient_archived_created ON public.inbox_item USING btree (workspace_id, recipient_type, recipient_id, archived, created_at DESC);
CREATE INDEX idx_invitation_invitee_email ON public.workspace_invitation USING btree (invitee_email) WHERE (status = 'pending'::text);
CREATE INDEX idx_invitation_invitee_user ON public.workspace_invitation USING btree (invitee_user_id) WHERE (status = 'pending'::text);
CREATE UNIQUE INDEX idx_invitation_unique_pending ON public.workspace_invitation USING btree (workspace_id, invitee_email) WHERE (status = 'pending'::text);
CREATE INDEX idx_issue_assignee ON public.issue USING btree (assignee_type, assignee_id);
CREATE INDEX idx_issue_description_trgm ON public.issue USING gin (lower(COALESCE(description, ''::text)) public.gin_trgm_ops);
CREATE INDEX idx_issue_first_executed_at ON public.issue USING btree (workspace_id, first_executed_at) WHERE (first_executed_at IS NOT NULL);
CREATE INDEX idx_issue_metadata_gin ON public.issue USING gin (metadata jsonb_path_ops);
CREATE INDEX idx_issue_origin ON public.issue USING btree (origin_type, origin_id) WHERE (origin_type IS NOT NULL);
CREATE INDEX idx_issue_parent ON public.issue USING btree (parent_issue_id);
CREATE INDEX idx_issue_project ON public.issue USING btree (project_id);
CREATE INDEX idx_issue_properties_gin ON public.issue USING gin (properties jsonb_path_ops);
CREATE INDEX idx_issue_property_workspace ON public.issue_property USING btree (workspace_id);
CREATE UNIQUE INDEX idx_issue_property_ws_name ON public.issue_property USING btree (workspace_id, lower(name));
CREATE INDEX idx_issue_pull_request_pr ON public.issue_pull_request USING btree (pull_request_id);
CREATE INDEX idx_issue_reaction_issue_id ON public.issue_reaction USING btree (issue_id);
CREATE INDEX idx_issue_status ON public.issue USING btree (workspace_id, status);
CREATE INDEX idx_issue_subscriber_user ON public.issue_subscriber USING btree (user_type, user_id);
CREATE INDEX idx_issue_title_trgm ON public.issue USING gin (lower(title) public.gin_trgm_ops);
CREATE INDEX idx_issue_vcs_pull_request_pr ON public.issue_vcs_pull_request USING btree (pull_request_id);
CREATE INDEX idx_issue_workspace ON public.issue USING btree (workspace_id);
CREATE INDEX idx_issue_workspace_assignee ON public.issue USING btree (workspace_id, assignee_type, assignee_id);
CREATE INDEX idx_issue_workspace_number ON public.issue USING btree (workspace_id, number);
CREATE INDEX idx_issue_workspace_parent ON public.issue USING btree (workspace_id, parent_issue_id);
CREATE INDEX idx_issue_workspace_position ON public.issue USING btree (workspace_id, "position", created_at DESC, id DESC);
CREATE INDEX idx_member_user_workspace ON public.member USING btree (user_id, workspace_id);
CREATE INDEX idx_member_workspace ON public.member USING btree (workspace_id);
CREATE UNIQUE INDEX idx_one_pending_task_per_issue_agent ON public.agent_task_queue USING btree (issue_id, agent_id) WHERE (status = ANY (ARRAY['queued'::text, 'dispatched'::text]));
CREATE UNIQUE INDEX idx_pat_token_hash ON public.personal_access_token USING btree (token_hash);
CREATE INDEX idx_pat_user ON public.personal_access_token USING btree (user_id, revoked);
CREATE INDEX idx_pinned_item_user_ws ON public.pinned_item USING btree (workspace_id, user_id, "position");
CREATE INDEX idx_project_description_trgm ON public.project USING gin (lower(COALESCE(description, ''::text)) public.gin_trgm_ops);
CREATE INDEX idx_project_resource_project ON public.project_resource USING btree (project_id, "position");
CREATE INDEX idx_project_resource_workspace ON public.project_resource USING btree (workspace_id);
CREATE INDEX idx_project_title_trgm ON public.project USING gin (lower(title) public.gin_trgm_ops);
CREATE INDEX idx_project_workspace ON public.project USING btree (workspace_id);
CREATE INDEX idx_provisioning_delivered_package_workspace ON public.provisioning_delivered_package USING btree (workspace_id);
CREATE INDEX idx_provisioning_pin_workspace ON public.provisioning_pin USING btree (workspace_id);
CREATE INDEX idx_runtime_profile_workspace ON public.runtime_profile USING btree (workspace_id);
CREATE INDEX idx_skill_file_skill ON public.skill_file USING btree (skill_id);
CREATE INDEX idx_skill_workspace ON public.skill USING btree (workspace_id);
CREATE INDEX idx_squad_member_entity ON public.squad_member USING btree (member_type, member_id);
CREATE INDEX idx_squad_member_squad ON public.squad_member USING btree (squad_id);
CREATE INDEX idx_squad_workspace ON public.squad USING btree (workspace_id);
CREATE INDEX idx_sys_cron_exec_failed_recent ON public.sys_cron_executions USING btree (job_name, plan_time DESC) WHERE (status = 'FAILED'::text);
CREATE INDEX idx_sys_cron_exec_finished ON public.sys_cron_executions USING btree (finished_at) WHERE (status = ANY (ARRAY['SUCCESS'::text, 'FAILED'::text]));
CREATE INDEX idx_sys_cron_exec_job_plan ON public.sys_cron_executions USING btree (job_name, scope_kind, scope_id, plan_time DESC);
CREATE INDEX idx_sys_cron_exec_running_stale ON public.sys_cron_executions USING btree (stale_after) WHERE (status = 'RUNNING'::text);
CREATE INDEX idx_task_chat_finalize_deferred ON public.agent_task_queue USING btree (chat_finalize_deferred_at) WHERE (chat_finalize_deferred_at IS NOT NULL);
CREATE INDEX idx_task_message_task_id_seq ON public.task_message USING btree (task_id, seq);
CREATE UNIQUE INDEX idx_task_token_hash ON public.task_token USING btree (token_hash);
CREATE INDEX idx_task_token_task ON public.task_token USING btree (task_id);
CREATE INDEX idx_task_usage_created_at ON public.task_usage USING btree (created_at);
CREATE INDEX idx_task_usage_created_at_legacy ON public.task_usage USING btree (created_at) WHERE (updated_at IS NULL);
CREATE INDEX idx_task_usage_hourly_dirty_enqueued_at ON public.task_usage_hourly_dirty USING btree (enqueued_at);
CREATE INDEX idx_task_usage_hourly_runtime_time ON public.task_usage_hourly USING btree (runtime_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_agent_time ON public.task_usage_hourly USING btree (workspace_id, agent_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_hourly_workspace_project_time ON public.task_usage_hourly USING btree (workspace_id, project_id, bucket_hour DESC) WHERE (project_id IS NOT NULL);
CREATE INDEX idx_task_usage_hourly_workspace_time ON public.task_usage_hourly USING btree (workspace_id, bucket_hour DESC);
CREATE INDEX idx_task_usage_task_id ON public.task_usage USING btree (task_id);
CREATE INDEX idx_task_usage_updated_at ON public.task_usage USING btree (updated_at);
CREATE INDEX idx_user_created_at ON public."user" USING btree (created_at);
CREATE UNIQUE INDEX idx_user_identity_provider_subject ON public.user_identity USING btree (provider, subject);
CREATE INDEX idx_user_identity_user_id ON public.user_identity USING btree (user_id);
CREATE INDEX idx_user_mfa_recovery_code_user ON public.user_mfa_recovery_code USING btree (user_id);
CREATE INDEX idx_user_session_user_created ON public.user_session USING btree (user_id, created_at DESC);
CREATE INDEX idx_vcs_commit_status_lookup ON public.vcs_commit_status USING btree (connection_id, sha);
CREATE INDEX idx_vcs_connection_workspace ON public.vcs_connection USING btree (workspace_id);
CREATE INDEX idx_vcs_pull_request_connection ON public.vcs_pull_request USING btree (connection_id);
CREATE INDEX idx_vcs_pull_request_workspace ON public.vcs_pull_request USING btree (workspace_id);
CREATE INDEX idx_verification_code_email ON public.verification_code USING btree (email, used, expires_at);
CREATE INDEX idx_verification_code_link_token_hash ON public.verification_code USING btree (link_token_hash) WHERE (link_token_hash IS NOT NULL);
CREATE INDEX idx_webhook_delivery_autopilot ON public.webhook_delivery USING btree (autopilot_id, created_at DESC);
CREATE UNIQUE INDEX idx_webhook_delivery_dedupe ON public.webhook_delivery USING btree (trigger_id, dedupe_key) WHERE ((dedupe_key IS NOT NULL) AND (status <> ALL (ARRAY['rejected'::text, 'failed'::text])));
CREATE INDEX idx_webhook_delivery_queue ON public.webhook_delivery USING btree (available_at, created_at) WHERE (status = 'queued'::text);
CREATE INDEX idx_webhook_delivery_run ON public.webhook_delivery USING btree (autopilot_run_id) WHERE (autopilot_run_id IS NOT NULL);
CREATE UNIQUE INDEX idx_workspace_mcp_server_workspace_name ON public.workspace_mcp_server USING btree (workspace_id, name);
CREATE INDEX idx_workspace_mcp_user_credential_workspace_user ON public.workspace_mcp_user_credential USING btree (workspace_id, user_id);
CREATE UNIQUE INDEX idx_workspace_template_key ON public.workspace USING btree (template_key) WHERE (template_key IS NOT NULL);
CREATE INDEX issue_label_workspace_type_idx ON public.issue_label USING btree (workspace_id, resource_type);
CREATE UNIQUE INDEX issue_label_workspace_type_name_lower_idx ON public.issue_label USING btree (workspace_id, resource_type, lower(name));
CREATE INDEX skill_to_label_label_idx ON public.skill_to_label USING btree (label_id);
CREATE UNIQUE INDEX uq_autopilot_run_trigger_planned ON public.autopilot_run USING btree (trigger_id, planned_at) WHERE ((trigger_id IS NOT NULL) AND (planned_at IS NOT NULL));
CREATE UNIQUE INDEX uq_autopilot_run_webhook_delivery ON public.autopilot_run USING btree (webhook_delivery_id) WHERE (webhook_delivery_id IS NOT NULL);
CREATE INDEX user_composio_connection_account_idx ON public.user_composio_connection USING btree (connected_account_id);
CREATE INDEX user_composio_connection_user_status_idx ON public.user_composio_connection USING btree (user_id, status);
CREATE TRIGGER attachment_tombstone_trigger AFTER DELETE ON public.attachment FOR EACH ROW EXECUTE FUNCTION public.attachment_tombstone_on_delete();
CREATE TRIGGER trg_atq_dirty_hourly BEFORE DELETE OR UPDATE OF runtime_id, issue_id ON public.agent_task_queue FOR EACH ROW EXECUTE FUNCTION public.enqueue_task_usage_hourly_dirty_for_atq();
CREATE TRIGGER trg_clear_runtime_mcp_overlay BEFORE UPDATE OF status ON public.agent_task_queue FOR EACH ROW EXECUTE FUNCTION public.clear_runtime_mcp_overlay_on_terminal_state();
CREATE TRIGGER trg_issue_delete_dirty_hourly BEFORE DELETE ON public.issue FOR EACH ROW EXECUTE FUNCTION public.enqueue_task_usage_hourly_dirty_for_issue_delete();
CREATE TRIGGER trg_issue_project_dirty_hourly BEFORE UPDATE OF project_id ON public.issue FOR EACH ROW EXECUTE FUNCTION public.enqueue_task_usage_hourly_dirty_for_issue_project();
CREATE TRIGGER trg_tu_dirty_hourly BEFORE DELETE ON public.task_usage FOR EACH ROW EXECUTE FUNCTION public.enqueue_task_usage_hourly_dirty_for_tu();
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT activity_log_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.activity_log
    ADD CONSTRAINT activity_log_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_archived_by_fkey FOREIGN KEY (archived_by) REFERENCES public."user"(id);
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public."user"(id);
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_runtime_id_fkey FOREIGN KEY (runtime_id) REFERENCES public.agent_runtime(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.agent_runtime
    ADD CONSTRAINT agent_runtime_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public."user"(id);
ALTER TABLE ONLY public.agent_runtime
    ADD CONSTRAINT agent_runtime_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_skill
    ADD CONSTRAINT agent_skill_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agent(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_skill
    ADD CONSTRAINT agent_skill_skill_id_fkey FOREIGN KEY (skill_id) REFERENCES public.skill(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agent(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_autopilot_run_id_fkey FOREIGN KEY (autopilot_run_id) REFERENCES public.autopilot_run(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_chat_session_id_fkey FOREIGN KEY (chat_session_id) REFERENCES public.chat_session(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_parent_task_id_fkey FOREIGN KEY (parent_task_id) REFERENCES public.agent_task_queue(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_runtime_id_fkey FOREIGN KEY (runtime_id) REFERENCES public.agent_runtime(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.agent_task_queue
    ADD CONSTRAINT agent_task_queue_trigger_comment_id_fkey FOREIGN KEY (trigger_comment_id) REFERENCES public.comment(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.agent
    ADD CONSTRAINT agent_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_chat_message_id_fkey FOREIGN KEY (chat_message_id) REFERENCES public.chat_message(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_chat_session_id_fkey FOREIGN KEY (chat_session_id) REFERENCES public.chat_session(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_comment_id_fkey FOREIGN KEY (comment_id) REFERENCES public.comment(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.attachment
    ADD CONSTRAINT attachment_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.autopilot
    ADD CONSTRAINT autopilot_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.project(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_autopilot_id_fkey FOREIGN KEY (autopilot_id) REFERENCES public.autopilot(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_squad_id_fkey FOREIGN KEY (squad_id) REFERENCES public.squad(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_task_queue(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.autopilot_run
    ADD CONSTRAINT autopilot_run_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES public.autopilot_trigger(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.autopilot_trigger
    ADD CONSTRAINT autopilot_trigger_autopilot_id_fkey FOREIGN KEY (autopilot_id) REFERENCES public.autopilot(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.autopilot
    ADD CONSTRAINT autopilot_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.chat_message
    ADD CONSTRAINT chat_message_chat_session_id_fkey FOREIGN KEY (chat_session_id) REFERENCES public.chat_session(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.chat_session
    ADD CONSTRAINT chat_session_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agent(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.chat_session
    ADD CONSTRAINT chat_session_creator_id_fkey FOREIGN KEY (creator_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.chat_session
    ADD CONSTRAINT chat_session_runtime_id_fkey FOREIGN KEY (runtime_id) REFERENCES public.agent_runtime(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.chat_session
    ADD CONSTRAINT chat_session_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.comment
    ADD CONSTRAINT comment_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.comment
    ADD CONSTRAINT comment_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.comment(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.comment_reaction
    ADD CONSTRAINT comment_reaction_comment_id_fkey FOREIGN KEY (comment_id) REFERENCES public.comment(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.comment_reaction
    ADD CONSTRAINT comment_reaction_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.comment
    ADD CONSTRAINT comment_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.daemon_connection
    ADD CONSTRAINT daemon_connection_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agent(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.daemon_token
    ADD CONSTRAINT daemon_token_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.feedback
    ADD CONSTRAINT feedback_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.feedback
    ADD CONSTRAINT feedback_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.github_installation
    ADD CONSTRAINT github_installation_connected_by_id_fkey FOREIGN KEY (connected_by_id) REFERENCES public."user"(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.github_installation
    ADD CONSTRAINT github_installation_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.github_pull_request_check_suite
    ADD CONSTRAINT github_pull_request_check_suite_pr_id_fkey FOREIGN KEY (pr_id) REFERENCES public.github_pull_request(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.github_pull_request
    ADD CONSTRAINT github_pull_request_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.inbox_item
    ADD CONSTRAINT inbox_item_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.inbox_item
    ADD CONSTRAINT inbox_item_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_dependency
    ADD CONSTRAINT issue_dependency_depends_on_issue_id_fkey FOREIGN KEY (depends_on_issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_dependency
    ADD CONSTRAINT issue_dependency_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_label
    ADD CONSTRAINT issue_label_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue
    ADD CONSTRAINT issue_parent_issue_id_fkey FOREIGN KEY (parent_issue_id) REFERENCES public.issue(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.issue
    ADD CONSTRAINT issue_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.project(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.issue_pull_request
    ADD CONSTRAINT issue_pull_request_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_pull_request
    ADD CONSTRAINT issue_pull_request_pull_request_id_fkey FOREIGN KEY (pull_request_id) REFERENCES public.github_pull_request(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_reaction
    ADD CONSTRAINT issue_reaction_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_reaction
    ADD CONSTRAINT issue_reaction_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_subscriber
    ADD CONSTRAINT issue_subscriber_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_to_label
    ADD CONSTRAINT issue_to_label_issue_id_fkey FOREIGN KEY (issue_id) REFERENCES public.issue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue_to_label
    ADD CONSTRAINT issue_to_label_label_id_fkey FOREIGN KEY (label_id) REFERENCES public.issue_label(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.issue
    ADD CONSTRAINT issue_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.member
    ADD CONSTRAINT member_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.member
    ADD CONSTRAINT member_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.notification_preference
    ADD CONSTRAINT notification_preference_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.notification_preference
    ADD CONSTRAINT notification_preference_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.personal_access_token
    ADD CONSTRAINT personal_access_token_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.pinned_item
    ADD CONSTRAINT pinned_item_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.pinned_item
    ADD CONSTRAINT pinned_item_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project_resource
    ADD CONSTRAINT project_resource_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.project(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project_resource
    ADD CONSTRAINT project_resource_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.project
    ADD CONSTRAINT project_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.skill
    ADD CONSTRAINT skill_created_by_fkey FOREIGN KEY (created_by) REFERENCES public."user"(id);
ALTER TABLE ONLY public.skill_file
    ADD CONSTRAINT skill_file_skill_id_fkey FOREIGN KEY (skill_id) REFERENCES public.skill(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.skill
    ADD CONSTRAINT skill_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.squad
    ADD CONSTRAINT squad_leader_id_fkey FOREIGN KEY (leader_id) REFERENCES public.agent(id) ON DELETE RESTRICT;
ALTER TABLE ONLY public.squad_member
    ADD CONSTRAINT squad_member_squad_id_fkey FOREIGN KEY (squad_id) REFERENCES public.squad(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.squad
    ADD CONSTRAINT squad_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_message
    ADD CONSTRAINT task_message_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_task_queue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_token
    ADD CONSTRAINT task_token_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agent(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_token
    ADD CONSTRAINT task_token_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_task_queue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_token
    ADD CONSTRAINT task_token_user_id_fkey FOREIGN KEY (user_id) REFERENCES public."user"(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_token
    ADD CONSTRAINT task_token_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.task_usage
    ADD CONSTRAINT task_usage_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.agent_task_queue(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_autopilot_id_fkey FOREIGN KEY (autopilot_id) REFERENCES public.autopilot(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_autopilot_run_id_fkey FOREIGN KEY (autopilot_run_id) REFERENCES public.autopilot_run(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_replayed_from_delivery_id_fkey FOREIGN KEY (replayed_from_delivery_id) REFERENCES public.webhook_delivery(id) ON DELETE SET NULL;
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES public.autopilot_trigger(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.webhook_delivery
    ADD CONSTRAINT webhook_delivery_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
ALTER TABLE ONLY public.workspace_invitation
    ADD CONSTRAINT workspace_invitation_invitee_user_id_fkey FOREIGN KEY (invitee_user_id) REFERENCES public."user"(id);
ALTER TABLE ONLY public.workspace_invitation
    ADD CONSTRAINT workspace_invitation_inviter_id_fkey FOREIGN KEY (inviter_id) REFERENCES public."user"(id);
ALTER TABLE ONLY public.workspace_invitation
    ADD CONSTRAINT workspace_invitation_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES public.workspace(id) ON DELETE CASCADE;
