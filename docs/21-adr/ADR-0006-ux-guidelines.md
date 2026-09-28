# ADR-0006: Учебники по UX, по которым проверяется и правится интерфейс

Дата: 28 сентября 2026 года
Статус: Accepted

## Контекст

После выкатки 1.1.0 владелец назвал четыре неудобства: чат спрятан внутри «Ленты», панель в десктопе не сворачивается и не разворачивается, пропала понятная возможность открывать вкладки наверху, разделы администрирования требуют системного подхода. Правки «по ощущению» дадут новые неудобства; нужен внешний эталон, по которому каждое решение проверяется.

## Рассмотренные варианты

- Собственный список правил — быстро / субъективно, не на что сослаться в споре;
- 10 эвристик Нильсена (NN/g) как общий чек-лист, Carbon Design System (IBM) как эталон паттернов для административных экранов и таблиц, Apple Human Interface Guidelines для боковых панелей и вкладок в десктопе — три признанных первоисточника, каждый закрывает свой слой / три документа вместо одного;
- Полный design system (Material 3) — целостно / тянет за собой визуальный язык, который мы только что заменили своим.

## Решение

Каждый экран проверяется по 10 эвристикам NN/g; административные разделы (Настройки → Деплой, Админ) строятся по паттернам Carbon (data table, common actions, dialog, notification, empty state, forms); поведение рельсы, панели и вкладок в десктопе — по HIG «Sidebars» и «Tab bars». Каждый тикет E7 ссылается на конкретный пункт эталона.

## Последствия

Положительные: у правки есть проверяемое основание; аудит удобства можно повторить по чек-листу.
Отрицательные: часть текущих решений (чат внутри «Ленты», рельса без сворачивания) противоречит эвристикам 6 и 7 и переделывается.
Что станет сигналом пересмотреть решение: если владелец после E7 назовёт три и больше неудобств, не покрытых этими тремя источниками, добавить четвёртый (Atlassian Design System, навигация).

10 эвристик Нильсена:  
https://www.nngroup.com/articles/ten-usability-heuristics/

Эвристики для сложных приложений:  
https://www.nngroup.com/articles/usability-heuristics-complex-applications/

Carbon Design System, паттерны и таблица данных:  
https://carbondesignsystem.com/patterns/overview/  
https://carbondesignsystem.com/components/data-table/usage/  
https://carbondesignsystem.com/patterns/common-actions/  
https://carbondesignsystem.com/patterns/dialog-pattern/  
https://carbondesignsystem.com/patterns/empty-states-pattern/  
https://carbondesignsystem.com/patterns/notification-pattern/

Apple Human Interface Guidelines, боковые панели и вкладки:  
https://developer.apple.com/design/human-interface-guidelines/sidebars  
https://developer.apple.com/design/human-interface-guidelines/tab-bars
