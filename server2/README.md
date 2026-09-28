# server2

Clean-room переписанный бэкенд Goosar (эпик E8, ветка `backend/clean-room`).
Схема БД спроектирована заново от `docs/50-api-contract.yaml` — см.
`docs/51-data-model.md`. Миграции лежат в `server2/migrations`
(`NNN_domain.{up,down}.sql`, PostgreSQL 16).

## Сборка и тесты

```sh
cd server2
go vet ./...
go build ./...
go test ./...
```

Модуль: `github.com/adanman/goosar/server2`, `go 1.24`. Postgres-драйвер —
`github.com/jackc/pgx/v5`.

## Импорт

`server2/cmd/import` переносит один воркспейс с работающего старого сервера
(контракт `docs/50-api-contract.yaml`, `v1.2.0`) в базу `server2`. Полное
описание алгоритма, порядка вызовов и того, что сознательно не переносится —
`docs/51-data-model.md`, раздел «Перенос данных»; решения по пробелам
спецификации, обнаруженным при реализации — `server2/docs/decisions.md`.

Источник читается **только** через `GET`-запросы контракта (не SQL-в-SQL);
запись в целевую базу `server2` идёт по SQL, одной транзакцией на весь
импорт — сам `server2` ещё не реализует контракт как HTTP API (это T-026+).
Исходные `uuid` сохраняются: все строки в `server2` создаются с теми же
`id`, что и в источнике, поэтому повторный запуск идемпотентен
(`ON CONFLICT (id) DO UPDATE`/`DO NOTHING` в зависимости от таблицы).

### Флаги

| Флаг | Обязателен | Смысл |
|---|---|---|
| `--source-url` | да | базовый URL исходного сервера, например `http://localhost:8199` |
| `--token` | да | `Authorization: Bearer` PAT или сессионный JWT с ролью `owner` в исходном воркспейсе |
| `--workspace` | да | slug или id исходного воркспейса |
| `--database-url` | да (кроме `--dry-run`) | DSN целевой базы server2 |
| `--migrate` | нет | применить `server2/migrations` к целевой базе перед импортом |
| `--dry-run` | нет | только прочитать источник и напечатать сводку в JSON, ничего не писать в базу |
| `--migrations-dir` | нет | каталог с `NNN_*.up.sql` (по умолчанию `server2/migrations`) |

### Пример запуска

```sh
createdb goosar2_import   # или: psql -c 'CREATE DATABASE goosar2_import'

go run ./server2/cmd/import \
  --source-url http://localhost:8199 \
  --token "$PAT" \
  --workspace acme-workspace \
  --database-url postgres://postgres@localhost:5432/goosar2_import \
  --migrate
```

Утилита печатает в stdout JSON-отчёт: сколько строк каждой сущности
перенесено (`transferred`) и какие категории данных сознательно пропущены с
причиной (`skipped_by_design`) — секреты сессий/PAT/webhook, зашифрованные
поля конфигурации агентов и воркспейса, OAuth-токены интеграций, биллинг,
аудит уровня деплоя, байты вложений, история inbox, чаты участников кроме
владельца токена импорта, сырой журнал очереди задач. Подробное обоснование
каждого пункта — `docs/51-data-model.md` и `server2/docs/decisions.md`.

### Что переносится

Воркспейс (`spaces`), участники (`space_members`, с созданием/обновлением
соответствующих `accounts`), runtime-профили (`agent_protocols`), runtime-
среды (`executors`), агенты (`operatives`, с целями вызова и привязанными
навыками), навыки (`capabilities` + `capability_files`), отряды (`crews` +
`crew_members`), метки (`tags`, по всем `resource_type`, если включены),
кастомные свойства (`field_defs`), проекты (`initiatives` + `initiative_resources`),
задачи (`tickets`, с метками, реакциями, родителем — вторым проходом),
комментарии задач (`ticket_notes` + `note_marks`, с ответами треда — вторым
проходом), подписчики и pull-request-ссылки задач, автопилоты (`sentinels` +
`sentinel_triggers`, без webhook/signing-секретов), чат-сессии владельца
токена и их сообщения (`convos`/`convo_messages`), MCP-серверы воркспейса
(`space_mcp_servers`, без секретов подключения) и незасекреченная часть
конфигурации воркспейса (`space_config`).

### Живая проверка

Проверено вручную против запущенного исходного сервера
(`http://localhost:8199`, вход по email-коду `424242` в dev-режиме): создан
тестовый воркспейс с runtime, агентом, проектом, метками, несколькими
задачами (включая под-задачу), комментариями (в т.ч. `/note`), реакцией,
отрядом и чат-сессией; количества всех перенесённых сущностей совпали с
данными исходного API; повторный запуск импорта не изменил число строк ни в
одной таблице (идемпотентность подтверждена).
