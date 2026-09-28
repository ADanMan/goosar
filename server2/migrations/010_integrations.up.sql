-- 010_integrations: source-control and chat-platform connections, and Composio
-- tool connections. Catalog-only schemas (GitHubRepository, ComposioToolkit) are
-- live API passthroughs and are not persisted -- see docs/51-data-model.md.

-- vcs_connections: schemas.VcsConnection (generic GitLab/Bitbucket/etc, distinct
-- from the native GitHub App flow below)
CREATE TABLE vcs_connections (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    vcs_provider             text NOT NULL,
    vcs_instance_uri         text NOT NULL,
    vcs_account_login        text,
    vcs_webhook_uri          text,
    vcs_webhook_path         text,
    vcs_access_token_sealed  bytea NOT NULL,
    vcs_webhook_secret_sealed bytea,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vcs_connections_workspace_ix ON vcs_connections (workspace_id);

-- github_installations: schemas.GitHubInstallation
CREATE TABLE github_installations (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id           uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    gh_installation_id     bigint,
    gh_account_login       text NOT NULL,
    gh_account_type        text NOT NULL,
    gh_account_avatar_uri  text,
    created_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX github_installations_workspace_ix ON github_installations (workspace_id);

-- slack_installations: schemas.SlackInstallation
CREATE TABLE slack_installations (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    operative_id             uuid NOT NULL REFERENCES operatives(id),
    sl_team_id               text NOT NULL,
    sl_bot_user_id           text,
    sl_bot_token_sealed      bytea,
    sl_app_token_sealed      bytea,
    sl_installer_account_id  uuid NOT NULL REFERENCES accounts(id),
    sl_status                text NOT NULL DEFAULT 'active',
    sl_installed_at          timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX slack_installations_workspace_ix ON slack_installations (workspace_id);

-- composio_connections: schemas.ComposioConnection, scoped per member per space
CREATE TABLE composio_connections (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id                uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    account_id                  uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    cx_toolkit_slug             text NOT NULL,
    cx_external_connection_id   text,
    cx_status                   text NOT NULL DEFAULT 'pending',
    cx_connected_at             timestamptz,
    cx_last_used_at             timestamptz,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT composio_connections_uk UNIQUE (workspace_id, account_id, cx_toolkit_slug)
);
