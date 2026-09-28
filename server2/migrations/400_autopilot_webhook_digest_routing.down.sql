DROP INDEX IF EXISTS sentinel_triggers_webhook_token_digest_uk;
ALTER TABLE sentinel_triggers ADD COLUMN strig_webhook_path text;
CREATE UNIQUE INDEX sentinel_triggers_webhook_path_uk ON sentinel_triggers (strig_webhook_path) WHERE strig_webhook_path IS NOT NULL;
