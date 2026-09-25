# Backlog: поверхность Goosar 1.1

Актуальность: 25 сентября 2026 года. Ветка `redesign/1.1` от `v1.0.1`, пуш запрещён до решения владельца.

### T-001 · Новые токены «Речная синь»
Эпик: E1
Оценка: 1 день
Зависит от: —

Задача: заменить значения в `packages/ui/styles/tokens.css` и `apps/web/app/custom.css` на систему из раздела 4.1 архитектуры для светлой и тёмной тем; добавить токены `--accent-beak`, `--rail`, `--status-backlog|todo|doing|review|done`; `--radius: 0.375rem`.

Acceptance criteria:
- [ ] `grep -c 'oklch(0.705 0.187 47.6)' packages/ui/styles/tokens.css apps/web/app/custom.css` возвращает 0;
- [ ] пять токенов `--status-*` и `--rail` объявлены в `@theme inline` и в обеих темах;
- [ ] `/{ws}/issues` открывается в обеих темах без ошибок консоли.

Затрагивает: `packages/ui/styles/tokens.css`, `apps/web/app/custom.css`
Риск: L0

### T-002 · Убрать захардкоженные цвета из компонентов
Эпик: E1
Оценка: 1 день
Зависит от: T-001

Задача: найти классы Tailwind с явными цветами (`bg-orange-*`, `text-amber-*`, `bg-yellow-*`, `border-blue-*` и т.п.) в `packages/ui`, `packages/views`, `apps/web`, `apps/desktop/src`, заменить на токены.

Acceptance criteria:
- [ ] `grep -rEn '(bg|text|border|ring)-(orange|amber|yellow|blue|green|red|purple)-[0-9]{3}' packages/ui packages/views apps/web/app apps/web/features apps/desktop/src --include=*.tsx | wc -l` возвращает 0;
- [ ] статусы задач в списке и на доске окрашены токенами `--status-*`;
- [ ] `pnpm --filter @goosar/views test` зелёный.

Затрагивает: `packages/views/**/*.tsx`, `packages/ui/components/**/*.tsx`
Риск: L0

### T-003 · Шрифты Manrope, Unbounded, JetBrains Mono
Эпик: E1
Оценка: 0.5 дня
Зависит от: —

Задача: подключить три шрифта через `next/font/google` в `apps/web/app/layout.tsx`, обновить `--font-sans`, `--font-heading`, `--font-mono` в `globals.css`; удалить Montserrat и Comfortaa; для десктопа положить woff2 в `apps/desktop/resources/fonts` и объявить `@font-face`.

Acceptance criteria:
- [ ] `grep -rn 'montserrat\|comfortaa' apps packages --include=*.css --include=*.tsx -i | grep -v node_modules | wc -l` возвращает 0;
- [ ] заголовок H1 лендинга рендерится Unbounded (проверка через devtools computed font-family);
- [ ] `node scripts/third-party-notices.mjs` перегенерирован, три шрифта в списке с OFL-1.1.

Затрагивает: `apps/web/app/layout.tsx`, `apps/web/app/globals.css`, `apps/desktop/src/renderer/**/globals.css`, `THIRD_PARTY_NOTICES.md`
Риск: L0

### T-004 · Рельса навигации
Эпик: E2
Оценка: 1.5 дня
Зависит от: T-001

Задача: добавить блок `sidebar-09` (`cd packages/ui && pnpm dlx shadcn@latest add sidebar-09`), из него взять первую (иконочную) `Sidebar collapsible="none"` как рельсу `packages/views/layout/nav-rail.tsx`: 56px, тёмный (`--rail`), шесть пунктов из раздела 4.2, переключатель воркспейса аватаром сверху, профиль снизу; подпись пункта по наведению и по фокусу через `SidebarMenuButton tooltip`.

Источник: shadcn block sidebar-09, MIT (ADR-0005).

Acceptance criteria:
- [ ] шесть пунктов ведут на `paths.workspace(slug).{inbox,issues,projects,agents,autopilots,settings}()`;
- [ ] активный пункт определяется `isNavActive` для вложенных путей;
- [ ] Tab-навигация проходит по всем пунктам, Enter открывает раздел; `aria-label` на каждом пункте;
- [ ] тест `nav-rail.test.tsx` покрывает активное состояние и клавиатуру.

Затрагивает: `packages/views/layout/nav-rail.tsx`, `packages/views/layout/nav-rail.test.tsx`
Риск: L1

### T-005 · Контекстная панель и командная строка
Эпик: E2
Оценка: 2 дня
Зависит от: T-004

Задача: вторая панель из того же блока `sidebar-09` становится `context-panel.tsx` 260px с содержимым по разделу (задачи: сохранённые фильтры, избранное, проекты; исполнители: агенты/отряды; настройки: подразделы среды, навыки, расход); сворачивание кнопкой и `[` с сохранением в `localStorage`; `command-bar.tsx` с поиском (`⌘K`), кнопкой «Новая задача» (`C`) и хлебными крошками. Заменить `app-sidebar.tsx` в `app-shell`.

Acceptance criteria:
- [ ] `app-sidebar.tsx` не импортируется нигде, кроме собственного теста, либо удалён вместе с тестом;
- [ ] состояние панели переживает перезагрузку страницы;
- [ ] избранное из старого сайдбара отображается в панели раздела «Задачи»;
- [ ] `pnpm --filter @goosar/views test` и `typecheck` зелёные.

Затрагивает: `packages/views/layout/context-panel.tsx`, `packages/views/layout/command-bar.tsx`, `packages/views/layout/app-shell.tsx`, `packages/views/layout/app-sidebar*.tsx`
Источник: shadcn block sidebar-09 и `command.tsx` (cmdk), MIT (ADR-0005).
Риск: L1

### T-006 · Словарь ru
Эпик: E3
Оценка: 1 день
Зависит от: подтверждение терминов владельцем

Задача: переписать значения в `packages/views/locales/ru/*.json` и `apps/web/features/landing/i18n/ru.ts` по разделу 4.4: задача, лента, мои задачи, исполнители, отряды, среды, навыки, расход. Ключи не менять.

Acceptance criteria:
- [ ] в значениях `packages/views/locales/ru/*.json` нет слов «issue», «Issues», «Skills», «Inbox» (проверка `grep -rn '"[^"]*[Ii]ssue' packages/views/locales/ru | grep -v 'ГУС-' | wc -l` → 0);
- [ ] `node scripts/check-ui-strings.mjs` зелёный;
- [ ] ручной проход по шести разделам рельсы на русской локали без англицизмов.

Затрагивает: `packages/views/locales/ru/*.json`, `apps/web/features/landing/i18n/ru.ts`
Риск: L0

### T-007 · Локали en, zh-Hans, ja с русского
Эпик: E3
Оценка: 1 день
Зависит от: T-006

Задача: перевести значения из ru в остальные локали; en использует task, feed, my tasks, crew, squads, runtimes, skills, usage.

Acceptance criteria:
- [ ] набор ключей во всех четырёх локалях идентичен (`scripts/check-ui-strings.mjs`);
- [ ] в значениях en нет строк «Issue» и «Inbox»;
- [ ] переключение локали в настройках не оставляет непереведённых ключей (нет `MISSING` в консоли).

Затрагивает: `packages/views/locales/{en,zh-Hans,ja}/*.json`, `apps/web/features/landing/i18n/en.ts`
Риск: L0

### T-008 · Список задач как вид по умолчанию
Эпик: E4
Оценка: 1 день
Зависит от: T-005

Задача: в `packages/views/issues` сделать список видом по умолчанию для нового воркспейса, доску оставить переключателем; группировка по статусу заголовками.

Acceptance criteria:
- [ ] новый воркспейс открывает `/issues` в виде списка;
- [ ] выбранный вид сохраняется существующим стором вида из `packages/core/issues/stores` без изменений в `packages/core`;
- [ ] переключение список/доска не теряет активные фильтры.

Затрагивает: контейнер вида в `packages/views/issues/`
Риск: L1

### T-009 · Строка задачи
Эпик: E4
Оценка: 1 день
Зависит от: T-008

Задача: новая строка списка: полоса статуса 3px слева цветом `--status-*`, ключ, заголовок, исполнитель аватаром (агент помечен точкой `--accent-beak`), срок справа; приоритет иконкой без цветного фона.

Acceptance criteria:
- [ ] в строке нет цветных бейджей приоритета и статуса;
- [ ] агент-исполнитель визуально отличим от человека (точка акцента) и это отражено в `aria-label`;
- [ ] высота строки 40px; при 500 задачах прокрутка без long tasks дольше 50 мс (вкладка Performance).

Затрагивает: `packages/views/issues/issue-row.tsx`
Источник: пример shadcn Tasks (TanStack Table, колонки status/priority с иконками) через уже имеющийся `packages/ui/components/ui/data-table.tsx`, MIT (ADR-0005).
Риск: L1

### T-010 · Карточка задачи с лентой агента
Эпик: E4
Оценка: 2 дня
Зависит от: T-009

Задача: две колонки: слева заголовок, описание, свойства; справа сплошная лента активности (комментарии, шаги агента, смены статуса) без вкладок; кнопка «Передать агенту» в шапке.

Acceptance criteria:
- [ ] лента показывает все три типа событий в одном потоке в хронологическом порядке;
- [ ] «Передать агенту» открывает существующий диалог выбора агента без изменений в API-клиенте;
- [ ] на ширине 1024px колонки складываются в одну, лента ниже описания;
- [ ] существующие тесты карточки проходят или переписаны с сохранением сценариев.

Затрагивает: `packages/views/issues/issue-detail*.tsx`
Риск: L1

### T-011 · Доска под новые токены
Эпик: E4
Оценка: 0.5 дня
Зависит от: T-009

Задача: карточка на доске использует ту же полосу статуса и иконку приоритета, что строка списка; убрать цветной фон колонок.

Acceptance criteria:
- [ ] фон всех колонок доски равен `--canvas`;
- [ ] карточка доски и строка списка используют общий компонент свойств задачи.

Затрагивает: `packages/views/issues/board/*.tsx`
Риск: L0

### T-012 · Лендинг: терминальная лента
Эпик: E5
Оценка: 1 день
Зависит от: T-003

Задача: добавить `@magicui/terminal` (`cd packages/ui && pnpm dlx shadcn@latest add @magicui/terminal`, компоненты Terminal, TypingAnimation, AnimatedSpan); первый экран `apps/web/features/landing/components/hero.tsx` без скриншота; вместо него лента на Terminal из статического сценария `demo-log.ts` (10–14 строк: постановка задачи, шаги агента, PR, статус «На проверке»); анимация отключается при `prefers-reduced-motion`.

Acceptance criteria:
- [ ] `apps/web/public/images/landing-hero.png` удалён и не упоминается;
- [ ] лента не делает сетевых запросов (вкладка Network пуста);
- [ ] при `prefers-reduced-motion: reduce` лента показана целиком без анимации;
- [ ] Lighthouse Performance главной не ниже 90 на десктопном профиле.

Затрагивает: `apps/web/features/landing/components/hero.tsx`, `apps/web/features/landing/demo-log.ts`, `apps/web/public/images/`, `packages/ui/components/ui/terminal.tsx`
Источник: Magic UI Terminal, MIT (ADR-0005); `THIRD_PARTY_NOTICES.md` не меняется (код копируется в репо, лицензия MIT указывается в шапке файла).
Риск: L0

### T-013 · Лендинг: секции и скриншоты нового интерфейса
Эпик: E5
Оценка: 1 день
Зависит от: T-010, T-012

Задача: секции «Агент как исполнитель», «Свой контур», «Hermes внутри», «Установка за пять минут»; два скриншота нового списка и карточки в светлой теме, 2x, русская локаль, снятые со стенда.

Acceptance criteria:
- [ ] на лендинге нет изображений старого интерфейса;
- [ ] тексты секций только в `i18n/ru.ts` и `i18n/en.ts`, без строк в JSX;
- [ ] секция «Установка» содержит команду `make selfhost` в блоке кода.

Затрагивает: `apps/web/features/landing/components/*.tsx`, `apps/web/features/landing/i18n/*.ts`, `apps/web/public/images/`
Риск: L0

### T-014 · Десктоп: заголовок окна и трей
Эпик: E6
Оценка: 0.5 дня
Зависит от: T-005

Задача: на macOS скрытый заголовок с кнопками окна поверх рельсы (`titleBarStyle: 'hiddenInset'`), отступ рельсы под кнопки; иконка трея из `goosar-icon-path.ts` монохромная.

Acceptance criteria:
- [ ] кнопки окна не перекрывают первый пункт рельсы;
- [ ] `electron-vite dev` открывает окно без белой полосы заголовка;
- [ ] иконка трея видна в светлом и тёмном меню-баре.

Затрагивает: файл создания окна в `apps/desktop/src/main/`, `apps/desktop/resources/`
Риск: L1

### T-015 · Онбординг под новый каркас
Эпик: E6
Оценка: 0.5 дня
Зависит от: T-005, T-006

Задача: `packages/views/onboarding/steps/*` используют новые термины и показывают рельсу на шаге «Что вы можете».

Acceptance criteria:
- [ ] демо-задачи в `step-welcome.tsx` имеют ключ `ГУС-` и русские заголовки;
- [ ] шаг «Что вы можете» ссылается на шесть разделов рельсы, не на двенадцать пунктов сайдбара.

Затрагивает: `packages/views/onboarding/steps/*.tsx`
Риск: L0

### T-016 · Документация и CLI-тексты под словарь
Эпик: E6
Оценка: 0.5 дня
Зависит от: T-006

Задача: `README.md`, `SELF_HOSTING.md` и help-тексты `server/cmd/goosar/cmd_issue.go` используют «задача» в русской прозе; имена команд CLI (`goosar issue`) не меняются.

Acceptance criteria:
- [ ] `grep -n 'issue' README.md SELF_HOSTING.md` возвращает только упоминания команд CLI и ключей `ГУС-`;
- [ ] `go test ./cmd/goosar/...` зелёный.

Затрагивает: `README.md`, `SELF_HOSTING.md`, `server/cmd/goosar/cmd_issue.go`
Риск: L0

### T-017 · Визуальная регрессия: эталоны
Эпик: E6
Оценка: 0.5 дня
Зависит от: T-010, T-013

Задача: снять эталонные скриншоты трёх экранов в двух темах через существующий Playwright (`playwright.config.ts`) в `e2e/visual/`, положить как baseline.

Acceptance criteria:
- [ ] шесть baseline-файлов в репозитории, тест падает при diff > 0.5%;
- [ ] тест запускается командой `pnpm e2e --project visual`.

Затрагивает: `e2e/visual/*.spec.ts`, `playwright.config.ts`
Риск: L0

### T-018 · Changelog, NOTICES, версия 1.1.0
Эпик: E6
Оценка: 0.5 дня
Зависит от: все выше

Задача: перенести `[Unreleased]` в `[1.1.0]`, поднять версии в семи `package.json` и `Chart.yaml`, перегенерировать `THIRD_PARTY_NOTICES.md`, пройти `43-release-checklist.md`. Без тега и пуша до решения владельца.

Acceptance criteria:
- [ ] `grep -rl '"version": "1.0.1"' --include=package.json . | grep -v node_modules | wc -l` возвращает 0;
- [ ] `41-changelog.md` содержит секцию `[1.1.0]` с датой;
- [ ] чек-лист заполнен, пункты «тег» и «пуш» отмечены как отложенные с причиной.

Затрагивает: `package.json`, `apps/*/package.json`, `packages/*/package.json`, `deploy/helm/goosar/Chart.yaml`, `docs/41-changelog.md`
Риск: L0
