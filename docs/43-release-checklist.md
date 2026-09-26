# Чек-лист выкатки Goosar 1.1.0

## Перед выкаткой
- [x] Тикеты T-001..T-018 закрыты, acceptance criteria проверены по одному.
- [x] `pnpm typecheck`, тесты `views`, `ui`, `go test ./...` зелёные на чистом клоне ветки `redesign/1.1`.
- [x] Визуальные эталоны T-017 обновлены и тест проходит.
- [x] `41-changelog.md`: секция `[1.1.0]` с датой, `[Unreleased]` пуст.
- [x] Версии 1.1.0 в семи `package.json` и `Chart.yaml`.
- [x] `THIRD_PARTY_NOTICES.md` перегенерирован, три новых шрифта в списке.
- [x] Миграций нет: `ls server/migrations` содержит только `001_init.*`.
- [x] Владелец подтвердил пуш ветки и тег (26.09.2026).

## Выкатка
- [x] Ветка слита в `main`, тег `v1.1.0` проставлен.
- [x] Образы собраны на сервере из `git archive v1.1.0`, запушены как `v1.1.0` и `latest`.
- [x] `GOOSAR_IMAGE_TAG=v1.1.0` в `/opt/goosar/.env`, `docker compose up -d`.
- [x] Смоук из `32-test-plan.md`, раздел 5, пройден.
- [x] GitHub Release создан локальным пайплайном (goreleaser, desktop, helm, selfhost-бандл, image-kit), без Actions.

## После
- [ ] Через 15 минут: `docker compose logs --since 15m backend | grep -c ERR` равен 0.
- [x] `00-overview.md`: статус и ближайший шаг обновлены.
- [x] Точка отката зафиксирована: `GOOSAR_IMAGE_TAG=v1.0.1`, откат безопасен без ограничений, миграций не было.
