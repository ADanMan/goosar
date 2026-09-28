-- 011_governance: deployment-wide (not per-workspace) administration: platform
-- admins, the admin/auth audit trail, the deployment MCP server registry, the
-- deployment policy document, and per-workspace provisioning pins.

-- platform_admins: schemas.DeploymentAdmin
CREATE TABLE platform_admins (
    account_id    uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    pa_granted_by uuid NOT NULL REFERENCES accounts(id),
    pa_granted_at timestamptz NOT NULL DEFAULT now()
);

-- platform_admin_requests: schemas.DeploymentAdminPendingRequest (grant/revoke
-- requests awaiting a second admin's confirmation)
CREATE TABLE platform_admin_requests (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    par_action             text NOT NULL CHECK (par_action IN ('grant','revoke')),
    par_target_account_id  uuid REFERENCES accounts(id),
    par_target_email       text,
    par_requested_by       uuid NOT NULL REFERENCES accounts(id),
    par_status             text NOT NULL DEFAULT 'pending' CHECK (par_status IN ('pending','confirmed','rejected','expired')),
    par_requested_at       timestamptz NOT NULL DEFAULT now(),
    par_resolved_at        timestamptz
);

-- platform_audit_log: schemas.DeploymentAuditEntry (source admin|auth); merges
-- old schema's admin_audit and auth_audit into one table distinguished by paud_source.
CREATE TABLE platform_audit_log (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    paud_source            text NOT NULL CHECK (paud_source IN ('admin','auth')),
    paud_action            text NOT NULL,
    paud_actor_account_id  uuid REFERENCES accounts(id),
    paud_actor_type        text,
    paud_actor_id          text,
    paud_actor_role        text,
    paud_target_type       text,
    paud_target_id         text,
    paud_outcome           text,
    paud_reason            text,
    paud_before_hash       text,
    paud_after_hash        text,
    workspace_id           uuid REFERENCES spaces(id) ON DELETE SET NULL,
    paud_request_id        text,
    paud_client_ip_digest  text,
    paud_client_agent      text,
    created_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX platform_audit_log_created_ix ON platform_audit_log (created_at DESC);
CREATE INDEX platform_audit_log_workspace_ix ON platform_audit_log (workspace_id) WHERE workspace_id IS NOT NULL;

-- platform_mcp_servers: schemas.DeploymentMcpServer (deployment-wide MCP catalog
-- that workspaces can enable into space_mcp_servers)
CREATE TABLE platform_mcp_servers (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pmcp_name              text NOT NULL,
    pmcp_transport         text NOT NULL DEFAULT 'unknown' CHECK (pmcp_transport IN ('stdio','http','unknown')),
    pmcp_config_sealed     bytea,
    pmcp_credential_schema jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT platform_mcp_servers_name_uk UNIQUE (pmcp_name)
);

ALTER TABLE space_mcp_servers
    ADD CONSTRAINT space_mcp_servers_platform_fk
    FOREIGN KEY (wmcp_platform_server_id) REFERENCES platform_mcp_servers(id) ON DELETE SET NULL;

-- platform_policy: schemas.DeploymentPolicyDocument (one document for the whole
-- deployment: llm/mcp/session locks)
CREATE TABLE platform_policy (
    id            smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    pp_body       jsonb NOT NULL DEFAULT '{}'::jsonb,
    pp_updated_at timestamptz
);

-- provisioning_pins: schemas.ProvisioningPin (per-workspace package version pins)
CREATE TABLE provisioning_pins (
    workspace_id       uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    prov_package_name  text NOT NULL,
    prov_package_type  text NOT NULL CHECK (prov_package_type IN ('skill','mcp-server','runtime')),
    prov_version       text NOT NULL,
    prov_enabled       boolean NOT NULL DEFAULT true,
    prov_updated_by    uuid REFERENCES accounts(id),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, prov_package_name, prov_package_type)
);
