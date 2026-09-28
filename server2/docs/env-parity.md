# Сверка переменных окружения с приложением контракта (T-026 доводка)

Источник — `docs/50-api-contract.md`, раздел «Приложение. Переменные
окружения сервера» (161 переменная) и «Спорные места (переменные
окружения)». Эта таблица построчно отвечает на вопрос «поддерживает ли
`server2` эту переменную под этим именем и с этим поведением».

Статусы:

- **Поддержана** — `server2` читает переменную под ИМЕНЕМ приложения, с тем
  же форматом/умолчанием, и её значение реально влияет на поведение так,
  как описывает контракт.
- **Частично** — переменная читается под правильным именем и что-то делает,
  но не всё описанное контрактом поведение реализовано; подробность — в
  колонке «Примечание» и (для решений, требующих обоснования) в
  `server2/docs/decisions.md`, раздел «T-026 доводка: конфигурация из тех же
  переменных окружения».
- **Прочитана, не влияет** — переменная читается под правильным именем
  (`config.Config`, поле есть), но соответствующая функциональность в этой
  версии server2 не реализована вовсе — сервер логирует предупреждение при
  старте (`Config.UnsupportedNotices`), поведения не меняет.

Итого: **101 поддержана полностью, 19 частично, 41 прочитана без эффекта**
(101+19+41 = 161).

## БД и запуск

| Переменная | Статус | Примечание |
|---|---|---|
| `DATABASE_URL` | Поддержана | |
| `DATABASE_MAX_CONNS` | Поддержана | `store.OpenPool` переопределяет `pgxpool.Config.MaxConns` после разбора DSN |
| `DATABASE_MIN_CONNS` | Поддержана | аналогично `MinConns` |
| `PORT` | Поддержана | |
| `APP_ENV` | Поддержана | + отказ старта при небезопасном `JWT_SECRET` на production |
| `GOOSAR_REPLICAS` | Поддержана | `Config.Validate()` отказывает старту при `>1` без `REDIS_URL`; сам Redis-бэкенд лимитера/realtime не реализован (см. группу Realtime/Redis) |
| `GOOSAR_SHUTDOWN_HOLD_DURATION` | Поддержана | `time.Sleep` перед `srv.Shutdown` |
| `GOOSAR_MIGRATION_LOCK_TIMEOUT` | Поддержана | `migrate.ApplyLocked`, пауза между попытками `pg_try_advisory_lock` |
| `GOOSAR_MIGRATION_STATEMENT_TIMEOUT` | Поддержана | `SET LOCAL statement_timeout` в транзакции каждого файла миграции |
| `GOOSAR_MIGRATION_LOCK_RETRIES` | Поддержана | |

## Аутентификация и сессии

| Переменная | Статус | Примечание |
|---|---|---|
| `JWT_SECRET` | Поддержана | + отказ старта на `production` при пустом/плейсхолдере (`config.knownInsecureSecrets`) |
| `JWT_SECRET_PREVIOUS` | Поддержана | `Signer.Verify` принимает подпись current ИЛИ любого из previous |
| `COOKIE_DOMAIN` | Поддержана | IP молча игнорируется (`net.ParseIP`) |
| `AUTH_TOKEN_TTL` | Поддержана | секунды целым или Go-длительность |
| `FRONTEND_ORIGIN` | Поддержана | |
| `ALLOWED_ORIGINS` | Поддержана | `Config.EffectiveAllowedOrigins()`, приоритет над `CORS_ALLOWED_ORIGINS` |
| `CORS_ALLOWED_ORIGINS` | Поддержана | при пустом — три локальных origin для разработки (буквальный список — решение этой сессии, см. decisions.md) |
| `GOOSAR_APP_URL` | Частично | используется в magic-link письма входа; НЕ используется для сравнения с «официальным облаком» (эта функциональность в server2 не реализована) |
| `GOOSAR_PUBLIC_URL` | Поддержана | (было реализовано раньше T-026/T-028) |
| `GOOSAR_TRUSTED_PROXIES` | Поддержана | `httpapi.ClientIP` доверяет X-Forwarded-For/X-Real-IP только из CIDR этого списка |
| `ALLOW_SIGNUP` | Поддержана | |
| `ALLOWED_EMAILS` | Поддержана | проверяется на email-коде и на OIDC/LDAP входе |
| `ALLOWED_EMAIL_DOMAINS` | Поддержана | то же |
| `DISABLE_WORKSPACE_CREATION` | Поддержана | |
| `GOOSAR_DEV_VERIFICATION_CODE` | Поддержана | |
| `GOOSAR_ROLE_WORKSPACES` | Поддержана | `auto`/`off` валидируются при старте; `auto` реально провижинит роли в `cmd/server` (раньше был подключён только к CLI `provision-roles`, несмотря на комментарий в коде, обещавший обратное) |
| `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` | Частично | сидирует существующих пользователей при старте, пока таблица администраторов пуста; резолв email, ещё не зарегистрированных, на их будущей регистрации — не реализован (нет хука на создание аккаунта) |
| `REALTIME_METRICS_TOKEN` | Поддержана | (было реализовано раньше) |

## Почта

| Переменная | Статус | Примечание |
|---|---|---|
| `RESEND_API_KEY` | Поддержана | |
| `RESEND_FROM_EMAIL` | Поддержана | дефолт `noreply@goosar.ru` |
| `SMTP_HOST` | Поддержана | приоритетнее Resend (`Config.MailProvider()`) |
| `SMTP_PORT` | Поддержана | дефолт 25; implicit TLS автоматически на 465 |
| `SMTP_USERNAME` | Поддержана | |
| `SMTP_PASSWORD` | Поддержана | |
| `SMTP_FROM_EMAIL` | Поддержана | наследует `RESEND_FROM_EMAIL` |
| `SMTP_TLS_INSECURE` | Поддержана | `InsecureSkipVerify` |
| `SMTP_TLS` | Поддержана | `starttls`/`implicit`, алиасы `smtps`/`ssl` |
| `SMTP_EHLO_NAME` | Поддержана | `client.Hello()` |

## OIDC/LDAP/MFA

| Переменная | Статус | Примечание |
|---|---|---|
| `GOOSAR_AUTH_METHODS` | Поддержана | гейтит эндпойнты email/oidc/ldap независимо от их настройки ниже |
| `GOOSAR_TOTP_ISSUER` | Поддержана | (было) |
| `GOOSAR_OIDC_ISSUER` | Поддержана | |
| `GOOSAR_OIDC_CLIENT_ID` | Поддержана | |
| `GOOSAR_OIDC_CLIENT_SECRET` | Поддержана | |
| `GOOSAR_OIDC_REDIRECT_URL` | Поддержана | |
| `GOOSAR_OIDC_SCOPES` | Поддержана | `openid` добавляется всегда |
| `GOOSAR_OIDC_ADMIN_CLAIM` | Прочитана, не влияет | заявка на роль администратора при совпадении claim не подаётся (нет callback-инфраструктуры между `authn` и `deployment` — архитектурный цикл импорта, см. decisions.md) |
| `GOOSAR_OIDC_ADMIN_VALUE` | Прочитана, не влияет | см. выше |
| `GOOSAR_OIDC_DISPLAY_NAME` | Поддержана | |
| `GOOSAR_OIDC_TRUST_UNVERIFIED_EMAIL` | Частично | отклоняется только id_token с явным `email_verified=false`; провайдер, вовсе не присылающий claim, не отклоняется (переменная — «в установке: Нет», не приоритетная) |
| `GOOSAR_LDAP_URL` | Поддержана | |
| `GOOSAR_LDAP_START_TLS` | Поддержана | гейтит доступность метода: `ldap://` без `START_TLS=true` не предлагается |
| `GOOSAR_LDAP_BIND_DN` | Поддержана | |
| `GOOSAR_LDAP_BIND_PASSWORD` | Поддержана | |
| `GOOSAR_LDAP_BASE_DN` | Поддержана | |
| `GOOSAR_LDAP_USER_FILTER` | Частично | клиент поддерживает только одиночный equality-фильтр `(attr=%s)`, не произвольную LDAP-грамматику (решение более ранней сессии T-029, сохранено) |
| `GOOSAR_LDAP_EMAIL_ATTR` | Поддержана | |
| `GOOSAR_LDAP_NAME_ATTR` | Поддержана | |
| `GOOSAR_LDAP_ADMIN_GROUP` | Прочитана, не влияет | та же причина, что `GOOSAR_OIDC_ADMIN_CLAIM` |
| `GOOSAR_LDAP_DISPLAY_NAME` | Поддержана | |

## Хранилище

| Переменная | Статус | Примечание |
|---|---|---|
| `S3_BUCKET` | Прочитана, не влияет | backend вложений в server2 — только локальный диск (решение более ранней сессии T-027) |
| `S3_REGION` | Прочитана, не влияет | |
| `AWS_ACCESS_KEY_ID` | Прочитана, не влияет | |
| `AWS_SECRET_ACCESS_KEY` | Прочитана, не влияет | |
| `AWS_ENDPOINT_URL` | Прочитана, не влияет | |
| `S3_USE_PATH_STYLE` | Прочитана, не влияет | |
| `ATTACHMENT_DOWNLOAD_MODE` | Поддержана | (было) |
| `ATTACHMENT_DOWNLOAD_URL_TTL` | Поддержана | теперь Go-длительность (раньше — целые минуты; целые числа без суффикса по-прежнему читаются как минуты для совместимости) |
| `CLOUDFRONT_KEY_PAIR_ID` | Прочитана, не влияет | подпись ссылок CloudFront не реализована |
| `CLOUDFRONT_PRIVATE_KEY_SECRET` | Прочитана, не влияет | |
| `CLOUDFRONT_PRIVATE_KEY` | Прочитана, не влияет | |
| `CLOUDFRONT_DOMAIN` | Частично | используется в `img-src` заголовка CSP (см. `GOOSAR_EXTERNAL_IMAGES`); не используется для подписи ссылок (сама подпись не реализована) |
| `LOCAL_UPLOAD_DIR` | Поддержана | |
| `LOCAL_UPLOAD_BASE_URL` | Поддержана | + используется в CSP |
| `GOOSAR_EXTERNAL_IMAGES` | Поддержана | заголовок `Content-Security-Policy` теперь стамплится на каждый ответ API (раньше — не было вовсе) |
| `GOOSAR_IMAGE_HOSTS` | Поддержана | |
| `GOOSAR_PROVISIONING_STORE` | Частично | `local` работает (переиспользует `LOCAL_UPLOAD_DIR`); `oci` — читается, зеркалирование не реализовано |
| `GOOSAR_PROVISIONING_LOCAL_PREFIX` | Поддержана | |
| `GOOSAR_PROVISIONING_OCI_URL` | Прочитана, не влияет | |
| `GOOSAR_PROVISIONING_OCI_REPOSITORY` | Прочитана, не влияет | |
| `GOOSAR_PROVISIONING_OCI_USERNAME` | Прочитана, не влияет | |
| `GOOSAR_PROVISIONING_OCI_PASSWORD` | Прочитана, не влияет | |
| `GOOSAR_PROVISIONING_OCI_PACKAGES` | Прочитана, не влияет | |
| `GOOSAR_PROVISIONING_SYNC_TIMEOUT` | Прочитана, не влияет | |

## Realtime/Redis

Пример из самого задания T-026: весь Redis-бэкенд (многоузловой лимитер,
fan-out realtime, кеш токенов) не реализован в этой версии server2 —
однопроцессный in-memory лимитер и in-process realtime-хаб остаются
единственной реализацией.

| Переменная | Статус | Примечание |
|---|---|---|
| `REDIS_URL` | Прочитана, не влияет | используется только в `Config.Validate()` для отказа при `GOOSAR_REPLICAS>1` |
| `REDIS_DISABLE_CLIENT_NAME` | Прочитана, не влияет | |
| `REALTIME_RELAY_MODE` | Прочитана, не влияет | |
| `REALTIME_RELAY_SHARDS` | Прочитана, не влияет | |
| `REALTIME_RELAY_STREAM_MAXLEN` | Прочитана, не влияет | |
| `REALTIME_RELAY_XREAD_COUNT` | Прочитана, не влияет | |
| `REALTIME_RELAY_XREAD_BLOCK` | Прочитана, не влияет | |
| `REALTIME_RELAY_REPLAY_GRACE` | Прочитана, не влияет | |
| `GOOSAR_RUNTIME_RECONNECT_GRACE` | Поддержана | (было реализовано раньше, `internal/daemon`) |

## Лимиты

Все десять уже читались под правильными именами (совпадение с контрактом —
результат более ранней сессии T-029). `RATE_LIMIT_TRUSTED_PROXIES` в этой
сессии реально подключён к `httpapi.SetTrustedProxies`.

| Переменная | Статус |
|---|---|
| `RATE_LIMIT_AUTH` | Поддержана |
| `RATE_LIMIT_AUTH_VERIFY` | Поддержана |
| `RATE_LIMIT_AUTH_EMAIL` | Поддержана |
| `RATE_LIMIT_MFA_VERIFY` | Поддержана |
| `RATE_LIMIT_TOKEN` | Поддержана |
| `RATE_LIMIT_API` | Поддержана |
| `RATE_LIMIT_CONTACT_SALES` | Поддержана |
| `RATE_LIMIT_EXPORT` | Поддержана |
| `RATE_LIMIT_JOIN` | Поддержана |
| `RATE_LIMIT_TRUSTED_PROXIES` | Поддержана |

## Интеграции

| Переменная | Статус | Примечание |
|---|---|---|
| `GITHUB_APP_SLUG` | Поддержана | |
| `GITHUB_WEBHOOK_SECRET` | Поддержана | |
| `GITHUB_APP_ID` | Поддержана | |
| `GITHUB_APP_PRIVATE_KEY` | Поддержана | |
| `GITHUB_TOKEN` | Прочитана, не влияет | импорт навыков из GitHub не подставляет его автоматически |
| `GOOSAR_VCS_INTEGRATION_ENABLED` | Поддержана | |
| `GOOSAR_VCS_SECRET_KEY` | Поддержана | |
| `GOOSAR_VCS_SECRET_KEY_PREVIOUS` | Поддержана | |
| `GOOSAR_SLACK_SECRET_KEY` | Поддержана | |
| `GOOSAR_SLACK_SECRET_KEY_PREVIOUS` | Поддержана | |
| `COMPOSIO_API_KEY` | Поддержана | |
| `COMPOSIO_STATE_SECRET` | Поддержана | |
| `COMPOSIO_CALLBACK_BASE_URL` | Поддержана | |
| `GOOSAR_CLOUD_FLEET_URL` | Поддержана | переименована из `GOOSAR_CLOUDRUNTIME_BASE_URL` |
| `GOOSAR_FLEET_URL` | Поддержана | алиас, ниже приоритетом |
| `GOOSAR_CLOUD_FLEET_TIMEOUT` | Поддержана | таймаут HTTP-клиента прокси (раньше не был настраиваемым) |
| `GOOSAR_RUNTIME_CONFIG_PATH` | Прочитана, не влияет | встроенный каталог рантаймов/моделей не переопределяем |

## LLM

| Переменная | Статус | Примечание |
|---|---|---|
| `GOOSAR_LLM_API_KEY` | Частично | используется только `GET /api/llm/health` и `GET /api/deployment/client-secrets` (`internal/deployment`); внутренний слой общих LLM-хелперов (например реальная генерация заголовка чата через LLM) не реализован — `generateTitle` остаётся эвристикой без сети |
| `GOOSAR_LLM_BASE_URL` | Частично | то же |
| `GOOSAR_LLM_DEFAULT_MODEL` | Частично | то же (используется как фоллбек модели в client-secrets) |
| `GOOSAR_DEPLOYMENT_LLM_API_BASE` | Поддержана | переименована из `GOOSAR_DEPLOYMENT_LLM_BASE_URL`; теперь только подсказка (см. decisions.md) |
| `GOOSAR_DEPLOYMENT_LLM_MODEL` | Поддержана | |

## Деплой/политика

| Переменная | Статус | Примечание |
|---|---|---|
| `GOOSAR_DELIVERY_PROFILE` | Поддержана | валидируется (`cloud`/`perimeter`), централизована в `config.Config` (раньше читалась напрямую `os.Getenv` внутри `internal/misc`) |
| `GOOSAR_DEPLOYMENT_PROFILE` | Частично | валидируется и отражается в `GET /api/status`; само по себе не переключает никакое поведение сверх валидации (закрытая регистрация обеспечивается отдельно через `ALLOWED_EMAIL_DOMAINS`/`ALLOWED_EMAILS`) |
| `GOOSAR_SKILL_SOURCES` | Частично | валидируются имена источников (`clawhub`/`github`/`skillssh`/`none`, иначе отказ старта) и отражаются в `GET /api/config`; сетевой enforcement на конкретный fetch не реализован |
| `GOOSAR_MCP_ALLOWED_HOSTS` | Прочитана, не влияет | проверка хостов записей MCP при сохранении `mcp_config` не реализована |
| `GOOSAR_MCP_ALLOWED_COMMANDS` | Прочитана, не влияет | то же для команд stdio-записей |
| `GOOSAR_ALLOWED_PROVIDERS` | Частично | только отражается в `GET /api/config`; не запрещает назначение задачи рантайму вне списка |
| `GOOSAR_OFFICIAL_CLOUD_HOST` | Прочитана, не влияет | |
| `GOOSAR_MIN_DAEMON_VERSION` | Поддержана | (было) |
| `GOOSAR_MCP_SECRET_KEY` | Поддержана | (было) |
| `GOOSAR_MCP_SECRET_KEY_PREVIOUS` | Поддержана | (было) |
| `GOOSAR_AUDIT_RETENTION_DAYS` | Поддержана | новый суточный фоновый цикл (`cmd/server`, `deployment.PurgeAuditLog`) — раньше не было ни разового, ни периодического удаления |
| `GOOSAR_DEPLOYMENT_JIRA_URL` | Поддержана | (было) |
| `GOOSAR_DEPLOYMENT_CONFLUENCE_URL` | Поддержана | (было) |
| `GOOSAR_DEPLOYMENT_EWS_URL` | Поддержана | (было) |
| `GOOSAR_DEPLOYMENT_MAIL_DOMAIN` | Частично | добавлена в `config.Config` и `GET /api/config`; не участвует в `mcp-library seed` (это не адрес сервиса, а домен почты) |
| `GOOSAR_DEPLOYMENT_BITRIX24_URL` | Поддержана | (было) |
| `GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL` | Поддержана | (было) |

## Хранение/retention

| Переменная | Статус | Примечание |
|---|---|---|
| `GOOSAR_SCHEDULER_AUDIT_RETENTION` | Прочитана, не влияет | периодического запуска нет — только `goosar_admin purge` вручную (не читает эту переменную отдельно) |
| `GOOSAR_UPLOAD_GC_GRACE` | Частично | задаёт дефолт флага `--grace` у `goosar_admin gc-uploads`; автоматического периодического запуска нет |
| `GOOSAR_HYGIENE_SWEEP_INTERVAL` | Прочитана, не влияет | нет фонового цикла в `cmd/server`, объединяющего оба сборщика |
| `GOOSAR_RETENTION_CHAT` | Частично | переименована из `GOOSAR_RETENTION_CHAT_HOURS` (int) в Go-длительность, дефолт изменён на «пусто = вечно» — задаёт дефолт `goosar_admin purge --chat`; применяется только по ручному запуску |
| `GOOSAR_RETENTION_TASKS` | Частично | то же |
| `GOOSAR_RETENTION_CLOSED_ISSUES` | Частично | то же |
| `GOOSAR_RETENTION_ACTIVITY` | Частично | то же |
| `GOOSAR_ATTACHMENT_PURGE_GRACE` | Частично | переименована из `GOOSAR_RETENTION_ATTACHMENT_GRACE_HOURS`; та же оговорка |
| `GOOSAR_EXPORT_DIR` | Прочитана, не влияет | server2 пишет экспорт через `internal/asset.Storage` (тот же backend, что вложения), отдельного каталога нет |
| `GOOSAR_EXPORT_TIMEOUT` | Поддержана | раньше был захардкожен константой 2ч; теперь читается |
| `GOOSAR_EXPORT_MAX_BYTES` | Прочитана, не влияет | ограничение размера архива не реализовано |
| `GOOSAR_EXPORT_RETENTION` | Поддержана | раньше захардкожен 7д; теперь читается |

## Наблюдаемость

| Переменная | Статус | Примечание |
|---|---|---|
| `METRICS_ADDR` | Прочитана, не влияет | Prometheus-listener не реализован |
| `POSTHOG_API_KEY` | Прочитана, не влияет | клиент PostHog не реализован |
| `POSTHOG_HOST` | Прочитана, не влияет | |
| `ANALYTICS_FRONTEND_ENABLED` | Прочитана, не влияет | |
| `ANALYTICS_DISABLED` | Прочитана, не влияет | |
| `ANALYTICS_ENVIRONMENT` | Прочитана, не влияет | |
| `LOG_FORMAT` / `GOOSAR_LOG_FORMAT` | Поддержана | новое: `cmd/server` строил только `JSONHandler`, теперь выбирает text/json |
| `LOG_LEVEL` / `GOOSAR_LOG_LEVEL` | Поддержана | новое: уровень теперь настраиваемый |

## server2 читает, но нет в приложении

Переменные, для которых сверка с приложением не нашла аналога — либо
server2-специфичная операционная деталь без эквивалента в контракте (версия
сборки, override для тестов), либо решение более ранней сессии зафиксировать
креды/адреса, которые контракт признаёт нужными, но не называет переменной
(см. `server2/docs/decisions.md`, разделы T-028/T-029). Ни одна из них не
переименовывалась в это имя приложения — либо потому что имени в приложении
для этой функции нет, либо потому что имя приложения уже занято другой
переменной с другим смыслом.

| Переменная | Назначение | Причина оставить |
|---|---|---|
| `MIGRATE` | `MIGRATE=true` — применить `server2/migrations` при старте, то же самое, что флаг `-migrate` | server2-специфичная операционная деталь запуска, не часть HTTP-контракта |
| `MIGRATIONS_DIR` | Каталог с `NNN_*.up.sql` | то же |
| `E2E_COMPAT_SQL_DIR` | Необязательный переводной слой для e2e фронтенда (см. `server2/README.md`, «e2e фронтенда») | инфраструктура этой clean-room сессии, не существует в приложении |
| `GOOSAR_SERVER_VERSION` | `server_version` в `GET /api/config` | приложение не называет переменную для версии сборки (обычно — ldflags) |
| `GOOSAR_CLOUDRUNTIME_API_KEY` | `Authorization: Bearer` к облачному fleet-сервису | приложение описывает поведение прокси (§6 «Облачный runtime»), но не называет переменную для его API-ключа — решение T-028 |
| `GOOSAR_GITHUB_API_BASE_URL` | override базового URL GitHub API | нужен для `httptest.Server` в тестах песочницы без доступа в интернет — решение T-029 |
| `GOOSAR_SLACK_API_BASE_URL` | override базового URL Slack API | то же |
| `GOOSAR_COMPOSIO_API_BASE_URL` | override базового URL Composio API | то же |

### Удалено этой сессией

- `GOOSAR_VCS_ALLOWED_PROVIDERS` — было объявлено в `config.Config` прежних
  сессий, но ни одним доменом не читалось (мёртвое поле); убрано при
  переписи `config.go` без замены.
- `MAIL_PROVIDER`, `MAIL_FROM_NAME` — заменены вычисляемым выбором
  транспорта (`Config.MailProvider()`) и переименованием `MAIL_FROM_EMAIL`
  → `RESEND_FROM_EMAIL`; отдельного «display name» для письма приложение не
  предусматривает.
- `SMTP_SECURITY` (значения `starttls`/`tls`/`none`) — заменена `SMTP_TLS`
  (`starttls`/`implicit`, дефолт `starttls`); значение `none` (совсем без
  TLS) в списке приложения нет — при отсутствии `SMTP_TLS` и STARTTLS,
  которую сервер relay не анонсирует, соединение остаётся plaintext де-факто
  (клиент пытается STARTTLS только если сервер заявил расширение).
- `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`,
  `OIDC_REDIRECT_URL`, `OIDC_DISPLAY_NAME`, `LDAP_URL`, `LDAP_BIND_DN`,
  `LDAP_BIND_PASSWORD`, `LDAP_BASE_DN`, `LDAP_USER_FILTER`,
  `LDAP_EMAIL_ATTRIBUTE`, `LDAP_NAME_ATTRIBUTE`, `LDAP_DISPLAY_NAME` —
  переименованы с добавлением префикса `GOOSAR_` (см. таблицу OIDC/LDAP
  выше); старые имена этой сессией нигде не читаются, синонимов не
  оставлено.
- `GOOSAR_CLOUDRUNTIME_BASE_URL` — переименована в `GOOSAR_CLOUD_FLEET_URL`.
- `GOOSAR_DEPLOYMENT_LLM_API_KEY` — убрана без замены: контракт явно
  говорит, что реальные креды LLM-контура деплоя берутся из
  `GOOSAR_LLM_API_KEY`, а не из отдельной `_DEPLOYMENT_`-переменной.
- `GOOSAR_PROVISIONING_CATALOG_DIR` — заменена вычисляемым путём
  `LOCAL_UPLOAD_DIR/GOOSAR_PROVISIONING_LOCAL_PREFIX` под управлением
  `GOOSAR_PROVISIONING_STORE=local` (см. таблицу «Хранилище» выше).
- `GOOSAR_RETENTION_{CHAT,TASKS,CLOSED_ISSUES,ACTIVITY}_HOURS`,
  `GOOSAR_RETENTION_ATTACHMENT_GRACE_HOURS` — переименованы в
  Go-длительность без суффикса `_HOURS` (см. таблицу «Хранение/retention»).
