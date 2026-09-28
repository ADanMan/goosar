-- 321_export_active_job: T-029, POST /api/workspaces/{id}/export — contract:
-- "Только один активный экспорт на пространство одновременно (409 при
-- попытке запустить второй)". Partial unique index instead of an application
-- lock, so the check-and-insert stays a single atomic statement.
CREATE UNIQUE INDEX space_export_jobs_one_active_uk
    ON space_export_jobs (workspace_id)
    WHERE exp_status IN ('pending', 'running');
