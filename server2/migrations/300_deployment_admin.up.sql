-- 300_deployment_admin: T-029 gap-fill for server2/internal/deployment (see
-- server2/docs/decisions.md, раздел T-029). 011_governance (T-025) уже завело
-- platform_admins/platform_admin_requests/platform_audit_log/
-- platform_mcp_servers/platform_policy/provisioning_pins — этой миграции
-- нужны только две узкие добавки, которых не хватает контракту деплоя:
--
-- 1. acct_deactivated_at/acct_anonymized_at — deactivateDeploymentUser/
--    deleteDeploymentUser (docs/50-api-contract.md §7) требуют состояние
--    учётной записи, которого 001_identity не заводит (там только сессии/
--    токены, не флаг самого аккаунта).
-- 2. ws_template_key — setDeploymentWorkspaceOpenJoin/listJoinTargets и CLI
--    `provision-roles` должны отличать "пространство создано из шаблона
--    роли" от обычного — 002_workspace.up.sql заводит только ws_open_join
--    (флаг уже есть), но не сам факт происхождения из шаблона.

ALTER TABLE accounts ADD COLUMN acct_deactivated_at timestamptz;
ALTER TABLE accounts ADD COLUMN acct_anonymized_at timestamptz;

ALTER TABLE spaces ADD COLUMN ws_template_key text;
CREATE INDEX spaces_template_key_ix ON spaces (ws_template_key) WHERE ws_template_key IS NOT NULL;
