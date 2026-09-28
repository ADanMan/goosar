-- 002_workspace: spaces (workspaces), memberships, invitations, per-space config
-- layers, per-space MCP server registry, export jobs and custom agent protocols.

-- spaces: schemas.Workspace
CREATE TABLE spaces (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ws_title             text NOT NULL,
    ws_slug              text NOT NULL,
    ws_summary           text,
    ws_operating_context text,
    ws_settings          jsonb NOT NULL DEFAULT '{}'::jsonb,
    ws_repo_refs         jsonb NOT NULL DEFAULT '[]'::jsonb,
    ws_ticket_prefix     text NOT NULL,
    ws_next_ticket_seq   bigint NOT NULL DEFAULT 0,
    ws_avatar_uri        text,
    ws_open_join         boolean NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX spaces_slug_uk ON spaces (ws_slug);

-- space_members: schemas.Member / WorkspaceMemberWithUser
CREATE TABLE space_members (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id           uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    sm_role              text NOT NULL CHECK (sm_role IN ('owner','admin','member')),
    sm_perimeter_access  boolean NOT NULL DEFAULT false,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT space_members_uk UNIQUE (workspace_id, account_id)
);
CREATE INDEX space_members_account_ix ON space_members (account_id);

-- space_invitations: schemas.WorkspaceInvitation
CREATE TABLE space_invitations (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    inv_inviter_account_id   uuid NOT NULL REFERENCES accounts(id),
    inv_invitee_email        text NOT NULL,
    inv_invitee_account_id   uuid REFERENCES accounts(id),
    inv_role                 text NOT NULL CHECK (inv_role IN ('owner','admin','member')),
    inv_status               text NOT NULL DEFAULT 'pending'
                             CHECK (inv_status IN ('pending','accepted','declined','revoked','expired')),
    inv_valid_until          timestamptz NOT NULL,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX space_invitations_email_ix ON space_invitations (lower(inv_invitee_email));
CREATE INDEX space_invitations_workspace_ix ON space_invitations (workspace_id, inv_status);

-- space_export_jobs: schemas.WorkspaceExportJob
CREATE TABLE space_export_jobs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    exp_status        text NOT NULL DEFAULT 'pending'
                       CHECK (exp_status IN ('pending','running','completed','failed')),
    exp_error         text,
    exp_size_bytes    bigint NOT NULL DEFAULT 0,
    exp_manifest      jsonb,
    exp_storage_uri   text,
    exp_completed_at  timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX space_export_jobs_workspace_ix ON space_export_jobs (workspace_id, created_at DESC);

-- space_config: schemas.WorkspaceConfigLayer (one row per workspace)
CREATE TABLE space_config (
    workspace_id           uuid PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
    cfg_llm_base_url       text,
    cfg_llm_model          text,
    cfg_llm_api_key_sealed bytea,
    cfg_mcp_defaults       jsonb NOT NULL DEFAULT '{}'::jsonb,
    cfg_updated_by         uuid REFERENCES accounts(id),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- space_config_overrides: schemas.WorkspaceUserConfigOverride (one row per member)
CREATE TABLE space_config_overrides (
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id               uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    cfgo_llm_base_url        text,
    cfgo_llm_model           text,
    cfgo_llm_api_key_sealed  bytea,
    cfgo_mcp_overrides       jsonb NOT NULL DEFAULT '{}'::jsonb,
    cfgo_updated_by          uuid REFERENCES accounts(id),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, account_id)
);

-- space_mcp_servers: schemas.WorkspaceMcpServer
CREATE TABLE space_mcp_servers (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    wmcp_name                text NOT NULL,
    wmcp_transport           text NOT NULL DEFAULT 'unknown' CHECK (wmcp_transport IN ('stdio','http','unknown')),
    wmcp_source              text NOT NULL DEFAULT 'workspace' CHECK (wmcp_source IN ('workspace','deployment')),
    wmcp_platform_server_id  uuid, -- soft reference to platform_mcp_servers(id), added in 011_governance
    wmcp_config_sealed       bytea,
    wmcp_credential_schema   jsonb NOT NULL DEFAULT '[]'::jsonb,
    wmcp_enabled             boolean NOT NULL DEFAULT true,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT space_mcp_servers_uk UNIQUE (workspace_id, wmcp_name)
);

-- space_mcp_credentials: per-member secret values for a workspace MCP server's
-- credential schema fields (schemas.SetWorkspaceMcpCredentialsRequest).
CREATE TABLE space_mcp_credentials (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    space_mcp_server_id   uuid NOT NULL REFERENCES space_mcp_servers(id) ON DELETE CASCADE,
    account_id            uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    wmcpc_field_key       text NOT NULL,
    wmcpc_value_sealed    bytea NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT space_mcp_credentials_uk UNIQUE (space_mcp_server_id, account_id, wmcpc_field_key)
);

-- agent_protocols: schemas.RuntimeProfile (custom daemon launch commands declared
-- per workspace; distinct from a live Runtime/executor instance).
CREATE TABLE agent_protocols (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    proto_display_title   text NOT NULL,
    proto_family          text NOT NULL,
    proto_command         text NOT NULL,
    proto_summary         text,
    proto_fixed_args      jsonb NOT NULL DEFAULT '[]'::jsonb,
    proto_visibility      text NOT NULL DEFAULT 'workspace',
    proto_created_by      uuid REFERENCES accounts(id),
    proto_enabled         boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_protocols_workspace_ix ON agent_protocols (workspace_id) WHERE proto_enabled;
