-- 402_task_pr_link_fields: docs/50-api-contract-changes.md, п.3 —
-- IssuePullRequestLink расширена с 6 до полного набора полей карточки PR
-- (совпадает с GitHub App / self-hosted VCS формой). Часть полей (checks_*,
-- snapshot_*, additions/deletions/changed_files, mergeable*) требует
-- полноценной обработки check_suite/check_run вебхуков и вызова API
-- провайдера за diff-статистикой — это уже задокументированный пробел
-- (см. internal/integration/webhooks.go, комментарий у "check_suite",
-- server2/docs/decisions.md T-029). Эта миграция только добавляет место для
-- них (nullable/со значением по умолчанию), проставляются пока лишь поля,
-- которые уже есть в теле вебхука pull_request/merge_request.
-- workspace_id не хранится отдельной колонкой: он тот же, что у
-- ticket_pr_links.ticket_id -> tickets.workspace_id (JOIN при чтении) —
-- так проще, чем держать денормализованную копию, и не требует правки
-- internal/importer (тот пишет в эту таблицу по старой схеме источника).
ALTER TABLE ticket_pr_links
    ADD COLUMN tpr_repo_owner     text,
    ADD COLUMN tpr_repo_name      text,
    ADD COLUMN tpr_branch         text,
    ADD COLUMN tpr_author_login   text,
    ADD COLUMN tpr_author_avatar_url text,
    ADD COLUMN tpr_merged_at      timestamptz,
    ADD COLUMN tpr_closed_at      timestamptz,
    ADD COLUMN tpr_pr_created_at  timestamptz,
    ADD COLUMN tpr_pr_updated_at  timestamptz,
    ADD COLUMN tpr_mergeable_state text,
    ADD COLUMN tpr_mergeable      text,
    ADD COLUMN tpr_merge_state_status text,
    ADD COLUMN tpr_snapshot_available boolean NOT NULL DEFAULT false,
    ADD COLUMN tpr_checks_rollup  text,
    ADD COLUMN tpr_checks_conclusion text,
    ADD COLUMN tpr_checks_total   integer,
    ADD COLUMN tpr_checks_passed  integer,
    ADD COLUMN tpr_checks_failed  integer,
    ADD COLUMN tpr_checks_running integer,
    ADD COLUMN tpr_checks_pending integer,
    ADD COLUMN tpr_failed_check_names text[],
    ADD COLUMN tpr_snapshot_stale boolean,
    ADD COLUMN tpr_snapshot_fetched_at timestamptz,
    ADD COLUMN tpr_additions     integer,
    ADD COLUMN tpr_deletions     integer,
    ADD COLUMN tpr_changed_files integer;
