-- Откатывает 001_views.up.sql (см. комментарий там) — ручная очистка,
-- обычный раннер сервера её не вызывает.
DROP TRIGGER IF EXISTS e2e_compat_issue_insert_trg ON issue;
DROP FUNCTION IF EXISTS e2e_compat_issue_insert();
DROP VIEW IF EXISTS issue;
DROP VIEW IF EXISTS workspace;
DROP VIEW IF EXISTS "user";
DROP VIEW IF EXISTS verification_code;
