# ADR-0005: Новые экраны собираются из готовых MIT-блоков, а не пишутся с нуля

Дата: 25 сентября 2026 года
Статус: Accepted

## Контекст

Владелец требует не писать GUI с нуля и брать готовое из интернета. В `packages/ui` уже подключён shadcn (стиль `base-nova`, `components.json` с registry, есть `sidebar.tsx`, `data-table.tsx`, `command.tsx`, `motion`). Реестр shadcn позволяет добавлять блоки командой `pnpm dlx shadcn@latest add <block>` прямо в пакет.

## Рассмотренные варианты

- Писать рельсу, панель, строку задачи и терминальную ленту вручную — полный контроль / 6–8 дней, свой набор багов доступности;
- Брать блоки из реестров под MIT (shadcn blocks, Magic UI) и адаптировать под токены и данные Goosar — 2–3 дня, доступность и клавиатура уже решены / дизайн ограничен тем, что предлагает блок; нужно следить за лицензией каждого источника;
- Брать код из чужих продуктов (Plane, Huly) — ближе к нашей задаче / AGPL-3.0 и EPL-2.0 несовместимы с MIT-редистрибуцией, запрещено.

## Решение

Экраны E2, E4, E5 собираются из блоков под MIT: `sidebar-09` (shadcn, «Collapsible nested sidebars»: иконочная рельса плюс вторая панель) для каркаса, пример `tasks` (shadcn, TanStack Table, уже есть `data-table.tsx`) для списка задач, `@magicui/terminal` для ленты на лендинге. Разрешённые источники: ui.shadcn.com, magicui.design, github.com/shadcn-ui/ui, пакеты с лицензией MIT/Apache-2.0/BSD/ISC. Запрещённые: любой код под GPL/AGPL/LGPL/EPL/MPL, платные наборы (Tailwind UI), код из репозиториев без LICENSE.

## Последствия

Положительные: E2 и E5 сокращаются примерно вдвое; доступность и клавиатура блоков shadcn уже проверены сообществом; стиль остаётся единым с существующими компонентами.
Отрицательные: перед добавлением каждого блока обязательна проверка лицензии источника (файл LICENSE в репозитории) и запись источника в тикете строкой «Источник:»; блоки shadcn приходят под стиль new-york, адаптация к `base-nova` и токенам делается руками.
Что станет сигналом пересмотреть решение: если адаптация блока под наши данные занимает дольше, чем оценка тикета, блок выбрасывается и экран пишется на примитивах `packages/ui`.

Лицензия shadcn/ui (MIT):  
https://github.com/shadcn-ui/ui/blob/main/LICENSE.md

Лицензия Magic UI (MIT):  
https://github.com/magicuidesign/magicui/blob/main/LICENSE.md

Блок sidebar-09:  
https://ui.shadcn.com/blocks/sidebar#sidebar-09

Пример Tasks на TanStack Table:  
https://ui.shadcn.com/examples/tasks

Компонент Terminal:  
https://magicui.design/docs/components/terminal
