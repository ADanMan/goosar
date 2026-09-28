-- 400_autopilot_webhook_digest_routing: T-029 доводка (безопасность вебхуков
-- автопилота, см. server2/docs/decisions.md). До этой миграции
-- sentinel_triggers.strig_webhook_path хранил секрет вебхука в открытом
-- виде — единственный способ маршрутизировать входящий
-- POST /api/webhooks/autopilots/{token} к строке-триггеру был поиск по
-- этому plaintext-значению (см. 006_sentinels.up.sql,
-- sentinel_triggers_webhook_path_uk). Координатор потребовал убрать открытый
-- токен из схемы: маршрутизация теперь — по strig_webhook_token_digest
-- (sha256(token), уже существовавшая колонка, до этой миграции не
-- участвовавшая в поиске); webhook_path/webhook_url теперь видны в ответе
-- API так же, как webhook_token — только сразу после create/rotate, не на
-- последующих чтениях (сервер физически не может восстановить путь без
-- открытого токена в базе).
DROP INDEX IF EXISTS sentinel_triggers_webhook_path_uk;
ALTER TABLE sentinel_triggers DROP COLUMN IF EXISTS strig_webhook_path;
CREATE UNIQUE INDEX sentinel_triggers_webhook_token_digest_uk
    ON sentinel_triggers (strig_webhook_token_digest)
    WHERE strig_webhook_token_digest IS NOT NULL;
