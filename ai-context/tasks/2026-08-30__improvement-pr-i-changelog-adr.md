# PR-I: CHANGELOG + ADR

- **Статус:** Done (PR #45 merged 2026-08-30; `make check`, local markdown link sanity зелёные)
- **Трек:** DX/документация (1/2)
- **Источник:** план улучшений, одобрен владельцем 2026-08-30.

## TL;DR
В проекте нет CHANGELOG.md и ADR-процесса. Завести формат Keep a Changelog и каталог `adr/` с ретроспективными ADR по уже принятым решениям.

## Скоуп
- `CHANGELOG.md`: начальная запись, отражающая текущий цикл улучшений (PR #37–#44) и правила ведения в CONTRIBUTING.
- `adr/0001..0004`: BubbleTea MVU для TUI; assistive-only модель безопасности (без auto-delete); стратегия Jira-интеграций HTTP/Playwright-CDP; релизы GoReleaser+GPG.

## Acceptance Criteria
- [ ] CHANGELOG соответствует Keep a Changelog, ссылается на GitHub Releases.
- [ ] 4 ADR в стандартном формате (контекст/решение/последствия), статус Accepted.
- [ ] CONTRIBUTING дополнен правилами ведения.
- [ ] `markdown-link-check` и `yaml-lint` не сломаны.

## Out of Scope
- Перенос истории всех релизов в CHANGELOG вручную.
