# PR-J: JSON Schema для config.yaml + strict-unmarshal

- **Статус:** Done (PR готов: schema validation, `make check`, `make test-race`, `make vulncheck` зелёные)
- **Трек:** DX/документация (2/2)
- **Источник:** план улучшений, одобрен владельцем 2026-08-30.

## TL;DR
Опечатки в ключах YAML молча игнорируются viper'ом. Добавить JSON Schema для editor-валидации пользователей и strict-decode в рантайме.

## Скоуп
- `config.schema.json`: секции `repos[]` (name/url/path/branch.keep/jira/autocheck), `jira[]` (group/url/type/login/token/ssl/playwright), `browser.cdp_url`, `state_dir`.
- Strict-decode через `viper.UnmarshalExact` с pre-check root-ключей YAML и allowlist существующих CLI/Viper ключей (`config`, `state_dir`) — неизвестные ключи становятся ошибкой конфигурации.
- Прогнать все встроенные примеры/фикстуры/тесты: вскрытые проблемы показывать владельцу до правок.
- Godoc-чистка только в затронутых файлах.
- Пустой пакет `internal/backup` — удаление только после отдельного подтверждения владельца.

## Acceptance Criteria
- [x] Schema валидирует `config.example.yaml` и примеры из README.
- [x] Strict-decode отклоняет опечатку в ключе с понятной ошибкой; легитимные конфиги проходят.
- [x] Существующие тесты config-пакета зелёные (или обновлены легитимно).
- [x] `make check` зелёный.

## Out of Scope
- Полная godoc-чистка всех 66 деклараций (отдельная итерация).
- Смена структуры конфига.
