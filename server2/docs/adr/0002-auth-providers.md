# ADR 0002 — OIDC и LDAP: свои реализации, без сторонних библиотек (T-029)

Статус: принято. Эпик E8, ветка `backend/clean-room`. Продолжает
`server2/docs/adr/0001-stack.md` — тот же принцип: совпадение со старым
сервером (`server/**`, не читался) допустимо только для стандартной
библиотеки, сторонние зависимости заводятся, только если без них
действительно не обойтись разумным объёмом кода.

## OIDC: своя discovery + JWKS + проверка id_token, без `coreos/go-oidc`

Задача не запрещает `github.com/coreos/go-oidc` прямо (в отличие от `chi`/
`gorilla/websocket` в ADR 0001), но и не обязывает его использовать — "лучше
своя реализация" сформулировано как предпочтение. Протокол, который
реально нужен `/api/auth/oidc/start` и `/api/auth/oidc/callback`, — это:

1. один `GET /.well-known/openid-configuration` (JSON, 4 поля нужны:
   `issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`);
2. один `GET jwks_uri` (JSON, RSA-ключи в форме JWK: `kty`/`kid`/`n`/`e`);
3. один `POST token_endpoint` (`application/x-www-form-urlencoded`,
   `grant_type=authorization_code`) → `id_token`;
4. проверка подписи `id_token` (RS256: `crypto/rsa.VerifyPKCS1v15` поверх
   SHA-256 от `header.payload`) и claim'ов `iss`/`aud`/`exp`.

Всё это укладывается в `internal/authn/oidc.go` (клиент с кэшем discovery/
JWKS на 10 минут) без генерации кода и без зависимости, которая иначе
притащила бы полный OAuth2-клиент (`golang.org/x/oauth2`) ради одного
`POST`. Ограничение: поддержан только `alg: RS256` — это алгоритм
подавляющего большинства IdP (Google, Microsoft Entra ID, Okta, Keycloak по
умолчанию); `ES256`/`HS256` не реализованы. Если понадобится IdP с другим
алгоритмом, добавить его — это один case в `verifyIDToken`, не
переписывание клиента.

Тест — `internal/authn/oidc_test.go`, `httptest.Server`, отдающий
discovery/JWKS/token с ключом, сгенерированным в тесте (`crypto/rsa.
GenerateKey`), подписывающий id_token им же: проверяет discovery, успешную
верификацию, и три отказа (чужой `aud`, истёкший `exp`, испорченная
подпись) — ровно то, что просит тикет ("тест на httptest-провайдере").

## LDAP: свой клиент на BER, `github.com/go-ldap/ldap` не подключён

В отличие от OIDC, здесь задача явно требует обоснования, если библиотека не
берётся. `go-ldap/ldap` — зрелая, полнофункциональная реализация (весь
диапазон LDAPv3: TLS/StartTLS, все формы Filter grammar, paging controls,
модификации записей и т.д.), но серверу нужна ровно одна операция:
"аутентифицировать логин/пароль через один equality-фильтр в одном
поддереве каталога" — bind служебной учётной записью, `search` по
`(attr=username)`, `bind` найденным DN паролем пользователя. Это три LDAP
PDU (`BindRequest`/`BindResponse`, `SearchRequest`/`SearchResultEntry`/
`SearchResultDone`), а не всё LDAPv3.

Решение — собственный минимальный BER-кодек (`internal/authn/ldap_ber.go`,
~150 строк: TLV-примитивы INTEGER/OCTET STRING/BOOLEAN/ENUMERATED/SEQUENCE,
без "high tag number" формы — она этому протоколу не нужна) и клиент поверх
него (`internal/authn/ldap.go`): `dialLDAP` (`ldap://`/`ldaps://` через
`net`/`crypto/tls`), `bind`, `search` с одним equality-фильтром. Явные,
осознанные ограничения объёма (не покрывает произвольную LDAP filter
grammar — `LDAP_USER_FILTER` обязан быть простым `(attr=%s)`, без
wildcard/`&`/`|`; не поддерживает `StartTLS`, только implicit TLS через
`ldaps://`; не читает referral) — они документированы в самом коде
(`parseEqualityFilterTemplate`, package doc) и не мешают заявленной задаче
("вход по логину/паролю через каталог"), но делают эту реализацию
непригодной как каталожный клиент общего назначения. Если T-030+
понадобится более широкий LDAP (групповые атрибуты, paging, произвольные
фильтры от администратора), `go-ldap/ldap` тогда — обоснованный выбор;
сейчас его тянуть ради 5% используемого функционала было бы избыточно (тот
же принцип, что вывел ADR 0001 к отказу от `chi`/`gorilla/websocket`).

Тест без живого LDAP-сервера — `internal/authn/ldap_test.go`: фейковый LDAP
на `net.Listener`, говорящий тем же BER-протоколом (`ldap_ber.go`) в обе
стороны — принимает `BindRequest`/`SearchRequest`, отвечает
`BindResponse`/`SearchResultEntry`/`SearchResultDone` с ожидаемыми
значениями, обслуживая ровно ту последовательность из двух TCP-соединений,
которую открывает `loginLDAP`. Это тест и протокола (кодек+клиент
интегрированы через реальный TCP), и слоя маппинга (атрибуты каталога →
email/displayName) одновременно — задача разрешает "юнит на слое
маппинга", если без живого LDAP нельзя; здесь можно чуть больше, раз
протокол свой и небольшой.

## Прочитанные файлы (кроме общих для сессии)

- `docs/50-api-contract.yaml` (пути `/api/auth/oidc/**`, `/api/auth/ldap/login`,
  схемы `LoginResult`/`AuthMethodsResponse`).
- `docs/50-api-contract.md` §1.9 ("Корпоративный вход: переменные OIDC/LDAP
  читаются пакетом `corpauth`... эффект виден в `/api/auth/methods`,
  `/api/auth/oidc/**`, `/api/auth/ldap/login`") — подтверждает, что имена
  самих переменных вне разрешённых для чтения файлов; имена в
  `server2/internal/config/config.go` — решение этой сессии
  (`server2/docs/decisions.md`, раздел T-029).
- `server2/docs/adr/0001-stack.md` (принцип, которому это ADR следует).

`server/**` и `packages/core/**` не открывались.
