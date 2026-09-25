# Руководство по самостоятельному развёртыванию

Руководство описывает, как развернуть сервер Goosar у себя: установку, настройку, вход в систему, сопровождение и обновление. Сервер не запускает агентов: их запускает демон на машине пользователя, поэтому развёртывание включает и подключение таких машин.

## Содержание

- [Архитектура и требования](#архитектура-и-требования)
- [Быстрая установка](#быстрая-установка)
- [Пошаговая установка](#пошаговая-установка)
- [Приёмка на чистой машине](#приёмка-на-чистой-машине)
- [Доверие к внутреннему CA](#доверие-к-внутреннему-ca)
- [Сборки десктопа и предупреждения ОС](#сборки-десктопа-и-предупреждения-ос)
- [Kubernetes (Helm)](#kubernetes-helm)
- [Локальный стенд разработчика](#локальный-стенд-разработчика)
- [Конфигурация](#конфигурация)
- [База данных](#база-данных)
- [Запуск без Docker Compose](#запуск-без-docker-compose)
- [Обратный прокси](#обратный-прокси)
- [Доступ из локальной сети](#доступ-из-локальной-сети)
- [Десктоп за корпоративным прокси](#десктоп-за-корпоративным-прокси)
- [Проверка состояния](#проверка-состояния)
- [Мониторинг](#мониторинг)
- [Агрегация статистики использования](#агрегация-статистики-использования)
- [Высокая доступность](#высокая-доступность)
- [Закрытый контур (offline-поставка)](#закрытый-контур-offline-поставка)
- [Каталог пакетов (provisioning store)](#каталог-пакетов-provisioning-store)
- [Дефолты развёртывания](#дефолты-развёртывания)
- [Профили развёртывания](#профили-развёртывания)
- [Профиль доставки и исходящий трафик](#профиль-доставки-и-исходящий-трафик)
- [Корпоративный вход: OIDC и LDAP/AD](#корпоративный-вход-oidc-и-ldapad)
- [MFA и сессии](#mfa-и-сессии)
- [Администраторы развёртывания](#администраторы-развёртывания)
- [Ротация ключей шифрования](#ротация-ключей-шифрования)
- [Аудит и экспорт в SIEM](#аудит-и-экспорт-в-siem)
- [Диагностика](#диагностика)
- [Резервное копирование и восстановление](#резервное-копирование-и-восстановление)
- [Обновление](#обновление)
- [Остановка сервисов](#остановка-сервисов)
- [Ручная настройка Docker Compose и CLI](#ручная-настройка-docker-compose-и-cli)
- [Лимиты запросов](#лимиты-запросов)

## Архитектура и требования

Goosar разворачивается на вашей инфраструктуре одной командой. Развёртывание состоит из трёх обязательных компонентов и нескольких необязательных сервисов вокруг них: TLS перед стеком, мониторинг, резервные копии.

### Компоненты

| Компонент | Назначение | Технология |
| --- | --- | --- |
| Backend | REST API и WebSocket-сервер | Go (один бинарник) |
| Frontend | Веб-приложение | Next.js 16 |
| База данных | Основное хранилище | PostgreSQL 17 с pgvector |

Каждый пользователь, который запускает агентов у себя, дополнительно ставит CLI `goosar` и запускает демон на своей машине. Агент работает там, где лежат его репозитории, инструменты и учётные данные, поэтому ни одно развёртывание не поднимает среду выполнения само: контейнер рядом с backend не имел бы ничего из этого. Установите десктоп-приложение или CLI из развёртывания и нажмите «Подключить компьютер».

### Ресурсы

| Пользователей | Одновременных демонов | vCPU | RAM | Диск |
| --- | --- | --- | --- | --- |
| 10 | 5 | 2 | 4 GiB | 20 GB |
| 50 | 20 | 4 | 8 GiB (оценка) | 50 GB (оценка) |
| 200 | 50 | 8 | 16 GiB (оценка) | 100 GB и объектное хранилище (оценка) |

Измерена только строка на 10 пользователей, остальные две экстраполированы. Полная таблица — какие параметры важны, по каким метрикам видно, что узел стал тесным, и когда пора выносить Postgres или хранилище файлов — ведётся отдельно от этого документа.

Две ветки руководства стоят отдельно от основного пути:

- если сети нет или она закрыта, основной путь не подходит: он скачивает скрипты и образы из интернета. Используйте раздел «Закрытый контур (offline-поставка)»;
- если нужен только локальный стенд с десктоп-клиентом, достаточно раздела «Локальный стенд разработчика» ниже.

### Потоки развёртывания

Всё управляется файлами `docker-compose.selfhost*.yml` и `.env`:

| Поток | Команда | Что получается |
| --- | --- | --- |
| Официальные образы | `make selfhost` | `docker-compose.selfhost.yml`: скачиваются образы релиза, создаётся `.env` со сгенерированными секретами, стек стартует на `http://localhost:3001` |
| Образы из этого checkout | `make selfhost-build` | Тот же стек с наложенным `docker-compose.selfhost.build.yml`: backend и web собираются локально с тегом `:dev` |
| TLS до базы | `bash scripts/selfhost-pg-tls.sh`, затем `-f docker-compose.selfhost.tls.yml` | Шифрованное проверяемое соединение с встроенным PostgreSQL |
| Мониторинг | `-f docker-compose.selfhost.monitoring.yml` | Prometheus и Grafana с теми же дашбордом и правилами алертов, что ставит Helm chart |
| Kubernetes | `helm install goosar deploy/helm/goosar` | Та же конфигурация в виде Helm chart |

Слои накладываются повторяющимися флагами `-f`:

```bash
docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.monitoring.yml up -d
```

`.env.example` описывает каждую переменную. `make selfhost` и `scripts/selfhost-env.sh` генерируют секреты и выводят публичные адреса; у остального есть рабочие значения по умолчанию. Обновление описано в разделе «Обновление», остановка — в разделе «Остановка сервисов».

Образы берутся из реестра, который задают переменные `GOOSAR_BACKEND_IMAGE` и `GOOSAR_WEB_IMAGE` (по умолчанию — GHCR). Если реестр требует авторизации, войдите один раз на машину: `docker login ghcr.io` с токеном, у которого есть право `read:packages`. Если нужные образы уже загружены локально (`bash offline/load-release-images.sh`), Compose в реестр не обращается.

### Профили

Переменная `GOOSAR_DELIVERY_PROFILE` управляет исходящими обращениями backend. Свежий `.env` получает значение `perimeter`: никаких самообновлений и обращений к внешним каталогам skills. Значение `cloud` возвращает поведение облака:

```bash
PROFILE=cloud make selfhost
```

Тип развёртывания задаёт `GOOSAR_DEPLOYMENT_PROFILE` со значениями `perimeter`, `demo`, `dev`, `local`; он записывает в `.env` целый набор переменных:

```bash
GOOSAR_DEPLOYMENT_PROFILE=demo make selfhost
```

Профиль применяется только при создании `.env`; на существующем файле установщик предупредит, что ничего не изменил. Профиль `perimeter` закрывает регистрацию (`ALLOW_SIGNUP=false`): до первого запуска задайте `ALLOWED_EMAIL_DOMAINS` или `ALLOWED_EMAILS`, иначе войти не сможет никто, включая вас. Полная таблица профилей и адресов, к которым обращается backend, — в разделе «Конфигурация».

### Транспорты почты

Коды входа backend отправляет одним из трёх способов. Какой именно, определяют переменные в `.env`:

| Транспорт | Переменные | Где уместен |
| --- | --- | --- |
| Собственный SMTP-релей | `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_FROM_EMAIL`, `SMTP_EHLO_NAME`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Любое развёртывание. Единственный транспорт для `perimeter` |
| Resend | `RESEND_API_KEY`, `RESEND_FROM_EMAIL` | Только demo, dev и local |
| Без почты (только разработка) | `APP_ENV=development` | Код печатается в лог backend; прочитайте его командой `make selfhost-code` |

Resend — внешний сервис, поэтому для периметра он не подходит: из сети заказчика `api.resend.com` недоступен, а развёртывание, отправляющее коды через третью сторону, — уже не то периметровое развёртывание, которое согласовывали. Если задан `SMTP_HOST`, backend выбирает SMTP. Настраивайте один транспорт: при двух путь письма непредсказуем.

Две неполные пары настроек ломаются позже: `GOOSAR_LLM_API_KEY` без `GOOSAR_LLM_BASE_URL` (серверу нужен явный базовый адрес) и `SMTP_HOST` без `SMTP_FROM_EMAIL` (без адреса отправителя сервер не стартует).

### Резервные копии

`scripts/backup.sh` пишет копию базы в указанный каталог; запускайте его из cron. Восстановление — `scripts/restore.sh`, подробности в разделе «Резервное копирование и восстановление». Храните `GOOSAR_MCP_SECRET_KEY` и `GOOSAR_VCS_SECRET_KEY` вместе с копией: дамп, восстановленный без них, не прочитать.

## Быстрая установка

Три команды поднимают сервер, ставят CLI и настраивают его. Нужны Docker и Docker Compose; если их нет, скрипт подскажет, где взять.

### macOS и Linux

```bash
# 1. Поднять развёртывание
git clone <адрес-репозитория> goosar
cd goosar
make selfhost

# 2. Поставить CLI из только что запущенного развёртывания
curl -fsSL http://localhost:3001/install.sh | bash -s -- \
  --app-url http://localhost:3001 \
  --server-url http://localhost:8081

# 3. Настроить CLI, войти и запустить демон
goosar setup self-host --port 8081 --frontend-port 3001
```

Шаг 2 отделён от шага 1 сознательно: `make selfhost` поднимает только сервер. CLI берётся из самого развёртывания, которое отдаёт установщик и собранные бинарники, без Homebrew и GitHub Releases. Перед запуском скрипт можно скачать и прочитать: `curl -fsSLO …/install.sh && less install.sh && bash install.sh …`. Команда шага 2 подойдёт и для машины, где нужен только CLI: замените оба адреса на публичные адреса развёртывания.

`make selfhost` создаёт `.env` через `scripts/selfhost-env.sh`. Скрипт:

- генерирует `JWT_SECRET`, `POSTGRES_PASSWORD`, `GOOSAR_VCS_SECRET_KEY`, `GOOSAR_MCP_SECRET_KEY` и `GRAFANA_ADMIN_PASSWORD`;
- закрепляет `GOOSAR_IMAGE_TAG` на новейшем релизном теге из вашего клона;
- выводит публичные адреса из `PUBLIC_HOST`, если он задан;
- ставит `GOOSAR_DELIVERY_PROFILE=perimeter`.

Существующий `.env` не переписывается. Явный тег побеждает: `GOOSAR_IMAGE_TAG=<тег> make selfhost` разворачивает именно его. Если клон старый, сначала выполните `git fetch --tags`: закрепление берётся из локальных тегов. Когда закреплённый в `.env` тег отстаёт от новейшего, `make selfhost` предупредит.

Ключи `GOOSAR_MCP_SECRET_KEY` и `GOOSAR_VCS_SECRET_KEY` шифруют все сохранённые учётные данные интеграций. Сохраняйте их вместе с базой: дамп без ключей не восстановить.

Почтовый транспорт и LLM-шлюз задаются переменными `.env` (набор `SMTP_*`, `RESEND_API_KEY`, `GOOSAR_LLM_API_KEY`, `GOOSAR_LLM_BASE_URL`); см. «Транспорты почты» выше и `.env.example`.

Установщика `install.sh --with-server` больше нет: он печатает `make selfhost` и завершается с кодом 2.

Откройте http://localhost:3001. Для входа настройте почту, как описано в разделе «Шаг 2. Вход» ниже, либо возьмите код из лога backend.

### Каталог пакетов

Свежий стенд не содержит каталога пакетов (skills, MCP-серверы, среды выполнения). Каталог — это HTTP-индекс, который вы размещаете сами; формат описан ниже:

```bash
# необязательный bearer-токен; read не оставляет его в истории shell
read -rs GOOSAR_PACKAGE_INDEX_TOKEN && export GOOSAR_PACKAGE_INDEX_TOKEN
make selfhost-packages INDEX_URL=https://packages.example.test/catalog/index.json
```

Команда загружает пакеты, проверяет их по sha256 и размеру из индекса, копирует их в том загрузок backend, записывает `GOOSAR_PROVISIONING_STORE=local` в `.env` и перезапускает backend. Пока каталога нет, точки выдачи пакетов отвечают `503`, а всё остальное работает.

### Windows (PowerShell)

Развёртывание поднимается на машине с Docker: либо на Linux или macOS по инструкции выше, либо прямо из PowerShell:

```powershell
git clone <адрес-репозитория> goosar
cd goosar
powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -WithServer

goosar setup self-host --port 8081 --frontend-port 3001
```

`install.ps1` создаёт `.env` так же, как `make selfhost`: генерирует те же секреты через `System.Security.Cryptography.RandomNumberGenerator` (два ключа secretbox — как 32 байта в base64, остальное — hex), закрепляет `GOOSAR_IMAGE_TAG` и ставит `GOOSAR_DELIVERY_PROFILE=perimeter`. Повторный запуск существующий `.env` не переписывает. CLI на Windows скрипт берёт из GitHub Releases; если они вам недоступны, поставьте CLI из самого развёртывания (см. «Шаг 3. CLI и демон» в разделе «Пошаговая установка»), а сервер поднимите без `-WithServer`.

Ключи PowerShell повторяют флаги bash-установщика:

| Действие | Ключ |
| --- | --- |
| Поднять сервер | `-WithServer` |
| Перейти на другой тег | `-Upgrade`, `-Tag <vX.Y.Z>` |
| Остановить | `-Stop` |
| Профиль | `-DeliveryProfile` (псевдоним `-Profile`) со значениями `perimeter`, `demo`, `dev`, `local`, `cloud` |
| Почта | `-ResendKey`, `-SmtpHost`, `-SmtpPort`, `-SmtpUser`, `-SmtpPassword`, `-SmtpFrom`, `-SmtpTls <starttls\|implicit>` |
| LLM-шлюз | `-LlmKey`, `-LlmBaseUrl` |
| Справка | `-Help` |

Параметр называется `-DeliveryProfile`, а не `-Profile`, потому что `$PROFILE` — автоматическая переменная PowerShell, и её затенение внутри установщика — ловушка. Как и на bash, профиль применяется только при создании `.env`; при повторном запуске установщик говорит об этом явно.

## Пошаговая установка

Тот же результат по шагам вручную.

### Шаг 1. Запуск сервера

Нужны Docker и Docker Compose.

```bash
git clone <адрес-репозитория> goosar
cd goosar
make selfhost
```

`make selfhost` создаёт `.env` из примера, генерирует секреты и поднимает все сервисы через Docker Compose. По умолчанию он скачивает свежие стабильные образы. Чтобы собрать backend и web из текущего checkout, выполните `make selfhost-build`: он использует локальные теги `goosar-backend:dev` и `goosar-web:dev` и не перезаписывает скачанные `:latest`. Если образов для выбранного тега ещё нет, `make selfhost` подскажет перейти на `make selfhost-build`.

Когда стек поднялся:

- Frontend: http://localhost:3001
- Backend API: http://localhost:8081

Оба порта привязаны к `127.0.0.1`. Для доступа с других машин поставьте перед стеком обратный прокси с TLS, который пересылает запросы на `127.0.0.1:8081` (backend) и `127.0.0.1:3001` (frontend).

Если порты заняты, измените `BACKEND_PORT` и `FRONTEND_PORT` в `.env` (значение `PORT` держите равным `BACKEND_PORT`) и снова выполните `make selfhost`. Если в конце запуска здоровый стек ошибочно назван «ещё стартует», передайте порт из окружения: `PORT=<порт backend> make selfhost` — проверка готовности берёт порт оттуда. Затем укажите новые порты в `goosar setup self-host --port <порт backend> --frontend-port <порт frontend>`.

### Шаг 2. Вход

Откройте http://localhost:3001. Docker-стек по умолчанию работает с `APP_ENV=production` (задано в `docker-compose.selfhost.yml`), а фиксированного кода подтверждения нет. Выберите один из вариантов:

- **Рекомендуется для production.** Задайте `RESEND_API_KEY` или SMTP-настройки в `.env` и перезапустите backend. Коды придут на введённую почту. Подробности — в разделе «Конфигурация».
- **Без почты.** Код генерируется на сервере и печатается в лог контейнера backend; `make selfhost-code` выводит последний код. Для этого в `.env` нужен `APP_ENV=development`. При `APP_ENV=production` и без почтового транспорта сервер отвечает `503 email_not_configured` и страница входа говорит об этом прямо, вместо того чтобы обещать письмо, которое дойдёт только до лога оператора. Годится для разового теста на одной машине.
- **Фиксированный код для локальной проверки.** Задайте `APP_ENV=development` и `GOOSAR_DEV_VERIFICATION_CODE=888888` в `.env` и перезапустите backend. При `APP_ENV=production` этот код игнорируется.

Не задавайте `GOOSAR_DEV_VERIFICATION_CODE` на доступном из сети экземпляре: любой, кто знает адрес почты, войдёт с этим кодом.

Изменения `ALLOW_SIGNUP` и `DISABLE_WORKSPACE_CREATION` вступают в силу после перезапуска backend. Web читает оба значения из `/api/config` во время работы, пересборка не нужна. Порядок закрытия создания рабочих пространств описан в разделе «Конфигурация».

### Шаг 3. CLI и демон

Демон работает на вашей машине, не в Docker. Он находит установленные CLI агентов, регистрирует их на сервере и выполняет задачи, когда агентам назначают работу. Каждому участнику, который хочет запускать агентов у себя, нужно сделать следующее.

**а) Установить CLI и CLI агента.** Работающее развёртывание отдаёт свой установщик по `/install.sh`, а подходящие бинарники — по `/cli/`. GitHub и Homebrew не нужны:

```bash
curl -fsSL https://goosar.example.com/install.sh | bash -s -- \
  --app-url https://goosar.example.com \
  --server-url https://goosar.example.com
```

Для стека из шага 1 это `--app-url http://localhost:3001 --server-url http://localhost:8081`.

По умолчанию ставится только CLI `goosar`: раннер агента не скачивается, пока вы его не выберете. Выбор — флаг `--runner <none|hermes>` (переменная `GOOSAR_RUNNER`). Без флага интерактивный запуск спросит один раз, по умолчанию `none`; неинтерактивный (например, `curl | bash` в CI) раннер не ставит. `--runner hermes` скачивает среду выполнения агентов `hermes` из `/cli/hermes-<os>-<arch>.tar.gz` с той же проверкой контрольных сумм, что и для самого `goosar`, и печатает `export GOOSAR_HERMES_PATH=...` для окружения демона. Флаги `--with-agent` и `--without-agent` — псевдонимы `--runner hermes` и `--runner none`. Если развёртывание не публиковало артефакты агента (образ собран с пустым или незаданным `GOOSAR_HERMES_DIST_DIR`), установщик предупредит и продолжит: CLI `goosar` встанет в любом случае. В десктоп-приложении раннер тоже ставится по выбору: только после того, как пользователь включил его в настройках демона.

При сборке образа `GOOSAR_HERMES_DIST_DIR` принимает вывод сборки агента как есть: либо каталоги `hermes-<version>-<os>-<arch>/`, либо готовые архивы `.tar.gz`. `Dockerfile.web` (стадия `cli`, скрипт `scripts/stage-agent-artifacts.sh`) приводит архитектуры к `amd64` и `arm64`, при двух сборках под одну платформу оставляет более новую и публикует каждую под стабильным именем `/cli/hermes-<os>-<arch>.tar.gz`. Переименовывать вручную не нужно.

Для Windows развёртывание отдаёт зеркало на PowerShell (`/install.ps1`) и архив `windows/amd64` в `/cli/`. Нужен PowerShell 5.1 или 7 и `tar.exe` (есть в Windows 10 1803 и новее):

```powershell
& ([scriptblock]::Create((irm https://goosar.example.com/install.ps1))) `
    -AppUrl https://goosar.example.com -ServerUrl https://goosar.example.com
```

Установщик кладёт файлы в `%USERPROFILE%\.goosar\bin`, так же строго проверяет `checksums.txt` и добавляет каталог в пользовательский PATH, не трогая уже стоящие там записи с `%VAR%`. На Windows допустим только `-Runner none`: у поставляемого раннера нет сборки под Windows.

Кроме того, на машине должен быть хотя бы один CLI агента. Демон ищет их по имени команды в PATH; соответствие кода среды выполнения (`runtime-a` … `runtime-r`) и имени команды задаёт реестр `server/pkg/agent/runtimeregistry/runtimes.yaml` (поле `cliName`).

**б) Настроить одной командой:**

```bash
goosar setup self-host --port 8081 --frontend-port 3001
```

Команда:

1. настраивает CLI на `localhost` (порты нужно передать явно: собственные значения CLI — 8080 и 3000, а Compose публикует 8081 и 3001);
2. открывает браузер для входа;
3. находит ваши рабочие пространства;
4. запускает демон в фоне.

Для развёртываний с собственными доменами:

```bash
goosar setup self-host --server-url https://api.example.com --app-url https://app.example.com
```

Проверить демон:

```bash
goosar daemon status
```

Ожидаемый результат — `running` и список найденных агентов. Если что-то не работает, смотрите `goosar daemon logs`, а для backend и frontend — `docker compose -f docker-compose.selfhost.yml logs backend` и `... logs frontend`. Живость и готовность backend проверяют `curl http://localhost:8081/health` и `curl http://localhost:8081/readyz`; второй учитывает зависимости.

### Шаг 4. Проверка

1. Откройте рабочее пространство в веб-приложении: http://localhost:3001.
2. Перейдите в раздел «Среды выполнения»: там должна быть ваша машина.
3. В разделе «Агенты» создайте агента.
4. Создайте задачу и назначьте её агенту: он возьмёт задачу сам.

## Приёмка на чистой машине

Порядок для оператора или тестировщика на **чистой macOS arm64**: без Homebrew-пакетов от прошлой установки, без `~/.goosar`, без CLI агентов. Он доказывает, что клиентская половина поставки действительно работает. Результат фиксируется в вашем протоколе приёмки. Бюджет — около 30 минут, большая часть уходит на загрузки.

**0. Исходное состояние.** Убедитесь, что машина действительно чистая:

```bash
ls ~/.goosar 2>/dev/null; which node git 2>/dev/null
# ожидается: каталога ~/.goosar нет, команды не найдены
```

**1. Установка клиента** из самого развёртывания, а не из интернета:

```bash
curl -fsSLO https://<goosar-host>/install.sh && bash install.sh
goosar --version          # ожидается версия релиза этого развёртывания
```

**2. Предпроверка.** До любой настройки спросите машину, чего ей не хватает:

```bash
goosar doctor
```

На действительно чистой машине ожидаются ненулевой код возврата и строки `нет` для `Agent CLI`, `Node.js`, `npm` и `git`, у каждой — строка `Как починить:`. `Kerberos (kinit)` и `Docker` показывают `не требуется`, если развёртывание их не использует (`goosar doctor --kerberos` делает Kerberos обязательным). Поставьте то, что названо, и повторяйте, пока не появится:

```text
Всё на месте: демон можно запускать (goosar daemon start).
```

`goosar doctor --output json` печатает ту же матрицу для скриптов и десктоп-панели.

**3. Вход и запуск демона:**

```bash
goosar login
goosar daemon start
goosar daemon status      # ожидается connected и список доставленных пакетов
```

**4. Пакеты дошли.** Демон должен перечислять пакеты, которые отдаёт сервер. Пустой список означает, что сервер не отдаёт пакетов (каталог не загружен, см. «Каталог пакетов» выше), а не проблему клиента:

```bash
goosar status             # строка provisioning: счётчики pinned и delivered
```

**5. Агент выполняет задачу с MCP-инструментом.** Назначьте задачу агенту, у которого в рабочем пространстве объявлен MCP-сервер, и убедитесь, что в журнале запуска завершился вызов инструмента `mcp__*`. Это конец цепочки, ради которой проводится вся приёмка: чистая машина, демон онлайн, пакеты доставлены, агент использует доставленный MCP-инструмент.

**6. Поведение периметра (только профиль `perimeter`):**

```bash
goosar update             # ожидается отказ с сообщением оператора
curl -s https://<goosar-host>/api/config | grep perimeter
```

**Вариант без сети.** Меняются шаги 1 и 2: вместо скачивания проверьте принесённый носитель и ставьте клиента с него; процедура описана в разделе «Закрытый контур (offline-поставка)».

```bash
bash offline/offline-manifest.sh verify --dir <каталог носителя>
```

Начиная с шага 3 всё одинаково.

## Доверие к внутреннему CA

Если TLS-сертификат развёртывания выпущен внутренним удостоверяющим центром, корень этого центра нужен на каждой клиентской машине. Файл требуется двум потребителям.

- **Браузеры.** Импортируйте корень в системное хранилище доверия: macOS — Keychain, System, Certificates, «Always Trust»; Windows — `certmgr.msc`, «Доверенные корневые центры сертификации»; Linux — каталог `/usr/local/share/ca-certificates/` и команда `update-ca-certificates`. Пока этого не сделано, браузер предупреждает на каждой странице.
- **CLI `goosar` и демон.** На macOS и Windows они не читают системное хранилище. Скопируйте файл на клиентскую машину и передайте его один раз:

  ```bash
  goosar setup self-host --server-url https://<host> --app-url https://<host> --ca-file /path/to/ca-root.crt
  ```

  Путь сохраняется в профиле как `ca_file`; `goosar login`, демон и запускаемые им агенты доверяют развёртыванию с этого момента. Эквивалент в окружении — `GOOSAR_CA_FILE`. Флага `--insecure` нет.

Если сертификат выдан публичным центром или стоит за вышестоящим прокси, который предъявляет такой сертификат, клиентам ничего настраивать не нужно: они доверяют ему через публичную цепочку.

## Сборки десктопа и предупреждения ОС

Релиз содержит десктоп-клиент для пяти целей. Их собирает `bash scripts/release-local.sh <тег> --all-platforms`; ключи `--mac-arm64`, `--mac-x64`, `--win-x64`, `--linux-x64`, `--linux-arm64` собирают подмножество. Результат прикладывается к релизу и пишется в `offline/kit-desktop/` для передачи без GitHub. Все флаги перечисляет `bash scripts/release-local.sh --help`. С `GOOSAR_RELEASE_PRINT_PLATFORMS=1` скрипт печатает итоговую матрицу вместе с целями, которые этот хост собрать не может, и завершается, ничего не собрав.

| Цель | Артефакт | Примечание |
| --- | --- | --- |
| macOS arm64 | `goosar-desktop-<version>-mac-arm64.dmg` (и `.zip` для автообновления) | платформа, на которой проводится приёмка на чистой машине |
| macOS x64 | `goosar-desktop-<version>-mac-x64.dmg` (и `.zip`) | нужен хост сборки с артефактом раннера под x64 |
| Windows x64 | `goosar-desktop-<version>-windows-x64.exe` (NSIS) | собирается кросс-сборкой с macOS или Linux, wine не нужен |
| Linux x64 | `.AppImage`, `.deb`, `.rpm` | кросс-сборка с macOS даёт только AppImage |
| Linux arm64 | `.AppImage`, `.deb`, `.rpm` | то же |

Имена файлов для Linux не приведены намеренно: каждый формат пакета диктует свой токен архитектуры (AppImage пишет `x86_64`, `.deb` — `amd64`), и любое имя здесь было бы неверным для части форматов. Смотрите имена в манифесте поставки: там то, что реально собрано.

Go-CLI (в нём же демон) кросс-компилируется для всех этих целей на том же этапе и попадает в `offline/kit-cli/` рядом с `checksums.txt`, который его покрывает.

Если сборка одной платформы падает, остальные не отменяются: журнал артефактов в конце запуска помечает её как MISSING с причиной, а сам запуск завершается с ненулевым кодом. Так частичную поставку нельзя выдать за полную.

### Сборки не подписаны

Подпись кода и нотаризация macOS появятся после проверки безопасности. Пока все десктоп-артефакты не подписаны, и каждая ОС при первом запуске сообщает об этом по-своему. **Прежде чем что-либо разрешать, сверьте sha256 с `DELIVERY-MANIFEST.md`.** Именно контрольная сумма, а не отсутствие предупреждения, говорит, что файл тот, который поставлялся.

**macOS (Gatekeeper).** Двойной щелчок сообщает, что приложение нельзя открыть, так как не удалось проверить разработчика (в macOS 15 формулировка — «повреждено»). Разрешите его в «Системные настройки → Конфиденциальность и безопасность»: сразу после первого отказа там появляется заблокированное приложение с кнопкой «Всё равно открыть». В macOS 14 и старше работает и контекстное меню: щёлкните приложение в `/Applications` с Control, выберите «Открыть» и подтвердите. В macOS 15 этот путь для неподписанных приложений убрали. В обоих случаях решение запоминается, и следующие запуски проходят без вопросов. При массовой раздаче администратор после проверки контрольной суммы может снять флаг карантина с распространяемой копии:

```bash
shasum -a 256 "Goosar.app.zip"   # сравните с DELIVERY-MANIFEST.md
xattr -dr com.apple.quarantine "/Applications/Goosar.app"
```

**Windows (SmartScreen).** Установщик показывает «Windows защитила ваш компьютер». Нажмите «Подробнее → Выполнить в любом случае». Если политика это запрещает, добавьте установщик в разрешённые по sha256 в AppLocker или WDAC: правило по хэшу файла работает без издателя, а правило по издателю — нет.

**Linux.** Установку ничто не блокирует, пакеты просто не подписаны. Сделайте AppImage исполняемым (`chmod +x goosar-desktop-*.AppImage`), ставьте `.deb` командой `sudo dpkg -i`, а `.rpm` — `sudo rpm -i --nodigest`. Контрольную сумму сверьте заранее: подписи репозитория, которая сделала бы это за вас, нет.

Когда подпись появится, предупреждения исчезнут, а в поставке добавится шаг проверки `codesign` или `signtool`. Больше в артефактах ничего не изменится.

## Kubernetes (Helm)

Если у вас уже есть Kubernetes-кластер, Goosar можно развернуть в нём вместо Docker Compose: из chart в `deploy/helm/goosar/` либо из OCI-пакета `oci://ghcr.io/adanman/charts/goosar`. Chart рассчитан на типичный k3s или k8s с Ingress-контроллером и `ReadWriteOnce` StorageClass по умолчанию; писался под k3s, Traefik и `local-path` и должен работать на любом кластере с небольшими правками.

Chart создаёт в целевом namespace:

- `goosar-postgres` — `pgvector/pgvector:pg17` с PVC на 10Gi;
- `goosar-backend` — Go API и WebSocket-сервер. По умолчанию использует PVC загрузок на 5Gi (`ReadWriteOnce`); при настроенном S3 (`backend.config.s3Bucket`) задайте `backend.uploads.persistence.enabled=false`, и chart не будет объявлять PVC вовсе;
- `goosar-frontend` — standalone-сервер Next.js;
- два ресурса `Ingress`: для веб-хоста и для хоста backend;
- ConfigMap `goosar-config`, собранный из `values.yaml`.

По умолчанию ставится одна реплика backend. Что должно быть выполнено, чтобы запускать несколько (Redis, общее хранилище, внешний Postgres), описано в разделе «Высокая доступность».

Secret `goosar-secrets` chart **не создаёт**: вы создаёте его один раз через `kubectl`, чтобы настоящие значения не попадали в git.

Актуальные образы `goosar-web` читают `REMOTE_API_URL` и `DOCS_URL` во время работы Next.js-сервера, поэтому смена upstream не требует пересборки web. Chart по умолчанию направляет `REMOTE_API_URL` на Service backend этого релиза. `frontend.compatibility.backendAlias` нужен только устаревшим образам, где `REMOTE_API_URL=http://backend:8080` был зашит при сборке.

Требуются `kubectl` и `helm` (v3.13+ ради `--take-ownership`, либо v4+), настроенные на нужный кластер, Ingress-контроллер (Traefik или NGINX) и StorageClass по умолчанию.

### Шаг 1. Направить имена хостов на кластер

Chart по умолчанию использует учебные хосты `*.dev.lan`; замените их своими. В примерах ниже это `goosar.example.local` (web) и `api.goosar.example.local` (backend). Выберите один из вариантов:

- запись в `/etc/hosts` на каждой машине, которой нужен доступ (ноутбуки разработчиков и машина с демоном):

  ```text
  192.0.2.10  goosar.example.local api.goosar.example.local
  ```

  Подставьте адрес любого узла, на котором доступен Service вашего Ingress-контроллера;

- локальный DNS (Pi-hole, Unbound и т. п.): A-записи для обоих имён на адрес Ingress кластера.

Значения хостов задаются при установке: `ingress.frontend.host`, `ingress.backend.host`, а также `backend.config.appUrl`, `backend.config.frontendOrigin` и `backend.config.localUploadBaseUrl`.

### Шаг 2. Создать namespace

```bash
kubectl create namespace goosar
```

### Шаг 3. Создать Secret `goosar-secrets`

Chart ссылается на этот Secret по имени. Создайте его один раз со случайными значениями:

```bash
kubectl -n goosar create secret generic goosar-secrets \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
  --from-literal=GOOSAR_MCP_SECRET_KEY="$(openssl rand -base64 32)" \
  --from-literal=GOOSAR_VCS_SECRET_KEY="$(openssl rand -base64 32)" \
  --from-literal=RESEND_API_KEY="" \
  --from-literal=CLOUDFRONT_PRIVATE_KEY="" \
  --from-literal=GOOSAR_DEV_VERIFICATION_CODE=""
```

Внешний Postgres (`postgres.external.enabled=true`): вместо `POSTGRES_PASSWORD` положите в Secret `DATABASE_URL` с адресом вашего кластера.

`GOOSAR_MCP_SECRET_KEY` и `GOOSAR_VCS_SECRET_KEY` должны декодироваться ровно в 32 байта (`openssl rand -base64 32`): backend не стартует с ключом неверного формата. Они шифруют все сохранённые учётные данные интеграций (MCP-серверы, Jira, Exchange, токены VCS). **Без `GOOSAR_MCP_SECRET_KEY` эти данные лежат в базе, а значит и в каждой резервной копии, открытым текстом**, а при `GOOSAR_DELIVERY_PROFILE=perimeter` backend без него не запустится. Храните оба ключа вместе с базой: потеряв их, вы не восстановите дамп.

Необязательные значения пока оставьте пустыми; заполнить их можно позже (см. «Шаг 5. Вход»).

### Шаг 4. Установить chart

```bash
helm install goosar oci://ghcr.io/adanman/charts/goosar \
  --version <версия-chart> \
  -n goosar \
  --set ingress.frontend.host=goosar.example.local \
  --set ingress.backend.host=api.goosar.example.local \
  --set backend.config.appUrl=http://goosar.example.local \
  --set backend.config.frontendOrigin=http://goosar.example.local \
  --set backend.config.localUploadBaseUrl=http://api.goosar.example.local
```

У версии выпущенного chart нет ведущей `v` из Git-тега: релизу `v1.2.3` соответствует chart `1.2.3`, а образы backend и frontend по умолчанию берут тег `v1.2.3`.

Чтобы изменить значения по умолчанию, выгрузите их, отредактируйте и передайте через `-f`:

```bash
helm show values oci://ghcr.io/adanman/charts/goosar \
  --version <версия-chart> > my-values.yaml
# правьте my-values.yaml: хосты Ingress, теги образов, лимиты ресурсов
helm install goosar oci://ghcr.io/adanman/charts/goosar \
  --version <версия-chart> \
  -n goosar \
  -f my-values.yaml
```

Из checkout используйте локальный путь: `helm install goosar deploy/helm/goosar -n goosar`.

Наблюдайте за запуском подов:

```bash
kubectl -n goosar get pods -w
```

На холодном кластере backend может несколько минут оставаться `Running`, но не `Ready`: он ждёт PostgreSQL и применяет миграции. `startupProbe` это учитывает, и под не перезапускается. Как только backend стал `Ready`, миграции завершены, и `/healthz` отвечает ОК:

```bash
curl -H "Host: api.goosar.example.local" http://<ingress-ip>/healthz
# {"status":"ok","checks":{"db":"ok","migrations":"ok"}}
```

После этого откройте http://goosar.example.local.

### Шаг 5. Вход

Chart по умолчанию задаёт `APP_ENV=production` (`backend.config.appEnv` в `values.yaml`), а фиксированного кода подтверждения нет. Варианты те же три, что и для Docker.

- **Рекомендуется для production.** Обновите Secret настоящим ключом Resend и перезапустите backend (для SMTP задайте `SMTP_*` в значениях chart):

  ```bash
  kubectl -n goosar patch secret goosar-secrets --type=merge \
    -p '{"stringData":{"RESEND_API_KEY":"re_xxx"}}'
  kubectl -n goosar rollout restart deploy/goosar-backend
  ```

- **Без почты.** Код генерируется на сервере и печатается в лог пода backend (строка `[DEV] Verification code for ...:`):

  ```bash
  kubectl -n goosar logs -f deploy/goosar-backend | grep "Verification code"
  ```

  Для этого нужен `backend.config.appEnv: development`. При `APP_ENV=production` и без `SMTP_HOST` и `RESEND_API_KEY` вход отклоняется с `503 email_not_configured`, а не печатает код, который никто не успеет прочитать.

  Работает только с одной репликой: код печатает под, обработавший запрос, поэтому при `backend.replicas` больше 1 он окажется в логе случайного пода, и команда выше (поток одного Deployment) его может не показать. На время этого варианта уменьшите backend до одной реплики или настройте почтовый транспорт.

- **Фиксированный код для локальной проверки.** Задайте `backend.config.appEnv: development` в файле значений и `GOOSAR_DEV_VERIFICATION_CODE=888888` в Secret, затем выполните `helm upgrade` и перезапустите backend. При `APP_ENV=production` код игнорируется.

  ```bash
  helm upgrade goosar oci://ghcr.io/adanman/charts/goosar \
    --version <версия-chart> \
    -n goosar \
    -f my-values.yaml --set backend.config.appEnv=development
  kubectl -n goosar patch secret goosar-secrets --type=merge \
    -p '{"stringData":{"GOOSAR_DEV_VERIFICATION_CODE":"888888"}}'
  kubectl -n goosar rollout restart deploy/goosar-backend
  ```

`ALLOW_SIGNUP` и `DISABLE_WORKSPACE_CREATION` живут в `backend.config.*` как `allowSignup` и `disableWorkspaceCreation`. После `helm upgrade` под backend перекатывается автоматически (меняется хэш ConfigMap), а web читает оба значения из `/api/config` во время работы, пересборка не нужна.

Не задавайте `GOOSAR_DEV_VERIFICATION_CODE` на доступном из сети экземпляре: любой, кто знает адрес почты, войдёт с этим кодом.

### Шаг 6. CLI и демон

Демон работает на вашей машине, не в кластере. Поставьте CLI и CLI агента, как в разделе «Шаг 3. CLI и демон», и направьте CLI на хосты Ingress:

```bash
goosar setup self-host \
  --server-url http://api.goosar.example.local \
  --app-url http://goosar.example.local
```

На машине с демоном должны быть те же записи `/etc/hosts` или DNS, что и в шаге 1.

### Обновление и удаление chart

Общий порядок обновления описан в разделе «Обновление». Для Helm достаточно следующего.

Чтобы подтянуть свежие образы, не меняя версию chart, при значениях с изменяемым тегом `latest`:

```bash
kubectl -n goosar rollout restart deploy/goosar-backend deploy/goosar-frontend
```

Чтобы перейти на конкретный релиз, обновите chart до соответствующей версии; он по умолчанию берёт образы с тем же тегом:

```bash
helm upgrade goosar oci://ghcr.io/adanman/charts/goosar \
  --version <версия-chart> \
  -n goosar \
  -f my-values.yaml
```

Чтобы задать теги образов отдельно от версии chart, укажите их в файле значений (`images.backend.tag` и `images.frontend.tag`) и выполните ту же команду. Откат при неудачном обновлении:

```bash
helm -n goosar rollback goosar
```

Backend и in-chart Postgres работают не от root: backend с `runAsNonRoot`, uid 1001 и корневой ФС только для чтения, Postgres с uid 999. Тома, записанные прежними версиями от root, chart исправляет через `fsGroup` (1001 и 999), если StorageClass его учитывает. Если нет, выполните `chown` тома из временного root-пода либо на время задайте `backend.podSecurityContext: {}` (для Postgres — `postgres.podSecurityContext: {}` и `postgres.securityContext: {}`). Развёртывания на S3 и внешний Postgres (`postgres.external.enabled: true`) это не затрагивает.

Удаление:

```bash
# Убрать workloads, сохранив PVC и Secret
helm -n goosar uninstall goosar

# Удалить всё, включая данные PostgreSQL и загрузки
kubectl delete namespace goosar
```

## Локальный стенд разработчика

Локальный стенд на одной машине: Docker-стек self-host плюс десктоп-клиент, подключённый к нему. Это самый быстрый способ попробовать продукт целиком, воспроизвести ошибку на конкретном релизе или разрабатывать против настоящего backend, не затрагивая Goosar Cloud. Нужны Docker с Docker Compose и checkout репозитория. Для боевой установки (CLI, демон, Kubernetes, TLS) идите по разделам выше, для закрытых сетей — «Закрытый контур (offline-поставка)».

### Шаг 1. Запуск стека

```bash
git clone <адрес-репозитория> goosar
cd goosar
make selfhost
```

`make selfhost` при первом запуске создаёт `.env` из `.env.example` (со случайными `JWT_SECRET`, `POSTGRES_PASSWORD`, `GOOSAR_VCS_SECRET_KEY`), скачивает официальные образы, поднимает стек и ждёт проверки здоровья backend.

Порты по умолчанию (`BACKEND_PORT` и `FRONTEND_PORT` в `.env`, оба привязаны только к `127.0.0.1`):

- Frontend: http://localhost:3001
- Backend API: http://localhost:8081

**Какая версия поднимется.** При первом запуске `make selfhost` закрепляет `GOOSAR_IMAGE_TAG` на новейшем теге `v*` из вашего клона, поэтому свежий checkout разворачивает последний известный ему релиз. Отсюда два следствия:

- если клон старый, выполните `git fetch --tags` до первого запуска: закрепление берётся из локальных тегов;
- явное значение всегда побеждает: `GOOSAR_IMAGE_TAG=<тег> make selfhost` разворачивает именно этот тег (переменная оболочки приоритетнее `.env` в Compose). Существующий `.env` не переписывается; если его тег отстаёт от новейшего локального, `make selfhost` предупредит.

### Шаг 2. Первый вход

Откройте http://localhost:3001 и введите почту. На стенде почта не настроена, поэтому **код подтверждения никуда не отправляется**: он генерируется на сервере и печатается в лог контейнера backend (для этого в `.env` нужен `APP_ENV=development`, см. «Шаг 2. Вход» выше):

```bash
docker compose -f docker-compose.selfhost.yml logs backend | grep "Verification code"
```

Найдите строку вида `[DEV] Verification code for you@example.com: 123456` и введите код в браузере. Код печатает контейнер **backend**, а не web. Если в `.env` настроен `RESEND_API_KEY` или SMTP, коды уходят по почте, а не в лог: проверьте ящик или уберите почтовый транспорт и перезапустите backend.

### Шаг 3. Подключение десктоп-клиента

Десктоп-приложение подключается к локальному backend **прямо с экрана входа**, файлы настроек не нужны:

1. Запустите приложение. На экране входа в строке «Сервер» показан текущий адрес (по умолчанию — Cloud).
2. Нажмите «Изменить», введите `http://localhost:8081` и нажмите «Подключиться».
3. Перед сохранением приложение проверяет адрес. Если сервер недоступен, проверка называет конкретную причину (сервер не запущен, неверный порт, прокси на маршруте, нет сети), а не отказывает уже на входе.
4. Когда проверка прошла, адрес сохраняется, и приложение перезагружается уже с локальным backend. Войдите тем же способом «почта и код из лога», что в шаге 2.

Файл `~/.goosar/desktop.json` тоже существует, но это способ **предварительно настроить** машину до первого запуска (например, при управляемой раздаче), а не обычный путь для стенда разработчика.

### Шаг 4 (необязательно). Локальное хранилище пакетов

По умолчанию точки выдачи пакетов отвечают `503 "provisioning is not configured"`, всё остальное работает. Шаг нужен только чтобы отдавать skills, MCP-серверы и среды выполнения со своего стенда.

Самый короткий путь:

```bash
make selfhost-packages INDEX_URL=https://packages.example.test/catalog/index.json
```

Команда описана в разделе «Каталог пакетов» выше. То же вручную: включите хранилище в `.env` и пересоздайте backend:

```bash
# .env
GOOSAR_PROVISIONING_STORE=local
```

```bash
docker compose -f docker-compose.selfhost.yml up -d --force-recreate backend
```

Затем загрузите каталог в том загрузок backend. Проект Compose называется `goosar` (`name: goosar` в `docker-compose.selfhost.yml`), поэтому том — `goosar_backend_uploads`. Из корня репозитория:

```bash
docker run --rm \
  -v "$(pwd)/offline:/offline" \
  -v goosar_backend_uploads:/store \
  -w /offline \
  ubuntu:24.04 \
  bash -c 'apt-get update -qq && apt-get install -y -qq curl >/dev/null && bash ./load-provisioning-packages.sh --index-url https://packages.example.test/catalog/index.json --store-dir /store'
```

Выбор образа важен: загрузчику нужны `curl` и GNU `sha256sum`. `ubuntu:24.04` с установленным `curl` подходит, минимальный `bash:5.2` без `curl` — нет.

Скрипт сверяет каждый пакет с sha256 и размером из индекса и раскладывает пакеты в структуру `provisioning/<name>/<version>/`, которую читает backend. Перезапуск после загрузки не нужен: хранилище читается на каждый запрос. Пока `catalog.json` нет, включённое хранилище означает «ничего не опубликовано», а не ошибку: клиенты видят пустой каталог, и онбординг идёт дальше без пакетов.

### Если что-то не работает

- **Порты заняты.** Другой checkout или приложение держит `3001` или `8081`. Измените `BACKEND_PORT` и `FRONTEND_PORT` в `.env` и запустите с переопределением из оболочки: `PORT=<новый порт backend> make selfhost`. Проверка готовности в Makefile обращается к `localhost:${PORT:-8081}` из окружения, а не из `.env`, и без переопределения здоровый стек ошибочно назван «ещё стартует». Клиент после этого должен подключаться к новому порту backend.
- **Стенд на старой версии.** Закрепление в `.env` отстаёт (см. «Какая версия поднимется»). Посмотрите, что запущено: `docker compose -f docker-compose.selfhost.yml images`, задайте нужный `GOOSAR_IMAGE_TAG` в `.env` и выполните `docker compose -f docker-compose.selfhost.yml pull && docker compose -f docker-compose.selfhost.yml up -d`.
- **Каталог пакетов пуст.** См. шаг 4: загрузите каталог, чтобы появилось содержимое.
- **В логах нет кода подтверждения.** См. шаг 2: его печатает контейнер backend, и только при `APP_ENV=development`.
- **Backend или frontend не поднимаются.** `docker compose -f docker-compose.selfhost.yml logs backend` и `... logs frontend`.

## Конфигурация

Вся настройка сервера делается переменными окружения. За основу возьмите `.env.example` из корня репозитория. Изменения читаются при старте: после правки перезапустите бэкенд (или стек Compose). Пересборка образов нужна только для переменных `NEXT_PUBLIC_*`, которые вшиваются на этапе сборки веб-образа.

Часть значений по умолчанию зависит от профиля поставки `GOOSAR_DELIVERY_PROFILE`: `cloud` (по умолчанию) или `perimeter`. Профиль `perimeter` описывает закрытый контур, где все обновления доставляет оператор, а лишние каналы во внешнюю сеть закрыты. Любое другое значение — ошибка при старте бэкенда. Профиль объявляется всем клиентам через `/api/config` (поле `delivery_profile`, на `cloud` оно не возвращается). Скрипт начальной настройки (`make selfhost`, `install.sh --with-server`) записывает `perimeter` в новый `.env`. Ниже у каждой переменной отмечено, чем различаются профили.

### Обязательные переменные

| Переменная | Описание | Пример |
| --- | --- | --- |
| `DATABASE_URL` | Строка подключения к PostgreSQL | `postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable` |
| `JWT_SECRET` | Секрет подписи токенов, длинная случайная строка. При `APP_ENV=production` бэкенд **не запускается**, если значение пустое или совпадает с известной заглушкой; `docker-compose.selfhost.yml` тоже требует его на уровне Compose. | `openssl rand -hex 32` |
| `FRONTEND_ORIGIN` | Адрес, по которому отдаётся веб-клиент. Используется для CORS и для ссылок в письмах со входом и приглашениями. Без него письма содержат только код и текст приглашения, без ссылки. | `https://app.example.com` |

Для одного публичного адреса достаточно задать `PUBLIC_HOST=<имя-хоста-или-origin>` в `.env`. Тогда `make selfhost` выведет из него `FRONTEND_ORIGIN`, `GOOSAR_APP_URL`, `GOOSAR_PUBLIC_URL` и `CORS_ALLOWED_ORIGINS` (к голому имени хоста добавляется `https://`). Явно заданные значения всегда побеждают: заменяются только стандартные значения из шаблона. `GOOSAR_TRUSTED_PROXIES` вручную: это список CIDR вашего обратного прокси, из имени хоста его не вывести.

### Пул соединений с БД

Значения по умолчанию подходят большинству установок; меняйте их только для крупных или стеснённых развёртываний. Приоритет (от высшего): переменная окружения, затем параметры `pool_*` в `DATABASE_URL`, затем значение по умолчанию.

| Переменная | Описание | По умолчанию |
| --- | --- | --- |
| `DATABASE_MAX_CONNS` | Максимум соединений pgxpool на один под. Произведение `число подов × DATABASE_MAX_CONNS` держите заметно ниже `max_connections` в PostgreSQL. Если перед базой стоит пулер (PgBouncer, RDS Proxy, Supavisor), значение можно поднять. | `25` |
| `DATABASE_MIN_CONNS` | Прогретые соединения на под. Автоматически ограничивается сверху `DATABASE_MAX_CONNS`. | `5` |

### Почта

Goosar поддерживает два транспорта. Если задан `SMTP_HOST`, используется SMTP; иначе `RESEND_API_KEY`. Без обоих коды подтверждения печатаются в журнал сервера (только при `APP_ENV`, отличном от `production`).

**Вариант A: Resend** (удобен для облачных развёртываний).

| Переменная | Описание |
| --- | --- |
| `RESEND_API_KEY` | Ключ API Resend |
| `RESEND_FROM_EMAIL` | Адрес отправителя (по умолчанию `noreply@goosar.ru`) |

**Вариант B: SMTP-релей** (для собственных серверов и закрытых сетей). Подходит, если сервер не выходит в интернет или у вас уже есть внутренний релей (Exchange, Postfix и т. п.).

| Переменная | Описание | По умолчанию |
| --- | --- | --- |
| `SMTP_HOST` | Имя хоста релея. Заданное значение включает режим SMTP. | — |
| `SMTP_PORT` | Порт SMTP | `25` |
| `SMTP_USERNAME` | Пользователь; оставьте пустым для релея без аутентификации | — |
| `SMTP_PASSWORD` | Пароль | — |
| `SMTP_TLS` | Режим TLS. `implicit` (синонимы `smtps`, `ssl`) включает TLS сразу при подключении; порт `465` включает его автоматически. Пусто или `starttls` — переход на TLS через STARTTLS. | `starttls` |
| `SMTP_TLS_INSECURE` | `true` отключает проверку сертификата (самоподписанные сертификаты и частный CA) | `false` |
| `SMTP_FROM_EMAIL` | Адрес отправителя. **Обязателен при заданном `SMTP_HOST`**: без него (и без `RESEND_FROM_EMAIL`) сервер не запустится. | — |
| `SMTP_EHLO_NAME` | Имя для EHLO/HELO. Задайте настоящее FQDN, если строгий релей отклоняет приветствие по умолчанию. | имя хоста машины |

Если не настроен ни Resend, ни SMTP, доставить код входа нечем. При `APP_ENV=production` (значение по умолчанию в Docker и Helm) и в профиле `perimeter` сервер поэтому **отказывает во входе**: `/api/auth/send-code` отвечает `503` с телом `{"code":"email_not_configured"}`, а страница входа просит обратиться к администратору. Иначе пользователю сообщили бы об успехе, а код увидел бы только оператор в журнале. При другом `APP_ENV` код попадает в журнал бэкенда, и его можно скопировать оттуда (только для одной реплики: код окажется в журнале того пода, что принял запрос).

Фиксированный код для локальных тестов (например, `888888`) включается только явно: `GOOSAR_DEV_VERIFICATION_CODE=888888` в `.env` при `APP_ENV` вне `production`. Стек Docker закрепляет `APP_ENV=production`, поэтому там переменная игнорируется. **Никогда не включайте фиксированный код на доступном извне экземпляре.**

### Регистрация

| Переменная | Описание |
| --- | --- |
| `ALLOW_SIGNUP` | `false` закрывает регистрацию новых пользователей на закрытом экземпляре |
| `ALLOWED_EMAIL_DOMAINS` | Необязательный список разрешённых доменов почты через запятую |
| `ALLOWED_EMAILS` | Необязательный список разрешённых адресов почты через запятую |
| `DISABLE_WORKSPACE_CREATION` | `true` заставляет `POST /api/workspaces` отвечать `403` всем: пользователи могут только вступать в рабочие пространства по приглашению |

Изменения вступают в силу после перезапуска бэкенда. Веб-клиент читает `ALLOW_SIGNUP` и `DISABLE_WORKSPACE_CREATION` из `/api/config` во время работы, поэтому пересобирать его не нужно.

`ALLOW_SIGNUP=false` блокирует создание новых учётных записей, но не мешает уже вошедшему пользователю создать ещё одно рабочее пространство. Если на экземпляре всё содержимое должно быть видно администратору платформы, закройте и этот путь через `DISABLE_WORKSPACE_CREATION=true`. Рекомендуемая последовательность:

1. Запустите экземпляр с `DISABLE_WORKSPACE_CREATION=false` (значение по умолчанию).
2. Войдите администратором и создайте общее рабочее пространство.
3. Задайте `DISABLE_WORKSPACE_CREATION=true` и перезапустите бэкенд. Если нужно закрыть и регистрацию, одновременно задайте `ALLOW_SIGNUP=false`.
4. Дальше новые люди приходят только по приглашению: кнопка создания рабочего пространства скрыта, прямой вызов API вернёт `403`.

`ALLOW_SIGNUP=false` блокирует **любую** новую учётную запись, в том числе у человека с уже отправленным приглашением. Если приглашённые должны иметь возможность зарегистрироваться, но не создавать свои рабочие пространства, оставьте `ALLOW_SIGNUP=true` (при необходимости вместе с `ALLOWED_EMAIL_DOMAINS` или `ALLOWED_EMAILS`) и включите только `DISABLE_WORKSPACE_CREATION=true`.

### Источники skills

| Переменная | Описание |
| --- | --- |
| `GOOSAR_SKILL_SOURCES` | Список включённых внешних источников skills через запятую: `clawhub`, `github`, `skillssh`; `none` отключает все. Если переменная не задана, действует значение профиля: на `cloud` включены **все**, на `perimeter` **все отключены**. Любое другое значение — ошибка при старте. |

Внешние источники — сторонние узлы, с которых бэкенд получает содержимое skills по запросу пользователей: ClawHub (поиск пересылает запрос пользователя на `clawhub.ai`, а импорт скачивает и позже исполняет чужие инструкции), `skills.sh` и `github.com`. В закрытом контуре каждый из них — канал выхода наружу и риск для цепочки поставки, поэтому профиль `perimeter` отключает их все, пока оператор явно не включит нужные (например, `GOOSAR_SKILL_SOURCES=github`).

Что происходит с отключённым источником:

- **Чёткий отказ, а не сбой.** `GET /api/skills/search` и `POST /api/skills/import` отвечают `403` с сообщением, где названа `GOOSAR_SKILL_SOURCES`, до того как с сервера уйдёт хоть один запрос.
- **Интерфейс скрывает лишнее.** `/api/config` отдаёт список включённых источников в поле `skill_sources` (на неявном значении `cloud` поле опускается), а диалог создания skill убирает карточки импорта по URL для отключённых источников.
- **`GITHUB_TOKEN` защищён.** Пока источник `github` отключён, сервер не прикладывает `GITHUB_TOKEN` ни к одному запросу за skills, даже если другой включённый источник (skills.sh обращается к API GitHub) ведёт туда.
- **Шаблоны агентов работают без сети.** Все skills встроенных шаблонов агентов вшиты в бинарник сервера, поэтому создание агента из шаблона не делает сетевых запросов ни в одном профиле. Живая выборка с GitHub остаётся только для skill шаблона без вшитой копии и только при включённом `github`.

Локальный импорт (`goosar skill import --file`, локальные skills среды выполнения) не относится к внешним источникам и доступен всегда.

### Allowlist периметра

Две серверные политики ограничивают, куда могут обращаться агенты, и одна — какие среды выполнения могут брать задачи. Действует то же правило, что и у профиля поставки: если переменные не заданы, профиль `cloud` ведёт себя так, будто политик нет, а профиль `perimeter` работает **по закрытому умолчанию** с встроенной корпоративной базой, которую переменные расширяют.

| Переменная | Описание |
| --- | --- |
| `GOOSAR_MCP_ALLOWED_HOSTS` | Имена хостов через запятую, на которые могут ссылаться записи MCP в `mcp_config` агента. Сравнивается имя хоста; порты и схемы игнорируются (записи вида `host:port` и полные URL сводятся к имени хоста). Проверяется `url` удалённых записей **и** каждый URL `http(s)://` внутри элементов `args` и значений `env`/`environment`: команда-ретранслятор вроде `mcp-proxy` передаёт настоящий адрес назначения в `args`. Петлевые адреса машины (`127.0.0.1`, `localhost`, `::1`, например локальный прокси px) исключены. |
| `GOOSAR_MCP_ALLOWED_COMMANDS` | Исполняемые файлы через запятую, которые могут запускать `stdio`-записи MCP. Сравнивается имя файла (запись `/opt/tools/mcp-proxy` означает `mcp-proxy`). При активном списке команда в самой записи конфигурации обязана быть **голым именем**: команды с путём (`/tmp/x/mcp-proxy`) отклоняются, потому что любой путь может оказаться ссылкой на любой бинарник, и одно сравнение по имени обходилось бы тривиально. |
| `GOOSAR_ALLOWED_PROVIDERS` | Коды сред выполнения через запятую (например, `runtime-b,runtime-c`), которые могут обслуживать агентов и брать задачи. Неизвестный код — ошибка при старте. Действующий список отдаётся в `/api/config` как `allowed_providers` (при отсутствии ограничений поле опускается), чтобы клиенты скрыли лишнее. |

Проверяются оба формата сохранённого `mcp_config`: конверт `{"mcpServers": {...}}` в «стандартном» виде и «родной» верхнеуровневый словарь `{"mcp": {...}}` (команда — строкой или массивом).

Итоговая политика по профилям:

| | `cloud` (или профиль не задан) | `perimeter` |
| --- | --- | --- |
| Переменная не задана | Без ограничений, поведение не меняется | Закрытая база: набор корпоративных пресетов MCP (`mcp-atlassian`, `mcp-server-fetch`, `mcp-proxy`, `ewsmcp`, `mcp-server-b24` и их корпоративные хосты) для списков MCP; для сред выполнения только `runtime-b` |
| Переменная задана | Ровно перечисленные записи | База **плюс** перечисленное (база остаётся разрешённой всегда, поэтому пресеты онбординга и канал встроенной среды выполнения не сломаются от слишком узкого списка) |

Проверки идут в несколько слоёв:

- **При записи.** Создание или изменение агента с записью `mcp_config` вне списков возвращает `400` с именем переменной, которую нужно изменить. Выбор среды выполнения, чей провайдер вне `GOOSAR_ALLOWED_PROVIDERS`, возвращает `403`. Проверка идёт по открытому тексту документа до шифрования ключом `GOOSAR_MCP_SECRET_KEY`.
- **При выдаче задачи.** Путь получения задачи повторно проверяет итоговый (расшифрованный и склеенный с наложениями) `mcp_config`, отбрасывает несоответствующие записи с предупреждением на сервере и вовсе пропускает среды выполнения запрещённых провайдеров. Существующие среды и агенты на других провайдерах остаются видимыми и редактируемыми, но задач не получают. Это страховка для записей, созданных до появления политики.
- **При запуске на машине.** Действующие списки приходят к демону вместе с задачей (`mcp_policy`), и демон применяет ту же проверку к серверам, которые подмешивает из собственных файлов машины: пользовательские файлы MCP-конфигурации самих CLI и `.mcp.json` репозитория. Порядок слияния: **политика развёртывания > библиотека рабочего пространства > наложение задачи > локальная конфигурация среды выполнения**. Заново проверяется только локальный слой: остальные три уже отфильтрованы на сервере. Отброшенная запись попадает в журнал демона по имени и в инвентарь эффективной конфигурации среды выполнения, который читает администратор развёртывания, как `blocked: <name> (source local)`.
- **Интерфейс.** `/api/config` отдаёт эффективный список провайдеров (`allowed_providers`).

#### Общие MCP-серверы работают с учётными данными инициатора

MCP-сервер из библиотеки рабочего пространства или развёртывания общий как **определение**: команда, URL и схема учётных данных. Сами значения секретов не общие: каждый пользователь запечатывает свои ключом `GOOSAR_MCP_SECRET_KEY`. Запуск собирается с учётными данными **того, для кого он выполняется** (инициатора задачи), а не владельца агента и не администратора библиотеки.

- **Участник, который внёс свои значения,** получает сервер с этими значениями. Его доступ к Jira, Exchange или почтовому ящику — его собственный, и в журнале стороннего сервиса записан он.
- **Участник, который не внёс,** сервер не получает вовсе. Он исключается из запуска с предупреждением `missing_fields` (называются только ключи полей), и интерфейс показывает тот же список недостающих полей. Чужие учётные данные подставляться не будут.
- **Запуск без человека** (расписание автопилота, вебхук, самостоятельное продолжение агента) не имеет инициатора, поэтому использует учётные данные владельца агента: агент выполняет собственную постоянную работу владельца, другого субъекта нет.

Поэтому `permission_mode=public_to_workspace` у агента с общими серверами безопасен с точки зрения «одалживания» чужих учётных данных, но **не** способ дать команде доступ к одному почтовому ящику. Для настоящего общего доступа заведите в стороннем сервисе служебную учётную запись, а человек, который будет ею управлять, внесёт её данные как свои значения для сервера. Не вносите личный токен в сервер, которым пользуются многие: всё, что агент сделает с этим токеном, будет приписано вам.

Известные ограничения этого слоя (принятый остаточный риск):

- **Контроль на этапе конфигурации.** Проверка хостов работает в разборе URL на Go при записи и выдаче конфигурации; MCP-клиент может разобрать необычный URL иначе, а сервер на разрешённом хосте может перенаправить запрос дальше. Allowlist управляет тем, куда конфигурация *указывает*, но не фильтрует сетевой выход: авторитетным остаётся сетевой периметр.
- **Слой запуска на машине знает 7 из 18 сред выполнения.** Для остальных демон не знает, где хранится локальная конфигурация MCP, и не может её отфильтровать; такой запуск пишет в журнал `deployment MCP allowlist is not enforced on runtime-local servers for this provider`, а проверки при записи и выдаче продолжают действовать для документа, которым управляет Goosar.
- **Общий сервер без `credential_schema` — общий секрет.** Слой личных учётных данных подставляет только значения, объявленные в схеме. Если автор библиотеки вписал токен прямо в `env` определения, подставлять нечего, и все, кому назначен сервер, работают под этим токеном.
- **Интерпретаторы и ретрансляторы сводят список команд на нет.** Добавление в `GOOSAR_MCP_ALLOWED_COMMANDS` любого интерпретатора или универсального запускателя (`npx`, `uvx`, `python`, `node`, `bash`, `docker`, `sh` и подобных) фактически разрешает произвольное исполнение и выход в сеть: настоящая программа придёт через `args`. Проверка URL в `args` и `env` сужает лазейку, но не закрывает её. Разрешайте только выделенные бинарники MCP-серверов.

### Граница исполнения агента

**Сначала главное: агент выполняется с полными правами пользователя ОС, от имени которого запущена среда выполнения.** Демон запускает CLI провайдера обычным дочерним процессом: тот же пользователь, тот же `$HOME`, те же SSH-ключи, тикет Kerberos, примонтированные ресурсы и учётные данные провайдера. Собственные диалоги подтверждения CLI при этом отключены, потому что задача должна завершаться без человека за клавиатурой. Goosar не создаёт вокруг процесса ни контейнера, ни пространства имён, ни профиля seccomp, и ничего в этом разделе не следует читать как песочницу. Изоляция — дело операционной системы и оператора.

Полная политика (что достаёт процесс агента, что обеспечивает демон и что вы, позиция по prompt injection, принятый остаточный риск) ведётся отдельно. Демон сам обеспечивает одну переменную:

| Переменная | Описание |
| --- | --- |
| `GOOSAR_ALLOWED_WORKDIR_ROOTS` | Каталоги (через разделитель списка путей ОС или запятую), внутри которых может оказаться рабочий каталог задачи. **Пусто — без ограничений:** для настольной и облачной конфигураций действует прежний чёрный список, который запрещает только корни дисков, `/`, `/Users`, `/home` и домашний каталог пользователя. Заданная на демоне сервера, переменная заставляет демон **отказаться запускать** любую задачу, чей настроенный каталог разрешается вне корней (`local_directory_error`, в сообщении названа переменная). |

Что важно при настройке:

- Сравнение идёт по **компонентам пути**, а не по префиксу строки: `/srv/projects-old` не лежит внутри `/srv/projects`.
- Сравнивается **путь с раскрытыми символьными ссылками** и только он: `/srv/projects/escape -> /home/user/.ssh` буквально лежит внутри корня, и принять буквальный путь как альтернативу значило бы вернуть границу обратно.
- **Закрыто при опечатке.** Корень, не существующий на этой машине, просто не входит в разрешённое множество; если не разрешается ни один, отказывают всем. Переменная никогда не расширяется до «где угодно».
- Она ограничивает, **где задача стартует**, а не что процесс делает потом. Агент внутри корня по-прежнему читает и пишет всё, что доступно его пользователю ОС.

Отдельно демон отвергает набор ключей окружения в `custom_env` агента, поскольку `custom_env` — конфигурация, которую может править второй человек в рабочем пространстве. Это хуки динамического загрузчика и интерпретаторов (`LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`, `NODE_OPTIONS`, `BASH_ENV`, `PYTHONSTARTUP`, `GIT_SSH_COMMAND` и др.), означающие «выполнить этот код в процессе агента», и базовые URL провайдеров (`ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL` и др.), направляющие ключ API CLI на выбранный автором адрес. Сами **ключи API** провайдеров устанавливать можно: для этого `custom_env` и нужен. Полный список — в §4 документа политики.

### Хранилище файлов

Для вложений и загрузок настройте S3 и (по желанию) CloudFront. Если `S3_BUCKET` не задан, файлы лежат в `LOCAL_UPLOAD_DIR` (по умолчанию `./data/uploads`; в стеке Docker это том `backend_uploads`).

| Переменная | Описание |
| --- | --- |
| `S3_BUCKET` | Только имя бакета (например, `my-bucket`). **Не** добавляйте суффикс `.s3.<region>.amazonaws.com`: публичный URL сервер строит из `S3_BUCKET` и `S3_REGION`. |
| `S3_REGION` | Регион AWS; должен совпадать с реальным регионом бакета (используется и для подписи, и для публичных URL). **Значения по умолчанию нет:** при заданном `S3_BUCKET` бэкенд не запустится, пока адрес назначения не указан явно, то есть `S3_REGION` для настоящего AWS S3 или `AWS_ENDPOINT_URL` для S3-совместимого хранилища. |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Статические учётные данные. Если обе не заданы, используется стандартная цепочка учётных данных AWS SDK. |
| `AWS_ENDPOINT_URL` | Свой S3-совместимый эндпоинт (MinIO, R2, B2, RustFS). При его наличии по умолчанию используются URL в стиле path-style. |
| `S3_USE_PATH_STYLE` | Режим адресации. Пусто — значение по умолчанию (`true` при заданном `AWS_ENDPOINT_URL`, `false` для AWS S3). `false` нужен провайдерам, которым требуются URL в стиле virtual-hosted. |
| `ATTACHMENT_DOWNLOAD_MODE` | Как отдаются вложения: `auto` (по умолчанию), `cloudfront`, `presign` или `proxy`. `proxy` нужен для закрытых бакетов за адресами, доступными только внутри Docker или VPC (например, `http://rustfs:9000`). |
| `ATTACHMENT_DOWNLOAD_URL_TTL` | Срок жизни подписанных URL CloudFront и предподписанных URL S3 (по умолчанию `30m`) |
| `CLOUDFRONT_DOMAIN` | Домен раздачи CloudFront; если задан, публичные URL используют этот хост вместо хоста S3 |
| `CLOUDFRONT_KEY_PAIR_ID` | Идентификатор пары ключей CloudFront для подписанных URL |
| `CLOUDFRONT_PRIVATE_KEY` | Закрытый ключ CloudFront (PEM) |

### Cookies

| Переменная | Описание |
| --- | --- |
| `COOKIE_DOMAIN` | Необязательный атрибут `Domain` для cookies сессии и CloudFront. **Оставьте пустым** при одном хосте (localhost, IP в локальной сети или единственное имя). Задавайте, только если веб-клиент и бэкенд расположены на разных поддоменах одного зарегистрированного домена (например, `.example.com`). **Не указывайте IP-адрес:** RFC 6265 запрещает IP-адреса в атрибуте `Domain`, и браузеры отбрасывают такие `Set-Cookie`. |

Флаг `Secure` у cookies сессии выводится автоматически из схемы `FRONTEND_ORIGIN`: для HTTPS cookies защищённые, для обычного HTTP (локальная сеть) — нет, иначе браузер не смог бы их сохранить.

### Сервер

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `PORT` | `8081` | Порт бэкенда (это значение самого бинарника; `.env.example` и стек Compose тоже публикуют `8081` на хосте) |
| `METRICS_ADDR` | пусто | Необязательный слушатель метрик Prometheus, например `127.0.0.1:9090`. Подробности в разделе «Мониторинг». |
| `FRONTEND_PORT` | `3001` | Порт веб-клиента (значение dev-сервера; `.env.example` и стек Compose публикуют `3001`) |
| `CORS_ALLOWED_ORIGINS` | значение `FRONTEND_ORIGIN` | Список разрешённых origin через запятую. Управляет **и** списком CORS для HTTP, **и** проверкой заголовка `Origin` для WebSocket. Если origin браузера здесь не указан (и это не `localhost`), обновление WebSocket отклоняется с `403`, и живые обновления перестают работать до ручного обновления страницы. |
| `LOG_LEVEL` | `info` в production, иначе `debug` | Уровень журнала: `debug`, `info`, `warn`, `error` |
| `GOOSAR_MIN_DAEMON_VERSION` | встроенный минимум (`0.2.21`) | Самая старая версия CLI и демона, которой сервер выдаёт задачи. Демон ниже минимума получает на каждую выдачу `426 daemon_too_old` с подсказкой об обновлении; регистрация, heartbeat и отчёт о результате продолжают работать. Значение `none` отключает проверку. Читается на каждый запрос. Не распознанная или отсутствующая версия клиента не блокируется. |
| `GOOSAR_RUNTIME_RECONNECT_GRACE` | `3h` | Сколько офлайн-среда выполнения может переподключаться, прежде чем её задачи завершатся с `runtime_offline`. Значения ниже 150 с поднимаются до окна свежести heartbeat. |

### Лимиты памяти контейнеров

Стек Compose ограничивает каждый сервис. Без лимита один вышедший из-под контроля запрос или большая загрузка позволяет OOM-killer хоста выбрать жертву сам. На небольшой ВМ это обычно PostgreSQL, который пропадает без единой строки в журналах Compose.

Compose применяет лимиты как `mem_limit`: ключ `deploy.resources` действует только в swarm. Значения по умолчанию совпадают с лимитами `1Gi` в чарте Helm, чтобы стенд вёл себя одинаково в обоих окружениях.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `POSTGRES_MEM_LIMIT` | `1g` | Лимит памяти встроенного контейнера PostgreSQL |
| `BACKEND_MEM_LIMIT` | `1g` | Лимит памяти контейнера бэкенда |
| `BACKEND_GOMEMLIMIT` | `900MiB` | Мягкая цель кучи для сборщика мусора Go. Держите её **ниже** `BACKEND_MEM_LIMIT` (около 90%): тогда рантайм чаще собирает мусор при приближении к цели, а не упирается в лимит cgroup и не умирает без следа на стороне Go. Поднимайте обе вместе. |
| `FRONTEND_MEM_LIMIT` | `1g` | Лимит памяти контейнера Next.js |
| `FRONTEND_NODE_OPTIONS` | `--max-old-space-size=900` | Ограничение кучи V8, те же соображения, что и для `BACKEND_GOMEMLIMIT` |

Пересборка образов не нужна. Обычный `docker compose restart` **не** применяет изменённый лимит: контейнер сохраняет прежний cgroup. Используйте `docker compose -f docker-compose.selfhost.yml up -d`, который пересоздаёт контейнеры с изменённой конфигурацией.

В Helm соответствующие значения: `postgres.resources`, `backend.resources`, `frontend.resources` (по умолчанию лимит `1Gi`, запрос `256Mi`) и `backend.config.goMemLimit` для цели кучи Go. `helm upgrade` перекатывает затронутые Deployment.

### Хранение и GC

Несколько задач работают внутри бэкенда по часовому таймеру (`server/cmd/server/hygiene_sweeper.go`): очистка аудита планировщика, уборка осиротевших загрузок, политика хранения и освобождение архивов экспорта. Все значения читаются при старте: нужен **перезапуск бэкенда, а не пересборка**. Длительности задаются в синтаксисе Go (`s`/`m`/`h`): `d` не единица измерения, поэтому 30 дней записываются как `720h`. Значение, которое не удаётся разобрать, попадает в журнал как предупреждение, и действует значение по умолчанию.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `GOOSAR_SCHEDULER_AUDIT_RETENTION` | `720h` (30 дней) | Как долго хранятся строки аудита `sys_cron_executions`. Планировщик пишет по строке на каждую тройку (задание, область, плановое время): при автопилоте с 5-минутным планом это около 100 тыс. строк в месяц. Строки в статусе `RUNNING` не удаляются, как бы давно ни было плановое время. `0` отключает очистку. |
| `GOOSAR_UPLOAD_GC_GRACE` | `168h` (7 дней) | Сколько вложение, не привязанное ни к задаче, ни к комментарию, ни к сообщению чата, остаётся нетронутым, прежде чем уборщик удалит строку и сохранённый объект. Композер сохраняет байты до появления строки-владельца, а черновики в браузере не истекают, поэтому окно должно переживать возврат к черновику после долгих выходных. `0` отключает уборку. |
| `GOOSAR_HYGIENE_SWEEP_INTERVAL` | `1h` | Как часто запускается каждая задача. Проход работает пачками (10 000 строк аудита, 500 сирот, 5 000 строк на таблицу политики хранения), поэтому большой исторический хвост разгребается за несколько проходов, а не одной долгой транзакцией. |

#### Политика хранения содержимого

Эти окна удаляют **содержимое**, и каждое по умолчанию равно `0`, то есть «хранить вечно». Это сознательно: вправе ли команда уничтожать закрытые задачи или историю чатов, решает уведомление оператора о сроках хранения, а продукт, поставляющий догадку, тихо удалил бы работу при обновлении. Задавайте окна, только когда знаете, что говорит ваша политика.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `GOOSAR_RETENTION_CHAT` | `0` | Возраст, с которого удаляется сессия чата с агентом вместе со всеми сообщениями |
| `GOOSAR_RETENTION_TASKS` | `0` | Возраст, с которого удаляется завершённый запуск агента со всей трассой (промпт, каждый вызов инструмента, каждый ответ) |
| `GOOSAR_RETENTION_CLOSED_ISSUES` | `0` | Возраст, с которого удаляется задача в завершённом состоянии вместе с комментариями, реакциями, метками, элементами входящих и активностью |
| `GOOSAR_RETENTION_ACTIVITY` | `0` | Возраст, с которого удаляются строки ленты активности |
| `GOOSAR_ATTACHMENT_PURGE_GRACE` | `168h` | Сколько сохранённый объект живёт после удаления строки вложения. Отсчёт идёт от **удаления**, а не от загрузки. `0` отключает освобождение и оставляет объекты на диске. |

Просроченные одноразовые коды входа (`verification_code`) удаляются на каждом проходе без настройки. Проход, который что-то удалил, пишет в журнал аудита одну строку `retention.purge` со счётчиками по таблицам; актор пустой намеренно: проход выполнила настроенная политика, а не человек.

`goosar_admin purge --dry-run` показывает, что удалила бы **текущая настроенная** политика (весь накопившийся объём по каждому окну, а не одну пачку), и ничего не меняет. Флаги `--chat=720h`, `--tasks=720h`, `--closed-issues=8760h`, `--activity=720h`, `--attachment-grace=168h` позволяют прикинуть окно до того, как фиксировать его в окружении.

#### Архивы экспорта рабочего пространства

`POST /api/workspaces/{id}/export` пишет tar.gz всего рабочего пространства в `GOOSAR_EXPORT_DIR`; уборщик освобождает готовые архивы старше `GOOSAR_EXPORT_RETENTION` и помечает ошибкой задания, чей исполнитель умер посреди работы.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `GOOSAR_EXPORT_DIR` | `<LOCAL_UPLOAD_DIR>/exports` (`./data/uploads/exports`) | Куда пишутся архивы. По умолчанию это **внутри тома загрузок**: это единственный записываемый, постоянный и уже попадающий в резервные копии путь у усиленного стенда (корневая ФС контейнера бэкенда доступна только для чтения). Указывайте другое место только на другом постоянном томе: задание, которое завершилось и потеряло файл после перезапуска контейнера, хуже отсутствия экспорта, потому что статус по-прежнему говорит `completed`. |
| `GOOSAR_EXPORT_TIMEOUT` | `2h` | Ограничение одного запуска. Незавершённое за это время задание помечается ошибкой, его слот на рабочее пространство освобождается. |
| `GOOSAR_EXPORT_MAX_BYTES` | не задан (без лимита) | Лимит размера одного архива. Запуск, превысивший его, завершается ошибкой с этой причиной, а не забивает том. |
| `GOOSAR_EXPORT_RETENTION` | `168h` (7 дней) | Сколько хранится готовый архив, прежде чем байты удаляются. Экспорт — полная копия персональных данных рабочего пространства на диске, поэтому срок у него самый короткий. `0` отключает освобождение. |
| `RATE_LIMIT_EXPORT` | `3` в час | Бюджет на пользователя для `GET /api/me/export` (экспорт по запросу субъекта данных) |

`goosar workspace export -o <каталог>` проводит весь процесс из CLI; `--wait=false` ставит задание и возвращается, не скачивая архив.

Вложение, чья строка-владелец **удалена**, отдельной уборки сирот не требует: вместе с владельцем удаляется и строка вложения. Но строка удаляется без объекта в хранилище, поэтому при удалении в журнал пишется запись `attachment_tombstone`, и объект освобождается, когда пройдёт `GOOSAR_ATTACHMENT_PURGE_GRACE`. Уборка сирот выше охватывает только загрузки, которые изначально так и не были привязаны.

#### Ручной проход

`goosar_admin gc-uploads` запускает ту же уборку вручную. Сначала используйте `--dry-run`: он перечисляет, что удалит настоящий проход, и ничего не меняет.

```bash
docker compose -f docker-compose.selfhost.yml exec backend ./goosar_admin gc-uploads --dry-run
docker compose -f docker-compose.selfhost.yml exec backend ./goosar_admin gc-uploads --grace=72h --limit=2000
```

Таймер и CLI пишут одинаковую структурированную строку (`upload GC: reclaimed orphaned attachments found=12 deleted=12 grace=168h0m0s dry_run=false`), так что один шаблон поиска покрывает оба канала. Команда отказывается работать, если хранилище не настроено (`S3_BUCKET` не задан, а локальный каталог загрузок нельзя создать): удаление строк оставило бы объекты без единой ссылки на них.

### Секреты в покое

Goosar шифрует столбцы с секретами отдельными ключами AES-256-GCM для каждой области. Ключ — 32 байта в base64; сгенерируйте его командой `openssl rand -base64 32`.

| Переменная | Что шифрует | Если не задана |
| --- | --- | --- |
| `GOOSAR_MCP_SECRET_KEY` | Документы `mcp_config` агентов (в них регулярно лежат учётные данные MCP-серверов: корпоративные PAT, bearer-токены шлюзов), а также секреты второго фактора. **Рекомендуется для любого развёртывания.** | `mcp_config` хранится **открытым текстом**, при старте в журнал пишется предупреждение; агенты продолжают работать. **В профиле `perimeter` отсутствие ключа — ошибка при старте:** открытые учётные данные в базе и во всех резервных копиях — как раз то, чему закрытый контур должен препятствовать. |
| `GOOSAR_VCS_SECRET_KEY` | Токены доступа и секреты вебхуков к Git-провайдерам (Forgejo, Gitea, GitLab) по рабочим пространствам. Интеграция дополнительно включается `GOOSAR_VCS_INTEGRATION_ENABLED=true` (стек Compose для самостоятельного размещения задаёт её). | Интеграция с VCS отключена (подключение и вебхук отвечают 503) |
| `GOOSAR_SLACK_SECRET_KEY` | Токены бота и приложения Slack по установкам | Интеграция со Slack отключена |

`make selfhost` и `install.sh --with-server` сами генерируют `GOOSAR_MCP_SECRET_KEY`.

Каждая из этих переменных принимает парную `<ПЕРЕМЕННАЯ>_PREVIOUS`: список **выведенных** ключей через запятую, которые принимаются только для расшифровки. Так ключ можно сменить, не осиротив уже зашифрованное. Пошаговая процедура, включая `goosar_admin rotate-secrets`, описана в разделе «Ротация ключей шифрования». Некорректная запись в списке `_PREVIOUS` — всегда ошибка, а не молчаливый пропуск.

О `GOOSAR_MCP_SECRET_KEY`:

- Задать ключ позже безопасно: новые записи шифруются сразу, а фоновая задача при старте зашифрует уже лежащие открытым текстом строки.
- Если убрать ключ, когда данные уже зашифрованы, значения станут нечитаемыми: интерфейс покажет их скрытыми, при выдаче задач они пропускаются (с ошибкой в журнале), пока ключ не вернётся. Сохранение нового `mcp_config` перезаписывает нечитаемое значение. Держите ключ стабильным.

### CLI и демон

Эти переменные задаются на машине каждого пользователя, а не на сервере.

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `GOOSAR_SERVER_URL` | `ws://localhost:8080/ws` | URL WebSocket для соединения демона с сервером. Стек Compose публикует бэкенд на `8081`, поэтому демонам обычно нужен `ws://localhost:8081/ws` (его записывает `goosar setup self-host --port 8081`). |
| `GOOSAR_APP_URL` | `http://localhost:3001` | Адрес веб-клиента для входа из CLI |
| `GOOSAR_DAEMON_POLL_INTERVAL` | `30s` | Резервный интервал опроса задач. Новые задачи доставляются через WebSocket почти мгновенно, опрос — только страховка. |
| `GOOSAR_DAEMON_HEARTBEAT_INTERVAL` | `15s` | Частота heartbeat |
| `GOOSAR_DAEMON_DRAIN_TIMEOUT` | `10m` | Сколько `goosar daemon stop`, `restart` и `SIGTERM` дают уже идущим задачам завершиться, прежде чем отказаться от ожидания. Ничего не отменяется: по истечении срока демон выходит без отчёта, а сервер возвращает работу в очередь сборщиком зависших задач. `goosar daemon stop --force` пропускает ожидание и отменяет идущие задачи. Неположительное или неразбираемое значение заменяется значением по умолчанию: ожидание нельзя выключить опечаткой. |
| `GOOSAR_DAEMON_AUTO_UPDATE` | зависит от профиля | `off` (также `false`, `0`, `no`) на машине со средой выполнения принудительно отключает опрос автообновления против любого сервера. |

Исполняемый файл и модель для каждой среды выполнения задаются тройкой переменных `GOOSAR_RUNTIME_<БУКВА>_PATH`, `_MODEL` и `_ARGS`, где буква — последняя буква кода среды (`runtime-c` даёт `C`). Пусто означает «искать CLI в `PATH` и следовать его собственной модели». Аргументы (`_ARGS`) поддерживают среды `C`, `D`, `E`, `Q`.

Буквы `A` … `R` соответствуют кодам `runtime-a` … `runtime-r`; имя исполняемого файла каждой среды указано в реестре `server/pkg/agent/runtimeregistry/runtimes.yaml` (поле `cliName`).

Пример: `GOOSAR_RUNTIME_E_PATH=/opt/tools/agent-e` и `GOOSAR_RUNTIME_E_MODEL=...`. Для некоторых сред модель определяется подпиской пользователя у внешнего сервиса, и переопределение может не учитываться.

## База данных

Goosar требует PostgreSQL 17 с расширением pgvector.

**Встроенная база.** `docker-compose.selfhost.yml` включает PostgreSQL; отдельная настройка не нужна.

**Собственный PostgreSQL.** Убедитесь, что расширение pgvector доступно:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

Задайте `DATABASE_URL` в `.env` и уберите сервис `postgres` из файла Compose.

### TLS для базы данных

**По умолчанию `sslmode=disable`.** Бэкенд и PostgreSQL стека Compose находятся в закрытой сети Docker, а встроенный образ `pgvector` поставляется без сертификатов, поэтому обязательный TLS «из коробки» сломал бы каждый существующий стенд. Переключатель — `POSTGRES_SSLMODE` в `.env`: из него Compose собирает `DATABASE_URL`.

Значения `sslmode` от слабого к сильному: `disable` (без TLS) → `require` (шифрование без проверки сертификата: защищает от пассивного перехвата, но не от активного MITM) → `verify-ca` → `verify-full` (шифрование, проверка цепочки сертификатов и имени хоста).

**Внешняя или управляемая база.** Укажите режим в `.env` и направьте `DATABASE_URL` на кластер:

```bash
POSTGRES_SSLMODE=require
```

Для `verify-full` добавьте CA самостоятельно: смонтируйте его в контейнер бэкенда и допишите к URL `&sslrootcert=/path/to/ca.crt`. Управляемые сервисы (RDS, Cloud SQL, Yandex Managed PostgreSQL) публикуют свои пакеты CA.

**Встроенный PostgreSQL по TLS.** Сгенерируйте частный CA и сертификат сервера для имени сервиса Compose, затем запустите стек с оверлеем TLS:

```bash
bash scripts/selfhost-pg-tls.sh          # пишет ./certs (в git-ignore)
docker compose -f docker-compose.selfhost.yml \
               -f docker-compose.selfhost.tls.yml up -d
```

Оверлей включает `ssl=on` в PostgreSQL, монтирует файлы сертификатов только для чтения по одному (PostgreSQL получает `server.crt` и `server.key`, бэкенд — только `ca.crt`) и подключается с `sslmode=verify-full&sslrootcert=/certs/ca.crt`. `certs/ca.key` остаётся на хосте и не попадает ни в один контейнер: им подписан сертификат, которому доверяет бэкенд, поэтому не копируйте его вместе со стендом и не кладите в резервные копии каталога. CN/SAN сертификата — `postgres`, имя сервиса, к которому обращается бэкенд, так что проверка имени хоста проходит. Чтобы пропустить проверку сертификата, задайте `POSTGRES_TLS_SSLMODE=require` (переключатель оверлея намеренно не `POSTGRES_SSLMODE`: тот равен `disable`, и оверлей тихо откатился бы на открытое соединение). Для ротации удалите `certs/`, повторно запустите скрипт и перезапустите стек.

Kubernetes: `postgres.sslMode` в значениях чарта относится к встроенному Deployment PostgreSQL (по умолчанию `disable`, по тем же причинам). При `postgres.external.enabled=true` весь `DATABASE_URL` берётся из вашего `existingSecret`, поэтому `?sslmode=require` (или `verify-full`) нужно вписать туда.

Проверка со стороны базы:

```sql
SELECT ssl, version FROM pg_stat_ssl JOIN pg_stat_activity USING (pid)
WHERE datname = 'goosar';
```

### Усиление контейнеров

Образ бэкенда работает от непривилегированного пользователя `goosar` (uid 1001), как и пользователь `nextjs` в веб-образе. Стек Compose дополнительно задаёт `read_only: true` (бэкенд), `no-new-privileges` и `cap_drop: ALL`; чарт Helm задаёт `runAsNonRoot`, `runAsUser: 1001`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false` и `capabilities.drop: [ALL]`.

Единственные пути, куда бэкенд пишет во время работы: каталог загрузок (`LOCAL_UPLOAD_DIR`, по умолчанию `/app/data/uploads`; не используется при S3) и `/tmp`, куда складываются multipart-загрузки, превысившие лимит памяти. Оба смонтированы записываемыми (том или PVC и tmpfs или emptyDir соответственно).

**Тома, созданные прежним образом.** Том `backend_uploads` или PVC загрузок, созданный бэкендом, работавшим от root, принадлежит root, и uid 1001 не может в него писать. Загрузки вложений будут падать, пока владелец не исправлен; при старте бэкенд печатает предупреждение с командой. Для Compose:

```bash
docker compose -f docker-compose.selfhost.yml run --rm --no-deps --user root --cap-add CHOWN \
  --entrypoint sh backend -c 'chown -R 1001:1001 /app/data/uploads'
```

`--cap-add CHOWN` обязателен: `compose run` наследует `cap_drop: ALL` сервиса, при котором даже root не может менять владельца.

В Kubernetes `fsGroup: 1001` из чарта решает вопрос на классах хранилища, которые его учитывают. На остальных исправьте том временным подом от root или на время задайте `backend.podSecurityContext: {}`.

**Фронтенд и PostgreSQL в чарте.** Обе нагрузки тоже работают с `readOnlyRootFilesystem: true`. Фронтенд получает тома emptyDir для `/tmp` и кэша fetch/ISR Next.js; PostgreSQL получает emptyDir для `/tmp` и `/var/run/postgresql` (unix-сокет), а PGDATA остаётся на PVC с данными. PostgreSQL работает от uid/gid 999 (учётная запись `postgres` в образе) с `fsGroup: 999`. Та же оговорка про владельца применима к PVC данных PostgreSQL: на классе хранилища, игнорирующем `fsGroup`, том, инициализированный прежним PostgreSQL от root, сохранит старого владельца, и под не запустится. Исправьте владельца временным подом от root или задайте `postgres.podSecurityContext: {}` и `postgres.securityContext: {}` на этот выпуск. Развёртывание с внешней базой (`postgres.external.enabled: true`) не затронуто: чарт вообще не создаёт под PostgreSQL.

Проверки локально: `bash scripts/backend-image-hardening.test.sh` (не root, read-only rootfs, `/health`) и `bash scripts/selfhost-pg-tls.test.sh` (рендеринг sslmode и рукопожатие TLS).

### Ручной запуск миграций

Docker Compose применяет миграции автоматически. Вручную: `./server/bin/migrate up` (собранный бинарник) или `cd server && go run ./cmd/migrate up` (из исходников).

## Запуск без Docker Compose

Если вы предпочитаете собрать и запустить сервисы вручную.

**Требования:** Go 1.26+, Node.js 22, pnpm 10.28+, PostgreSQL 17 с pgvector.

```bash
# PostgreSQL: свой или `docker compose up -d postgres`
make build                                                        # сборка бэкенда
DATABASE_URL="your-database-url" ./server/bin/migrate up          # миграции
DATABASE_URL="your-database-url" PORT=8081 JWT_SECRET="your-secret" ./server/bin/server

# Фронтенд (production-режим)
pnpm install && pnpm build
cd apps/web && REMOTE_API_URL=http://localhost:8081 pnpm start
```

## Обратный прокси

В production поставьте перед бэкендом и фронтендом обратный прокси, который завершает TLS и маршрутизирует запросы.

### Caddy (рекомендуется)

**Один домен**: фронтенд и бэкенд на одном имени хоста (так по умолчанию устроен `docker-compose.selfhost.yml`):

```text
goosar.example.com {
    # Маршрут WebSocket должен идти раньше общего правила
    @goosar_ws path /ws /ws/*
    handle @goosar_ws {
        reverse_proxy localhost:8081 {
            flush_interval -1
        }
    }

    # Всё остальное — во фронтенд
    reverse_proxy localhost:3001
}
```

> Даже на одном домене задайте на бэкенде `FRONTEND_ORIGIN` и `CORS_ALLOWED_ORIGINS` равными публичному origin (например, `https://goosar.example.com`). Список origin бэкенда по умолчанию содержит только `localhost`, и без этого он отклоняет обновление WebSocket с публичного адреса с кодом `403`, а живые обновления тихо пропадают. Подробнее в разделе «Доступ из локальной сети».

**Раздельные домены**: фронтенд и бэкенд на разных именах:

```text
app.example.com {
    reverse_proxy localhost:3001
}

api.example.com {
    @goosar_ws path /ws /ws/*
    handle @goosar_ws {
        reverse_proxy localhost:8081 {
            flush_interval -1
        }
    }

    reverse_proxy localhost:8081
}
```

В блоке `/ws` два неочевидных момента: это частые причины, по которым живые обновления «пропадают» за Caddy.

- **`path /ws /ws/*`, а не `/ws*`.** Голое `handle /ws` — точное совпадение, и будущие пути под `/ws/` провалились бы к фронтенду. Очевидное сокращение `handle /ws*` перегибает в другую сторону: `*` в Caddy — глоб без границы сегмента пути, и правило поймало бы и посторонние пути вроде `/ws-foo`, а это законный адрес рабочего пространства (зарезервирован только слаг `ws`). Явные `/ws` и `/ws/*` покрывают оба настоящих случая без перебора.
- **`flush_interval -1`.** Отключает буферизацию ответа, и кадры WebSocket пересылаются, как только приходят. Без этого кадры могут лежать в окне сброса Caddy по умолчанию, что выглядит как запаздывающие комментарии, пропавшие индикаторы набора текста и «комментарии появляются только после обновления страницы».

### Nginx

```nginx
# Фронтенд
server {
    listen 443 ssl;
    server_name app.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:3001;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# API бэкенда
server {
    listen 443 ssl;
    server_name api.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Поддержка WebSocket
    location /ws {
        proxy_pass http://localhost:8081;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400;
    }
}
```

За обратным прокси задайте `GOOSAR_TRUSTED_PROXIES` (и, если нужно другое, `RATE_LIMIT_TRUSTED_PROXIES`): список CIDR прокси, чьим `X-Forwarded-For` доверяют лимитеры запросов. Пустое значение означает «не доверять заголовкам»: тогда все пользователи окажутся под адресом прокси и разделят один бюджет запросов на вход. Для прокси на том же хосте подойдёт `127.0.0.1/32,::1/128`.

При раздельных доменах задайте переменные:

```bash
# Бэкенд
FRONTEND_ORIGIN=https://app.example.com
CORS_ALLOWED_ORIGINS=https://app.example.com

# Фронтенд (только если вы собираете веб-образ из исходников через docker-compose.selfhost.build.yml)
REMOTE_API_URL=https://api.example.com
NEXT_PUBLIC_API_URL=https://api.example.com
NEXT_PUBLIC_WS_URL=wss://api.example.com/ws
```

## Доступ из локальной сети

По умолчанию Goosar работает на `localhost`. Если вы открываете его с другой машины в локальной сети (например, `http://192.168.1.100:3001`), нужно изменить две вещи. Во-первых, `docker-compose.selfhost.yml` привязывает все опубликованные порты к `127.0.0.1`, и из сети ничего не доступно, пока вы не поставите перед стеком обратный прокси (рекомендуется, см. «Обратный прокси») или не измените привязки `ports:` на интерфейс локальной сети. Во-вторых, бэкенду нужно разрешить этот origin:

```bash
# .env: подставьте IP вашего сервера в локальной сети
FRONTEND_ORIGIN=http://192.168.1.100:3001
CORS_ALLOWED_ORIGINS=http://192.168.1.100:3001
```

Затем перезапустите стек: `docker compose -f docker-compose.selfhost.yml up -d`.

### WebSocket в локальной сети и не на localhost

HTTP-запросы (задачи, комментарии, загрузки) работают в локальной сети сразу: rewrite-правила Next.js проксируют `/api`, `/auth` и `/uploads` на бэкенд. **WebSocket не работает**: rewrite пересылают только HTTP-запросы, но не рукопожатие `Upgrade`, которое нужно WebSocket. Если вы открываете приложение на `http://<lan-ip>:3001` (порт хоста, который публикует стек), функции реального времени (потоковый чат, живые обновления задач, уведомления) не подключатся, пока вы не сделаете одно из двух:

1. **Поставьте обратный прокси перед стеком (рекомендуется).** Nginx или Caddy завершает обновление WebSocket и пересылает его на бэкенд на порту хоста 8081 (готовые конфигурации в разделе «Обратный прокси»). Браузер подключается напрямую через прокси, поэтому пересобирать фронтенд не нужно.

2. **Вшейте адрес WebSocket в веб-образ.** Если прокси нет, пересоберите веб-образ с `NEXT_PUBLIC_WS_URL`, указывающим прямо на бэкенд (порт хоста 8081 должен быть доступен из браузера, а значит, привязку только к loopback придётся изменить):

   ```bash
   # В .env
   NEXT_PUBLIC_WS_URL=ws://<lan-ip>:8081/ws

   # Пересоберите веб-образ, чтобы значение сборки было вшито
   docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build
   ```

   `NEXT_PUBLIC_WS_URL` — переменная времени сборки (см. `Dockerfile.web`), поэтому одной записи в `environment:` для готового образа недостаточно: нужен оверлей `selfhost.build.yml`, пересобирающий образ. Он же нужен, если по другой причине требуется вшить в веб-образ иной публичный адрес API или WebSocket.

**Нужно и разрешить origin браузера.** Оба способа выше решают проксирование *обновления* WebSocket, но подключение проверяется ещё одним независимым параметром: бэкенд сверяет заголовок `Origin` WebSocket со списком, где по умолчанию только `localhost`. Если вы открываете Goosar с любого другого origin (IP в локальной сети **или публичный домен за обратным прокси**), задайте на бэкенде `CORS_ALLOWED_ORIGINS` (или `FRONTEND_ORIGIN`) равным этому origin и перезапустите, как показано выше. Иначе обновление отклоняется с `403`: бэкенд пишет в журнал `websocket: request origin not allowed by Upgrader.CheckOrigin`, а в консоли браузера повторяется `disconnected, reconnecting in 3s`, тогда как HTTP-запросы и ручные обновления страницы работают, потому что они same-origin. Одно значение управляет и CORS для HTTP, и проверкой origin WebSocket.

## Десктоп за корпоративным прокси

На машинах внутри корпоративного периметра исходящий трафик обычно идёт через локальный прокси с аутентификацией (например, px), которому нужен действующий тикет Kerberos. Десктопное приложение следует **системной** настройке прокси (PAC/WPAD), не изобретает свой маршрут и помогает пользователю поддерживать Kerberos в рабочем состоянии.

### Realm в `desktop.json`

Оператор задаёт realm Kerberos в блоке `perimeter` файла `~/.goosar/desktop.json` (того же, которым заранее настраивают адреса сервера):

```json
{
  "schemaVersion": 1,
  "apiUrl": "https://goosar.your-company.com",
  "perimeter": {
    "realm": "CORP.EXAMPLE.COM",
    "principalDomain": "CORP.EXAMPLE.NET",
    "noProxy": [".internal.example"]
  }
}
```

- `realm` — область, под которую строятся принципалы для `kinit`. Приложение выводит принципал как `<локальная часть почты входа>@<REALM>` (realm при чтении приводится к верхнему регистру). Без realm в качестве области берётся домен почты (в верхнем регистре); задайте `realm` явно, если область Kerberos отличается от домена почты. Сообщение «не удалось определить принципал» появляется, только если принципал вообще нельзя собрать из почты. Это лишь **значение по умолчанию**: пользователь, чей Kerberos UPN отличается от адреса входа в Goosar (другой корпоративный домен, другая локальная часть), переопределяет его у себя в **Настройки → Kerberos** (см. ниже).
- `principalDomain` — необязательный домен, под который строится принципал, если пользователь не задал свой: для организаций, где адрес входа и принципал Kerberos живут в разных доменах. Тогда принципал становится `<локальная часть почты входа>@<PRINCIPALDOMAIN>` (в верхнем регистре) вместо `…@<REALM>`; realm по-прежнему описывает сам realm Kerberos. Не указывайте, если домены совпадают. Если контроллер домена всё равно отвечает «принципал неизвестен», диалог получения тикета спросит учётную запись и сохранит её как личное переопределение пользователя.
- `noProxy` — необязательные внутренние хосты и суффиксы доменов, которые нельзя проксировать; объединяются с локальными значениями по умолчанию (`localhost`, `127.0.0.1`). Разбор терпим к ошибкам: испорченный блок превращается в «ничего не задано», а не блокирует запуск приложения.

### Значения по умолчанию, запечённые в DMG

Класть `~/.goosar/desktop.json` в домашний каталог каждого пользователя — запасной путь. Сборка может нести значения вашего развёртывания внутри пакета приложения: первый запуск сразу обращается к вашему серверу. Запишите тот же документ, что и для `~/.goosar/desktop.json`, только с нужными ключами, и укажите его сборке:

```bash
export GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS=/secure/corp/deployment.json
(cd apps/desktop && node scripts/package.mjs --mac --arm64)
```

Файл попадает в `Contents/Resources/deployment/deployment.json`. При запуске приложение сливает три слоя, по каждому ключу побеждает первый: `~/.goosar/desktop.json` (файл пользователя) > `deployment.json` (запечено при сборке) > `DEFAULT_RUNTIME_CONFIG` (публичная сборка). Вложенные объекты сливаются; массивы и скаляры заменяются целиком.

Исключение из правила «по ключу»: `apiUrl`, `wsUrl` и `appUrl` меняются только вместе. Пользовательский файл с собственным `apiUrl` отбрасывает запечённые адреса целиком, чтобы клиент не ходил по HTTPS на один хост, а по WebSocket на другой.

Правила сборки:

- Переменная не задана: ничего не запекается, это публичная сборка (не ошибка).
- Задана, но путь отсутствует, файл не читается или не является JSON-объектом: сборка завершается ошибкой, а не превращается в тихую публичную.
- С `--thin` переменная игнорируется (как и `GOOSAR_PRESET_OVERLAY`): публичная сборка никогда не называет корпоративный сервер. `scripts/release-local.sh` собирает десктоп именно с `--thin`, поэтому DMG для контура соберите отдельной командой выше или запустите релиз с `--no-desktop` и приложите свой DMG.
- Испорченный *запечённый* файл при запуске считается отсутствующим: пользователь его не выбирал и исправить не может, поэтому запуск откатывается к значению по умолчанию (или к файлу самого пользователя), а не ломается.

Оверлей предназначен для адресов и несекретных значений. `bundle-preset-overlay` отказывается принимать документ со значением, похожим на учётные данные (`Bearer` в `args`, непустое значение `env` с именем вроде token, secret, password или API key): запечённое лежит в `Contents/Resources/preset-overlay` открытым текстом в каждом установщике и меняется только пересборкой. Общие учётные данные MCP относятся в личную конфигурацию пользователя. Если запечь такое значение — осознанное решение, передайте `--allow-preset-overlay-secrets` или задайте `GOOSAR_PRESET_OVERLAY_ALLOW_SECRETS=1` для `pnpm build` и `scripts/release-local.sh`.

Тот же документ несёт блок `deployment` (адреса Jira, Confluence, почты и шлюза LLM, которые называет онбординг).

### Тикет Kerberos из приложения

Панель состояния периметра предлагает **«Получить Kerberos-тикет»**, чтобы пользователю не приходилось уходить в терминал ради `kinit`. Диалог спрашивает корпоративный пароль и запускает системный `kinit` (`/usr/bin/kinit` на macOS) с принципалом как единственным аргументом командной строки. **Пароль уходит в `kinit` только через stdin** (`--password-file=STDIN`), не попадает в argv, не пишется в журнал и не сохраняется. Отказы возвращаются как отобранные причины (неверный пароль, контроллер домена недоступен, `kinit` отсутствует): сырой stderr `kinit` классифицируется и отбрасывается, наружу не выдаётся.

Приложение перечитывает тикет через `klist` раз в 5 минут, при фокусе окна и при пробуждении из сна и предупреждает до истечения тикета.

#### Переопределение принципала (UPN)

Принципал, выведенный из почты входа, — значение по умолчанию, а не правило. В **Настройки → Kerberos** есть поле **«Учётная запись Kerberos (UPN)»**, которое переопределяет его для этого пользователя на этой машине: тикет вполне может жить на другом адресе, чем тот, под которым входят в Goosar. Поле проверяется на месте (вид `user@REALM`, без пробелов, realm приводится к верхнему регистру); пустое значение возвращает выведенный принципал по умолчанию.

Переопределение — **личная настройка, а не политика**: оно лежит в файле настроек десктопа, и блок `perimeter` оператора его не задаёт. Пароль не записывается никуда. Диалог входа и продление `kinit -R` называют принципал явно, а `klist` читает кэш машины по умолчанию, поэтому панель показывает рядом принципал из `klist` и запрошенный приложением: расхождение означает, что переопределение не подействовало. MCP-серверы на Kerberos (например, `ews-mcp` для Outlook) отдельной настройки не требуют: они берут учётные данные из общего кэша.

#### Жизненный цикл тикета

Приложение действует по одной чистой политике, исходя из текущего времени, срока действия и `Renew till` из `klist`, последнего успешного `kinit` и порога пользователя:

| Ситуация | Что происходит |
| --- | --- |
| Запас большой | Ничего |
| Осталось меньше порога (по умолчанию **30 мин**), тикет истёк или с последнего успешного `kinit` прошло больше **10 ч** | `kinit -R`, пока тикет ещё можно продлевать: без пароля и тихо |
| Тикета нет или продление уже невозможно | Открывается диалог входа по Kerberos с объяснением причины |

Диалог открывают два события: фоновая проверка и **проверка перед запуском задачи или чата** на машине, где через поставку получен MCP-сервер на Kerberos. Спросить тикет до отправки стоит один диалог; не спросить — задача, которая работает какое-то время и умирает с `401`.

Фоновое напоминание предлагает **«Напомнить через час»**; проверка перед запуском отсрочку игнорирует намеренно: откладывать нечего, если альтернатива — падающая задача. В **Настройки → Kerberos** видны используемый принципал, срок действия и `renew until`, результат последнего `kinit` и кнопка **«Проверить сейчас»**.

#### Что означает статус «тикет активен»

Панель сопоставляет два независимых сигнала. Первый — **срок действия в локальном кэше** (вывод `klist`): он ничего не спрашивает у контроллера домена. Второй — наблюдаемое поведение локального прокси px: принимает ли корпоративный прокси запросы с этим тикетом.

- Зелёный «тикет активен» ставится, только если прокси реально пропускает запрос. Если сервисы контура не отвечают, а тикет по кэшу цел, но прокси отвечает отказом в аутентификации, панель показывает красное «тикет не принимается».
- Если прокси не участвует в маршруте (машина вне периметра) или проверка не дала однозначного ответа, состояние показывается как «неизвестно», а не как ложный зелёный: по срокам кэша нельзя узнать, принимает ли домен тикет (пароль сменили, тикет отозвали на стороне домена, службе не выдали сервисный тикет).
- Если тикет в кэше выдан на другую учётную запись, чем ожидает приложение, панель показывает несоответствие принципала.

Практическое правило: **если интеграции контура начали падать, перезапустите вход по Kerberos из приложения независимо от того, что показывает статус.** Свежий тикет получить дёшево, и он разрешает любое состояние «локально действителен, на деле отклонён».

## Проверка состояния

Бэкенд публикует открытые эндпоинты состояния:

```text
GET /health   → {"status":"ok"}
GET /readyz   → {"status":"ok","checks":{"db":"ok","migrations":"ok"}}
GET /healthz  → тот же ответ, что у /readyz
```

`/health` подходит для базовой проверки живости и достижимости. `/readyz` нужен для проб готовности с учётом зависимостей и внешнего мониторинга, который должен срабатывать, когда база недоступна или миграции применены не полностью. `/healthz` — псевдоним `/readyz`.

`/health/realtime` (метрики WebSocket) без токена отвечает только прямым запросам с loopback и `404` всем остальным. За обратным прокси все запросы выглядят как loopback, поэтому задайте `REALTIME_METRICS_TOKEN` и передавайте его как `Authorization: Bearer <токен>`.

## Мониторинг

Бэкенд экспортирует метрики Prometheus. Раздел объясняет, что именно экспортируется, как собирать метрики в Compose и в Helm и какие алерты поставляются с обоими способами.

### Включение слушателя метрик

Бэкенд отдаёт `/metrics` на **отдельном управляющем слушателе**, выключенном по умолчанию:

```bash
METRICS_ADDR=127.0.0.1:9090 ./server/bin/server
curl http://127.0.0.1:9090/metrics
```

`METRICS_ADDR` по умолчанию пуст, и слушатель не запускается. Публичный порт API `/metrics` не отдаёт; для доступных из интернета развёртываний так и оставьте. Метрики HTTP-запросов начинают копиться только после включения слушателя. Метрики могут раскрыть внутренние маршруты, объём трафика, состояние зависимостей и здоровье среды выполнения.

Для Docker и Kubernetes лучше закрытый путь сбора: привяжите слушатель к внутреннему интерфейсу и защитите его частной сетью, списками доступа, NetworkPolicy или аутентификацией на прокси. Если внутри контейнера вы привязываете `METRICS_ADDR=0.0.0.0:9090`, публикуйте порт только в доверенную сеть, например, привязкой хоста `127.0.0.1:9090:9090`. Оверлей Compose и чарт Helm ниже привязывают `0.0.0.0` **внутри** контейнера или пода и нигде этот порт не публикуют.

| Переменная | По умолчанию | Вступает в силу | Значение |
| --- | --- | --- | --- |
| `METRICS_ADDR` | пусто (нет слушателя) | после перезапуска бэкенда | Адрес слушателя `/metrics`. Оверлей мониторинга закрепляет `0.0.0.0:9090` на контейнере бэкенда и эту переменную игнорирует: цель сбора фиксирована, `backend:9090`. Не публикуйте этот порт. |
| `PROMETHEUS_PORT` | `9090` | `docker compose ... up -d` (пересоздание контейнера) | Порт хоста на loopback для интерфейса Prometheus из оверлея |
| `GRAFANA_PORT` | `3002` | то же | Порт хоста на loopback для Grafana из оверлея |
| `GRAFANA_ADMIN_USER` | `admin` | то же | Логин администратора Grafana |
| `GRAFANA_ADMIN_PASSWORD` | генерируется в `.env` | то же | Пароль администратора Grafana. Оверлей не запускается с пустым значением, поэтому стек не может подняться на пароле Grafana по умолчанию `admin`/`admin`. |
| `PROMETHEUS_RETENTION` | `15d` | то же | Сколько Prometheus хранит отсчёты на диске |
| `PROMETHEUS_MEM_LIMIT` / `GRAFANA_MEM_LIMIT` | `1g` / `512m` | то же | `mem_limit` для двух контейнеров мониторинга |

Пересборка образов не нужна, но `docker compose restart` изменения не применяет: используйте `up -d`, который пересоздаёт контейнеры с изменённой конфигурацией.

### Docker Compose: оверлей Prometheus и Grafana

`docker-compose.selfhost.monitoring.yml` добавляет к стеку Prometheus и преднастроенную Grafana:

```bash
docker compose -f docker-compose.selfhost.yml \
               -f docker-compose.selfhost.monitoring.yml up -d
```

- Grafana: `http://127.0.0.1:3002`; войдите как `admin` с `GRAFANA_ADMIN_PASSWORD` из `.env` (`make selfhost` генерирует его вместе с `JWT_SECRET`).
- Prometheus: `http://127.0.0.1:9090`; цели в **Status → Targets**, сработавшие алерты в **Alerts**.
- Панель «Goosar — Overview» подключается автоматически.

Оверлей задаёт бэкенду `METRICS_ADDR=0.0.0.0:9090`, чтобы контейнер Prometheus доставал его по закрытой сети Compose. Этот порт **не** публикуется на хост. Оба образа закреплены по digest, как и встроенный образ `pgvector`, а оба интерфейса привязаны только к `127.0.0.1`: Docker обходит межсетевые экраны хоста, и привязка `0.0.0.0` открыла бы в интернет Prometheus без аутентификации. Чтобы обратиться к ним с другой машины, поставьте их за тот же обратный прокси, что и приложение.

**Маршрутизация** алертов оверлеем не покрывается: этот Prometheus вычисляет правила и показывает сработавшие алерты в собственном интерфейсе. Чтобы получать уведомления вне хоста, подключите Alertmanager или используйте оповещения Grafana на том же источнике данных.

Убрать контейнеры мониторинга, не трогая приложение: `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.monitoring.yml rm -sfv prometheus grafana`.

### Kubernetes: ServiceMonitor, правила алертов и панель

Чарт Helm содержит те же три части, все выключены по умолчанию:

```yaml
monitoring:
  enabled: true          # METRICS_ADDR + именованный порт `metrics` + аннотации сбора
  serviceMonitor:
    enabled: true        # нужен CRD Prometheus Operator
    additionalLabels:
      release: kube-prometheus-stack   # под serviceMonitorSelector вашего Operator
  prometheusRule:
    enabled: true        # нужен CRD Prometheus Operator
  dashboard:
    enabled: true        # ConfigMap для sidecar панелей Grafana
```

Одного `monitoring.enabled` достаточно для Prometheus, который находит поды по аннотации (`prometheus.io/scrape`), а не по `ServiceMonitor`; оставьте `monitoring.podAnnotations: false`, если ваш Prometheus аннотации не использует. Порт метрик добавляется в `Service` бэкенда под именем `metrics` и никогда не маршрутизируется Ingress, поэтому остаётся внутри кластера; добавьте NetworkPolicy, если границей доверия служит не сам кластер.

`monitoring.prometheusRule` и `monitoring.dashboard` включаются независимо от `monitoring.enabled`, поэтому кластер, который собирает метрики Goosar иным путём, всё равно может поставить правила и панель. Объекты `ServiceMonitor` и `PrometheusRule` используют `monitoring.coreos.com/v1`; ConfigMap панели несёт метку `grafana_dashboard: "1"`, за которой следит стандартный sidecar Grafana.

### Правила алертов

Одни и те же алерты поставляются обоими способами: `deploy/monitoring/prometheus/alerts.yml` для Compose и `PrometheusRule` чарта для Kubernetes, поэтому один регламент покрывает оба.

| Алерт | Важность | Срабатывает, когда |
| --- | --- | --- |
| `GoosarBackendDown` | critical | Эндпоинт метрик недоступен для сбора 2 минуты |
| `GoosarNotReady` | critical | `goosar_ready == 0` 5 минут: процесс отвечает на сбор метрик, но `/readyz` не принимает трафик |
| `GoosarMigrationsOutOfDate` | critical | В базе нет миграций, которые требует этот бинарник, 10 минут |
| `GoosarRealtimeRedisErrors` | critical | Реле реального времени через Redis не может публиковать или читать 10 минут. Без Redis не срабатывает; при нескольких репликах означает, что события и отзывы соединений больше не ходят между подами. |
| `GoosarNoOnlineRuntime` | warning | На установке, где среды выполнения были онлайн, их не было 15 минут; задачи из очереди выдать нельзя. Стенд, где за последние 24 часа демон ни разу не подключался, молчит. |
| `GoosarTasksStuck` | warning | Задачи выполняются дольше порога зависания 30 минут |
| `GoosarTaskFailureRateHigh` | warning | Больше 25% завершившихся задач агентов провалились за 30 минут (условие держится 15 минут) |
| `GoosarHTTPErrorRateHigh` | warning | Больше 5% HTTP-запросов возвращают 5xx за 10 минут |
| `GoosarHTTPLatencyHigh` | warning | p95 задержки HTTP выше 2 с 15 минут |
| `GoosarWebhookRateLimited` | info | Входящие вебхуки задерживаются ограничителем безопасности |
| `GoosarDBPoolSaturated` | warning | Пул PostgreSQL загружен более чем на 90% 10 минут |
| `GoosarDBPoolAcquireStalls` | warning | Запросы стоят в очереди за свободным соединением PostgreSQL |
| `GoosarBusinessSamplerQueryErrors` | warning | Запрос сэмплера, выполняемый при сборе метрик, завершается ошибкой; сэмплируемые gauge устаревают |
| `GoosarBusinessSamplerQueryLatencyHigh` | warning | p95 запроса сэмплера выше 300 мс, близко к его `statement_timeout` 500 мс |
| `GoosarDiskSpaceLow` | warning | Свободно меньше 10% диска. **Без node-exporter не срабатывает**, см. ниже. |

### Что мониторинг не покрывает

- **Диск.** Goosar не экспортирует метрик диска: процесс не видит том, на который пишет. `GoosarDiskSpaceLow` написан по `node_filesystem_*` и молчит, пока тот же Prometheus не собирает node exporter: добавьте `prom/node-exporter` в оверлей Compose или используйте node-exporter из kube-prometheus-stack в Kubernetes. PostgreSQL и том загрузок делят один диск, так что подключить это стоит.
- **Ошибки приёмника аудита.** Журнал аудита пишет в stdout и в базу; счётчика ошибок приёмника нет, и алерта тоже. Ближайший к этому сигнал частоты событий, который экспортирует бэкенд, — доставки входящих вебхуков (`goosar_webhook_delivery_total`); панель их рисует.
- **Маршрутизация алертов.** Alertmanager не поставляется ни с одним из способов.
- **Сбор журналов.** Compose ограничивает и ротирует журналы JSON (`LOG_MAX_SIZE`, `LOG_MAX_FILE`); в Kubernetes это забота kubelet.

### Флот: перекличка администратора

Метрики отвечают на вопрос «сколько демонов офлайн», но не «какая машина, в каком рабочем пространстве, чьи агенты». Это вторая половина картины: **Настройки → Деплой → Флот** (`GET /api/deployment/fleet`, только администраторам развёртывания и только людям). Экран показывает:

- четыре счётчика по всему развёртыванию: машины на связи, машины офлайн, устаревшие версии, застрявшие задачи;
- строку на машину (среды выполнения, сгруппированные по демону): имя устройства, рабочее пространство, почту владельца, индикатор онлайн с давностью последнего heartbeat, версию CLI с пометкой «устарела», если она ниже `GOOSAR_MIN_DAEMON_VERSION`, число идущих и застрявших задач и каждую среду выполнения с видимостью, статусом и привязанными агентами;
- фильтры «на связи», «офлайн», «устаревшие»; представление обновляется каждые 30 секунд, пока вкладка видима;
- не более 200 машин в ответе: при усечении сохраняются тревожные строки (сначала застрявшая работа, затем офлайн, затем устаревший CLI).

«Застряла» — вердикт самого сборщика: задача в `running` дольше того же лимита времени, что использует `FailStaleTasks`, на демоне, который больше не подтверждает, что жив. Долгий запуск с живым демоном застрявшим не считается. «Устарела» — версия, которая разбирается **и** ниже `GOOSAR_MIN_DAEMON_VERSION`; демон без версии не обвиняется. Эндпоинт только читает существующие таблицы и ничего не пишет. Метрики и алерты сообщают, что что-то не так; этот экран отвечает, **какая машина** и **кому звонить**.

### Инвентарь метрик

<!-- goosar-metric-inventory:begin -->

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `goosar_active_users` | gauge | `window` | Distinct users with chat / task activity in the rolling window. Sampled from the database; stale up to the sampler cache TTL. |
| `goosar_active_workspaces` | gauge | `window` | Distinct workspaces with chat / task activity in the rolling window. Sampled from the database. |
| `goosar_agent_created_total` | counter | `runtime_mode`, `source` | Total agents created. |
| `goosar_agent_task_dispatched_total` | counter | `source`, `runtime_mode` | Total agent tasks dispatched to a runtime. |
| `goosar_agent_task_enqueued_total` | counter | `source`, `runtime_mode` | Total agent tasks enqueued. |
| `goosar_agent_task_failed_total` | counter | `source`, `runtime_mode`, `failure_reason` | Total failed agent tasks by canonical failure reason. |
| `goosar_agent_task_in_progress` | gauge | `source`, `runtime_mode` | Current agent tasks dispatched by this process and not yet terminal. |
| `goosar_agent_task_iteration_count` | histogram | `source`, `terminal_status` | Retry attempt count observed when an agent task reaches a terminal state. |
| `goosar_agent_task_queue_wait_seconds` | histogram | `source`, `runtime_mode` | Time agent tasks spent queued before dispatch. |
| `goosar_agent_task_queued` | gauge | `source` | Current agent_task_queue rows in `queued` status by inferred source. Sampled from the database. |
| `goosar_agent_task_run_seconds` | histogram | `source`, `runtime_mode`, `terminal_status` | Time agent tasks spent running before a terminal state. |
| `goosar_agent_task_running` | gauge | `source`, `runtime_mode` | Current agent_task_queue rows in `dispatched` or `running` status by inferred source and runtime mode. Sampled from the database. |
| `goosar_agent_task_started_total` | counter | `source`, `runtime_mode`, `provider` | Total agent tasks that reached running state. |
| `goosar_agent_task_stuck_total` | gauge | `source` | Current `running` agent_task_queue rows whose started_at is older than the stuck threshold. Sampled from the database. |
| `goosar_agent_task_terminal_total` | counter | `source`, `runtime_mode`, `terminal_status` | Total agent tasks that reached a terminal state. |
| `goosar_agent_task_total_seconds` | histogram | `source`, `runtime_mode`, `terminal_status` | Total time from agent task creation to terminal state. |
| `goosar_autopilot_created_total` | counter | `cadence` | Total autopilots created. |
| `goosar_autopilot_run_skipped_total` | counter | `cadence`, `reason` | Total autopilot runs that admission-skipped (concurrency / cooldown / other). |
| `goosar_autopilot_run_started_total` | counter | `cadence`, `trigger_kind` | Total autopilot runs started. |
| `goosar_autopilot_run_terminal_total` | counter | `cadence`, `trigger_kind`, `terminal_status` | Total autopilot runs that reached a terminal status. |
| `goosar_build_info` | gauge | `version`, `commit` | Build information for the Goosar server binary. |
| `goosar_business_sampler_query_errors_total` | counter | `name` | Per-query error count. Includes statement_timeout cancellations, which are the expected outcome of a hung database and the smoking gun for SET LOCAL working as intended. |
| `goosar_business_sampler_query_seconds` | histogram | `name` | Per-query duration of the BusinessSamplerCollector. The `name` label is one of the fixed query identifiers and never user-controlled. |
| `goosar_channel_media_pending_objects` | gauge | — | Live intent rows awaiting bind or reclaim (excludes tombstones). |
| `goosar_channel_media_reconciler_delete_failures_total` | counter | — | Object-storage deletes that failed and were scheduled for retry. |
| `goosar_channel_media_reconciler_objects_deleted_total` | counter | — | Unreferenced media objects deleted by the reconciler. |
| `goosar_channel_media_reconciler_rows_referenced_total` | counter | — | Ledger rows cleared because a durable attachment references the object. |
| `goosar_channel_media_reconciler_tombstone_referenced_total` | counter | — | Tombstone passes that found an attachment referencing the object (invariant violation; the object is kept). |
| `goosar_channel_media_tombstoned_objects` | gauge | — | Deleted objects still tombstoned for scheduled re-deletion. |
| `goosar_chat_message_sent_total` | counter | `platform` | Total user chat messages sent (excludes agent replies). |
| `goosar_chat_output_local_path_total` | counter | `kind` | Total agent chat replies that referenced a runtime-local path, by evidence kind. Observation only — the reply is still delivered. |
| `goosar_cloud_waitlist_joined_total` | counter | — | Total users that joined the cloud waitlist. |
| `goosar_cloudruntime_request_duration_seconds` | histogram | `op` | Outbound cloud runtime request duration (seconds). |
| `goosar_cloudruntime_request_total` | counter | `op`, `status` | Total outbound cloud runtime requests by op and status bucket. |
| `goosar_contact_sales_submitted_total` | counter | `source` | Total contact-sales inquiries submitted. |
| `goosar_daemon_ws_message_received_total` | counter | `kind` | Total daemon WebSocket inbound messages by handler kind. |
| `goosar_daemonws_active_connections` | gauge | — | Current daemon WebSocket connections. |
| `goosar_daemonws_connects_total` | counter | — | Total daemon WebSocket connections opened. |
| `goosar_daemonws_disconnects_total` | counter | — | Total daemon WebSocket connections closed. |
| `goosar_daemonws_slow_evictions_total` | counter | — | Total daemon WebSocket clients evicted for slow consumption. |
| `goosar_daemonws_wakeup_delivered_total` | counter | `result` | Total daemon wakeup local delivery attempts. |
| `goosar_daemonws_wakeup_publish_errors_total` | counter | — | Total daemon wakeup Redis publish errors. |
| `goosar_daemonws_wakeup_published_total` | counter | — | Total daemon wakeups published to the Redis relay. |
| `goosar_daemonws_wakeup_received_total` | counter | — | Total daemon wakeups received from the Redis relay. |
| `goosar_db_pool_acquire_count` | counter | — | Total successful PostgreSQL connection acquires. |
| `goosar_db_pool_acquire_duration_seconds_total` | counter | — | Total time spent acquiring PostgreSQL connections. |
| `goosar_db_pool_acquired_conns` | gauge | — | Currently acquired PostgreSQL connections. |
| `goosar_db_pool_canceled_acquire_count` | counter | — | Total canceled PostgreSQL connection acquires. |
| `goosar_db_pool_constructing_conns` | gauge | — | PostgreSQL connections currently being established. |
| `goosar_db_pool_empty_acquire_count` | counter | — | Total acquires that waited because the PostgreSQL pool was empty. |
| `goosar_db_pool_empty_acquire_wait_seconds_total` | counter | — | Total time spent waiting for PostgreSQL connections when the pool was empty. |
| `goosar_db_pool_idle_conns` | gauge | — | Currently idle PostgreSQL connections. |
| `goosar_db_pool_max_conns` | gauge | — | Maximum PostgreSQL connections allowed by the pool. |
| `goosar_db_pool_max_idle_destroy_count` | counter | — | Total PostgreSQL connections destroyed due to idle limits. |
| `goosar_db_pool_max_lifetime_destroy_count` | counter | — | Total PostgreSQL connections destroyed due to max lifetime. |
| `goosar_db_pool_new_conns_count` | counter | — | Total PostgreSQL connections created by the pool. |
| `goosar_db_pool_total_conns` | gauge | — | Total PostgreSQL connections currently in the pool. |
| `goosar_feedback_submitted_total` | counter | `kind`, `platform` | Total in-app feedback submissions. |
| `goosar_github_event_received_total` | counter | `event_kind`, `action` | Total GitHub webhook events received by event kind and action. |
| `goosar_github_pr_merge_seconds` | histogram | — | Time from PR opened to merged (seconds). |
| `goosar_github_pr_review_total` | counter | `result` | Total GitHub pull request reviews observed by result. |
| `goosar_http_daemon_workspace_response_size_bytes` | histogram | `status` | Response bytes written by the daemon workspace-set endpoint. |
| `goosar_http_in_flight_requests` | gauge | — | Current number of in-flight HTTP requests served by the API server. |
| `goosar_http_request_duration_seconds` | histogram | `method`, `route`, `status` | HTTP request duration observed by the API server. |
| `goosar_http_requests_total` | counter | `method`, `route`, `status` | Total HTTP requests served by the API server. |
| `goosar_issue_created_total` | counter | `source`, `platform` | Total issues created (any source). |
| `goosar_issue_executed_total` | counter | `source` | First task completion per issue (per-issue exactly-once activation keystone). |
| `goosar_llm_cost_usd_total` | counter | `provider`, `model`, `token_type`, `runtime_mode`, `source` | Total estimated priced LLM token cost in USD. |
| `goosar_llm_request_total` | counter | `provider`, `model`, `runtime_mode` | Total task usage reports by normalized LLM provider and model. |
| `goosar_llm_tokens_total` | counter | `provider`, `model`, `token_type`, `runtime_mode`, `source` | Total priced LLM tokens by provider, model, token type, runtime mode, and task source. |
| `goosar_llm_unpriced_tokens_total` | counter | `provider`, `model_alias`, `token_type` | Total LLM tokens for model aliases without a fixed TSR price. |
| `goosar_migrations_out_of_date` | gauge | — | 1 when the database is missing migrations this binary requires, 0 otherwise. |
| `goosar_onboarding_completed_total` | counter | `path` | Total onboarding flows completed. |
| `goosar_onboarding_questionnaire_submitted_total` | counter | — | Total onboarding questionnaires submitted. |
| `goosar_onboarding_source_submitted_total` | counter | — | Total acquisition-source answers or declines recorded (workspace backfill prompt). |
| `goosar_onboarding_started_total` | counter | `platform` | Total onboarding flows started. |
| `goosar_ready` | gauge | — | 1 when the readiness check passes (same verdict as /readyz), 0 otherwise. |
| `goosar_realtime_active_connections` | gauge | — | Current realtime WebSocket connections. |
| `goosar_realtime_connects_total` | counter | — | Total realtime WebSocket connections opened. |
| `goosar_realtime_disconnects_total` | counter | — | Total realtime WebSocket connections closed. |
| `goosar_realtime_messages_dropped_total` | counter | — | Total realtime messages dropped. |
| `goosar_realtime_messages_sent_total` | counter | — | Total realtime messages sent. |
| `goosar_realtime_redis_ack_total` | counter | — | Total Redis stream acknowledgements by the realtime relay. |
| `goosar_realtime_redis_connected` | gauge | — | Whether the realtime Redis relay is connected. |
| `goosar_realtime_redis_mirror_divergence_total` | counter | — | Total Redis mirror divergence events by the realtime relay. |
| `goosar_realtime_redis_mirror_errors_total` | counter | `target` | Total Redis mirror write errors by the realtime relay. |
| `goosar_realtime_redis_xadd_errors_total` | counter | — | Total Redis XADD errors by the realtime relay. |
| `goosar_realtime_inbound_too_large_total` | counter | — | Total realtime connections closed for exceeding the inbound message size limit. |
| `goosar_realtime_redis_xadd_total` | counter | — | Total Redis XADD operations by the realtime relay. |
| `goosar_realtime_redis_xread_errors_total` | counter | — | Total Redis XREAD errors by the realtime relay. |
| `goosar_realtime_redis_xread_total` | counter | — | Total Redis XREAD operations by the realtime relay. |
| `goosar_realtime_slow_evictions_total` | counter | — | Total realtime clients evicted for slow consumption. |
| `goosar_runtime_failed_total` | counter | `runtime_mode`, `provider`, `failure_reason`, `recoverable` | Total runtime failures by canonical reason. |
| `goosar_runtime_heartbeat_age_seconds` | histogram | `runtime_mode` | Distribution of (now() - agent_runtime.last_seen_at) for runtimes considered online by the sampler. |
| `goosar_runtime_offline_total` | counter | `runtime_mode`, `provider` | Total runtime offline transitions. |
| `goosar_runtime_online` | gauge | `runtime_mode`, `provider` | Count of agent_runtime rows with last_seen_at within the online heartbeat window. Sampled from the database. |
| `goosar_runtime_ready_seconds` | histogram | `runtime_mode`, `provider` | Time from runtime registration to ready (seconds). |
| `goosar_runtime_ready_total` | counter | `runtime_mode`, `provider` | Total runtimes that reached ready state. |
| `goosar_runtime_registered_total` | counter | `runtime_mode`, `provider` | Total first-time runtime registrations. |
| `goosar_signup_total` | counter | `signup_source` | Total user signups (account creations). |
| `goosar_squad_created_total` | counter | — | Total squads created. |
| `goosar_task_lease_expired_total` | counter | `source` | Total dispatched or running task leases expired by the scheduler. |
| `goosar_task_queued_expired_total` | counter | `source`, `runtime_mode` | Total queued tasks expired by the scheduler. |
| `goosar_team_invite_accepted_total` | counter | — | Total workspace invitations accepted. |
| `goosar_team_invite_sent_total` | counter | — | Total workspace invitations sent. |
| `goosar_webhook_delivery_total` | counter | `provider`, `status` | Total inbound webhook deliveries by provider and outcome. |
| `goosar_webhook_rate_limited_total` | counter | `gate` | Total webhook admissions or worker dispatches delayed by a bounded safety gate. |
| `goosar_workspace_created_total` | counter | `source` | Total workspaces created. |
| `goosar_workspace_total` | gauge | — | Lifetime workspace row count. Useful for sizing alerts and dashboards. |

<!-- goosar-metric-inventory:end -->

## Агрегация статистики использования

Панели Usage и Runtime читают производную таблицу `task_usage_hourly`, которую заполняет функция `rollup_task_usage_hourly()`. Бэкенд выполняет эту агрегацию **внутри процесса** на каждой реплике через планировщик на базе БД (`sys_cron_executions`). На свежей установке ничего делать не нужно, а встроенный образ `pgvector/pgvector:pg17` работает без изменений: заменять его образом с `pg_cron`, регистрировать внешнее задание cron, заводить таймер systemd или `CronJob` Kubernetes не нужно.

### Как работает встроенный планировщик

Каждая реплика бэкенда тикает раз в 30 секунд и пытается захватить текущий 5-минутный план (UTC) в `sys_cron_executions`. Уникальный ключ `(job_name, scope_kind, scope_id, plan_time)` делает захват состязанием с одним победителем среди всех реплик, поэтому при нескольких экземплярах данные не пишутся дважды. Победитель вызывает `SELECT rollup_task_usage_hourly()`; SQL-функция сама держит advisory lock `4246`, поэтому случайное задание `pg_cron` или ручной вызов могут работать рядом с планировщиком, не сталкиваясь на самой агрегации. В штатной работе состояние смотрится в таблице аудита:

```sql
SELECT plan_time, status, attempt, runner_id,
       error_code, error_msg, started_at, finished_at
  FROM sys_cron_executions
 WHERE job_name = 'rollup_task_usage_hourly'
 ORDER BY plan_time DESC
 LIMIT 20;
```

### Внешние планировщики (только для существующих развёртываний)

Внешние планировщики (`pg_cron`, внешний cron, таймер systemd, `CronJob` Kubernetes), которые вызывают `SELECT rollup_task_usage_hourly()` напрямую, были единственным вариантом до появления встроенного планировщика и остаются поддерживаемым путём совместимости, но новым развёртываниям они не нужны. Advisory lock `4246` не даёт двойной записи: проигравший вызов ничего не делает.

Чтобы убрать такое задание, сначала убедитесь, что встроенный планировщик здоров: в `sys_cron_executions` регулярно появляются строки `SUCCESS` для `rollup_task_usage_hourly` (тот же запрос с условием `AND status = 'SUCCESS'`). Затем снимите лишнее задание (для `pg_cron`):

```sql
SELECT cron.unschedule('rollup_task_usage_hourly')
  FROM cron.job WHERE jobname = 'rollup_task_usage_hourly';
```

Сам `pg_cron` не удаляйте, если от него может зависеть другая нагрузка; встроенный образ `pgvector/pgvector:pg17` его не содержит. Внешний cron, таймер и `CronJob` просто удаляются.

### Отдельная команда дозаполнения

`rollup_task_usage_hourly()` обрабатывает только новые корзины после начала работы. Если в `task_usage` уже есть строки, появившиеся до первого захвата агрегации (обычно при обновлении базы с месяцами накопленной статистики), запустите `backfill_task_usage_hourly`, чтобы заполнить исторические корзины:

```bash
# Docker Compose
docker compose -f docker-compose.selfhost.yml exec backend \
  ./backfill_task_usage_hourly --sleep-between-slices=2s

# Kubernetes
kubectl -n goosar exec deploy/goosar-backend -- \
  ./backfill_task_usage_hourly --sleep-between-slices=2s
```

Команда обходит весь диапазон времени `task_usage` месячными срезами и вызывает тот же идемпотентный примитив, что и встроенный планировщик, поэтому её безопасно перезапускать, прерывать по Ctrl-C и запускать параллельно с планировщиком (advisory lock `4246` их упорядочивает). Флаги:

| Флаг | Описание |
| --- | --- |
| `--sleep-between-slices` | Пауза между месячными срезами, чтобы снизить нагрузку чтения на загруженных базах (например, `2s`). Рекомендуется на боевых базах с многолетней историей. |
| `--months-back N` | Дозаполнить только последние N месяцев. **Требует `--force-partial`**: водяной знак всё равно продвигается за пропущенные старые корзины, и они навсегда остаются потерянными. |
| `--force-partial` | Подтверждает, что `--months-back` необратимо оставляет пустыми корзины старше границы. |
| `--dry-run` | Записывает в журнал срезы, которые были бы обработаны, ничего не записывая. |

После завершения дозаполнения водяной знак состояния агрегации ставится в `now() - 5 minutes`, поэтому первый плановый тик не перерабатывает историю.

### Порядок при обновлении

Миграция `103` (удаляет прежние дневные агрегаты) содержит защиту по принципу «закрыто при сомнении»: пока `task_usage_hourly` не догнала данные, она отказывается их удалять. Команда `migrate` автоматически, непосредственно перед применением миграции `103`, запускает идемпотентное дозаполнение месячными срезами (под advisory lock `4246`), поэтому обновление базы с накопленной статистикой завершается одним запуском `migrate up`, и дополнительных действий от оператора не требуется.

Если автоматический шаг не сработал (например, по причине среды), запустите `backfill_task_usage_hourly` против базы и повторите `migrate up` (или перезапустите контейнер бэкенда: миграции выполняются при старте). Свежие установки не затронуты: при пустом `task_usage` защита пропускается.

## Высокая доступность

Стек Compose — одна машина, и он так и заявлен. Этот раздел описывает другую форму: несколько реплик бэкенда в Kubernetes, чтобы потеря узла или перекат при обновлении не останавливали API. Это не «больший установщик», а другой набор допущений; части, которые нельзя запускать дважды, перечислены ниже. Установку чарта описывает раздел «Kubernetes (Helm)».

### Топология

```text
браузеры, CLI, демоны ──▶ Ingress ──▶ под бэкенда #1 ... под бэкенда #N   (Deployment, replicas: N;
                                        │                │                  каждый под: API + WS + GC)
                                        └───────┬────────┘
                                                ├──▶ Redis: реле реального времени, бюджеты лимитов,
                                                │    отзывы соединений, живость
                                                └──▶ PostgreSQL (внешний, HA): выбор лидера, всё
                                                     постоянное состояние; хранилище: S3 или RWX-том
```

Чтобы `backend.replicas > 1` был безопасен, должны выполняться три условия. Первое бэкенд проверяет сам и без него не запускается.

1. **Redis.** `redis.url` в значениях или `REDIS_URL` в существующем Secret, если URL содержит пароль. Без него у каждого пода был бы собственный бюджет лимитов запросов, собственный хаб реального времени, и под никогда не узнавал бы об отзывах соединений в других подах. Helm передаёт число реплик поду как `GOOSAR_REPLICAS`; если оно больше 1, а `REDIS_URL` отсутствует или не разбирается, бэкенд завершается при старте.
2. **Хранилище, доступное каждому поду.** Либо S3 (`backend.config.s3Bucket`), либо PVC загрузок с `accessModes: [ReadWriteMany]`. Том `ReadWriteOnce` по умолчанию нельзя подключить ко второму поду, и вторая реплика зависает в Pending с ошибкой Multi-Attach.
3. **PostgreSQL вне чарта.** Встроенный PostgreSQL — один Deployment на одном PVC: без репликации, без отказоустойчивости и с перезапуском при каждом `helm upgrade`. Задайте `postgres.external.enabled=true` и положите `DATABASE_URL` в Secret.

Redis чарт не поставляет. Один под Redis внутри релиза стал бы ещё одной единой точкой отказа посреди истории про доступность: используйте управляемый Redis или установите его рядом с релизом.

### Внешний PostgreSQL

Направьте `DATABASE_URL` на управляемый сервис или кластер под управлением оператора (CloudNativePG, Patroni), который сам делает переключение при отказе, и потребуйте TLS на этом участке:

```text
postgres://goosar:<password>@pg.internal:5432/goosar?sslmode=require
```

`sslmode=require` шифрует, но не аутентифицирует сервер; `sslmode=verify-full` проверяет ещё сертификат и имя хоста и нужен, как только CA смонтирован в под. Полный список режимов и того, что каждый проверяет, приведён в документации libpq. `postgres.sslMode` в значениях относится только к встроенной базе чарта; при `external.enabled=true` весь URL, включая sslmode, берётся из Secret.

### Что нельзя запускать дважды

Каждый под бэкенда запускает один и тот же бинарник, включая фоновые обработчики. Каждый из них либо выбирает лидера, либо безопасен на любой реплике, и никогда не «наверное, нормально».

| Компонент | Стратегия при N репликах |
| --- | --- |
| Запуск миграций (при старте) | **Выбор лидера.** Блокирующий advisory lock PostgreSQL в `cmd/migrate`; остальные ждут в очереди и затем пропускают уже применённые файлы. |
| Планировщик выполнения (`sys_cron_executions`) | **Лидер на запуск задания.** Аренда на уровне строки с идентификатором исполнителя, heartbeat и перехватом просроченной аренды: одна реплика на (задание, область, плановое время). |
| Уборщик гигиены (очистка аудита, GC загрузок) | **Лидер на тик.** `pg_try_advisory_lock`; проигравшие пропускают тик. Оба вида работы удаляют пачками, поэтому гонка дублировала бы работу, а два прохода GC освобождали бы пачки друг друга. |
| Уборщик сред выполнения (просроченные среды, осиротевшие задачи) | **Безопасен на N репликах.** Каждая ветвь — условный `UPDATE ... RETURNING`: у проигравшего обновление не затрагивает ни одной строки, поэтому события срабатывают один раз. |
| Ограничитель запросов | **Общий через Redis.** Без `REDIS_URL` бюджет свой у каждого пода, и каждый лимит молча умножается на N; отсюда отказ при старте. |
| Хаб реального времени / рассылка WebSocket | **Общий через реле Redis.** У каждого пода свои сокеты; события переходят между подами по потокам Redis для каждой области. |
| Отзыв соединений (удаление участника, деактивация пользователя) | **Рассылается через реле.** Отзыв идёт по области пользователя, и поды, держащие его сокеты, их закрывают. |
| Начальное создание администратора и синхронизация каталога пакетов (первый запуск) | **Администратор: выбор лидера** (advisory lock и проверка пустой таблицы в одной транзакции). **Каталог: безопасна на N репликах** (два пода пишут одинаковые байты, проверенные по sha256, каталог записывается последним). |
| Получение задач демоном | **Единственный писатель по построению.** Получение — условное обновление строки: задачу выигрывает ровно один демон вне зависимости от пода, принявшего запрос. |
| Обработчик доставки вебхуков | **Безопасен на N репликах.** Очередь и аренда — строки PostgreSQL, берущиеся через `FOR UPDATE SKIP LOCKED`: реплики берут разные доставки, а аренда упавшего пода истекает. |
| Супервизор входящих каналов | **Лидер на установку.** Аренда в базе с токеном-ограждением, TTL и продлением: входящий WebSocket канала держит ровно один под, а аренда упавшего держателя истекает. |
| Согласователь медиа каналов, монитор отказов автопилотов, обновление снимков PR | **Безопасны на N репликах.** Строки берутся по одной под арендой, приостановка автопилота — условный `UPDATE ... RETURNING` (проигравший не находит строк), снимок PR идемпотентен. |
| Пакетная запись heartbeat, журнал статистики БД | **На каждом поде по замыслу.** Оба сбрасывают только состояние, которым владеет этот под. |

### Порядок обновления при нескольких репликах

При `replicas > 1` чарт переключает Deployment на `RollingUpdate` с `maxUnavailable: 0`, поэтому текущее число реплик остаётся в работе, пока поднимаются новые поды. Проба готовности (`/readyz`) проверяет базу и состояние миграций; проба живости (`/health`) намеренно проверяет только, что процесс отвечает: направь её на проверку зависимости, и короткий сбой базы превратится в цикл перезапусков во всём Deployment. `startupProbe` смотрит на `/health` по той же причине и должна покрыть только выполнение миграций: пока `migrate up` не вернулся, на порту никто не отвечает.

Каждый новый под запускает `migrate up`, прежде чем начать обслуживать запросы. Они соревнуются, и гонка разрешается, а не избегается:

1. Первый под берёт advisory lock миграций и применяет файлы.
2. Остальные блокируются на том же замке; это ожидание намеренно не ограничено, потому что стоять в очереди за миграцией правильно.
3. `lock_timeout` и `statement_timeout` выставляются уже *после* получения advisory lock, поэтому отдельный `ALTER TABLE` всё равно сдаётся, а не стоит в очереди за посторонней долгой транзакцией и не блокирует таблицу для всех.
4. Когда поздний под наконец получает замок, все миграции уже записаны, и каждая пропускается.

Миграции **только вперёд**: откат выкатки возвращает образы, а не схему. Сначала снимите резервную копию: см. «Резервное копирование и восстановление» и «Обновление».

```bash
helm upgrade goosar deploy/helm/goosar -f values.yaml --wait
```

`--wait` держит команду, пока новые поды не пройдут готовность; без него команда возвращается раньше, чем выкатка что-либо доказала. Следите за ней через `kubectl rollout status deploy/goosar-backend`.

PodDisruptionBudget с `minAvailable: 1` создаётся только при `replicas > 1` (`backend.podDisruptionBudget.enabled: auto`), поэтому drain узлов и обновления кластера не могут вытеснить последний под бэкенда. При одной реплике чарт намеренно бюджета не создаёт: бюджет, который нельзя выполнить, блокирует любой добровольный drain, а ничего не защищает.

### Честные ограничения

Несколько реплик убирают один вид отказа. Систему непрерывной они не делают:

- **Соединения WebSocket не переезжают.** Когда под погибает, сокеты гибнут с ним. Клиенты переподключаются к другому поду и подписываются заново; видимый эффект — кратковременное переподключение, а события, опубликованные за время разрыва, пропускаются, а не воспроизводятся.
- **Работа на потерянном поде не продолжается.** Идущий HTTP-запрос падает, и клиент должен повторить его. Задание планировщика, чей под умер посреди работы, держит аренду, пока она не протухнет, после чего другая реплика её перехватывает: это задержка, а не потеря.
- **Задачи агентов принадлежат демонам, а не подам.** Демон работает вне кластера и продолжает работу при перезапуске бэкенда: он переподключается и возобновляет отчёты. Чего он не может, так это передать своё получение задачи: задача получена ровно одним демоном, и если он умирает, уборщик сред выполнения завершает её ошибкой по истечении окна (около 3 минут), а не переносит.
- **Фронтенд и база остаются единичными точками**, пока вы сами их не масштабируете и не реплицируете. `frontend.replicas` не хранит состояния и может свободно расти; доступность PostgreSQL — задача внешнего кластера.
- **Нулевой простой ограничен самой миграцией.** Миграция, берущая эксклюзивную блокировку на горячей таблице, блокирует и старые поды. Делайте миграции аддитивными и используйте `CREATE INDEX CONCURRENTLY`, которого репозиторий и так требует.

## Закрытый контур (offline-поставка)

Как собрать и установить Goosar на машине **без доступа в интернет**: это приёмочный тест для развёртываний в закрытой сети под управлением оператора («периметр»).

Обычная сборка обращается в сеть в нескольких местах: базовые образы Docker, `go mod download`, загрузка pnpm через corepack, `pnpm install` и `next/font/google`, который скачивает Google Fonts **во время сборки**. Offline-набор фиксирует всё это на подключённой машине, чтобы изолированная машина собирала образы с `--network=none`: любой шаг, который всё же пытается выйти в интернет, ломает сборку. Это и есть критерий приёмки.

Всё необязательно. Обычные онлайн-сборки не затронуты: offline-поведение включается аргументом сборки `--build-arg GOOSAR_OFFLINE_BUILD=1` и переопределением базовых образов, значения по умолчанию которых совпадают с прежними зашитыми.

В профиле `perimeter` (`GOOSAR_DELIVERY_PROFILE=perimeter`) выключены все каналы самообновления: демон не обновляется сам, `goosar update` отказывает и указывает на оператора, страница сред выполнения заменяет кнопку обновления пометкой «управляется оператором» (а бэкенд отвечает `403` на `POST /api/runtimes/{id}/update`), а десктоп прекращает проверки обновлений, пока подключён к такому серверу. На машинах со средой выполнения можно закрепить профиль локально: `GOOSAR_DELIVERY_PROFILE=perimeter` в окружении отключает и опрос автообновления демона, и `goosar update` даже офлайн и вопреки `GOOSAR_DAEMON_AUTO_UPDATE=true`. Зашейте эту переменную в образы машин периметра. Для DMG, которые распространяются *внутри* периметра, при упаковке дополнительно задайте `GOOSAR_DESKTOP_NO_UPDATER=1` в окружении сборки: флаг вшивается в пакет приложения и намертво отключает electron-updater и загрузку управляемого CLI независимо от состояния во время работы.

### Общая схема

На **подключённой** машине из checkout нужного ref командой `offline/make-kit.sh` собирается каталог `offline/kit/`. Он копируется вместе с checkout на **изолированную** машину, где `offline/build-offline.sh` проверяет контрольные суммы, загружает образы (`docker load`), распаковывает `server/vendor` и собирает образы `goosar-backend:offline-<rev>` и `goosar-web:offline-<rev>` с `--network=none`. Затем стек запускается через `docker compose`.

### Шаг 1. Соберите набор (подключённая машина)

Требования: docker, git, node и выполненный `pnpm install` в checkout (генератор шрифтов берёт закреплённую версию Next.js из `apps/web`). Переключитесь на тот ref, который собираетесь развёртывать, затем:

```bash
make offline-kit
# или напрямую, с явной целевой платформой:
bash offline/make-kit.sh --platform linux/amd64
```

`--platform` (по умолчанию `linux/amd64`) должна совпадать с **изолированной** машиной. Если подключённая машина отличается, Docker эмулирует чужую платформу при сборке набора: сборка медленнее, но offline-сборка на целевой машине идёт нативно.

Набор содержит:

| Элемент | Для чего |
| --- | --- |
| `images/images.tar` | `docker load`: базовые образы golang и node, `pgvector/pgvector` для Compose и производный образ `goosar-offline-runtime` с уже установленными `ca-certificates` и `tzdata` (заменяет `apk add` в среде выполнения) |
| `go-vendor.tar.gz` | `server/vendor/`: сборки Go автоматически используют `-mod=vendor`, поэтому `go mod download` и git не нужны |
| `context/pnpm-store/` | `pnpm install --offline --store-dir …` внутри `Dockerfile.web` |
| `context/corepack/` | кэш corepack с закреплённой версией pnpm, чтобы corepack не обращался к реестру npm |
| `context/fonts/` | вендорные CSS и woff2 Google Fonts, подключаемые через `NEXT_FONT_GOOGLE_MOCKED_RESPONSES`, чтобы `next/font/google` читал с диска |
| `manifest.txt`, `checksums.txt` | происхождение (git rev, платформа, теги образов) и sha256 каждого файла набора |

Рассчитывайте на несколько ГБ (основной объём — хранилище pnpm и базовые образы).

### Шаг 2. Перенос и проверка

Скопируйте checkout **вместе с `offline/kit/`** на изолированную машину любым носителем, который допускает ваш периметр. Перед использованием проверьте целостность:

```bash
bash offline/build-offline.sh --verify-only
```

Скрипт пересчитывает sha256 каждого файла набора и сверяет с `checksums.txt`. Для сквозной проверки происхождения запишите контрольную сумму самого архива передачи на подключённой стороне и сверьте после переноса.

**`checksums.txt` подтверждает только сам себя.** Он лежит *внутри* набора, и тот, кто подменит содержимое, может пересчитать `checksums.txt` под него: `--verify-only` доказывает лишь внутреннюю согласованность, а не то, что набор именно тот, что вы собрали. Когда `offline/make-kit.sh` заканчивает сборку, он печатает sha256 самого `checksums.txt` (строка вида `checksums.txt sha256: <64-hex sha256>` в рамке из знаков `=`). Передайте это одно значение тому, кто обслуживает изолированную машину, по каналу, **отдельному от переноса набора** (чат, телефон, комментарий в заявке), и пусть он сравнит его до запуска `build-offline.sh` и до того, как доверять `--verify-only` (`sha256sum offline/kit/checksums.txt` на Linux, `shasum -a 256 offline/kit/checksums.txt` на macOS). Если не совпадает, не продолжайте: либо перенос повредил набор, либо содержимое подменили по дороге.

Отдельно `build-offline.sh` сверяет записанный в набор git-коммит (`git_rev` в `manifest.txt`) с `HEAD` checkout и отказывается собирать при расхождении (`--allow-rev-mismatch` задавайте, только если проверили, что расхождение намеренное, например повторное применение более старого набора). Эта проверка ловит «не тот коммит», а не «подменённое содержимое»: от подмены защищает сверка `checksums.txt` по отдельному каналу.

### Шаг 3. Сборка без сети (изолированная машина)

Требования: docker со стандартным сборщиком (удалённый или контейнерный драйвер buildx не видит образов, загруженных через `docker load`).

```bash
make offline-build
# или: bash offline/build-offline.sh [--tag TAG]
```

Скрипт проверяет контрольные суммы, загружает образы, распаковывает `server/vendor` и собирает оба образа с `--network=none` и `--build-arg GOOSAR_OFFLINE_BUILD=1`. **Флаг `--network=none` и есть приёмочный тест:** если какой-то шаг всё же пытается выйти в интернет, сборка падает, а не молча утекает. (`--allow-network` нужен только для отладки.)

Результат: `goosar-backend:offline-<rev>` и `goosar-web:offline-<rev>`.

### Шаг 4. Развёртывание

`docker-compose.selfhost.yml` уже поддерживает переопределение образов. В `.env` (рядом с `JWT_SECRET` и остальным):

```bash
GOOSAR_BACKEND_IMAGE=goosar-backend
GOOSAR_WEB_IMAGE=goosar-web
GOOSAR_IMAGE_TAG=offline-<rev>          # печатается build-offline.sh
GOOSAR_DELIVERY_PROFILE=perimeter
# затем: docker compose -f docker-compose.selfhost.yml up -d
```

`pgvector/pgvector:pg17` загружен из набора, поэтому Compose стартует без скачивания.

### Установка CLI внутри периметра

Никогда не запускайте в периметре установочные скрипты, полученные из чужих сетей: это относится и к однострочникам `curl … | bash` из README, и к установщикам сред выполнения агентов от их поставщиков. Собранное вами развёртывание **само отдаёт свой установщик и бинарники**:

- `https://<ваш-goosar>/install.sh`: установщик, который обращается только к развёртыванию, отдавшему его;
- `https://<ваш-goosar>/cli/goosar-cli-<os>-<arch>.tar.gz`: бинарники CLI, собранные из того же дерева исходников, что и образы;
- `https://<ваш-goosar>/cli/checksums.txt`: суммы sha256; установщик сверяет с ними скачанное и прерывается при расхождении.

Рекомендуемый порядок (скачать, просмотреть, закрепить, запустить, без «pipe to shell»):

```bash
curl -fsSLO https://goosar.internal.example/install.sh
less install.sh                                # просмотрите перед запуском
bash install.sh --app-url https://goosar.internal.example \
                --server-url https://goosar.internal.example
```

CLI сред выполнения агентов — сторонее ПО: оператор доставляет их своим утверждённым каналом. Подсказки онбординга с `curl | bash` с сайтов поставщиков в периметре неприменимы.

### Манифест offline-поставки

«Что положить на носитель и всё ли дошло целым?» — на оба вопроса отвечает один скрипт, с sha256 на каждый файл:

```bash
# ПОДКЛЮЧЁННАЯ машина: соберите всё в каталог, затем составьте перечень
bash offline/offline-manifest.sh generate --tag vX.Y.Z --dir ./carry --strict
#   -> ./carry/offline-manifest.txt

# ИЗОЛИРОВАННАЯ машина, после копирования ./carry через воздушный зазор
bash offline/offline-manifest.sh verify --dir ./carry
```

`--strict` завершается ошибкой, если целый класс артефактов отсутствует; эту проверку стоит делать до того, как носитель покинет здание. `make offline-manifest DIR=./carry STRICT=1` и `make offline-verify DIR=./carry` — те же два вызова. Третий режим, `delivery`, формирует `DELIVERY-MANIFEST.md`: единый передаточный документ для акта приёмки (артефакты с контрольными суммами, digest образов, версия чарта, диапазон миграций, минимальная версия демона, файлы SBOM и уведомления о сторонних компонентах).

Что должно лежать в каталоге переноса:

| Класс | Файлы | Чем получены |
| --- | --- | --- |
| `images` | `release-images.tar`, `images.list` | `offline/save-release-images.sh` (или `offline/make-kit.sh` для сборки из исходников) |
| `desktop` | сборка десктопного клиента (`*.dmg`, `*.zip`, `*.exe`, `*.AppImage`) | десктопный этап релиза |
| `cli` | `goosar_<version>_<os>_<arch>.tar.gz` | CLI-этап релиза; развёртывание также отдаёт их по `/install.sh` |
| `helm` | `goosar-<chart-version>.tgz` | Helm-этап релиза, только для установок в Kubernetes |

CLI агентов и Node не являются артефактами Goosar и в релиз не входят: перенесите их из собственного зеркала в том виде, в каком ваш парк их ставит (достаточно распакованного архива в `PATH`). `goosar doctor` на целевой машине скажет, какого из них не хватает, и покажет строку «Как починить».

Вывод проверки намеренно прямой: `MISSING`, `SIZE MISMATCH`, `CHECKSUM MISMATCH` и ненулевой код возврата при любом из них. Манифест — плоский текст, потому что на изолированной машине нет ни `jq`, ни `node`, ни сети.

> Передайте sha256 самого манифеста другим каналом, чем носитель, так же как для `checksums.txt` на шаге 2: контрольная сумма, которая едет вместе с тем, что защищает, доказывает лишь самосогласованность копии.

### Приёмочный чек-лист

На изолированной машине убедитесь, что:

1. `offline/build-offline.sh --verify-only` проходит (целостность набора), **и** sha256 файла `offline/kit/checksums.txt` совпадает со значением, которое сборщик набора передал отдельным каналом (см. шаг 2).
2. `offline/build-offline.sh` завершается: оба образа собираются под `--network=none`.
3. `docker compose -f docker-compose.selfhost.yml up -d` поднимает PostgreSQL, бэкенд и фронтенд, ничего не скачивая.
4. Веб-интерфейс отображается с правильной типографикой (шрифты вшиты в образ, запросов к `fonts.googleapis.com` из браузера нет).
5. `curl -fsSLO https://<goosar>/install.sh` и `bash install.sh ...` устанавливают рабочий `goosar` CLI из самого развёртывания.
6. `/api/config` объявляет `"delivery_profile":"perimeter"`, а `goosar update` отказывает с сообщением оператору.
7. `docker run --rm --entrypoint apk goosar-web:<tag> list --installed libssl3` показывает версию, которую несёт эта поставка. Сборка из исходников под `--network=none` сохраняет версию базового образа (3.5.7-r0): см. «Сборка OpenSSL в закрытом контуре», где объяснено почему и как обойтись готовым образом.

### Сборка OpenSSL в закрытом контуре

Сборка из исходников внутри периметра несёт OpenSSL из базового образа, закреплённого по digest, а не самый свежий из Alpine. Стадия среды выполнения `Dockerfile.web` обновляет `libssl3` и `libcrypto3` из репозитория Alpine: так онлайн-образ получает 3.5.8-r0 и закрывает критичную уязвимость OpenSSL CVE-2026-14456 (HIGH по оценке trivy). При `--network=none` репозитория нет, и шаг печатает

```text
WARN: could not upgrade libssl3/libcrypto3 — no Alpine repository reachable
```

а сборка продолжается с тем, что несёт база (`node:22-alpine` в нынешнем закреплении: 3.5.7-r0, то есть эта уязвимость HIGH остаётся открытой). Это сознательный компромисс: падение сборки сделало бы изолированную сборку из исходников невозможной, а тега `node:*-alpine` с 3.5.8 для закрепления у апстрима нет.

Проверьте, что именно поставлено, в журнале сборки или в образе: `docker run --rm --entrypoint apk goosar-web:<tag> list --installed libssl3`.

Если поставка обязана проходить сканирование без замечаний, не собирайте веб-образ из исходников в периметре: несите готовый релизный образ (`carry the prebuilt release image`), собранный при доступном репозитории (раздел «Готовые релизные образы» ниже). Пункт 7 приёмочного чек-листа фиксирует, каким из двух путей вы пошли.

### Неполадки offline-сборки

- **`Missing mocked response for URL: …` при сборке веба**: вызов `next/font/google` изменился (семейство, начертания, стили) после генерации набора. Обновите `FONT_DECLARATIONS` в `offline/gen-font-kit.mjs` по образцу layout и сгенерируйте набор заново.
- **`ERR_PNPM_NO_OFFLINE_META` или отсутствующий tarball при `pnpm install`**: `pnpm-lock.yaml` изменился после генерации набора. Сгенерируйте набор заново из того же ref, который собираете.
- **`exec format error` во время сборки** или **определение базового образа лезет в сеть**: платформа набора не совпадает с машиной (пересоберите набор с нужной `--platform`) либо используется драйвер buildx, не видящий загруженные образы (переключитесь на стандартный сборщик docker).
- **`checkout is at … but the kit was built from … — refusing to build`**: git-коммит checkout не совпадает с `git_rev` из `manifest.txt`. Переключитесь на ref, из которого собран набор, или сгенерируйте набор заново из этого checkout. Если расхождение действительно намеренное, добавьте `--allow-rev-mismatch` (скрипт всё равно громко предупредит и продолжит).

### Готовые релизные образы (без сборки из исходников)

Набор выше пересобирает образы из исходников внутри периметра. Если периметр может принимать готовые релизные образы (просто не может достучаться до публичного реестра), пропустите сборку и перенесите опубликованные образы напрямую.

**Задайте `GOOSAR_BACKEND_DIGEST` и `GOOSAR_WEB_DIGEST` до экспорта чего-либо: это рекомендуемый якорь происхождения для этого пути.** `save-release-images.sh` разрешает точные ссылки на образы из вашего `.env`, поэтому то, что закреплено этими двумя переменными, ровно и будет сохранено, перенесено и загружено; если они пусты, весь путь доверяет изменяемому `repo:tag` от начала до конца, и ничто не связывает загруженный образ с тем, что вы собирали. Получить digest:

```bash
docker buildx imagetools inspect "$GOOSAR_BACKEND_IMAGE:<tag>"
docker buildx imagetools inspect "$GOOSAR_WEB_IMAGE:<tag>"
# скопируйте строку верхнего уровня «Digest:» в .env как @sha256:<digest>
```

```bash
# ПОДКЛЮЧЁННАЯ машина: с тем .env, который будете разворачивать (образы для
# экспорта определяют GOOSAR_IMAGE_TAG и GOOSAR_BACKEND_DIGEST / GOOSAR_WEB_DIGEST;
# сначала задайте digest, см. выше):
bash offline/save-release-images.sh --env-file .env --platform linux/amd64
#   └─ offline/kit-images/{release-images.tar,images.list,checksums.txt}

# скопируйте offline/kit-images/ через воздушный зазор, затем на ИЗОЛИРОВАННОЙ машине:
bash offline/load-release-images.sh
docker compose -f docker-compose.selfhost.yml up -d
```

`save-release-images.sh` разрешает точные ссылки на образы из `docker-compose.selfhost.yml` (поэтому закрепление по digest учитывается), скачивает их и сохраняет все три (`postgres`, `backend`, `web`) через `docker save` в один tar с контрольной суммой; `load-release-images.sh` проверяет сумму перед `docker load` и отказывается принимать испорченный или неподписанный tar. Для этого варианта архитектура процессора (`--platform`) на сохраняющей и запускающей машинах должна совпадать.

### Каталог пакетов внутри периметра

Skills, MCP-серверы и общие среды выполнения (Playwright/Chromium) в поставку не входят: это каталог пакетов, точка расширения. Оператор публикует индекс по HTTP(S) и загружает его в хранилище бэкенда; настройку хранилища и его переменные см. в разделе «Каталог пакетов (provisioning store)».

```bash
# Внутри периметра, против индекса, который отдаёт ваше зеркало:
export GOOSAR_PACKAGE_INDEX_TOKEN=...   # необязательный bearer-токен
bash offline/load-provisioning-packages.sh \
  --index-url https://packages.internal.example/catalog/index.json
```

Загрузчик проверяет каждый пакет по sha256 и размеру из индекса, раскладывает его в структуру каталогов `LocalPackageStore` и пересобирает `catalog.json`. Каталог хранилища он берёт из тех же `LOCAL_UPLOAD_DIR` (по умолчанию `./data/uploads`) и `GOOSAR_PROVISIONING_LOCAL_PREFIX` (по умолчанию `provisioning`), что и бэкенд; переопределяется флагами `--store-dir` и `--prefix`. Зеркало, смонтированное на изолированной машине, читается через `file://` в адресе индекса. Перед стартом бэкенда включите хранилище в `.env`: `GOOSAR_PROVISIONING_STORE=local`.

### Профиль десктопа: тонкий и полный

`apps/desktop/scripts/package.mjs` собирает десктоп в одном из двух профилей:

- **Тонкий (`--thin`)** поставляется без вшитых skills, MCP-серверов и среды Playwright/Chromium. Клиент получает их из своего развёртывания при первом запуске по описанному выше потоку пакетов, поэтому установщик легче на сотни МБ, а обновление skill не требует новой сборки. Распаковка идёт встроенным в Node кодеком zstd (внешний `zstd` не нужен), а Playwright/Chromium из каталога (пакет `playwright-browsers`) разрешается раньше вшитого через `PLAYWRIGHT_BROWSERS_PATH`. После первой установки любого пакета демон десктопа сам перезапускается (сразу или когда завершится идущая работа агентов), чтобы установленное стало доступно.
- **Полный (`--require-mcp-servers`, `--require-skills`)** вшивает то, во что разрешаются `$GOOSAR_MCP_SERVERS_DIR`, `$GOOSAR_SKILLS_DIR` и `$GOOSAR_PLAYWRIGHT_BROWSERS_DIR` на машине сборки, и падает, если они не заданы. Нужен для машины, которая не достанет до поставки пакетов даже после установки.

`--thin` несовместим с `--require-*` (отклоняется при разборе аргументов). `scripts/release-local.sh` собирает публичные установщики с `--thin`; полную сборку вызывайте напрямую: `node scripts/package.mjs ... --require-mcp-servers --require-skills`.

Набор покрывает образ бэкенда, веб-образ (в него встроены архивы CLI) и стек Compose; `apps/docs` разворачивается отдельно и в образы не входит. `make dev` и разработка из исходников по-прежнему требуют подключённой машины.

## Каталог пакетов (provisioning store)

Provisioning store — каталог устанавливаемых пакетов (skills, MCP-серверы и среды выполнения), который бэкенд отдаёт десктопным клиентам. Каталог — точка расширения, а не часть поставки: на свежей установке он **пуст по умолчанию**, пакеты не вшиты в образ бэкенда, а сервер лишь читает то хранилище, на которое указывает `GOOSAR_PROVISIONING_STORE`.

Заполнить хранилище можно двумя способами, их можно сочетать:

| Источник | Как | Когда подходит |
| --- | --- | --- |
| HTTP-индекс | `offline/load-provisioning-packages.sh --index-url <url>` раскладывает все пакеты из индекса в локальное хранилище бэкенда | Каталог лежит на любом статическом веб-сервере или в репозитории артефактов, в том числе внутри периметра |
| OCI-реестр | `GOOSAR_PROVISIONING_STORE=oci` — бэкенд зеркалирует реестр при первом запуске | В контуре уже есть реестр, до которого бэкенд достаёт |

### Загрузка каталога из HTTP-индекса

Индекс — один JSON-документ со списком пакетов, их платформой, размером и sha256. Загрузчик скачивает каждый пакет из списка, сверяет каждый файл с sha256 и размером из индекса и только после этого записывает раскладку, которую читает бэкенд. Необязательный bearer-токен передаётся через переменную окружения, а не аргументом командной строки:

```bash
read -rs GOOSAR_PACKAGE_INDEX_TOKEN && export GOOSAR_PACKAGE_INDEX_TOKEN   # необязательно
make selfhost-packages INDEX_URL=https://packages.example.local/catalog/index.json
```

`make selfhost-packages` запускает загрузчик во временный каталог, копирует результат в том загрузок бэкенда, идемпотентно прописывает в `.env` `GOOSAR_PROVISIONING_STORE=local` и перезапускает бэкенд. Сам скрипт можно вызвать и отдельно, например чтобы подготовить хранилище на другой машине:

```bash
bash offline/load-provisioning-packages.sh \
  --index-url https://packages.example.local/catalog/index.json \
  --store-dir ./catalog-store
# ./catalog-store/provisioning/ теперь содержит раскладку хранилища
```

Токен отправляется только на origin самого индекса и только по `https://` (или на loopback-хост). Правила для токена приведены выше в этом разделе.

### Синхронизация каталога из OCI-реестра

Если в контуре есть доступный OCI-реестр, загрузчик не нужен: укажите бэкенду реестр, и при первом запуске он зеркалирует каталог в свой том загрузок.

```bash
# .env
GOOSAR_PROVISIONING_STORE=oci
GOOSAR_PROVISIONING_OCI_URL=https://registry.example.local:5000
GOOSAR_PROVISIONING_OCI_REPOSITORY=goosar/provisioning   # необязательный префикс пути
GOOSAR_PROVISIONING_OCI_USERNAME=goosar                  # необязательно
GOOSAR_PROVISIONING_OCI_PASSWORD=<token>                  # необязательно
```

Что бэкенд делает при запуске:

1. Если том загрузок уже содержит каталог, **ничего не происходит**: каталог, загруженный вручную или перенесённый через воздушный зазор, никогда не перезаписывается.
2. Иначе он перечисляет содержимое реестра (или берёт явный список из `GOOSAR_PROVISIONING_OCI_PACKAGES`), скачивает каждый пакет и **сверяет sha256 и размер каждого файла с его собственным манифестом до записи**. Несовпадение прерывает синхронизацию. `catalog.json` пишется последним, поэтому прерванная синхронизация не оставляет частичного каталога, а следующий запуск повторит попытку.
3. Дальше пакеты отдаются с тома, так что реестр может исчезнуть без ущерба для provisioning. Если синхронизация не удалась целиком, бэкенд пишет причину в лог и продолжает отдавать пакеты прямо из реестра.

Две настройки для неудобных реестров:

| Переменная | По умолчанию | Когда нужна |
| --- | --- | --- |
| `GOOSAR_PROVISIONING_OCI_PACKAGES` | пусто | Реестры, у которых `_catalog` нельзя использовать для перечисления. GHCR — один из них: на `/v2/_catalog` он отдаёт все публичные репозитории реестра, а не запрошенное пространство имён, поэтому перечисление никогда не завершается. Задайте список в виде `name@version,name@version`. |
| `GOOSAR_PROVISIONING_SYNC_TIMEOUT` | `3m` | Крупные пакеты сред выполнения или медленный канал. Синхронизация идёт до того, как HTTP-сервер начинает слушать порт, поэтому это же значение — худший случай добавки ко времени старта. |

Реестры с токенами (GHCR, Harbor, Docker Hub) поддерживаются: бэкенд отвечает на challenge `WWW-Authenticate: Bearer` настроенными именем и паролем и кэширует выданный токен. В `.env` уже лежат остальные секреты развёртывания, файл на хосте имеет права `0600`; относитесь к паролю реестра так же, как к `JWT_SECRET`, и предпочитайте зеркало реестра внутри периметра публичному реестру в продакшене.

### Диагностика provisioning store

| Симптом | Вероятная причина | Что делать |
| --- | --- | --- |
| Десктопные клиенты видят пустой список пакетов; бэкенд записал `provisioning kit is missing: …` | `GOOSAR_PROVISIONING_STORE=local`, а в томе загрузок нет `catalog.json` | Выполните `make selfhost-packages INDEX_URL=…`, либо загрузите каталог командой `bash offline/load-provisioning-packages.sh --index-url <url>`, либо перейдите на синхронизацию из реестра (см. выше). |
| Загрузчик останавливается с `sha256 mismatch for <name>@<version>` | Файл, на который указывает индекс, не тот, что описан в индексе: повреждение, устаревший индекс или подмена | В хранилище ничего не записано. Переопубликуйте пакет или индекс; обходить эту проверку нельзя. |
| Загрузчик останавливается с `refusing to send GOOSAR_PACKAGE_INDEX_TOKEN over plain http` | Токен задан, а адрес индекса — `http://` на не-loopback хосте | Отдавайте индекс по `https://` или уберите токен, если индекс публичный. |
| Бэкенд записал `first-boot kit sync failed … serving from the registry directly` | Реестр недоступен, учётные данные отклонены или синхронизация превысила `GOOSAR_PROVISIONING_SYNC_TIMEOUT` | Прочитайте поле `error=` рядом. Ошибки аутентификации называют `GOOSAR_PROVISIONING_OCI_USERNAME/PASSWORD`; для таймаута достаточно увеличить лимит. Provisioning продолжает работать, просто пока не зеркалирован. |
| Бэкенд записал `blob sha256 … does not match its manifest` | Реестр отдаёт пакет, который расходится с собственным манифестом: повреждение или подмена | Ничего не записано. Отправьте пакет в реестр заново и перезапустите бэкенд; обходить проверку нельзя. |
| Бэкенд записал `GOOSAR_PROVISIONING_STORE=oci requires GOOSAR_PROVISIONING_OCI_URL` | Хранилище включено, но никуда не направлено | Задайте `GOOSAR_PROVISIONING_OCI_URL` или используйте `local` с загруженным каталогом. |
| `GOOSAR_PROVISIONING_OCI_PACKAGES is malformed` | Ссылка не в форме `name@version` или имя содержит разделитель пути | Исправьте список; бэкенд перешёл на перечисление реестра, которое GHCR не поддерживает. |

### Загрузка каталога в развёртывание на Kubernetes

Helm-чарт выставляет параметр хранилища (`backend.config.provisioningStore`), но положить в него нечего: пакетов в образе бэкенда нет. В режиме `local` бэкенд читает их из PVC загрузок, поэтому каталог нужно скопировать туда.

Весь рецепт — `kubectl cp` прямо в под бэкенда: второй под и init Job не нужны. Образ бэкенда основан на Alpine (в нём есть `tar`, который нужен `kubectl cp`), PVC уже смонтирован в `/app/data/uploads`, а копирование в *работающий* под как раз обходит ограничение ReadWriteOnce, описанное ниже.

```bash
# 1. На машине, которая достаёт индекс: загрузите его в локальный каталог
#    в точной раскладке, которую читает бэкенд (catalog.json + <name>/<version>/…).
bash offline/load-provisioning-packages.sh \
  --index-url https://packages.example.local/catalog/index.json \
  --store-dir ./catalog-store
# ./catalog-store/provisioning/ теперь содержит раскладку хранилища

# 2. Скопируйте её в том загрузок пода бэкенда.
POD=$(kubectl -n goosar get pod -l app.kubernetes.io/component=backend -o name | head -n1)
kubectl -n goosar cp ./catalog-store/provisioning "${POD#pod/}:/app/data/uploads/"

# 3. Проверьте, что бэкенд её видит.
kubectl -n goosar exec "${POD#pod/}" -- ls /app/data/uploads/provisioning

# 4. Включите хранилище и дайте Deployment перекатиться.
helm upgrade goosar oci://ghcr.io/adanman/charts/goosar \
  --version <chart-version> -n goosar --reuse-values \
  --set backend.config.provisioningStore=local
```

Закрытый контур: выполните шаг 1 на стороне с доступом, перенесите `catalog-store/provisioning/` через зазор и начните с шага 2 (можно вместо сети использовать зеркало `file://`).

> **Ограничение ReadWriteOnce.** PVC загрузок по умолчанию имеет `accessModes: [ReadWriteOnce]` и потому подключается только к одному узлу за раз. Отдельный под-загрузчик или Job будет бороться с бэкендом за том (`Multi-Attach error`), если только случайно не окажется на том же узле. Копирование в *работающий под бэкенда* этого избегает полностью, поэтому в рецепте нет Job. Если Job всё же нужен (например, чтобы запускать копирование из CI), либо переведите PVC на StorageClass с `ReadWriteMany` (NFS/EFS/CephFS), либо на время остановите бэкенд:
>
> ```bash
> kubectl -n goosar scale deploy/goosar-backend --replicas=0
> # ... запустите Job-загрузчик, дождитесь завершения ...
> kubectl -n goosar scale deploy/goosar-backend --replicas=1
> ```
>
> Файлы окажутся во владении uid бэкенда (1001), потому что `kubectl cp` распаковывает архив внутри контейнера от этого пользователя. Не копируйте в том из вспомогательного пода, запущенного от root, без последующей правки владельца.

Вариант, которому вообще не нужна работа с PVC, — `provisioningStore: oci`: отправьте пакеты в реестр внутри периметра и направьте на него `backend.config.provisioningOci*`. Если реестр есть, выбирайте его; рецепт с `kubectl cp` — для кластеров без реестра.

### Закрытый контур

Для машин, которые не достают хост каталога, отзеркальте индекс и перечисленные в нём пакеты на носитель, перенесите его через зазор и внутри периметра выполните `offline/load-provisioning-packages.sh --index-url file:///path/to/index.json`. Полный сценарий — в разделе «Закрытый контур (offline-поставка)».

## Дефолты развёртывания

В репозитории нет ничего, что относилось бы к вашей организации. Универсальная сборка содержит зарезервированные заглушки (`example.local`, `EXAMPLE.LOCAL`, `sso.example.local`), а в исходниках, локалях и фикстурах настоящий бренд, хост или Kerberos-realm заказчика не фигурирует.

Ваши собственные адреса попадают в систему уже во время работы и через **один** документ. Второго файла, который надо синхронизировать, нет: это тот же JSON с дефолтами развёртывания, который вшивается в DMG, той же формы, что и `~/.goosar/desktop.json`, с добавленным блоком `deployment`.

### Поля

| Поле | Где лежит | Что его читает |
| --- | --- | --- |
| `apiUrl` / `wsUrl` / `appUrl` | верхний уровень | с каким сервером работает десктоп |
| `perimeter.realm` | блок `perimeter` | Kerberos-realm, относительно которого строятся principal для `kinit` |
| `perimeter.principalDomain` | блок `perimeter` | домен, относительно которого строится principal, если адрес для входа и Kerberos-аккаунт живут в разных доменах (необязательно; по умолчанию — realm) |
| `perimeter.noProxy` | блок `perimeter` | внутренние хосты и суффиксы, которые нельзя пускать через прокси |
| `deployment.jiraUrl` | блок `deployment` | адрес Jira в онбординговой подсказке «где взять токен» |
| `deployment.confluenceUrl` | блок `deployment` | адрес Confluence в той же подсказке |
| `deployment.ewsUrl` | блок `deployment` | endpoint Exchange Web Services для почтового инструмента |
| `deployment.mailDomain` | блок `deployment` | подсказка адреса ящика (`name@<mailDomain>`) |
| `deployment.llmApiBase` | блок `deployment` | LLM-шлюз внутри периметра, предлагаемый как пресет «Контур» |
| `deployment.llmModel` | блок `deployment` | идентификатор модели, предлагаемый вместе с этим шлюзом (по умолчанию `openai/coding-medium`) |

Две вещи намеренно не являются полями:

- **Адреса KDC.** `kinit` берёт их из `krb5.conf` самой машины. Настройка, которую никто не читает, — мёртвая конфигурация, и она разошлась бы с настоящей.
- **Сами адреса MCP-серверов.** Они уже приходят из provisioning-оверлея (`GOOSAR_PRESET_OVERLAY`). Если оверлей задаёт `JIRA_URL`, `CONFLUENCE_URL` и `EWS_SERVER_URL`, онбординговые подсказки подхватят их автоматически, и остаётся добавить только `llmApiBase` и `mailDomain`. Явный блок `deployment` побеждает оверлей независимо от порядка загрузки.

Пустое значение остаётся пустым: незаданное поле не заполняется заглушкой. Тогда подсказка онбординга сообщает, что адрес сообщает администратор, а пресет LLM «Контур» не предлагается вовсе — это лучше, чем предзаполнить хост, который никому не отвечает.

### Клиентские секреты

Всё описанное выше — то, что десктопное приложение показывает человеку при онбординге; значение он всё ещё может ввести сам. `GET /api/deployment/client-secrets` — отдельный, более узкий канал: **сервер** передаёт собственные `GOOSAR_LLM_API_KEY` / `GOOSAR_LLM_BASE_URL` / `GOOSAR_LLM_DEFAULT_MODEL` и простые адреса тех записей `deployment_mcp_server`, которые включили рабочие пространства вызывающего, — напрямую управляемой среде выполнения агента, которую установило само приложение. Не среде, найденной в `PATH` пользователя, и не любому другому вызывающему API. Запрос обязан нести заголовок `X-Goosar-Launched-By: desktop`; без него эндпоинт отвечает 403 `client_secrets_not_managed`. Каждая успешная выдача записывается в `admin_audit` как `deployment.client_secrets.issued` с перечнем выданных полей, но без их значений.

Десктоп пишет этим способом только `llm.api_base` / `llm.model` / `llm.api_key` через штатный канал `config set --stdin` CLI агента и только в те поля, которые пользователь не задал сам. Адреса MCP-серверов через этот канал пока **не** записываются: для точечной записи `mcp_servers.<name>.env.<KEY>_URL` целевая запись сервера должна уже существовать как полная, валидная по схеме запись `mcp_servers`, а в поставке нет ничего, что бы её определяло. Ротированный `GOOSAR_LLM_API_KEY` доходит до уже настроенной машины при её следующем запуске или переустановке агента, а не сразу: принудительной инвалидации нет.

`GOOSAR_LLM_API_KEY` / `GOOSAR_LLM_BASE_URL` / `GOOSAR_LLM_DEFAULT_MODEL` должны быть заданы в `.env`, иначе блок `llm` в ответе этого эндпоинта всегда будет `null`. Это другая тройка, чем `GOOSAR_DEPLOYMENT_LLM_API_BASE` / `GOOSAR_DEPLOYMENT_LLM_MODEL` выше: те только предзаполняют форму онбординга, которую человек может править. `docker-compose.selfhost.yml` передаёт контейнеру бэкенда все три `GOOSAR_LLM_*`.

### Вшивание в DMG

```bash
cat > /secure/corp/deployment.json <<'JSON'
{
  "schemaVersion": 1,
  "apiUrl": "https://goosar.example.local",
  "perimeter": {
    "realm": "EXAMPLE.LOCAL",
    "principalDomain": "EXAMPLE.NET",
    "noProxy": [".example.local", ".internal.example.local"]
  },
  "deployment": {
    "jiraUrl": "https://jira.example.local",
    "confluenceUrl": "https://wiki.example.local",
    "ewsUrl": "https://mail.example.local/EWS/Exchange.asmx",
    "mailDomain": "example.local",
    "llmApiBase": "https://llm.example.local/v1",
    "llmModel": "openai/coding-medium"
  }
}
JSON

export GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS=/secure/corp/deployment.json
(cd apps/desktop && node scripts/package.mjs --mac --arm64)
```

Файл кладётся в `Contents/Resources/deployment/deployment.json`. Собственный `~/.goosar/desktop.json` пользователя по-прежнему побеждает для endpoint-ключей и ключей `perimeter`; блок `deployment` — утверждение о том, где живут корпоративные сервисы, и на отдельной машине не переопределяется.

### Задание на сервере

У веб-клиента нет DMG, поэтому тот же набор полей публикует `/api/config` из окружения сервера. Только адреса и никогда не учётные данные: эндпоинт публичный и доступен анонимно, так что относитесь к этим строкам как к тексту на вики-странице, где сотрудникам объясняют, где живёт Jira.

```bash
GOOSAR_DEPLOYMENT_JIRA_URL=https://jira.example.local
GOOSAR_DEPLOYMENT_CONFLUENCE_URL=https://wiki.example.local
GOOSAR_DEPLOYMENT_EWS_URL=https://mail.example.local/EWS/Exchange.asmx
GOOSAR_DEPLOYMENT_MAIL_DOMAIN=example.local
GOOSAR_DEPLOYMENT_LLM_API_BASE=https://llm.example.local/v1
GOOSAR_DEPLOYMENT_LLM_MODEL=openai/coding-medium
```

Незаданные переменные вообще не попадают в ответ, так что развёртывание, где их нет, сохраняет прежний вид `/api/config` байт в байт. Значения перечитываются на каждый запрос: исправление опечатки требует обновить секрет, а не перезапускать сервис.

Есть ещё две переменные `GOOSAR_DEPLOYMENT_*_URL` — `GOOSAR_DEPLOYMENT_BITRIX24_URL` и `GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL`, — но `/api/config` их не читает: они питают только `goosar_admin mcp-library seed` (см. «Администраторы развёртывания»), а не эндпоинт подсказок онбординга. Как и остальные пять, это только адреса, не учётные данные.

```bash
GOOSAR_DEPLOYMENT_BITRIX24_URL=https://b24.example.local/rest/
GOOSAR_DEPLOYMENT_MCP_GATEWAY_URL=https://mcp-gateway.example.local/mcp-proxy
```

## Профили развёртывания

`GOOSAR_DEPLOYMENT_PROFILE` объявляет, **какого рода** это стенд. Это другой вопрос, чем профиль доставки ниже, который объявляет, как стенд доставляется и обновляется: демо-стенд доставляется оператором *и* имеет открытую регистрацию, и одна переменная не может нести оба смысла.

| | `perimeter` (по умолчанию) | `demo` | `dev` | `local` |
| --- | --- | --- | --- | --- |
| `ALLOW_SIGNUP` | `false` | `true` | `true` | `true` |
| `GOOSAR_ROLE_WORKSPACES` | `auto` | `auto` | `auto` | `auto` |
| `GOOSAR_DOWNLOAD_GITHUB_RELEASES` | `off` | `off` | `off` | `off` |
| `GOOSAR_EXTERNAL_IMAGES` | `block` | по умолчанию | по умолчанию | по умолчанию |
| `GOOSAR_PROVISIONING_STORE` | `local` | `local` | `local` | `local` |
| `RATE_LIMIT_API` / `RATE_LIMIT_AUTH` | по умолчанию | по умолчанию | ослаблены (`6000` / `100`) | по умолчанию |
| `GOOSAR_DELIVERY_PROFILE` | `perimeter` | `perimeter` | `perimeter` | `perimeter` |

Установщики записывают этот набор вместе с именем профиля:

```bash
PROFILE=perimeter make selfhost                            # demo | dev | local
.\install.ps1 -WithServer -Profile demo                     # Windows (псевдоним -DeliveryProfile)
GOOSAR_DEPLOYMENT_PROFILE=demo make selfhost               # то же через окружение
```

Пресеты — возможность установщика: они пишут `.env`. У Helm-чарта пресетов профилей нет, он рендерит ровно те значения, что вы задали, поэтому в Kubernetes строки таблицы выбирайте сами.

```yaml
# Helm values — строка `dev`, выписанная явно
backend:
  config:
    deploymentProfile: dev
    roleWorkspaces: auto
    rateLimitApi: "6000"
    rateLimitAuth: "100"
```

Переменную `GOOSAR_DEPLOYMENT_PROFILE` из окружения оба установщика тоже учитывают, если флага нет (`GOOSAR_DEPLOYMENT_PROFILE=perimeter bash scripts/install.sh --with-server`); флаг важнее переменной. Значения обрезаются по краям и приводятся к нижнему регистру, как в собственном парсере сервера. Обрезка только по краям, так что `de mo` — опечатка, а не `demo`. Оба установщика проверяют имя **до** копирования `.env.example`: отклонённое значение не оставляет за собой `.env`, поэтому повторный запуск с исправлением действительно применит профиль, а не пойдёт по пути «существующий `.env` не трогать».

Шаги, которые установщики намеренно оставляют оператору, потому что верное значение — факт о хосте, который установщик угадать не может:

- **На `perimeter` задайте `ALLOWED_EMAIL_DOMAINS` (или `ALLOWED_EMAILS`) до первого запуска, иначе не войдёт никто, включая вас.** Профиль записывает `ALLOW_SIGNUP=false`, а поток входа — единственный путь создания аккаунта, поэтому эти два списка — единственный способ пройти через гейт. `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` их **не** заменяет: он выдаёт роль администратора развёртывания уже существующему аккаунту и никогда его не создаёт.
- `open_join` для ролей — не переменная профиля. Сервер выводит его из режима регистрации: пока регистрация открыта без списка разрешённых, «любой аутентифицированный пользователь» ничем не ограничен, поэтому роли-рабочие пространства создаются **закрытыми**. На `demo`/`dev`/`local` либо задайте список до первого запуска, либо откройте нужные роли в интерфейсе. На `perimeter` создание ролей ждёт появления администратора развёртывания, поэтому роли появляются после первого входа администратора.
- `POSTGRES_SSLMODE=require` (рекомендуется для `perimeter`) требует TLS-оверлея и его сертификатов — см. раздел «Конфигурация».
- `GOOSAR_MAC_DMG_URL` и соседние ссылки на сборки указывают на файлы, которые загружает оператор. Угаданное имя файла дало бы на `/download` битую ссылку, поэтому все профили оставляют их пустыми.
- `GOOSAR_DEPLOYMENT_ADMIN_EMAILS`, `GOOSAR_AUTH_METHODS`, `GOOSAR_TRUSTED_PROXIES` и адреса собственных систем заказчика.

Неизвестное значение — жёсткая ошибка запуска. Как и профиль доставки, профиль записывается только при создании `.env`: на существующем стенде правьте `GOOSAR_DEPLOYMENT_PROFILE` и перезапускайте бэкенд; повторный запуск, в котором назван профиль, предупреждает, что он НЕ применён, а не проходит молча. Если регистрация оказалась закрытой без списка разрешённых, оба установщика повторяют это предупреждение в итоговой сводке. Действующее значение отдаёт `GET /api/status` (`perimeter.deployment_profile`) и `goosar support-bundle`.

## Профиль доставки и исходящий трафик

`GOOSAR_DELIVERY_PROFILE` объявляет, какого рода это развёртывание. Это главный переключатель всех исходящих соединений, которые продукт открывает по собственной инициативе.

| Профиль | Смысл |
| --- | --- |
| `perimeter` | **По умолчанию для self-host.** Установка в закрытой сети под управлением оператора. Самообновления выключены (обновления доставляет оператор), внешние источники skills выключены, а бэкенд не запускается без `GOOSAR_MCP_SECRET_KEY`. |
| `cloud` | Поведение управляемого облака: все источники и каналы самообновления включены по умолчанию. Выбирайте его явно, если хотите, чтобы self-hosted стенд вёл себя как облачный Goosar. |

Установщики задают его так:

```bash
make selfhost                                             # профиль доставки perimeter (по умолчанию)
make selfhost PROFILE=cloud                               # исходящие дефолты облака
```

Профиль записывается в `.env` только при первом создании `.env`. На существующей установке правьте `GOOSAR_DELIVERY_PROFILE` в `.env` и перезапускайте бэкенд.

> **Следствие `perimeter`:** каталог skills ClawHub и импорт skills с GitHub/skills.sh отвечают «source disabled on this deployment»; десктопное приложение и демон перестают предлагать обновления и опрашивать GitHub. Это намеренная цена стенда, который не должен ходить в интернет. Отдельные источники возвращайте через `GOOSAR_SKILL_SOURCES` (см. ниже), а не переключайте всё развёртывание на `cloud`.

### Исходящие соединения по умолчанию и как их выключить

Всё, куда код может обратиться наружу, и чем это отключается. «Да» в колонке про `perimeter` означает, что адрес уже выключен на профиле self-host по умолчанию.

| Адрес | Компонент | Зачем вызывается | Выключен по умолчанию на perimeter? | Переключатель |
| --- | --- | --- | --- | --- |
| `clawhub.ai/api/v1` | бэкенд (поиск и импорт skills) | поисковая строка пользователя уходит в каталог как есть | да | `GOOSAR_SKILL_SOURCES=none` (или уберите `clawhub` из списка) |
| `api.github.com`, `raw.githubusercontent.com` | бэкенд (импорт skill из репозитория) | забрать содержимое skill и метаданные коммита | да | `GOOSAR_SKILL_SOURCES` (уберите `github`) |
| `skills.sh` → `api.github.com` | бэкенд (импорт из skills.sh) | по slug из skills.sh найти репозиторий | да | `GOOSAR_SKILL_SOURCES` (уберите `skillssh`) |
| `api.github.com/.../releases` | опрос автообновления демона | проверить наличие новой версии CLI | да | закрепление профиля на машине или `GOOSAR_DAEMON_AUTO_UPDATE=off` |
| `api.github.com/.../releases` | `goosar update` (CLI) | ручное самообновление | да | профиль (объявленный сервером или заданный локально) |
| `github.com/.../releases/latest/download` | обновление десктопа, начальная установка CLI десктопом | скачать новую сборку десктопа или бинарник `goosar` | да | профиль, который объявляет сервер, к которому подключён десктоп |
| `api.resend.com` | e-mail бэкенда | доставить коды входа | нет, транспорт выбираете вы | оставьте `RESEND_API_KEY` пустым и настройте `SMTP_HOST`/`SMTP_FROM_EMAIL` (внутренний relay) |
| `us.i.posthog.com` (или `POSTHOG_HOST`) | аналитика бэкенда и, при желании, браузер | продуктовая аналитика | нет, выключено, пока не настроено | оставьте `POSTHOG_API_KEY` пустым (по умолчанию); `GOOSAR_ANALYTICS_DISABLED` принудительно включает пустой клиент |
| `api.github.com` (GitHub App) | интеграция бэкенда с GitHub | снимки репозиториев и PR для подключённых рабочих пространств | нет, выключено, пока не настроено | оставьте переменные `GITHUB_APP_*` пустыми |
| ваш LLM-шлюз (`GOOSAR_LLM_BASE_URL`) | LLM-слой бэкенда | функции агентов и LLM | нет, выключено, пока не настроено | оставьте `GOOSAR_LLM_API_KEY`/`GOOSAR_LLM_BASE_URL` пустыми; направьте их на шлюз внутри периметра, чтобы трафик остался внутри |
| ваш OCI-реестр (`GOOSAR_PROVISIONING_OCI_URL`) | provisioning store бэкенда | отдавать пакеты skills/MCP/сред выполнения десктопным клиентам | нет, выключено, пока не настроено | используйте `GOOSAR_PROVISIONING_STORE=local` с каталогом, загруженным с диска (см. «Каталог пакетов (provisioning store)»), или оставьте хранилище незаданным |
| S3 / CloudFront (`S3_BUCKET`, `CLOUDFRONT_DOMAIN`) | хранилище бэкенда | вложения | нет, выключено, пока не настроено | оставьте `S3_BUCKET` пустым, чтобы вложения оставались на локальном томе |
| `api.github.com` (release assets) | страница `/download` веб-контейнера | перечислить скачиваемые сборки CLI и десктопа | нет, включено по умолчанию | `GOOSAR_DOWNLOAD_GITHUB_RELEASES=off` (собственный установщик и ссылки `GOOSAR_MAC_DMG_URL` / `GOOSAR_MAC_X64_DMG_URL` / `GOOSAR_WIN_X64_EXE_URL` / `GOOSAR_LINUX_*_APPIMAGE_URL` продолжают работать) |
| `backend.composio.dev` | интеграция бэкенда с Composio | проксировать вызовы инструментов и подключённые аккаунты через Composio | нет, выключено, пока не настроено | оставьте `COMPOSIO_API_KEY` пустым (интеграция тогда отвечает 503) |
| `logos.composio.dev` | **браузер оператора** | иконки наборов инструментов на вкладках Composio и MCP агента | нет, страница запрашивает их всякий раз, когда вкладки показывают набор | надёжно закрыть можно только на границе сети; при неудачном запросе вкладки показывают буквенный аватар |
| `player.bilibili.com`, `www.youtube-nocookie.com` | **браузер читателя** | видеоплееры, встроенные в страницы документации (`apps/docs/components/video-embed.tsx`) | нет, страница запрашивает их при открытии страницы документации с видео | отдавайте документацию без страниц с видео или закройте на границе сети; плеер — iframe, сервер его не вызывает |
| сторонние MCP-серверы, Jira, Exchange | интеграции бэкенда | всё, что подключит рабочее пространство | нет, настраивается пользователем | не подключайте их; `GOOSAR_ALLOWED_PROVIDERS` ограничивает, какие slug провайдеров вообще можно использовать |

CLI агентов, которые работают на машинах демона, общаются со своими вендорами по собственной конфигурации; профиль perimeter их не ограничивает и не может ограничить. Настраивайте их на самой машине, а не в `.env` Goosar.

Каждый хост, записанный в исходниках литералом, сверяется с этой таблицей при каждом прогоне CI (`scripts/egress-inventory.sh --check` против `scripts/egress-allowlist.txt`), так что новый адрес, написанный в нашем коде, не попадёт в поставку без документации. Проверка не видит хост, спрятанный внутри зависимости: `api.resend.com` живёт в SDK Resend, а не в нашем коде, поэтому в списке разрешённых такие адреса помечены строками `~host` — они задокументированы, но не сверяются автоматически. Добавление SDK, который ходит наружу, — единственный случай, который проверка за вас не поймает. Та же таблица, прочитанная как «какие персональные данные уходят», плюс что продукт хранит и где это можно удалить, ведётся отдельно (для специалиста заказчика по защите данных).

## Корпоративный вход: OIDC и LDAP/AD

По умолчанию у продукта ровно одна дверь: адрес электронной почты и одноразовый код. Во внутреннем развёртывании этого обычно мало: аккаунты заводятся в корпоративном каталоге, и увольнение тоже происходит там.

Двери выбираются переменной **`GOOSAR_AUTH_METHODS`** — списком через запятую из `email`, `oidc`, `ldap`. Пустое значение или отсутствие означает `email`, так что при обновлении ничего не меняется, пока вы этого не скажете.

| Метод | Что это | Когда использовать |
| --- | --- | --- |
| `oidc` | OpenID Connect, Authorization Code + PKCE | **Предпочтительный.** Работает с Keycloak, ADFS, Yandex ID и Yandex Cloud Organization, Okta, Authentik — с любым провайдером, отдающим discovery-документ. |
| `ldap` | простой bind в LDAP / Active Directory, имя пользователя и пароль | Стенды, где есть AD, но перед ним нет провайдера идентификации. |
| `email` | одноразовый код по почте | По умолчанию. Оставьте включённым как путь входа, пока настраиваете остальные. |

**Предпочитайте OIDC.** Простой bind в LDAP означает, что доменный пароль сотрудника проходит через этот продукт: ещё один держатель учётных данных внутри периметра, причём пароль не интеграционный и оседать здесь не должен. OIDC оставляет пароль у провайдера: сервер получает подписанный `id_token` и никогда не видит ни пароль, ни второй фактор. Провайдер, как правило, закрывает и MFA, чего простой bind не даёт никогда.

Полное обоснование решения, потоки и то, что сознательно вне рамок, ведётся отдельно от этого документа.

### Настройка OIDC

Зарегистрируйте у провайдера клиента с redirect URI `https://goosar.example.local/api/auth/oidc/callback`, затем задайте:

```bash
GOOSAR_AUTH_METHODS=email,oidc
GOOSAR_OIDC_ISSUER=https://sso.example.local/realms/corp
GOOSAR_OIDC_CLIENT_ID=goosar
GOOSAR_OIDC_CLIENT_SECRET=...          # пусто = публичный клиент; PKCE применяется в любом случае
GOOSAR_OIDC_REDIRECT_URL=https://goosar.example.local/api/auth/oidc/callback
GOOSAR_OIDC_DISPLAY_NAME=Вход через Keycloak
```

### Настройка LDAP / Active Directory

```bash
GOOSAR_AUTH_METHODS=email,ldap
GOOSAR_LDAP_URL=ldaps://dc.example.local:636
GOOSAR_LDAP_BIND_DN=CN=svc-goosar,OU=Service,DC=example,DC=local
GOOSAR_LDAP_BIND_PASSWORD=...          # служебная учётная запись, используется только для ПОИСКА
GOOSAR_LDAP_BASE_DN=OU=Users,DC=example,DC=local
```

**Обычный `ldap://` без StartTLS отклоняется.** Простой bind передаёт пароль по сети открытым текстом (RFC 4513, §3), поэтому метод не предлагается вовсе, а не деградирует молча. Используйте `ldaps://` либо `ldap://` вместе с `GOOSAR_LDAP_START_TLS=true`.

### Что делает первый корпоративный вход

1. Аккаунт **создаётся при первом входе** из адреса в claim (`email`) или из атрибута каталога (по умолчанию `mail`). При последующих входах записанный адрес и отметка последнего входа обновляются.
2. **Списки разрешённых для регистрации продолжают действовать.** `ALLOW_SIGNUP`, `ALLOWED_EMAILS` и `ALLOWED_EMAIL_DOMAINS` управляют первым корпоративным входом ровно так же, как e-mail-входом: успешный вход в вашем IdP — не разрешение регистрироваться здесь.
3. Существующий аккаунт сопоставляется **сначала по subject каталога, потом по адресу**, поэтому переименование ящика человека сохраняет его аккаунт, задачи и членства, а не заводит незаметно второй аккаунт.
4. **Деактивированный аккаунт отклоняется** независимо от того, что говорит каталог. Отключение человека в каталоге прекращает его доступ при следующей попытке входа; чтобы оборвать живую сессию немедленно, деактивируйте его и здесь (см. «Администраторы развёртывания»).

### Сопоставление группы каталога с администратором развёртывания

```bash
GOOSAR_OIDC_ADMIN_CLAIM=groups
GOOSAR_OIDC_ADMIN_VALUE=goosar-admins
# либо для LDAP:
GOOSAR_LDAP_ADMIN_GROUP=CN=goosar-admins,OU=Groups,DC=example,DC=local
```

**Роль это никогда не выдаёт.** Совпадение создаёт тот же ожидающий запрос, что и API, и оператор подтверждает его на сервере:

```bash
docker compose exec backend ./goosar_admin list-pending
docker compose exec backend ./goosar_admin confirm <id>
```

Выдача роли администратора развёртывания — операция по второму каналу, а группа каталога вторым каналом не является: один и тот же человек администрирует и стенд, и каталог.

### Неполадки корпоративного входа

Страница входа спрашивает у сервера, какие методы показывать (`GET /api/auth/methods`), и ничего не угадывает. Если настроенная вами кнопка не появилась, бэкенд решил, что метод **включён, но настроен не полностью**; какого поля не хватает, он пишет в стартовом логе:

```bash
docker compose logs backend | grep "corporate sign-in"
```

Недоступный каталог или провайдер не роняют продукт: страница входа по-прежнему открывается, предлагает остальные методы и называет причину («Корпоративный каталог не ответил»). Секреты — client secret OIDC и пароль bind в LDAP — читаются только из окружения и никогда не логируются, не попадают в аудит и не отдаются ни одним эндпоинтом.

## MFA и сессии

Два вопроса, которые всегда задаёт проверяющий по информационной безопасности, и ответы, которые может дать это развёртывание: **второй фактор** поверх кода из письма или доменного пароля и явная **политика сессий** — сколько живёт сессия, сколько их может быть у одного человека и кто обязан иметь второй фактор.

### Второй фактор

TOTP (RFC 6238): шестизначный код из приложения-аутентификатора, обновляется каждые 30 секунд. Параметры зафиксированы: **HMAC-SHA-1, 30 с, 6 цифр**. Это не вкус в криптографии, а то, что реально реализуют телефонные аутентификаторы: некоторые вообще игнорируют параметры `algorithm` и `digits` в URI регистрации, и SHA-256 дал бы регистрации, которые сканируются без ошибок, а потом отвергают каждый код.

Где применяется:

| Метод входа | Второй фактор |
| --- | --- |
| Код из письма, одноразовая ссылка для входа | запрашивается, если у аккаунта есть активная регистрация |
| LDAP / Active Directory | запрашивается, если у аккаунта есть активная регистрация |
| OIDC / SSO | **не** запрашивается: аутентификацию выполнил IdP, и у него своя политика MFA. Если вы ей не доверяете, переопределите через `require_mfa = "all"` (см. ниже). |
| Персональные токены доступа, токены CLI, токены задач агента | никогда: это отдельные классы учётных данных, они выдаются процессу и отзываются по строке или по эпохе сессии |

Человек настраивает его в **Настройки → Безопасность**: сканирует QR-код, вводит один код для активации и сохраняет **десять кодов восстановления**, показанных на этом экране. Они показываются один раз и больше не повторяются: каждый действует однократно, и это единственный способ войти, если телефон потерян. Перегенерация делает недействительными все прежние.

**Хранение.** Общий секрет запечатывается на диске кольцом `GOOSAR_MCP_SECRET_KEY` (тот же контейнер, что и для `mcp_config` агентов), а коды восстановления хранятся только как дайджесты SHA-256. Развёртывание без `GOOSAR_MCP_SECRET_KEY` **не может зарегистрировать никого**: эндпоинт регистрации отвечает 503 с `mfa_unavailable`, потому что «второй фактор в открытом виде» не тот вариант, который стоит поставлять. Ключ ротируется командой `goosar_admin rotate-secrets --mfa`, см. «Ротация ключей шифрования».

**Повтор.** Код отклоняется, как только его 30-секундное окно израсходовано, поэтому подсмотренный через плечо код к моменту ввода бесполезен.

**Подбор.** Эндпоинт проверки стоит за тем же ограничителем по IP, что и форма кода из письма, и за вторым бюджетом `RATE_LIMIT_MFA_VERIFY` (10) попыток **на ожидающий тикет входа**: потолок по IP не потолок для того, у кого больше одного адреса, а шесть цифр легко подобрать. Исчерпание бюджета обходится ещё одним первым фактором, который сам ограничен по адресу и по имени пользователя. Блокировки на уровне аккаунта **намеренно нет**: на эндпоинте кода она стала бы примитивом отказа в обслуживании против владельца аккаунта, поэтому бюджет привязан к тикету, а не к аккаунту.

`GOOSAR_TOTP_ISSUER` (необязательно) задаёт название записи в списке аутентификатора. По умолчанию `Goosar`. Задавайте его, если у вас больше одного стенда, чтобы люди различали их в телефоне.

### Break-glass: человек потерял телефон

```bash
docker compose -f docker-compose.selfhost.yml exec backend \
  ./goosar_admin mfa-reset user@example.local
```

Команда снимает фактор и все коды восстановления одного аккаунта и отзывает его сессии; человек входит первым фактором и регистрирует новый аутентификатор. Операция записывается в `admin_audit` как `mfa.reset`.

В отличие от выдачи `deployment_admin`, это действие **выполняется напрямую, а не через ожидающий запрос**. Ожидающий запрос нужен, чтобы изменение *полномочий* шло по второму каналу; здесь полномочия не меняются — у аккаунта снимается учётный фактор, а права остаются какими были. Двухшаговость сделала бы путь восстановления заблокированного человека зависящим от системы, в которую он не может войти. Оператор, который её запускает, и так держит `DATABASE_URL` и мог бы удалить строку руками; команда добавляет к этому запись в аудите.

### Политика сессий

До этой возможности сессии были чистым JWT. Тайм-аут простоя, абсолютное время жизни и ограничение числа одновременных сессий — утверждения о сессии, которые **переживают запрос, создавший её**, а bearer-токен не может честно нести ни одно из них: `exp` выражает фиксированное время жизни и ничего больше. Его нельзя сократить, когда человек ушёл от ноутбука, его нельзя посчитать и нельзя показать человеку списком «вот устройства, где выполнен вход под вами». Поэтому у каждой выданной сессии теперь есть строка (`user_session`), а JWT называет её в claim `sid`.

Эпоха `token_version`, введённая для увольнения сотрудников, не изменилась и остаётся общим для развёртывания рубильником: именно она реально инвалидирует токены, в том числе выданные до появления `sid`.

Политика лежит в **документе политики развёртывания** — том же, что и настройки LLM и MCP, — в блоке `session`. Её можно править в **Настройки → Развёртывание → Сессии и второй фактор** или через API:

```bash
curl -X PUT https://goosar.example.local/api/deployment/policy \
  -H 'Content-Type: application/json' -b goosar_auth=... \
  -d '{"policy":{"session":{"idle_timeout_hours":12,
                            "absolute_lifetime_days":30,
                            "max_concurrent_sessions":0,
                            "require_mfa":"admins"}}}'
```

| Поле | По умолчанию | Диапазон | Что делает |
| --- | --- | --- | --- |
| `idle_timeout_hours` | `12` | 1–720 | Сессия без запросов в течение этого времени отклоняется на следующем запросе. |
| `absolute_lifetime_days` | `30` | 1–365 | Сессия заканчивается через это время после начала, как бы активно её ни использовали. |
| `max_concurrent_sessions` | `0` | 0–100 | `0` — без ограничения. При превышении новый вход отзывает самую старую сессию, но никогда не новую, чтобы забытая сессия на десктопе не могла никого заблокировать. |
| `require_mfa` | `"none"` | `none` / `admins` / `all` | Кто обязан иметь второй фактор. **Рекомендуется `admins`.** |

Значения вне этих диапазонов отклоняются на `PUT`, а не обнаруживаются при следующем входе: часовая блокировка всего развёртывания и «политика» на сто лет — оба способа сломать стенд легальной на вид записью.

**`require_mfa` никого не блокирует.** Включение на работающем развёртывании иначе выбросило бы всех, кто ещё не зарегистрировал фактор. Поэтому аккаунт без фактора всё равно входит, и ему сообщают, что нужно зарегистрировать фактор в Настройки → Безопасность. Принуждение здесь социальное и видимое, а не захлопнутая в 09:00 дверь перед сотрудниками.

Два способа завершить сессии:

- **сам человек** — Настройки → Безопасность показывают все устройства, где выполнен вход под ним (описание браузера или приложения и время последней активности; IP-адрес никогда не показывается: список нужен, чтобы узнавать устройство, а не копить историю местоположений сотрудников), с кнопками «выйти» для устройства и «выйти везде»;
- **администратор развёртывания** — кнопка *Выйти* для пользователя в Настройки → Развёртывание, на случай потерянного ноутбука. От деактивации она отличается намеренно: деактивация — блокировка, которую оператору надо не забыть снять, а это действие человек может обратить сам — просто войдя снова.

Оба способа увеличивают эпоху сессий **и** помечают строки: увеличение эпохи инвалидирует токены, а строки нужны, чтобы список оставался честным.

**Что обычно спрашивает проверяющий и где ответ:**

| Вопрос | Ответ |
| --- | --- |
| Есть ли второй фактор? | TOTP, RFC 6238, для каждого аккаунта; коды восстановления хранятся хешами |
| Где хранится секрет TOTP? | `user_mfa.totp_secret_sealed`, AES-256-GCM под `GOOSAR_MCP_SECRET_KEY`, ротируется |
| Можно ли повторить код? | Нет: принятый 30-секундный шаг записывается и потом отклоняется |
| Можно ли подобрать второй фактор? | Ограничение по IP **и** по ожидающему тикету входа (`RATE_LIMIT_MFA_VERIFY`, 10 попыток); сравнение за постоянное время; блокировки аккаунта нет намеренно |
| Время жизни сессии? | По умолчанию простой 12 ч, абсолютное 30 сут; оба значения настраиваются для развёртывания |
| Может ли администратор оборвать сессию сразу? | Да: для конкретного пользователя, без деактивации аккаунта; действует со следующего запроса |
| Это аудируется? | Каждая регистрация, активация, отключение, проверка, неудача, использование кода восстановления и отзыв попадают в `auth_audit`; `mfa-reset` и записи политики — в `admin_audit` |
| Запрашивается ли второй фактор дважды при входе через SSO? | Нет, если только развёртывание не задаёт `require_mfa = "all"` |

Подробнее о журналах — в разделе «Аудит и экспорт в SIEM».

## Администраторы развёртывания

**Администратор развёртывания** стоит выше владельцев рабочих пространств: эта роль управляет слоем политик развёртывания, общей библиотекой MCP и конфигурацией каждого рабочего пространства. Она хранится в таблице `deployment_admin`, и источник истины — эта таблица, а не переменная окружения.

### Первый администратор

`GOOSAR_DEPLOYMENT_ADMIN_EMAILS` — **начальный список** адресов через запятую, который применяется, только пока таблица ролей пуста:

```bash
# .env (compose)
GOOSAR_DEPLOYMENT_ADMIN_EMAILS=root@example.local,ops@example.local
GOOSAR_MCP_SECRET_KEY=...   # обязателен, пока включена админ-поверхность
```

```yaml
# Helm values
backend:
  config:
    deploymentAdminEmails: "root@example.local,ops@example.local"
```

Адрес из списка получает роль, как только принадлежит реальному пользователю: при запуске бэкенда и — поскольку на свежей установке никто ещё не регистрировался — повторно при **регистрации** этого человека. Обычный первый запуск такой: задайте список, поднимите стек, войдите с этим адресом — и вы сразу администратор.

Когда в таблице есть хотя бы одна строка, список перестаёт что-либо выдавать (он только логируется, если расходится с таблицей). Так задумано: иначе удаление администратора через интерфейс отменялось бы следующим перезапуском. Состав дальше меняйте через API и поток подтверждения ниже, а не правкой `.env`.

> **Список — это заявление о доверии к этим почтовым ящикам.** Поскольку начальный список разрешается и при регистрации, тот, кто первым докажет контроль над адресом из списка (получив код входа), становится первым администратором, и так остаётся, пока таблица ролей пуста. Указывайте только адреса, которые вы уже контролируете, проверьте список на опечатки до первого запуска и сразу войдите с одним из них: окно закрывается, как только появляется первая строка. На экземпляре с открытой регистрацией (`ALLOW_SIGNUP=true`) список ничего лишнего не даёт — регистрация всё равно требует владения ящиком, — но адрес с опечаткой или ещё не принадлежащий вам адрес становится постоянным приглашением на роль.

Обратите внимание: при включённой админ-поверхности `GOOSAR_MCP_SECRET_KEY` обязателен, и бэкенд не запускается без него, потому что иначе администраторы не смогли бы хранить ни ключ шлюза, ни учётные данные MCP. Делайте резервную копию этого ключа вместе с базой данных (см. «Резервное копирование и восстановление»).

### Изменение состава: запрос, затем подтверждение на сервере

API никогда не меняет действующий состав самостоятельно. В **Настройки → Развёртывание** кнопки «Запросить выдачу» / «Запросить отзыв» создают *ожидающий запрос* (HTTP 202), и вкладка показывает его вместе с точной командой, которая его завершает. Очередь ожиданий хранится на сервере, поэтому она переживает перезагрузку страницы и видна каждому администратору.

Второй канал — `goosar_admin`, который поставляется внутри образа бэкенда:

```bash
# Docker Compose
make selfhost-admin ARGS="list-pending"
make selfhost-admin ARGS="confirm <request-id>"
make selfhost-admin ARGS="reject <request-id>"

# ...что равно:
docker compose -f docker-compose.selfhost.yml exec backend ./goosar_admin list-pending

# Kubernetes
kubectl exec deploy/<release>-backend -- ./goosar_admin list-pending
kubectl exec deploy/<release>-backend -- ./goosar_admin confirm <request-id>
```

`confirm` заново проверяет все инварианты в момент решения (цель по-прежнему имеет или не имеет роль; последнего администратора отозвать нельзя никогда), а каждый шаг — запрос, подтверждение, отклонение — записывается в журнал аудита, который вкладка «Развёртывание» показывает только для чтения.

### Break-glass: выдача напрямую

`grant` пропускает ожидающий запрос и сразу делает существующего пользователя администратором. Используйте его для восстановления, когда не осталось администратора, который мог бы создать запрос из интерфейса:

```bash
make selfhost-admin ARGS="grant root@example.local"
kubectl exec deploy/<release>-backend -- ./goosar_admin grant root@example.local
```

Команда идемпотентна (повторный запуск для существующего администратора ничего не меняет), отклоняет адрес, который не принадлежит ни одному пользователю, и пишет собственное действие аудита `deployment_admin.grant.break_glass`. Его легко найти именно потому, что это единственная выдача без запроса за ней.

### Восстановление, когда единственного администратора больше нет

Если последний администратор ушёл из компании или потерял ящик:

1. Убедитесь, что у замены **есть аккаунт пользователя**: пусть войдёт один раз (роль выдаётся только существующим пользователям).
2. Выполните `goosar_admin grant <их-адрес>` на работающем бэкенде, как описано выше.
3. Проверьте через `goosar_admin list-pending` или вкладку «Развёртывание», что состав именно тот, какой вы ожидаете, и отзовите аккаунт ушедшего обычным потоком запроса и подтверждения.

Сброс `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` с перезапуском здесь **не** поможет: начальный список применяется только к пустой таблице. Поддерживаемый путь — `grant`, ручной SQL не нужен.

### Увольнение сотрудника

Три выхода с разной глубиной. Выполняйте их в таком порядке, и живых учётных данных ни у кого не останется.

**Ушёл из одной команды, продолжает работать здесь.** Уберите его из рабочего пространства в Настройки → Участники. Его сессия остаётся действительной — он может состоять в других рабочих пространствах, — но это пространство для него исчезает со следующего запроса, его открытая вкладка отключается от потока событий, каждая среда выполнения агента, которой он владел в этом пространстве, принудительно переводится в офлайн, а её задачи в очереди отменяются. Удаление записывается как `workspace_member.remove`.

**Ушёл из компании.** Администратор развёртывания деактивирует аккаунт:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  https://goosar.example.local/api/deployment/users/<user-id>/deactivate
```

Этот единственный вызов увеличивает эпоху сессий аккаунта — каждый уже выданный ему JWT отклоняется со следующего запроса, без ожидания 30-дневного TTL и без ротации `JWT_SECRET` для всех остальных, — отзывает все его персональные токены доступа и закрывает все живые WebSocket-соединения. Ответ сообщает, что было оборвано (`revoked_tokens`, `closed_connections`), а действие записывается как `user.deactivate`. Его задачи, комментарии и история остаются на месте: след ушедшего сотрудника — то, что делает рабочее пространство аудируемым потом.

`POST .../reactivate` снимает блокировку, если человек вернулся. Он снимает только блокировку: прежние сессии и токены остаются мёртвыми, и человек входит заново. Администратор не может деактивировать собственный аккаунт.

**Потерян ноутбук.** Человек отзывает этот единственный токен в Настройки → Токены; он перестаёт работать немедленно, включая закэшированные проверки.

Права администратора развёртывания — отдельная история: отзывайте их через описанный выше поток «запрос, затем подтверждение». Роль перечитывается из базы на каждом запросе, поэтому отозванный администратор теряет вкладку «Развёртывание» сразу, вмешательство в сессии не требуется.

### Общая библиотека MCP: определения общие, учётные данные нет

MCP-сервер, опубликованный в библиотеке развёртывания (или владельцем рабочего пространства в библиотеке пространства), распространяется как **определение** — команда или URL и *схема* нужных ему учётных данных. Самих значений в нём нет. Каждый пользователь запечатывает свои под `GOOSAR_MCP_SECRET_KEY`, и каждый запуск собирается с учётными данными **того человека, от чьего имени он выполняется**: не администратора библиотеки и не владельца агента.

- участник, который ввёл свои значения, получает сервер с этими значениями, и журнал стороннего сервиса называет *его*;
- участник, который их не ввёл, получает сервер **исключённым из этого запуска**, а в интерфейсе показываются ключи недостающих полей; чужие учётные данные вместо них не подставляются никогда, даже молча;
- запуск, который не разрешал никакой человек (расписание автопилота, webhook), использует учётные данные владельца агента: это его собственная постоянная работа, и другой личности нет.

Практический вывод для администратора: **публикуйте определение, а не раздавайте токен.** Если команде действительно нужен один общий ящик или одна общая учётная запись Jira, создайте в той системе **служебную учётную запись** и передайте её данные тому, кто должен ею оперировать, чтобы он ввёл их как свои значения. Личный токен, введённый для сервера, который может вызывать много людей, делает каждое их действие приписываемым вам.

Детали применения, порядок слияния и то, что демон дополнительно блокирует из собственных файлов MCP-конфигурации машины, описаны в разделе «Конфигурация» (политики allowlist периметра).

#### Наполнение библиотеки из адресов развёртывания

`goosar_admin mcp-library seed` превращает пять переменных `GOOSAR_DEPLOYMENT_*_URL` (Jira, Confluence, EWS, Bitrix24, mcp-gateway; см. «Дефолты развёртывания») в записи `deployment_mcp_server` при любом профиле, так что библиотека MCP свежего развёртывания заполнена без того, чтобы администратор сначала открывал интерфейс.

```bash
make selfhost-admin ARGS="mcp-library seed"                  # записывает записи
make selfhost-admin ARGS="mcp-library seed --dry-run"          # только отчёт
kubectl exec deploy/<release>-backend -- ./goosar_admin mcp-library seed
```

- Сервис, чья переменная с адресом пуста или не задана, пропускается целиком: запись для него не создаётся и не трогается.
- Идемпотентность по `name` (`jira`, `confluence`, `ews`, `bitrix24`, `mcp-gateway`): повторный запуск после смены адреса обновляет только адрес этой записи. Запись, которую администратор потом правил вручную (любое поле, кроме адреса, отличается от того, что дал бы seed), остаётся как есть, с предупреждением, где названы запись и отличие, и никогда не перезаписывается молча.
- `credential_schema` по записям: Jira просит у каждого пользователя `JIRA_PERSONAL_TOKEN`, Confluence — `CONFLUENCE_PERSONAL_TOKEN`, EWS — `EWS_USERNAME`/`EWS_PASSWORD`, Bitrix24 — `B24_WEBHOOK`. Это те же имена полей, что у пресетов онбординга, так что корпоративному развёртыванию не приходится сводить два разных названия одного значения. У mcp-gateway `credential_schema` нет: его bearer-токен — заглушка в аргументах команды, как и в пресете онбординга, а не поле учётных данных пользователя.
- Нужен `GOOSAR_MCP_SECRET_KEY` (библиотека развёртывания запечатана на диске, как и везде в этом разделе): без него команда отказывается работать, а не пишет что-то незапечатанным.

### Создание рабочих пространств

При `DISABLE_WORKSPACE_CREATION=true` обычные пользователи не могут создавать рабочие пространства. Администраторы развёртывания освобождены от запрета, поэтому подключение новой команды не требует править `.env` и перезапускать бэкенд. Каждое такое создание записывается в журнал аудита как `workspace.create.deployment_admin_bypass`.

### Рабочие пространства ролей

Стенд поставляется с каталогом **ролей** как данными — включённые строки `workspace_template` (HR, финансы, юристы, продажи, закупки, маркетолог). При каждом запуске бэкенд создаёт рабочие пространства, которые описывает этот каталог, так что сотрудник выбирает готовую роль, а не собирает её сам. При появлении новых шаблонов в обновлённой версии их пространства создаются на следующем запуске, уже существующие роли не трогаются.

К каждой роли идут также **восемь автопилотов**: расписания её работы, которые создаются вместе с рабочим пространством и стоят на паузе, пока нет исполнителя. Большинство ждёт общего агента роли, который появляется вместе с первой опубликованной средой выполнения.

```bash
# .env (compose)
GOOSAR_ROLE_WORKSPACES=auto   # значение по умолчанию, если пусто
GOOSAR_ROLE_WORKSPACES=off    # ничего не создавать
```

```yaml
# Helm values
backend:
  config:
    roleWorkspaces: "off"
```

Любое другое значение **не даёт запуститься**, называя переменную: `false` или `1` — опечатка, а не синоним `off`.

Что один прогон создаёт для каждого включённого шаблона:

| | |
|---|---|
| рабочее пространство | slug из ключа шаблона (`hr`, `finance`, …; ключ, который продукт зарезервировал как корневой маршрут, например `legal`, или который уже занят существующим пространством, получает суффикс `-role`), название и описание из `display_name` / `description` шаблона |
| владелец | **первый** администратор развёртывания по `granted_at`, как владелец рабочего пространства |
| состав | закрепления пакетов шаблона, его пресеты MCP, запечатанные `GOOSAR_MCP_SECRET_KEY`, и дефолты Helper — ровно то, что применяет «создать рабочее пространство из шаблона» в интерфейсе |
| автопилоты | расписания роли из колонки `autopilots` шаблона: по одному автопилоту на элемент, вместе с его триггерами |

Два предусловия, и оба падают громко, а не на полпути:

- **Хотя бы один администратор развёртывания.** Рабочему пространству роли нужен владелец. При пустой таблице `deployment_admin` прогон ничего не создаёт, логирует причину и повторяется при следующем запуске. Поэтому обычный первый запуск свежего стенда ничего не создаёт, а роли появляются при перезапуске после первого входа администратора (см. «Первый администратор»).
- **Должен быть задан `GOOSAR_MCP_SECRET_KEY`.** Пресеты MCP шаблонов запечатываются на диске; без ключа прогон падает, называя переменную, и не записывает вообще ничего: ни рабочего пространства, ни конфигурации.

Прогон идемпотентен. Роль, чьё рабочее пространство уже существует, остаётся нетронутой, включая название: связь — ключ шаблона, хранящийся в `workspace.template_key`, поэтому переименование пространства роли безопасно, и следующий запуск не создаст второе. Каждое действие журналируется: `role_workspace.provisioned` при создании и `role_workspace.skipped` для роли, которая уже была (пишется при каждом прогоне, и именно это показывает, что провизионер роль увидел и оставил).

Ту же процедуру можно выполнить по требованию, например после включения шаблона или на стенде с `off`:

```bash
make selfhost-admin ARGS="provision-roles"
kubectl exec deploy/<release>-backend -- ./goosar_admin provision-roles
```

Команда печатает по строке на роль (`created` / `skipped` / `error`) и итог и завершается с ненулевым кодом, если какая-то роль не удалась или прогон отложен из-за отсутствия администратора на стенде, чтобы скрипт развёртывания не принял «ничего не создано» за «роли готовы».

#### Автопилоты роли

Каждая строка `workspace_template` описывает автопилоты своей роли в колонке `autopilots` (JSONB, по умолчанию `[]`), и создание ролей заводит их вместе с рабочим пространством, в той же транзакции. Четыре роли из первоначального набора получают по одному расписанию: `hr` — план онбординга, `finance` — сверка таблиц, `legal` — реестр сроков, `sales` — бриф по клиентам; все запускаются по понедельникам утром в часовом поясе `Europe/Moscow`.

Каждый созданный автопилот получает `external_key` вида `template:<ключ шаблона>:<slug>`. По нему повторный прогон узнаёт свой автопилот и **не создаёт второй**, при любом статусе строки: заархивированный автопилот шаблона — это решение администратора развёртывания снять расписание, и создание ролей его не отменяет. Действия журналируются как `role_workspace.autopilot_provisioned` и `role_workspace.autopilot_skipped`, а `goosar_admin provision-roles` печатает число созданных автопилотов в строке каждой роли.

**Уже существующая роль догоняет.** Всё остальное в роли — рабочее пространство, владелец, состав — пишется один раз и больше не пересматривается, но `autopilots` — это данные: стенд, поднятый до появления колонки, и шаблон, которому позже добавили расписание, иначе остались бы без расписаний навсегда. Поэтому прогон, видящий роль «уже созданной», всё равно дописывает недостающие автопилоты, в отдельной транзакции под тем же ключом блокировки роли. Для полной роли это ноль записей; в выводе такая роль остаётся `skipped`, а неудача дозаписи считается ошибкой прогона: `goosar_admin provision-roles` не завершится нулём на роли без расписаний.

**Исполнитель появляется позже рабочего пространства.** Общий агент роли создаётся лениво, при первой опубликованной среде выполнения (см. ниже), поэтому в момент создания ролей его обычно ещё нет: автопилоты создаются в статусе `paused` и без исполнителя. Как только агент роли появляется, автопилоты роли получают его и переходят в `active` — автоматически, в той же транзакции, что и создание агента. Заодно пересчитывается витринное `next_run_at` триггеров: пауза его не двигает, а агент может появиться через недели, и роль иначе показывала бы «следующий запуск» в прошлом. Автопилот, который поставил на паузу или переназначил человек, при этом не трогается.

**Пауза не срабатывает, активация не срабатывает задним числом.** Планировщик берёт только триггеры активных автопилотов, так что расписание, созданное вместе с ролью, молчит до появления исполнителя. В момент активации пропущенный слот тоже не срабатывает: он отбрасывается, если опоздал больше чем на пять минут, и роль начинает работать со следующего понедельника, а не с прошлого.

**Что допускает валидация.** Виды триггеров — только `schedule` и `webhook`; устаревший `api` отклоняется явной ошибкой, а не пропускается молча. `schedule` обязан нести и `cron_expression`, и `timezone`; cron разбирается тем же парсером, что и API, так что нерабочее расписание ломает тесты сида, а не стенд заказчика. Токенов в шаблоне быть не может: токен триггера `webhook` выпускается в момент создания, ровно как на `POST /api/autopilots/{id}/triggers`, а шаблон с полем токена разбор не проходит.

#### Общий агент роли

Каждое рабочее пространство роли обслуживает **один общий агент** — не персональный Helper участника, а агент самой роли: `system_key = goosar_role_<ключ шаблона>`, без владельца, виден всем участникам и доступен им для вызова. Персональный Helper при этом никуда не девается: он остаётся по одному на участника и решает личные задачи, а агент роли общий для всей роли.

Агента нельзя создать вместе с рабочим пространством: он обязан быть привязан к среде выполнения, а у только что созданного пространства их нет. Поэтому он появляется **лениво — при первой среде выполнения с видимостью `public`** в этом пространстве, при первом же обращении к списку агентов или при регистрации демона. Приватная среда выполнения агента не создаёт, и это не недоработка: приватная машина принадлежит одному участнику и никому не предложена, а общего агента может вызвать вся роль, то есть выполнить код на этой машине. Опубликуйте среду выполнения в рабочее пространство, и агент появится сам.

Инструкции агента собираются в одном закреплённом порядке, от общего к частному: встроенный промпт продукта; затем методика общего агента роли (читает всё, что нужно роли; пишет только документы и комментарии к задачам; изменение во внешней системе ставит задачей системному специалисту); затем `helper.extra_instructions` строки шаблона — текст самой роли. Последнее переопределяет предыдущее, поэтому правка одной строки `workspace_template` меняет поведение роли без выкатки кода.

**Кто может его удалить.** Агента роли может архивировать, восстанавливать и редактировать **только администратор развёртывания**. Участник получает 403, и владелец рабочего пространства тоже, если он не администратор развёртывания: агент определяет, чем является роль для всех её участников, и у этого решения один держатель.

То же правило действует на **назначение MCP-серверов** этому агенту: выдать общему агенту роли сервер из библиотеки значит отправить его расшифрованный конфиг (URL, заголовки, токены) на машину, которую может вызвать вся роль, и это то же решение, что правка инструкций. Собственных env-переменных у агента роли не бывает вовсе: значения env выполняются на машине владельца агента, а владельца у него нет, поэтому записать их не может никто, включая администратора развёртывания.

**Правило после удаления.** Архивация агента роли администратором развёртывания — решение, и создание ролей его не отменяет: следующий запуск, как и любое обращение к списку агентов, агента **не воссоздаёт**. Вернуть его значит восстановить строку, а не перезапустить сервис. Ровно два исключения:

- строку заархивировал **сам сервер**, отозвав участника, чья опубликованная машина держала агента: такая архивация освобождает `system_key`, и роль получает своего агента заново на следующей опубликованной машине;
- строку удалили из базы целиком.

Если у строки шаблона снят `enabled` или у рабочего пространства нет `template_key`, агент роли не создаётся вообще — это то же условие, которое останавливает пересоздание самого рабочего пространства роли.

**Вступление в роль без приглашения (`open_join`).** До рабочих пространств ролей единственным входом было приглашение на конкретный адрес, и стенду на сотню сотрудников пришлось бы выписывать по приглашению на каждого человека и роль. Поэтому у рабочего пространства есть колонка `workspace.open_join`: `true` означает, что роль показывается **любому аутентифицированному пользователю развёртывания** на экране выбора рабочего пространства и вступить в неё можно одним щелчком, с ролью `member`.

- **Кто получает `true`.** Только те рабочие пространства ролей, которые создало само создание ролей, и только в момент создания. Уже существующая роль никогда не «дотягивается» до `true` на следующем запуске: роль, которую администратор закрыл руками, не должна открываться обратно от перезапуска. У всех остальных рабочих пространств колонка по умолчанию `false`, то есть поведение стенда, не пользующегося ролями, не меняется.
- **Когда роль создаётся закрытой.** Ограничение «любой аутентифицированный пользователь» осмысленно ровно настолько, насколько ограничена сама регистрация. Поэтому `open_join = true` ставится, только если регистрация ограничена: заданы `ALLOWED_EMAILS`/`ALLOWED_EMAIL_DOMAINS` либо `ALLOW_SIGNUP=false`. При открытой регистрации без списков роли создаются закрытыми, а в лог уходит предупреждение: иначе любой, кто может получить код на почту, вступал бы в HR, финансы, юристов и продажи одним щелчком. Открыть роль после настройки списков: `PATCH /api/deployment/workspaces/{id}` с `{"open_join": true}`.
- **Что защищает эндпоинты.** `GET /api/deployment/join-targets` и `POST /api/deployment/join-targets/{id}/join` — единственные в группе `/api/deployment`, которые отвечают не администратору развёртывания, а обычному сотруднику; в этом их смысл. Границу держат три вещи: регистрация ограничена `ALLOWED_EMAILS` / `ALLOWED_EMAIL_DOMAINS`, деактивированный аккаунт отсекается в аутентификации до хендлера, а агент-актор в эту группу не проходит вовсе. Закрытая или обычная (не ролевая) цель отвечает `404`, а не `403`: закрытая роль не должна подтверждать своё существование по идентификатору. Повторное вступление возвращает `200` и второго членства не создаёт.
- **Переключатель администратора.** `PATCH /api/deployment/workspaces/{id}` с телом `{"open_join": false}` закрывает роль: она пропадает из списка, прямой вызов вступления начинает отвечать `404`, и единственным входом снова становится приглашение. `{"open_join": true}` открывает обратно, но только для рабочего пространства роли: у обычного рабочего пространства `template_key` пуст, оба запроса вступления его всё равно отфильтруют, поэтому запрос отвечает `400`, а не пишет в журнал открытие, которого не произошло. Обе операции пишутся в `admin_audit` действием `workspace.open_join.set`; само вступление сотрудника — не административный акт и в этот журнал не попадает, оно журналируется как обычное добавление участника, ровно как принятие приглашения.

**Отключение на работающем стенде.** Задайте `GOOSAR_ROLE_WORKSPACES=off` и перезапустите бэкенд. Существующие рабочие пространства ролей не затрагиваются: `off` лишь останавливает будущие прогоны. Чтобы вывести из обращения одну роль, а не сам механизм, снимите `enabled = false` со строки её `workspace_template` **в той же операции**, в которой удаляете её рабочее пространство; если удалить одно только пространство, следующий запуск создаст его снова.

## Ротация ключей шифрования

Любой `GOOSAR_*_SECRET_KEY` и `JWT_SECRET` можно ротировать без простоя и без потери уже зашифрованного. Механизм одинаков во всех случаях: **один текущий ключ, которым пишут, плюс список выведенных из обращения ключей, которые ещё принимаются для чтения**; ротация закончена только тогда, когда этот список снова пуст.

### `GOOSAR_MCP_SECRET_KEY`

Этим ключом шифруются документы `mcp_config` агентов, секреты TOTP, определения MCP-серверов рабочих пространств и развёртывания, учётные данные MCP отдельных пользователей, слои конфигурации `mcp_defaults` / `mcp_overrides` и сохранённые API-ключи LLM. Смена ключа без перешифрования оставляет без доступа каждую строку, запечатанную старым ключом.

> **`rotate-secrets` покрывает только первые три пункта этого списка.** Прохода для `workspace_mcp_server.config`, `deployment_mcp_server.config`, `workspace_mcp_user_credential.sealed_values`, `workspace_config.mcp_defaults` / `user_config_override.mcp_overrides` и сохранённых API-ключей LLM пока нет, поэтому чистое `stale=0 unreadable=0` **не** означает, что выведенный ключ больше не используется. Если очистить `GOOSAR_MCP_SECRET_KEY_PREVIOUS` (шаг 6), пока такие строки существуют, они навсегда перестанут расшифровываться. Пока проходов нет: либо оставьте выведенный ключ в `_PREVIOUS`, либо введите эти учётные данные заново вручную после ротации. Та же команда печатает это же предупреждение.

Пятнадцать минут, один человек, без перерыва в работе сервиса.

Тот же ключ запечатывает и **секреты TOTP** второго фактора. Они входят в тот же проход: добавьте `--mfa` на шагах 4 и 5, запустите `--mcp --mfa` вместе или укажите `--all`, что равно им обоим. Не пропускайте их: фактор, оставшийся запечатанным выведенным ключом, хуже нечитаемых учётных данных, потому что человек проходит первый фактор, а потом получает отказ на верно введённом коде.

`GOOSAR_VCS_SECRET_KEY` и `GOOSAR_SLACK_SECRET_KEY` принимают тот же переходный список `_PREVIOUS`, но **у `rotate-secrets` для них прохода пока нет**; прежде чем трогать любой из них, прочитайте «Ключи VCS / Slack» ниже.

```bash
# 1. Сгенерируйте новый ключ. Старое значение сохраните: оно нужно на шаге 2.
openssl rand -base64 32

# 2. Правьте .env: СТАРЫЙ ключ переезжает в _PREVIOUS, новый занимает его место.
#    GOOSAR_MCP_SECRET_KEY_PREVIOUS=<старый ключ>
#    GOOSAR_MCP_SECRET_KEY=<новый ключ>
$EDITOR .env

# 3. Перезапустите бэкенд. С этого момента он ЗАПЕЧАТЫВАЕТ новым ключом и
#    ОТКРЫВАЕТ любым из двух: ничего не сломалось, но и перешифровано пока ничего.
docker compose -f docker-compose.selfhost.yml up -d backend

# 4. Сколько ещё лежит на старом ключе? (ничего не пишет)
docker compose -f docker-compose.selfhost.yml exec backend \
  ./goosar_admin rotate-secrets --mcp --mfa --dry-run

# 5. Перешифруйте. Идемпотентно и прерываемо: если остановилась, запустите снова.
docker compose -f docker-compose.selfhost.yml exec backend \
  ./goosar_admin rotate-secrets --mcp --mfa

# 6. Убедитесь, что `stale=0 unreadable=0` (команда завершается с НЕНУЛЕВЫМ
#    кодом, пока есть нечитаемая строка), затем ОПУСТОШИТЕ
#    GOOSAR_MCP_SECRET_KEY_PREVIOUS в .env и перезапустите. Только теперь
#    старый ключ мёртв, и его можно уничтожить.
```

`rotate-secrets` отказывается работать без текущего ключа, пропускает строки, уже перешифрованные им, выполняет каждую запись как compare-and-swap (так что пользователь, редактирующий агента в тот же момент, побеждает и не затирается), не трогает и сообщает любую строку, которую не открывает ни один ключ кольца, записывает завершённый проход в `admin_audit` как `secret.rotate` и **завершается с ненулевым кодом, если оставил нечитаемую строку**, так что скриптовая ротация не примет «ничего не удалось расшифровать» за готовность.

`_PREVIOUS` принимает список через запятую, поэтому прерванную ротацию можно продолжить позже, даже после второй смены ключа. **Некорректная запись в нём — ошибка запуска, а не предупреждение**: эта переменная существует только во время ротации, а это ровно тот момент, когда молча проигнорированный ключ навсегда делает сохранённые учётные данные нечитаемыми.

> **Не пропускайте шаг 6.** Развёртывание, где `_PREVIOUS` остался заполненным, — это развёртывание, где скомпрометированный ключ по-прежнему открывает каждую резервную копию.

**В Kubernetes** та же последовательность, ключи лежат в Secret:

```bash
kubectl create secret generic goosar-secrets --dry-run=client -o yaml \
  --from-literal=GOOSAR_MCP_SECRET_KEY="$(openssl rand -base64 32)" \
  --from-literal=GOOSAR_MCP_SECRET_KEY_PREVIOUS="$OLD_KEY" \
  ... | kubectl apply -f -
kubectl rollout restart deployment/goosar-backend
kubectl exec deploy/goosar-backend -- ./goosar_admin rotate-secrets --mcp
# затем уберите GOOSAR_MCP_SECRET_KEY_PREVIOUS из Secret и перекатите снова
```

### Ключи VCS / Slack

Сегодня `rotate-secrets` перешифровывает только то, что запечатывает `GOOSAR_MCP_SECRET_KEY` (`mcp_config` агентов). У ключей VCS и Slack то же кольцо ключей: новый ключ запечатывает, выведенные ключи в `_PREVIOUS` продолжают расшифровывать, но **перезапечатать их сохранённые строки пока нечем**. Поэтому для этих трёх:

- держите выведенный ключ в `<VAR>_PREVIOUS`, пока существуют строки интеграций, созданные под ним, **либо**
- отключите и заново подключите затронутые интеграции (учётные данные вводятся заново и запечатываются новым ключом), затем опустошите `_PREVIOUS`.

Если опустошить `_PREVIOUS` для одного из этих трёх, пока строки ещё зависят от выведенного ключа, эти учётные данные станут нечитаемыми: интеграция начнёт отвечать 503, и её придётся подключать заново.

### `JWT_SECRET`

`JWT_SECRET` подписывает сессии; он ничего не шифрует на диске, так что перешифровывать нечего. Его ротация делает недействительным каждый выданный токен: после утечки в этом смысл, в остальных случаях — лишний массовый выход из системы. Разницу делает `JWT_SECRET_PREVIOUS`:

```bash
# Ротация для гигиены, никто не разлогинивается:
#   JWT_SECRET_PREVIOUS=<старый секрет>     ← по-прежнему проверяет подпись
#   JWT_SECRET=<openssl rand -hex 32>       ← подписывает с этого момента
# Подождите один TTL токена аутентификации (по умолчанию 30 суток), затем опустошите _PREVIOUS.

# Компрометация: смените JWT_SECRET и оставьте _PREVIOUS ПУСТЫМ. Каждая сессия
# в развёртывании умирает на следующем запросе.
```

У переходного окна **нет собственного срока**: выведенный секрет продолжает выдавать действительные сессии, пока вы не опустошите переменную, поэтому бэкенд пишет предупреждение при каждом запуске, пока задан `JWT_SECRET_PREVIOUS`. При `APP_ENV=production` известное значение-заглушка в ней (например, встроенный секрет для разработки) не даёт запуститься, как и в `JWT_SECRET`: иначе первая ротация с dev-секрета оставила бы публичное значение по умолчанию способным подделывать сессии.

**Чтобы разлогинить одного человека, `JWT_SECRET` не трогайте вообще.** Деактивация аккаунта (или отзыв его сессий) увеличивает эпоху сессий этого аккаунта, которую middleware аутентификации проверяет на каждом запросе: действие немедленное, точечное и не затрагивает чужие сессии. См. «Увольнение сотрудника» в разделе «Администраторы развёртывания». Ротация ключа подписи — молоток для всего развёртывания, оставленный для утечки самого секрета.

## Аудит и экспорт в SIEM

Журналы аудита хранятся в двух таблицах:

| Журнал | Что записывает |
| --- | --- |
| `admin_audit` | Изменения администрирования развёртывания: выдача и отзыв ролей, запись политик и конфигурации рабочего пространства, завершённые ротации ключей. Хранит **хеши** значений до и после изменения, сами значения не хранит |
| `auth_audit` | Аутентификация и доступ к секретам: отправленные и проверенные коды входа, неудачные входы с причиной, вход по magic-ссылке, отклонённые учётные данные, жизненный цикл PAT, изменения состава рабочего пространства, чтение и запись конфигурации MCP агента (`secret.read` / `secret.write`). Остальные зашифрованные колонки как обращение к секретам пока не журналируются: MCP-серверы развёртывания попадают в `admin_audit` как `deployment_mcp_server.*`, а MCP-серверы рабочего пространства, пользовательские MCP-учётные данные и сохранённые ключи LLM API строк доступа к секретам не порождают |

Ни один из журналов не хранит секреты: ни кодов входа, ни токенов, ни хешей токенов, ни паролей, ни расшифрованной конфигурации. Поле `reason` — машиночитаемый слаг (`invalid_code`, `session_revoked`, `deactivated` и т. д.). Актор до аутентификации записывается как несолёный псевдоним адреса почты, поэтому неудачные попытки одного человека сопоставляются между собой, а журнал не превращается в копию каталога пользователей.

### Push: сбор из stdout контейнера

Каждое событие дополнительно пишется одним JSON-объектом на строку в **stdout**, тогда как обычные логи приложения уходят в **stderr**. Коллектор, подключённый к stdout, получает только события аудита, и собственный парсер не нужен:

```json
{"time":"2026-09-08T09:12:44.183Z","logger":"audit","schema":1,
 "action":"auth.login_code.failed","actor_type":"anonymous",
 "actor_id":"9f86d081884c7d65","target_type":"email","outcome":"failure",
 "reason":"invalid_code","client_ip":"198.51.100.9","request_id":"req-7f3c",
 "user_agent":"Mozilla/5.0"}
```

| Поле | Всегда есть | Значение |
| --- | --- | --- |
| `time` | да | RFC3339 с наносекундами, UTC |
| `logger` | да | всегда `audit`; по нему выбирают этот поток |
| `schema` | да | версия набора полей; растёт только при несовместимом изменении |
| `action` | да | имя события (`auth.login_code.failed`, `secret.read` и т. д.) |
| `actor_type` | да | `user`, `agent`, `system` или `anonymous` |
| `outcome` | да | `success`, `failure` или `denied` |
| `target_type` | да | над чем выполнено действие (`user`, `session`, `agent_mcp_config` и т. д.) |
| `actor_id`, `actor_role`, `target_id` | нет | опускаются, если пусты |
| `reason` | нет | слаг, для неудач и отказов |
| `workspace_id`, `request_id` | нет | `request_id` связывает событие со строкой лога приложения |
| `client_ip`, `user_agent` | нет | `client_ip` учитывает `GOOSAR_TRUSTED_PROXIES`: `X-Forwarded-For` от недоверенного узла игнорируется, поэтому записанный адрес нельзя подделать |

Vector:

```toml
[sources.goosar]
type = "docker_logs"
include_containers = ["goosar-backend"]

[transforms.audit]
type = "remap"
inputs = ["goosar"]
source = '''
  . = object!(parse_json!(.message))
'''
[transforms.audit_only]
type = "filter"
inputs = ["audit"]
condition = '.logger == "audit"'
```

Filebeat:

```yaml
filebeat.inputs:
  - type: container
    paths: ["/var/lib/docker/containers/*/*.log"]
    json.keys_under_root: true
processors:
  - drop_event:
      when.not.equals.logger: "audit"
```

rsyslog через драйвер логирования compose: контейнер получает тег, а пересылает записи демон Docker.

```yaml
# docker-compose.selfhost.yml, сервис backend
logging:
  driver: syslog
  options:
    syslog-address: "udp://siem.example.local:514"
    tag: "goosar-audit"
```

### Pull: опрос API

`GET /api/deployment/audit` (только администраторы развёртывания) отдаёт **оба** журнала одной объединённой лентой.

```bash
# Сначала новые записи: так показывает интерфейс администратора, это режим по умолчанию.
curl -H "Authorization: Bearer $TOKEN" \
  "https://goosar.example.local/api/deployment/audit?limit=100"

# Коллектор: начните с пустого курсора и при каждом опросе передавайте курсор
# ПОСЛЕДНЕЙ записи. Так эндпоинт переключается на порядок «сначала старые».
curl -H "Authorization: Bearer $TOKEN" \
  "https://goosar.example.local/api/deployment/audit?cursor=&limit=200"
```

| Параметр | Значение |
| --- | --- |
| `limit` | размер страницы; сервер ограничивает его сверху |
| `action` | точное имя события |
| `actor` | идентификатор пользователя или псевдоним адреса почты актора до аутентификации |
| `since`, `until` | границы по времени, RFC3339 |
| `source` | `admin`, `auth` или `all` (по умолчанию) |
| `cursor` | позиция продолжения; пустая строка означает «с начала» |

Для коллектора используйте курсор, а не `since`. При порядке «сначала новые» коллектор, отставший больше чем на страницу, молча пропустит всё, что между ними; курсор идёт вперёд и пропустить ничего не может.

### Срок хранения

`GOOSAR_AUDIT_RETENTION_DAYS` (по умолчанию **365**) действует на оба журнала. Задача запускается при старте и затем раз в сутки, удаляя строки старше окна. Значение `0` хранит журналы бессрочно; это правильный выбор, когда удалением управляет внешний архиватор или регламент. Прежде чем сокращать срок, выгрузите данные: очистка удаляет строки, а не архивирует их.

## Диагностика

Логи приложения идут в **stderr**, а журнал аудита занимает **stdout** (см. «Аудит и экспорт в SIEM»). `docker compose logs` показывает оба потока вместе; если нужны только события аудита, подключайте коллектор к одному stdout.

```bash
docker compose -f docker-compose.selfhost.yml logs -f backend
```

### Формат и уровень

| Переменная | Значения | По умолчанию |
| --- | --- | --- |
| `LOG_FORMAT` (или `GOOSAR_LOG_FORMAT`) | `json`, `text` | `json` в Docker-стеке и Helm-чарте; `text` при прямом запуске бинарного файла |
| `LOG_LEVEL` (или `GOOSAR_LOG_LEVEL`) | `debug`, `info`, `warn`, `error` | `info` при `APP_ENV=production`, иначе `debug` |
| `LOG_MAX_SIZE` | предел размера одного файла драйвера Docker `json-file` | `50m` |
| `LOG_MAX_FILE` | число хранимых файлов на контейнер | `5` |

В обоих форматах каждая строка содержит полную метку времени: RFC3339 с миллисекундами и смещением UTC, так что лог, переживший полночь или пересёкший часовой пояс, остаётся привязанным ко времени. В формате `json` строка — один объект:

```json
{"time":"2026-09-08T12:04:11.482+03:00","level":"INFO","msg":"http request",
 "method":"GET","path":"/api/issues","status":200,"duration":"14.2ms",
 "request_id":"c0ffee-000012","user_id":"1f2e..."}
```

Первая строка, которую бэкенд пишет после старта, называет фактически выбранные уровень и формат (`msg="logging configured" level=info format=json`). Прежде чем считать, что переменная применилась, проверьте эту строку.

Размер логов контейнера ограничивает драйвер Docker (`LOG_MAX_SIZE` x `LOG_MAX_FILE` на сервис, по умолчанию около 250 МБ). **В Kubernetes ротация лежит на кластере**: kubelet ограничивает логи контейнеров параметрами `containerLogMaxSize` / `containerLogMaxFiles` (по умолчанию 10Mi x 5), и Helm-чарт их задать не может. Проверьте значения на своих узлах.

В логах нет адресов почты, токенов и зашифрованных значений. Человек в них — это `user_id`, а актор до аутентификации — `email_hash`, тот же несолёный псевдоним, что и в журнале аудита; строки об одном человеке сопоставляются, и хранилище логов не становится копией каталога пользователей. Единственное исключение — строка `[DEV] Verification code for ...`. Она существует только при `APP_ENV` не `production` **и** без настроенного почтового транспорта: это путь входа для разработчика (`make selfhost-code`). При `APP_ENV=production` такого пути нет, сервер отказывает во входе ответом `503 email_not_configured`.

### Как связать ошибку, которую видит пользователь, со строкой лога

Каждый ответ содержит заголовок `X-Request-ID`, и каждое тело ошибки повторяет его:

```json
{"error": "workspace not found", "request_id": "c0ffee-000012"}
```

Попросите у человека это значение и найдите его в логах. Оно есть в строке запроса и в каждой серверной строке, записанной при его обработке:

```bash
docker compose -f docker-compose.selfhost.yml logs backend | grep c0ffee-000012
```

### Support bundle

Когда с проблемой должен разбираться кто-то ещё, `goosar support-bundle` собирает один архив вместо переписки с вставленными логами:

```bash
goosar support-bundle --dry-run          # показать, что будет собрано
goosar support-bundle                    # goosar-support-<UTC>.tar.gz в текущем каталоге
goosar support-bundle --output /tmp/b.tar.gz --lines 2000
```

| Файл | Что внутри |
| --- | --- |
| `manifest.txt` | каждый файл с размером и sha256; усечённый архив виден сразу |
| `versions.txt` | версия и коммит CLI, состояние демона, ответ сервера `/api/config` (включая `min_daemon_version`), версия desktop-приложения |
| `doctor.json` | отчёт `goosar doctor --output json` для этой машины |
| `daemon.log.txt`, `daemon.err.log.txt` | хвосты лога демона и его вывода при аварийном завершении |
| `config.txt` | действующая конфигурация CLI и переменные окружения `GOOSAR_*` / `LOG_*`, прокси и Kerberos |
| `os.txt` | платформа, число ядер, имя хоста, время UTC и локальное (расхождение часов видно здесь) |
| `network.txt` | доступность и задержка `/healthz`, `/readyz`, `/api/config` |
| `server.txt` | **только если задан `DATABASE_URL`**: состояние миграций, готовность, 15 самых больших таблиц, число строк аудита за последние 24 часа |

`goosar doctor --server` дополнительно вызывает `GET /api/llm/health`, и результат попадает в строку `llm` файла `doctor.json`. Это собственная аутентифицированная проверка сервера: доступность `GOOSAR_LLM_BASE_URL` с ключом `GOOSAR_LLM_API_KEY`, результат кэшируется на 60 с. Вердикты: `ok`, `auth_rejected`, `degraded`, `unreachable`, `unconfigured`; ключ и тело ответа апстрима в нём не возвращаются. Ограничение доступа то же, что у `/api/effective-config`: аутентифицированный человек.

Строка онбординга desktop о прокси px опирается на `POST /api/runtimes/{id}/mcp-verified`. Эта проверка на деле запускает единственный настроенный в рабочем пространстве stdio MCP-сервер с `HTTPS_PROXY` / `HTTP_PROXY`, направленными на локальный прокси px, и ждёт ответа на `initialize`: `ok`, `proxy_unreachable` или `mcp_failed(...)`. Сервер хранит только последний результат по каждой среде выполнения и **только в памяти**: после перезапуска процесса он теряется, истории и постоянного хранилища нет.

Запускайте команду на машине, где возникла проблема. На рабочей станции она отвечает на вопрос «что видит этот демон», а на хосте сервера добавляется раздел `server.txt`.

Каждый текстовый файл перед записью проходит маскирование в `server/pkg/redact`: собственные токены Goosar `gsl_` / `mdt_` / `mat_`, токены сторонних сервисов, строки подключения, заголовки `Authorization: Bearer` / `Basic`, пути к keytab, российские номера телефонов, ИНН, СНИЛС и номера паспортов при наличии подписи, номера карт, проходящие проверку Луна, и кириллические почтовые адреса заменяются на `[REDACTED ...]`. Секрет с ключом в кавычках (`"password": "..."` в JSON-логе демона), строка подключения или подпись `пароль:` на кириллице маскируются так же, как простое `NAME=value`. Голые числа, лишь похожие на идентификаторы (номера миграций, идентификаторы задач, метки времени), намеренно не трогаются, чтобы архив оставался читаемым. Перед отправкой архива просмотрите `manifest.txt` и сами файлы.

## Резервное копирование и восстановление

Два хранилища содержат всё, что нельзя восстановить из образов:

| Хранилище | Что в нём | Как сохраняется |
| --- | --- | --- |
| PostgreSQL (том `pgdata`) | рабочие пространства, пользователи, задачи, комментарии, агенты, сессии, зашифрованные учётные данные интеграций | `db.dump` (`pg_dump -Fc`) |
| Том `goosar_backend_uploads` | пакеты хранилища provisioning и вложения (локальный дисковый бэкенд хранения) | `uploads.tar` |

### Создание резервной копии

```bash
# из каталога, где лежат .env и docker-compose.selfhost.yml
bash scripts/backup.sh                 # пишет ./backups/goosar-<метка UTC>/
bash scripts/backup.sh --out /srv/backups
```

Стек можно не останавливать: база выгружается через работающий сервис `postgres`, а том с загрузками читает одноразовый контейнер, так что ничего не прерывается.

**Окно согласованности.** `db.dump` — один транзакционный снимок. `uploads.tar` снимается *после* него с работающего тома, поэтому два файла относятся к разным моментам. Каждый файл, на который ссылается восстановленная строка, существовал на момент чтения архива (если его не удалили в промежутке), а файлы, загруженные в это окно, попадают в архив как «осиротевшие»: восстановленная база о них не знает. Если оба хранилища нужны с одного момента, сначала остановите `backend` (`docker compose -f docker-compose.selfhost.yml stop backend`), сделайте копию и запустите его снова.

В каталоге копии лежат `db.dump`, `uploads.tar` и `manifest.json`:

```json
{
  "format": 1,
  "created_at": "2026-09-07T12:20:50Z",
  "image_tag": "v1.0.0",
  "schema_version": 265,
  "postgres_db": "goosar",
  "postgres_user": "goosar",
  "files": {
    "db.dump": { "sha256": "42568b82...", "bytes": 323280 },
    "uploads.tar": { "sha256": "6d4e4166...", "bytes": 3072 }
  }
}
```

`schema_version` — наибольший применённый номер миграции. `restore.sh` по нему отказывается восстанавливать дамп новее образа бэкенда, в который идёт восстановление.

Скрипт никогда не оставляет частичную копию незамеченной. Он собирает её в `goosar-<метка>.partial` и переименовывает только после того, как каждая часть создана и для неё посчитана контрольная сумма. Если падает `pg_dump` или архивация загрузок, запуск завершается с ненулевым кодом и не оставляет каталога. В cron проверяйте код возврата: отсутствие каталога и есть сигнал сбоя.

### Проверка копии без восстановления

```bash
cd backups/goosar-20260907T122050Z
shasum -a 256 db.dump uploads.tar    # сравните с manifest.json
```

### Восстановление

Восстановление разрушает текущие данные и требует явного `--confirm`:

```bash
bash scripts/restore.sh backups/goosar-20260907T122050Z --confirm
```

Скрипт останавливает `backend` и `frontend`, поднимает `postgres`, удаляет и заново создаёт базу, выполняет `pg_restore` в одной транзакции (при сбое загрузка откатывается целиком, и остаётся пустая база, а не частично загруженная), очищает том загрузок и заново распаковывает в него архив, затем запускает стек. Бэкенд при старте применяет миграции вперёд, поэтому копию более старого выпуска можно восстановить в более новый.

Целевая база определяется значениями `POSTGRES_DB` / `POSTGRES_USER`, с которыми запущен стек: именно на неё указывает `DATABASE_URL` бэкенда. Имена в `manifest.json` носят справочный характер: если они отличаются, скрипт сообщит об этом и всё равно восстановит данные в базу стека.

Если шаг с загрузками упал после успешного `pg_restore`, скрипт завершается с ненулевым кодом, говорит об этом и **не** запускает стек: база восстановлена, том нет. Запустите восстановление повторно (оно повторяет оба шага), а не поднимайте стек вручную.

До любых изменений скрипт громко и без побочных эффектов отказывается работать, если:

- отсутствует `manifest.json`, `db.dump` или `uploads.tar`;
- sha256 не совпадает с манифестом (копия повреждена или усечена);
- `schema_version` копии **новее** самой новой миграции в образе бэкенда, с которым работает стек (её читает одноразовый контейнер с `--no-deps`, так что свежескачанный checkout скрипт не обманет). Дамп новее кода восстановить нельзя: пути отката миграций нет. Задайте `GOOSAR_IMAGE_TAG` равным выпуску из `image_tag` и восстанавливайте с ним;
- не указан `--confirm`.

### Что входит в копию: относитесь к ней как к хранилищу учётных данных

`db.dump` — вся база: каждое сообщение, задача и комментарий, почта каждого пользователя, хеши сессий и API-токенов, все учётные данные интеграций развёртывания. Учётные данные интеграций зашифрованы ключами `GOOSAR_*_SECRET_KEY`, **кроме случая**, когда `GOOSAR_MCP_SECRET_KEY` не задан: тогда `mcp_config` агента (в нём обычно лежат корпоративные PAT и bearer-токены шлюзов) хранится, а значит и выгружается, открытым текстом. В каталоге копии нет `.env`, `POSTGRES_PASSWORD` и ключей подписи; `manifest.json` содержит только имена базы и роли.

Храните копии с теми же ограничениями доступа, что и саму базу, а их копии шифруйте на носителе.

### Что копия не покрывает

- **`.env` и его секреты.** `JWT_SECRET`, `POSTGRES_PASSWORD`, `GOOSAR_VCS_SECRET_KEY`, `GOOSAR_MCP_SECRET_KEY`, `GOOSAR_SLACK_SECRET_KEY` и остаются на ответственности оператора: храните их в собственном менеджере секретов. Потеря `GOOSAR_MCP_SECRET_KEY` или `GOOSAR_VCS_SECRET_KEY` навсегда делает нечитаемыми зашифрованные колонки в восстановленной базе, потеря `JWT_SECRET` аннулирует все сессии.
- **Вложения в S3.** Если задан `S3_BUCKET`, вложения лежат в бакете, а не в томе загрузок. Используйте версионирование или репликацию самого бакета.
- **Образы Docker.** Их заново скачивают из реестра или берут из комплекта закрытого контура (см. «Закрытый контур (offline-поставка)»).
- **Восстановление на момент времени.** Это полные снимки, а не архив WAL. Если ваш RPO меньше интервала копирования, параллельно настройте непрерывную архивацию (`pgBackRest`, WAL-G).
- **Развёртывание в Kubernetes.** `scripts/backup.sh` работает с Docker Compose. В Helm-чарте те же два хранилища — по одному PVC: `<release>-postgres-data` и `<release>-backend-uploads`. Эквивалент: `kubectl exec` в под postgres для `pg_dump -Fc`, `kubectl exec ... tar -cf -` в поде бэкенда для PVC с загрузками либо снимки томов средствами вашего CSI-драйвера. Дисциплина манифеста, контрольных сумм и отказов, описанная выше, применяется без изменений.

### Расписание и хранение

Ночные полные копии на другой хост со схемой «дед-отец-сын»:

```cron
# /etc/cron.d/goosar-backup: каждую ночь в 03:15, хранить 14 дней
15 3 * * * goosar cd /opt/goosar && BACKUP_DIR=/srv/backups bash scripts/backup.sh >>/var/log/goosar-backup.log 2>&1
40 3 * * * goosar find /srv/backups -maxdepth 1 -name 'goosar-*' -type d -mtime +14 -exec rm -rf {} +
```

Рекомендуемое хранение: 14 ежедневных, 8 еженедельных, 12 ежемесячных копий. Хотя бы еженедельные копируйте на другую машину или в другой бакет: копия на том же диске, что и защищаемый том, копией не является. Шифруйте её на носителе (см. «Что входит в копию» выше).

### Тренировка восстановления (restore drill)

Проводите её раз в квартал и после любого обновления, менявшего миграции. Рабочий стек она не затрагивает: тренировка идёт отдельным проектом Compose со своими томами и портами.

```bash
# 1. Снимите копию с рабочего стека
bash scripts/backup.sh --out /srv/backups

# 2. Поднимите одноразовый стек: отдельное имя проекта (свои тома) и копия .env
#    с другими BACKEND_PORT / FRONTEND_PORT. Оставьте тот же GOOSAR_IMAGE_TAG,
#    что и в рабочем стеке, чтобы проверка схемы видела тот же образ.
#    POSTGRES_DB можно не менять: том изолирован именем проекта, а restore.sh
#    восстанавливает в ту базу, с которой запущен стек.
export DRILL='docker compose -p goosardrill --env-file .env.drill'
$DRILL -f docker-compose.selfhost.yml up -d

# 3. Восстановите рабочую копию в него. COMPOSE — команда compose, которую
#    запускает restore.sh, поэтому все вызовы, в том числе проверки только на
#    чтение, идут в проект тренировки, а не в рабочий.
COMPOSE="$DRILL" bash scripts/restore.sh /srv/backups/goosar-<метка> --confirm

# 4. Убедитесь, что данные на месте, затем снесите тренировочный стек
curl -s localhost:<порт бэкенда тренировки>/health
$DRILL -f docker-compose.selfhost.yml down -v
```

Тренировка, которая закончилась без проверки из шага 4, ничего не доказала. Проверьте число строк, которое вы знаете, и один файл в `/app/data/uploads`.

## Обновление

Обновление переводит образы вперёд и применяет миграции базы данных. Раздел описывает односерверный стек Compose; о поэтапном обновлении нескольких реплик бэкенда в Kubernetes, в том числе о том, как N подов конкурируют за запуск миграций, см. «Высокая доступность». Миграции **только вперёд**: откат образов не откатывает схему, поэтому копия, снятая перед обновлением, — единственное, что отделяет неудачное обновление от потери данных, записанных после него. Прочитайте подраздел «Откат» до начала обновления, а не после.

### Перед обновлением

Четыре проверки в этом порядке. Все дёшевы; плохо кончается пропуск первой.

1. **Снимите копию и убедитесь, что она появилась.**

   ```bash
   bash scripts/backup.sh                       # compose
   ls -lh backups/                              # каталог goosar-<UTC>/, а не .partial
   ```

   `backup.sh` пишет `db.dump` и `uploads.tar` в `backups/goosar-<метка UTC>/` и убирает `.partial` из имени только после того, как каждая часть создана и получила контрольную сумму. Каталог с `.partial` означает, что копия не удалась и её у вас нет. В Helm снимите дамп базы сами из пода Postgres (в образе бэкенда `pg_dump` нет):

   ```bash
   kubectl -n goosar exec deploy/goosar-postgres -- \
     sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > db.dump
   ```

   С управляемым Postgres сделайте снимок средствами провайдера.

2. **Проверьте свободное место.** При обновлении новые образы скачиваются, пока старые ещё на диске, а миграции, переписывающие таблицу, требуют места под её вторую копию. Заложите размер образов плюс размер самой большой таблицы.

   ```bash
   df -h /var/lib/docker
   docker system df
   sudo du -sh /var/lib/docker/volumes/*postgres*
   ```

3. **Оцените разрыв версий.** `GOOSAR_IMAGE_TAG` в `.env` (compose) или `images.backend.tag` (Helm) — это то, где вы сейчас; тег, на который вы переходите, — то, куда вы идёте. Прочитайте заметки к выпуску для **каждого** тега между ними: обновления накапливаются, и заметка о конкретной миграции применима, даже если вы перешагнули её выпуск.

4. **Выберите окно.** Миграции выполняются при старте бэкенда, пока API недоступен. Большинство занимает меньше секунды, но те, что переписывают таблицу или строят индекс на `comment` / `activity_log` / `issue`, растут вместе с данными. На стенде с миллионами строк планируйте минуты, а не секунды.

### Развёртывание из checkout (`make selfhost`)

Файлы поставки (compose, скрипты, миграции) должны соответствовать образам, которые вы запускаете. Поэтому сначала переключите checkout на нужный тег, затем поменяйте тег образов:

```bash
git fetch --tags
git checkout --force vX.Y.Z
$EDITOR .env            # GOOSAR_IMAGE_TAG=vX.Y.Z
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

`make selfhost` при первом создании `.env` закрепляет `GOOSAR_IMAGE_TAG` за самым новым тегом выпуска в вашем клоне, так что свежая установка запускает текущий выпуск. Дальше закрепление не двигается: `pull` заново получает тот же тег, а `make selfhost` предупреждает, если закреплённый тег отстал от самого нового в checkout (сначала выполните `git fetch --tags`, чтобы новые теги были видны). Вместо правки `.env` можно выполнить `GOOSAR_IMAGE_TAG=vX.Y.Z make selfhost`. Миграции применяются автоматически при старте бэкенда.

Если образы для выбранного тега ещё не опубликованы, соберите их из checkout: `make selfhost-build` или `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml up -d --build`.

`.env` и тома Docker обновление не трогает: `.env` не отслеживается git, поэтому переключение checkout его не меняет, а `up -d` подключает те же именованные тома.

#### Скрипт `install.sh --upgrade`

Для развёртывания, которое лежит в клоне с `.env` в каталоге `$GOOSAR_INSTALL_DIR` (по умолчанию `~/.goosar/server`), те же шаги выполняет скрипт:

```bash
bash scripts/install.sh --upgrade                 # самый новый тег выпуска в клоне
bash scripts/install.sh --upgrade --tag vX.Y.Z    # конкретный выпуск
GOOSAR_INSTALL_DIR=/opt/goosar bash scripts/install.sh --upgrade   # клон в другом каталоге
```

Он запрашивает теги, переключает клон на файлы поставки нужного выпуска, переписывает `GOOSAR_IMAGE_TAG` в `.env`, скачивает образы, запускает `docker compose up -d` и печатает рецепт отката к тегу, с которого вы ушли. Если тег не существует, скрипт отказывается работать до каких-либо изменений; если вы уже на этом теге, он ничего не делает. Изменяется только `GOOSAR_IMAGE_TAG`. Новое развёртывание флаг `--with-server` больше не создаёт: он лишь печатает команду `make selfhost`.

#### Профиль при обновлении не меняется

`GOOSAR_DELIVERY_PROFILE` записывается только при создании `.env`, а пустое или отсутствующее значение означает `cloud`. Поэтому существующий стенд после обновления сохраняет поведение исходящего трафика `cloud` и то состояние `GOOSAR_MCP_SECRET_KEY`, которое у него было: если ключ пуст, бэкенд всё равно стартует и по-прежнему пишет предупреждение об открытом тексте, потому что строгая проверка контура применяется только к `GOOSAR_DELIVERY_PROFILE=perimeter`. Чтобы перевести такой стенд на умолчания для развёртывания в закрытой сети, задайте оба значения вручную и перезапустите бэкенд:

```bash
cd /opt/goosar
openssl rand -base64 32   # вставьте в GOOSAR_MCP_SECRET_KEY (он должен быть задан ДО профиля)
$EDITOR .env              # GOOSAR_MCP_SECRET_KEY=..., GOOSAR_DELIVERY_PROFILE=perimeter
docker compose -f docker-compose.selfhost.yml up -d backend
```

Ключ задаётся первым: смена профиля при пустом ключе — это отказ запускаться, а не предупреждение. Учётные данные, уже сохранённые открытым текстом, шифруются фоновым заполнением при старте, как только появляется ключ.

#### Откат

```bash
cd /opt/goosar                             # или $GOOSAR_INSTALL_DIR
git checkout --force <предыдущий-тег>
sed -i.bak 's/^GOOSAR_IMAGE_TAG=.*/GOOSAR_IMAGE_TAG=<предыдущий-тег>/' .env
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

> **Образы откатываются, база нет.** Ничто не мешает старому бэкенду запуститься на новой схеме: `migrate up` пропускает миграции, которых нет в его поставке, а `/readyz` проверяет лишь, что применены миграции, нужные *этому* бинарному файлу, поэтому бэкенд поднимается «зелёным» и затем ведёт себя неправильно на колонках и таблицах, которых не знает. Откат образов — лишь половина отката, нужно ещё восстановить дамп, снятый *до* обновления:
>
> ```bash
> bash scripts/restore.sh <копия-до-обновления> --confirm
> ```
>
> **Окно потери данных — всё, что записано между копией и откатом:** каждая задача, комментарий, вложение и настройка, созданные или изменённые после запуска `scripts/backup.sh`. Частичного пути нет: откат через миграцию — это восстановление. Поэтому копия обязательна и должна сниматься непосредственно перед обновлением, а не по ночному расписанию.
>
> `restore.sh` отказывается восстанавливать дамп, чья версия схемы новее миграций в образе, поэтому восстанавливайте дамп до обновления *после* того, как закрепили предыдущий тег, а не до.

### Helm

Проверки из раздела «Перед обновлением» применяются без изменений, отличается только механика.

```bash
helm upgrade goosar deploy/helm/goosar \
  -n goosar \
  -f my-values.yaml \
  --atomic --timeout 15m
```

Путь `deploy/helm/goosar` берётся из checkout нужного тега. Если вы устанавливаете чарт из реестра OCI, замените путь ссылкой на чарт вашего реестра и добавьте `--version <версия-чарта>`.

`--atomic` автоматически откатывает релиз, если новые поды так и не стали готовыми, так что неудачное обновление оставляет вас на предыдущем *релизе*, а не на наполовину применённом. Миграции он **не** откатывает, см. ниже. Задайте `--timeout` больше ожидаемого времени миграции: `--atomic` считает миграцию, которая ещё идёт к истечению таймаута, неудачей и откатывает поды прямо под ней.

Миграции выполняются при старте пода бэкенда, и чарт не сериализует их между репликами; это делает advisory-блокировка исполнителя миграций, поэтому лишние реплики встают в очередь, а не конкурируют. Пока первая не закончит, все они выглядят как `Init` / `CrashLoopBackOff`. Это ожидаемо и не является сбоем.

#### Откат (Helm)

```bash
helm -n goosar history goosar
helm -n goosar rollback goosar <предыдущая-ревизия>
```

Правило то же, что для compose: образы откатываются, база нет, и ничто не мешает старому бэкенду подняться «зелёным» на новой схеме. Если обновление применило миграцию, восстановите и дамп базы, снятый до него; `helm rollback` сам по себе откатом не является.

### Что делают миграции при обновлении

Бэкенд выполняет `migrate up` до начала обработки запросов (`docker/entrypoint.sh` или запуск пода бэкенда в Helm). На практике это означает следующее.

- **Только вперёд.** Миграции `down` есть в репозитории, но путь обновления их не запускает, и ни одна поддерживаемая процедура их не использует. Откат через миграцию — это восстановление.
- **Сериализация.** Каждый исполнитель берёт advisory-блокировку Postgres, поэтому одновременные запуски (несколько реплик, оператор, вручную запустивший `migrate up`) встают в очередь, а не конкурируют. Уже применённые миграции пропускаются, так что повторный запуск после сбоя безопасен.
- **Ограниченное ожидание блокировок.** Исполнитель выставляет [`lock_timeout`](https://www.postgresql.org/docs/17/runtime-config-client.html) в 5 с. Миграция, которой нужен `ACCESS EXCLUSIVE` на таблицу, занятую кем-то другим (`pg_dump`, запрос BI, `psql` оператора), завершает эту попытку неудачей, а не ждёт вечно (и не заставляет все последующие запросы к таблице ждать за собой). Затем она повторяется с экспоненциальной паузой, по умолчанию пять раз, и каждая попытка пишет в лог блокирующий сеанс:

  ```
  WARN migration blocked on a lock, retrying version=254_... attempt=1
       lock_timeout=5s retry_in=2s
       blocking_sessions="pid=812 state=\"idle in transaction\" xact_age=430s query=\"...\""
  ```

  Если блокирующий сеанс переживает все повторы, запуск завершается ошибкой с тем же списком блокировщиков, и бэкенд не стартует. Закройте блокирующий сеанс и перезапустите бэкенд: ничего не применено наполовину, потому что неудавшаяся миграция не записывается.
- **`statement_timeout` по умолчанию не ограничен** и выставляется явно как «без ограничения», а не остаётся как есть, чтобы `statement_timeout`, заданный на роли или базе, не наследовался прогоном миграций. Заполнение данных законно идёт минутами и не должно обрываться на середине.
- **Миграции индексов обходят оба таймаута.** `CREATE INDEX CONCURRENTLY` ждёт не только собственной блокировки: он ждёт и каждую транзакцию, которая ещё может видеть таблицу (`pg_dump` — как раз такая), а `lock_timeout` прерывает это ожидание, как будто что-то не так. Прерванная конкурентная сборка оставляет индекс `INVALID`, поэтому исполнитель распознаёт миграцию индекса и запускает её вовсе без `lock_timeout` и `statement_timeout`. Такая миграция может идти долго: это единственный шаг обновления без потолка, по замыслу.

| Переменная | По умолчанию | Что делает |
| --- | --- | --- |
| `GOOSAR_MIGRATION_LOCK_TIMEOUT` | `5s` | Сколько один оператор ждёт блокировку, прежде чем эта попытка завершится неудачей. К конкурентным миграциям индексов не применяется |
| `GOOSAR_MIGRATION_LOCK_RETRIES` | `5` | Дополнительные попытки после превышения ожидания блокировки, с экспоненциальной паузой |
| `GOOSAR_MIGRATION_STATEMENT_TIMEOUT` | не задан (без ограничения) | Жёсткий предел для одного оператора миграции. Задавайте, только если знаете свои данные. К конкурентным миграциям индексов не применяется |

Заведомо долгие миграции, для планирования окна: перестроение индексов на `comment` и `activity_log` и смена типа колонки в `issue` (`ALTER COLUMN ... TYPE DATE`, полная перезапись таблицы). Обе растут вместе с числом строк.

### Проверка после обновления

```bash
# 1. Бэкенд запущен, схема соответствует бинарному файлу
#    (порты — BACKEND_PORT / FRONTEND_PORT из .env; по умолчанию 8081 / 3001)
curl -fsS http://localhost:8081/readyz | jq
# {"status":"ok","checks":{"db":"ok","migrations":"ok"}}
```

`/readyz` возвращает 503, пока в `schema_migrations` не записана **каждая** миграция, которая нужна этому бинарному файлу. Поэтому `"migrations":"out_of_date"` означает, что обновление не завершено (или `migrate up` упал; смотрите логи бэкенда). `"migrations":"error"` означает, что сама проверка не смогла выполниться.

```bash
# 2. Сам прогон миграций
docker compose -f docker-compose.selfhost.yml logs backend | grep -i migrat
kubectl -n goosar logs deploy/goosar-backend | grep -i migrat   # Helm

# 3. Проверка работоспособности, в таком порядке
curl -fsS http://localhost:8081/healthz            # процесс жив
curl -fsS http://localhost:3001/ >/dev/null        # web отвечает
```

Затем в интерфейсе: войдите, откройте рабочее пространство, откройте задачу, оставьте комментарий и убедитесь, что демон в разделе «Среды выполнения» имеет статус online. Демон, переподключившийся после перезапуска, не отбраковывается первые 150 секунд работы сервера (`GOOSAR_SWEEPER_BOOT_GRACE`), поэтому задачи, шедшие во время перезапуска, переживают его, но чтобы их среда выполнения осталась в сети, демон должен вернуться в это окно.

Среда выполнения, ушедшая в offline позже (обрыв сети, ноутбук в спящем режиме), не теряет выполняющиеся задачи сразу: они остаются живыми в течение `GOOSAR_RUNTIME_RECONNECT_GRACE` (по умолчанию `3h`, минимум `150s`) и завершаются с `runtime_offline`, только когда демон молчал весь этот срок. Их автоматический повтор ждёт в состоянии `deferred`, пока эта среда выполнения снова не пришлёт свежий heartbeat, и истекает с `runtime_reconnect_timeout` через ещё один такой срок, если этого не случилось.

### Расхождение версий

Сервер не задаёт минимальную версию для desktop-приложения и web-интерфейса: оба — клиенты API и разбирают ответы с запасом, поэтому старый клиент при новом бэкенде теряет возможности, но не ломается. **Демон** устроен иначе: версия CLI `goosar`, которую он сообщает при регистрации, включает или отключает отдельные сценарии.

| Компонент | Минимум для текущего бэкенда | Как обеспечивается |
| --- | --- | --- |
| CLI `goosar` / демон: **приём любой задачи** | `0.2.21` (`GOOSAR_MIN_DAEMON_VERSION`) | **Жёстко.** Ниже этой версии каждый захват задачи, по HTTP и по WebSocket, отклоняется с `426 daemon_too_old`; ответ содержит `min_daemon_version` и заголовок `X-Min-Daemon-Version`. Демон пишет в лог подсказку об обновлении; порог публикуется как `min_daemon_version` в `/api/config`. Регистрация, heartbeat и отправка результатов продолжают работать, так что устаревший демон виден, а не молча простаивает |
| CLI `goosar` / демон: quick-create | `0.2.21` | **Жёстко.** Ниже API возвращает `422 daemon_version_unsupported` с `current_version` / `min_version`, а интерфейс показывает предложение обновиться |
| CLI `goosar` / демон: quick-create с приоритетом и сроком | `0.4.3` | **Жёстко**, но только для запросов, использующих эти поля |
| CLI `goosar` / демон: заметки о передаче при назначении | `0.3.28` | **Мягко.** Назначение выполняется, заметка отбрасывается, а поле заметки в интерфейсе затемняется |
| Desktop-приложение | не требуется | Поставляется со своим демоном, поэтому обновление desktop-приложения — способ довести демон на рабочей станции до нужных версий |
| Web-интерфейс | не требуется | Раздаётся самим стеком и обновляется вместе с бэкендом |
| CLI агентов с проверкой минимальной версии | `2.0.0` / `0.100.0` / `1.0.0` / `0.2.89` / `0.20.0` | **Жёстко**, проверяет демон при пробе среды выполнения: CLI ниже минимума помечается недоступным и не запускается |

Демоны, собранные из исходников, сообщают версию вида `git describe` (`vX.Y.Z-N-g<хеш>`) и ко всему перечисленному не применимы.

Безопасный порядок: **сначала бэкенд, потом демоны**. Новый бэкенд принимает старые демоны во всём, кроме перечисленных сценариев, а демон новее своего бэкенда может вызывать эндпоинты, которых там ещё нет.

Для этого окна служит `GOOSAR_MIN_DAEMON_VERSION`. По умолчанию он равен порогу `0.2.21` выше, так что без изменений ничего не меняется. После обновления, менявшего контракт задач, поднимите его до поставленной версии: демоны ниже перестают получать работу и говорят почему, а не деградируют молча. Значение `none` полностью отключает проверку; это удобно, пока парк демонов обновляется и вы предпочитаете работающие устаревшие демоны простаивающим.

Цикл выпусков, окно поддержки (N-1, 90 дней), компоненты, версии которых должны совпадать, и сроки исправления уязвимостей описаны отдельно; таблица выше — техническая половина этой политики.

Отсутствующая или не разбираемая версия **не** отклоняется. `X-Client-Version` контролируется клиентом и указывается по возможности, поэтому это подсказка обновиться, а не граница безопасности: отказ при отсутствии заголовка, который демон просто забыл отправить, оставил бы без работы весь парк из-за ошибки в заголовке.

### Что стоит смена переменной

Большая часть `.env` читается бэкендом при старте, так что изменение означает перезапуск. Переменные, которые compose вшивает в *сборку*, требуют пересборки, и только на пути сборки (`make selfhost-build`); на официальных образах «пересборка» означает переход на другой `GOOSAR_IMAGE_TAG`.

| Переменная | Как применить |
| --- | --- |
| `JWT_SECRET`, `POSTGRES_PASSWORD`, `GOOSAR_MCP_SECRET_KEY`, `GOOSAR_VCS_SECRET_KEY` | перезапустить бэкенд (`docker compose ... up -d backend`). Смена ключа шифрования делает нечитаемым всё, что запечатано старым ключом, если не провести ротацию правильно: см. «Ротация ключей шифрования» |
| `JWT_SECRET_PREVIOUS`, `GOOSAR_*_SECRET_KEY_PREVIOUS`, `GOOSAR_AUDIT_RETENTION_DAYS` | перезапустить бэкенд |
| `GOOSAR_DELIVERY_PROFILE`, `GOOSAR_SKILL_SOURCES`, `GOOSAR_ALLOWED_PROVIDERS` | перезапустить бэкенд |
| `GOOSAR_MIN_DAEMON_VERSION` | ничего: значение перечитывается из окружения при каждом запросе, так что `docker compose ... up -d backend` с новым значением применяется сразу; пересборка не нужна |
| `GOOSAR_SCHEDULER_AUDIT_RETENTION`, `GOOSAR_UPLOAD_GC_GRACE`, `GOOSAR_HYGIENE_SWEEP_INTERVAL` | перезапустить бэкенд (читаются один раз при старте) |
| `GOOSAR_RETENTION_*`, `GOOSAR_ATTACHMENT_PURGE_GRACE`, `GOOSAR_EXPORT_*` | перезапустить бэкенд (читаются один раз при старте); см. «Конфигурация» |
| `POSTGRES_MEM_LIMIT`, `BACKEND_MEM_LIMIT`, `BACKEND_GOMEMLIMIT`, `FRONTEND_MEM_LIMIT`, `FRONTEND_NODE_OPTIONS` | `up -d` (compose должен **пересоздать** контейнер: простой `restart` сохраняет старую cgroup); пересборка не нужна |
| `RESEND_API_KEY`, `SMTP_*`, `GOOSAR_LLM_*`, `GOOSAR_PROVISIONING_*`, `S3_*`, `POSTHOG_API_KEY` | перезапустить бэкенд |
| `APP_ENV`, `LOG_LEVEL`, `LOG_FORMAT`, `GOOSAR_DEPLOYMENT_ADMIN_EMAILS` | перезапустить бэкенд (читаются один раз при старте) |
| `LOG_MAX_SIZE`, `LOG_MAX_FILE` | `up -d` (compose должен **пересоздать** контейнер: драйвер логов задаётся при создании); пересборка не нужна |
| `BACKEND_PORT`, `FRONTEND_PORT`, `POSTGRES_*` (порты и тома) | `up -d` (compose пересоздаёт контейнер: публикация портов относится к контейнеру) |
| `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_WS_URL`, `GOOSAR_MAC_DMG_URL`, `GOOSAR_MAC_X64_DMG_URL`, `GOOSAR_WIN_X64_EXE_URL`, `GOOSAR_LINUX_X64_APPIMAGE_URL`, `GOOSAR_LINUX_ARM64_APPIMAGE_URL`, `DOCS_URL` | `up -d web`: `docker-compose.selfhost.yml` передаёт их контейнеру web как окружение времени выполнения, пересборка не нужна |
| `NEXT_PUBLIC_APP_VERSION` и всё остальное, объявленное в compose как `build.args` | **пересобрать образ web** (`make selfhost-build`): аргументы сборки вшиваются; на официальных образах их можно сменить только другим `GOOSAR_IMAGE_TAG` |
| `PUBLIC_HOST`, `FRONTEND_ORIGIN`, `GOOSAR_APP_URL`, `GOOSAR_PUBLIC_URL`, `CORS_ALLOWED_ORIGINS`, `GOOSAR_TRUSTED_PROXIES` | `up -d` (бэкенд читает их при старте, а контейнер web получает источники как окружение времени выполнения) |
| `GOOSAR_IMAGE_TAG`, `GOOSAR_BACKEND_IMAGE`, `GOOSAR_WEB_IMAGE` | `pull` + `up -d` (это и есть путь обновления, описанный выше) |

> **Том загрузок, созданный образом, где бэкенд работал от root.** Контейнер бэкенда больше не запускается от root (uid 1001). Том `backend_uploads`, созданный старым образом, принадлежит root, и загрузка вложений не работает, пока вы не передадите его новому пользователю. Если это ещё не сделано, бэкенд при старте печатает ровно эту команду:
>
> ```bash
> docker compose -f docker-compose.selfhost.yml run --rm --no-deps --user root --cap-add CHOWN \
>   --entrypoint sh backend -c 'chown -R 1001:1001 /app/data/uploads'
> ```
>
> Развёртывания на S3 это не касается. Подробности см. в разделе «Конфигурация» (усиление защиты контейнеров).

## Остановка сервисов

Если развёртывание лежит в клоне в `$GOOSAR_INSTALL_DIR` (по умолчанию `~/.goosar/server`):

```bash
# из любого checkout; скрипт останавливает стек в $GOOSAR_INSTALL_DIR
# и локальный демон, если он запущен
bash scripts/install.sh --stop
```

Если вы клонировали репозиторий вручную:

```bash
# Остановить сервисы Docker Compose (бэкенд, фронтенд, база данных)
make selfhost-stop

# Остановить локальный демон
goosar daemon stop
```

Обе команды выполняют `docker compose ... down` без `-v`: контейнеры удаляются, а именованные тома с базой и загрузками остаются, так что следующий запуск продолжает с теми же данными.

## Ручная настройка Docker Compose и CLI

### Docker Compose вручную

Если вы предпочитаете выполнять шаги Docker Compose самостоятельно, а не через `make selfhost`:

```bash
git clone <адрес репозитория поставки> goosar
cd goosar
cp .env.example .env
```

Отредактируйте `.env`: как минимум задайте `JWT_SECRET` (обязательно; и Docker Compose, и бэкенд отказываются запускаться без настоящего значения):

```bash
JWT_SECRET=$(openssl rand -hex 32)
```

Затем запустите всё:

```bash
docker compose -f docker-compose.selfhost.yml pull
docker compose -f docker-compose.selfhost.yml up -d
```

### Настройка CLI вручную

Если вы предпочитаете настраивать CLI по шагам, а не через `goosar setup self-host`:

```bash
# Указать CLI на ваш локальный сервер
goosar config set server_url http://localhost:8081
goosar config set app_url http://localhost:3001

# Войти (открывает браузер)
goosar login

# Запустить демон
goosar daemon start
```

Для рабочих развёртываний с TLS:

```bash
goosar config set app_url https://app.example.local
goosar config set server_url https://api.example.local
goosar login
goosar daemon start
```

Остальные переменные окружения, установка без Docker, настройка обратного прокси, подготовка базы данных и прочее описаны в разделе «Конфигурация».

## Лимиты запросов

Сервер ограничивает частоту запросов к своим страницам входа и API. Ничего устанавливать не нужно: без `REDIS_URL` ограничитель считает в памяти процесса, что подходит односерверному стеку Compose, который описывает это руководство. Задавайте `REDIS_URL` только когда запускаете несколько узлов API и хотите, чтобы они делили один бюджет; см. «Высокая доступность», где многорепликовый бэкенд без него отказывается запускаться. Лог при старте называет бэкенд и действующие лимиты:

```text
rate limiting enabled backend=memory auth_send_code_per_ip_per_min=5 ...
```

При превышении бюджета API отвечает `429` с заголовком `Retry-After` (в секундах) и телом `{"error":"too many requests","code":"rate_limited"}`.

| Переменная | По умолчанию | Что ограничивает |
| --- | --- | --- |
| `RATE_LIMIT_AUTH` | `5` | `POST /auth/send-code`, на IP в минуту |
| `RATE_LIMIT_AUTH_VERIFY` | `20` | `POST /auth/verify-code` и `/auth/verify-link`, на IP в минуту |
| `RATE_LIMIT_AUTH_EMAIL` | `10` | те же два маршрута с кодами, на адрес почты в минуту |
| `RATE_LIMIT_MFA_VERIFY` | `10` | попытки подобрать второй фактор, на ожидающий тикет входа (5 мин) |
| `RATE_LIMIT_TOKEN` | `20` | `POST /api/cli-token` и группа `/api/tokens`, на пользователя в час |
| `RATE_LIMIT_API` | `600` | все остальные аутентифицированные маршруты `/api`, на пользователя в минуту |
| `RATE_LIMIT_CONTACT_SALES` | `5` | `POST /api/contact-sales`, на IP в час |
| `RATE_LIMIT_TRUSTED_PROXIES` | берётся из `GOOSAR_TRUSTED_PROXIES` | чьему `X-Forwarded-For` доверяет ограничитель |

Два пункта стоит перепроверить перед выходом в работу.

**За обратным прокси задайте список доверенных прокси.** Пустой список означает, что ограничитель никогда не читает `X-Forwarded-For`, и каждый запрос выглядит пришедшим от прокси: тогда всё развёртывание делит один бюджет входа в 5 запросов в минуту. Обычно `GOOSAR_TRUSTED_PROXIES` уже задан по той же причине, и ограничитель использует его; `RATE_LIMIT_TRUSTED_PROXIES` нужен, только чтобы дать ограничителю другой список.

**Не отключайте лимиты ради нагрузочного теста.** Поднимите число. Выключателя нет намеренно: публичный стенд без ограничения входа — именно та дыра, которую он закрывает.
