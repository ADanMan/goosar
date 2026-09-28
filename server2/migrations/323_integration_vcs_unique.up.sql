-- 323_integration_vcs_unique: T-029, POST /api/workspaces/{id}/vcs/connections
-- contract: "Подключено (или обновлено — upsert по паре provider+instance_url)".
-- 010_integrations.up.sql creates vcs_connections without a unique constraint
-- on (workspace_id, vcs_provider, vcs_instance_uri), so the upsert's
-- ON CONFLICT target needs this index to exist.
CREATE UNIQUE INDEX vcs_connections_provider_instance_uk
    ON vcs_connections (workspace_id, vcs_provider, vcs_instance_uri);
