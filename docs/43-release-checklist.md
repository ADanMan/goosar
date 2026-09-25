# Чек-лист выкатки Goosar 1.1.0

## Перед выкаткой
- [ ] Тикеты T-001..T-018 закрыты, acceptance criteria проверены по одному.
- [ ] `pnpm typecheck`, тесты `views`, `ui`, `go test ./...` зелёные на чистом клоне ветки `redesign/1.1`.
- [ ] Визуальные эталоны T-017 обновлены и тест проходит.
- [ ] `41-changelog.md`: секция `[1.1.0]` с датой, `[Unreleased]` пуст.
- [ ] Версии 1.1.0 в семи `package.json` и `Chart.yaml`.
- [ ] `THIRD_PARTY_NOTICES.md` перегенерирован, три новых шрифта в списке.
- [ ] Миграций нет: `ls server/migrations` содержит только `001_init.*`.
- [ ] Владелец подтвердил пуш ветки и тег (до этого пункта репозиторий локальный).

## Выкатка
- [ ] Ветка слита в `main`, тег `v1.1.0` проставлен.
- [ ] Образы собраны на сервере из `git archive v1.1.0`, запушены как `v1.1.0` и `latest`.
- [ ] `GOOSAR_IMAGE_TAG=v1.1.0` в `/opt/goosar/.env`, `docker compose up -d`.
- [ ] Смоук из `32-test-plan.md`, раздел 5, пройден.
- [ ] GitHub Release создан локальным пайплайном (goreleaser, desktop, helm, selfhost-бандл, image-kit), без Actions.

## После
- [ ] Через 15 минут: `docker compose logs --since 15m backend | grep -c ERR` равен 0.
- [ ] `00-overview.md`: статус и ближайший шаг обновлены.
- [ ] Точка отката зафиксирована: `GOOSAR_IMAGE_TAG=v1.0.1`, откат безопасен без ограничений, миграций не было.
