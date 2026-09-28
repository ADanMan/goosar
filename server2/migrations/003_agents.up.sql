-- 003_agents: execution runtimes (executors), agents (operatives) and skills
-- (capabilities). Ordered before tasks/crews because tickets and crews assign
-- work to operatives.

-- executors: schemas.Runtime (== schemas.AgentRuntime, same underlying entity
-- described twice in the contract: a daemon-registered execution slot).
CREATE TABLE executors (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    ex_daemon_id       text,
    ex_title           text NOT NULL,
    ex_custom_title    text,
    ex_mode            text NOT NULL,
    ex_provider        text NOT NULL,
    ex_launch_header   text,
    ex_status          text NOT NULL DEFAULT 'offline' CHECK (ex_status IN ('online','offline')),
    ex_device_info     text NOT NULL DEFAULT '',
    ex_metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
    ex_owner_account_id uuid REFERENCES accounts(id),
    ex_visibility      text NOT NULL DEFAULT 'private' CHECK (ex_visibility IN ('private','public')),
    ex_protocol_id     uuid REFERENCES agent_protocols(id),
    ex_last_seen_at    timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX executors_workspace_ix ON executors (workspace_id);
CREATE INDEX executors_daemon_ix ON executors (ex_daemon_id) WHERE ex_daemon_id IS NOT NULL;

-- executor_probes: consolidates the daemon self-update / model-list / local-skills /
-- local-skill-import request-response cycles (schemas.RuntimeUpdateRequest,
-- RuntimeModelListRequest, RuntimeLocalSkillListRequest, RuntimeLocalSkillImportRequest)
-- into one polymorphic-by-kind table instead of four near-identical ones.
CREATE TABLE executor_probes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    executor_id    uuid NOT NULL REFERENCES executors(id) ON DELETE CASCADE,
    probe_kind     text NOT NULL CHECK (probe_kind IN ('self_update','model_list','local_skills','local_skill_import')),
    probe_status   text NOT NULL DEFAULT 'pending'
                   CHECK (probe_status IN ('pending','running','completed','failed','timeout')),
    probe_request  jsonb NOT NULL DEFAULT '{}'::jsonb,
    probe_outcome   jsonb,
    probe_error    text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX executor_probes_pending_ix ON executor_probes (executor_id, probe_kind) WHERE probe_status = 'pending';

-- capabilities: schemas.Skill / SkillWithFiles / SkillSummary
CREATE TABLE capabilities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    cap_title        text NOT NULL,
    cap_summary      text,
    cap_config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    cap_body_md      text NOT NULL DEFAULT '',
    cap_created_by   uuid REFERENCES accounts(id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX capabilities_workspace_ix ON capabilities (workspace_id);
CREATE INDEX capabilities_title_trgm_ix ON capabilities USING gin (cap_title gin_trgm_ops);

-- capability_files: schemas.SkillFile
CREATE TABLE capability_files (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    capability_id   uuid NOT NULL REFERENCES capabilities(id) ON DELETE CASCADE,
    capf_path       text NOT NULL,
    capf_body       text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT capability_files_uk UNIQUE (capability_id, capf_path)
);

-- operatives: schemas.Agent
CREATE TABLE operatives (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    executor_id              uuid NOT NULL REFERENCES executors(id),
    op_title                 text NOT NULL,
    op_summary               text,
    op_instructions          text NOT NULL DEFAULT '',
    op_avatar_uri            text,
    op_runtime_mode          text NOT NULL CHECK (op_runtime_mode IN ('local','cloud')),
    op_runtime_config_sealed bytea,
    op_custom_args           jsonb NOT NULL DEFAULT '[]'::jsonb,
    op_mcp_config_sealed     bytea,
    op_mcp_config_encrypted  boolean NOT NULL DEFAULT true,
    op_custom_env_sealed     bytea,
    op_permission_mode       text NOT NULL DEFAULT 'private' CHECK (op_permission_mode IN ('private','public_to')),
    op_status                text NOT NULL DEFAULT 'idle'
                             CHECK (op_status IN ('idle','working','blocked','error','offline')),
    op_max_concurrent_tasks  integer NOT NULL DEFAULT 6,
    op_model                 text,
    op_thinking_level        text,
    op_service_tier          text,
    op_composio_allowlist    jsonb,
    op_owner_account_id      uuid REFERENCES accounts(id),
    op_kind                  text NOT NULL DEFAULT 'user' CHECK (op_kind IN ('user','system')),
    op_system_key            text,
    op_archived_at           timestamptz,
    op_archived_by           uuid REFERENCES accounts(id),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operatives_workspace_ix ON operatives (workspace_id) WHERE op_archived_at IS NULL;
CREATE UNIQUE INDEX operatives_system_key_uk ON operatives (workspace_id, op_system_key) WHERE op_system_key IS NOT NULL;

-- operative_targets: schemas.AgentInvocationTarget (permission_mode=public_to)
CREATE TABLE operative_targets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operative_id   uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    opt_target_type text NOT NULL CHECK (opt_target_type IN ('workspace','member','team')),
    opt_target_id  uuid,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX operative_targets_operative_ix ON operative_targets (operative_id);

-- operative_mcp_links: per-agent enable/disable of a workspace MCP server
-- (old schema's agent_mcp_server join, renamed).
CREATE TABLE operative_mcp_links (
    operative_id         uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    space_mcp_server_id  uuid NOT NULL REFERENCES space_mcp_servers(id) ON DELETE CASCADE,
    opml_enabled         boolean NOT NULL DEFAULT true,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operative_id, space_mcp_server_id)
);

-- operative_capabilities: agent <-> skill assignment (AgentSkillSummary), with a
-- per-agent enable flag and optional config override.
CREATE TABLE operative_capabilities (
    operative_id          uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    capability_id         uuid NOT NULL REFERENCES capabilities(id) ON DELETE CASCADE,
    opcap_enabled         boolean NOT NULL DEFAULT true,
    opcap_config_override jsonb,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operative_id, capability_id)
);

-- operative_disabled_local_skills: schemas.DisabledRuntimeSkill
CREATE TABLE operative_disabled_local_skills (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operative_id  uuid NOT NULL REFERENCES operatives(id) ON DELETE CASCADE,
    executor_id   uuid NOT NULL REFERENCES executors(id) ON DELETE CASCADE,
    opdis_provider text NOT NULL CHECK (opdis_provider IN ('runtime-c','runtime-e')),
    opdis_root    text NOT NULL CHECK (opdis_root IN ('provider','universal','plugin')),
    opdis_key     text NOT NULL,
    opdis_title   text,
    opdis_plugin  text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT operative_disabled_local_skills_uk UNIQUE (operative_id, executor_id, opdis_root, opdis_key)
);
