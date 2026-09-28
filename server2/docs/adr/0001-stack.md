# ADR 0001 — стек и раскладка `server2/cmd/server` (T-026)

Статус: принято. Эпик E8, ветка `backend/clean-room`. Правило: совпадение с
выбором старого сервера (`server/**`, не читался — см. «Прочитанные файлы»
ниже) допустимо только для стандартной библиотеки; поэтому здесь нет
`chi`, `gorilla/websocket`, `sqlc`, `cobra`, даже если они и были бы разумным
выбором — они прямо исключены правилами чистой комнаты этой сессии.

## Роутер: `net/http.ServeMux` (Go 1.22+)

С Go 1.22 стандартный `ServeMux` умеет шаблоны путей (`"/api/workspaces/{id}"`)
и метод в паттерне (`"GET /api/workspaces"`), чего раньше не хватало и что
раньше оправдывало сторонний роутер. Контракт (`docs/50-api-contract.yaml`)
описывает ровно такую форму путей — `{id}`, `{memberId}` и т.д. — без каких-либо
кастомных ограничений на сегменты пути (regex-констрейнтов, versioned routing),
которые оправдывали бы `chi` или `gorilla/mux`. Взамен написана тонкая
обёртка `internal/httpapi.Router` (не сторонняя библиотека, а ~90 строк в этом
репозитории): она не подменяет матчинг `ServeMux`, а добавляет только то, чего
в нём нет — реестр "кто уже занял этот путь", нужный для вытеснения заглушек
доменами (см. решение 3 ниже).

## БД: `github.com/jackc/pgx/v5`, без кодогенерации

Уже выбран в T-024/T-025 (см. `server2/go.mod`, `server2/internal/importer`,
`server2/internal/migrate`) — переиспользуется, а не выбирается заново, чтобы
не плодить два разных клиента Postgres в одном модуле. `sqlc` и подобные
кодогенераторы исключены явно правилами тикета; ручной SQL к тому же проще
контролировать по колонкам, намеренно переименованным относительно
`server/migrations` (см. `server2/migrations/*.up.sql`).

## WebSocket: `github.com/coder/websocket`

`gorilla/websocket` исключён явно. Из активно поддерживаемых альтернатив
взят `coder/websocket` (бывший `nhooyr.io/websocket`, теперь официально
живёт под `github.com/coder/websocket`) — минимальный API поверх
`context.Context`, не тянет собственный event loop и хорошо ложится на
`net/http` без адаптеров. Используется только в `internal/realtime` для
самого upgrade и record-фреймов; протокол подписок поверх этого канала —
задел на T-027+ (см. «Пробелы спецификации» в `decisions.md`).

## Логирование: `log/slog`

Стандартная библиотека, JSON-хендлер в `cmd/server`, ничего стороннего не
нужно — контракт не предписывает конкретный формат логов.

## Метрики: свой минимальный экспорт (не Prometheus text, не VictoriaMetrics/metrics)

Контракт не документирует отдельный `/metrics`-эндпоинт: единственная
метрическая поверхность — `GET /health/realtime`, и её схема ответа —
`application/json` (`additionalProperties: true`), не Prometheus text
exposition format. Поэтому вместо тяжёлой зависимости (`VictoriaMetrics/metrics`)
реализован простой JSON-снимок счётчиков хаба (`internal/realtime.Hub.Stats`) —
ровно то, что просит контракт, ничего сверх. Если T-027+ добавят настоящий
`/metrics` в Prometheus text формате, `VictoriaMetrics/metrics` — кандидат
номер один на тот момент (лёгкая зависимость без кодогенерации), но заводить
её сейчас ради эндпоинта, которого нет в контракте, преждевременно.

## JWT: своя HMAC-подпись на `crypto/hmac` (не `golang-jwt`)

Контракт фиксирует ровно один алгоритм и ровно один набор клеймов
(`securitySchemes.cookieAuth`: `HS256`, `sub, email, name, tv, sid, iat, exp`) —
никакой гибкости по алгоритмам/ключам/JWKS не требуется. `golang-jwt/jwt`
хорошо решает задачу "поддержать много алгоритмов и форматов заголовков", но
здесь это не нужно: `internal/authn.Signer` — это ~70 строк на
`crypto/hmac`+`encoding/base64`+`encoding/json`, ровно под один тип claims
(`internal/authn/jwt.go`). Меньше поверхности для несовместимых версий и
меньше косвенности при чтении кода, который и так весь в одном пакете.

## Почта: `internal/mail.Sender`, dev-реализация — логгер

Resend/SMTP — предмет T-029. Сейчас — интерфейс `Sender` и `NewLoggerSender`
(пишет письмо в `slog` вместо реальной отправки), плюс `mail.Validating` и
`mail.Fanout` как заготовки на комбинирование транспортов, когда Resend
появится.

## Раскладка пакетов

```
server2/
  cmd/server            — main(): конфиг, миграции, сборка Deps, HTTP-сервер
  cmd/import            — не менялся (T-025)
  internal/config       — переменные окружения (те же имена, что в контракте)
  internal/store        — пул pgx, транзакции, общие SQL-хелперы
  internal/httpapi       — Router, JSON/Error-ответы, декодирование,
                           httpapi.Actor и его извлечение из контекста,
                           резолв воркспейса по заголовкам, роли, rate limit
  internal/authn        — коды по email, сессии, cookie+CSRF, JWT, PAT, статус MFA
  internal/mail         — интерфейс отправки почты + dev-логгер
  internal/realtime     — хаб /ws, интерфейс Publisher
  internal/identity     — /api/me, онбординг, /api/cli-token, /api/tokens
  internal/workspace    — /api/workspaces/**, /api/invitations/**, runtime-profiles
  internal/contractspec — разбор docs/50-api-contract.yaml (переиспользуют
                          tools/genstubs и тест покрытия маршрутов)
  internal/app          — Deps (сборка всех доменных Deps), routes.go
                          (регистрация доменов + заглушек), health/config
  internal/migrate      — не менялся (T-024/025)
  tools/genstubs        — генератор internal/app/stubs_gen.go
  docs/adr/0001-stack.md — этот файл
  docs/decisions.md      — пробелы спецификации и другие решения
```

### Как добавить домен (для параллельных реализаторов T-027+)

1. Новый пакет `internal/<domain>` со своим `Deps` (только то, что нужно
   этому домену — не весь `app.Deps`: так пакеты не образуют цикл `app <->
   домен`, и не нужно знать структуру `app.Deps`, чтобы начать работу).
2. `func Register(router *httpapi.Router, deps *Deps)` в этом пакете,
   регистрирующая свои маршруты через `router.Handle` (не `HandleStub` — это
   только для генератора).
3. В `internal/app/deps.go` завести домену Deps-поле и собрать его в `New`.
4. В `internal/app/routes.go` — одна строка вызова `<domain>.Register(...)`,
   **до** `RegisterStubs(router)`.
5. Перегенерировать заглушки не обязательно каждый раз: `RegisterStubs`
   вызывает `router.HandleStub`, который сам пропускает уже занятые домeном
   пути — но если контракт успел обновиться, `cd server2 && go run
   ./tools/genstubs -spec ../docs/50-api-contract.yaml -out
   internal/app/stubs_gen.go` синхронизирует список операций.

## Прочитанные файлы (кроме docs/50-api-contract.{md,yaml}, docs/51-data-model.md)

- `docs/31-backlog.md` (эпик E8, только раздел T-026).
- `server2/go.mod`, `server2/README.md`, `server2/docs/decisions.md`.
- `server2/internal/migrate/migrate.go` (переиспользован как есть).
- `server2/migrations/001_identity.up.sql`, `002_workspace.up.sql` (схема,
  на которую опирается authn/identity/workspace); остальные `NNN_*.up.sql` не
  требовались для доменов T-026 и не читались.
- `e2e/contract/README.md`, `e2e/contract/client.go`, `e2e/contract/harness.go`,
  `e2e/contract/contract_test.go` (только функции `testAuth`/`testWorkspaces`/`testMe`
  и общие `call`/`ensureX` хелперы, использованные ими).
- `scripts/similarity-check.py` (алгоритм проверки, чтобы соответствовать порогу).

`server/**` и `packages/core/**` не открывались.
