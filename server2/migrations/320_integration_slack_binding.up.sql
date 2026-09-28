-- 320_integration_slack_binding: T-029, POST /api/slack/binding/redeem.
-- Contract documents only the redeem operation; the token itself is minted by
-- the Slack bot's slash command, a component outside this contract (spec gap,
-- see server2/docs/decisions.md, T-029). sbt_token_hash stores sha256(token),
-- never the token itself, so a leaked row cannot be replayed.

CREATE TABLE slack_binding_tokens (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sbt_installation_id uuid NOT NULL REFERENCES slack_installations(id) ON DELETE CASCADE,
    sbt_workspace_id    uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    sbt_slack_user_id   text NOT NULL,
    sbt_token_hash      text NOT NULL,
    sbt_status          text NOT NULL DEFAULT 'pending' CHECK (sbt_status IN ('pending','redeemed')),
    sbt_expires_at      timestamptz NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX slack_binding_tokens_hash_uk ON slack_binding_tokens (sbt_token_hash);

-- slack_account_bindings: which Goosar account a given (installation, Slack
-- user) pair has redeemed a binding token into (contract: "если Slack-аккаунт
-- уже привязан к другому пользователю — 409").
CREATE TABLE slack_account_bindings (
    sab_installation_id uuid NOT NULL REFERENCES slack_installations(id) ON DELETE CASCADE,
    sab_slack_user_id   text NOT NULL,
    account_id          uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (sab_installation_id, sab_slack_user_id)
);
