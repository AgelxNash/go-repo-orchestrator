# Инженерные наблюдения по репозиторию

**Дата:** 2026-03-31

## Структура и организация

- Проект следует стандартной Go-структуре: `cmd/` + `internal/`, без `pkg/`.
- Пустые директории `internal/backup/` и `cmd/git-branch-cleaner/` удалены как legacy-placeholder; актуальный статус зафиксирован в [заметке от 2026-08-31](2026-08-31__remove-empty-placeholders.md).
- В `internal/usecase/` накопилось много артефактов-скриптов (`*.sh`) — это сгенерированные тестовые скрипты, не удалённые после тестирования.

## Зависимости

- Go 1.24 — используется современный стек.
- `go-git/v5` — основной Git-клиент; CLI используется как fallback для clone/fetch/pull.
- `bubbletea` + `lipgloss` + `bubbles` — TUI-стек от Charm.
- `playwright-go` — опциональная зависимость для browser transport.
- `cobra` + `viper` — CLI и конфигурация.
- `zap` — логгер, но в production-режиме `NewNop()` (намеренно, чтобы не ломать TUI).

## Тестирование

- Тесты есть в: `config/`, `git/`, `usecase/`, `tui/`, `cli/`, `browser/`, `jira/`, `filter/`.
- Integration test: `internal/usecase/integration_test.go`.
- Makefile: `make test`, `make check` (fmt + test + vet + build + lint).

## Потенциальные зоны внимания

1. **Артефакты в usecase/** — множество сгенерированных `.sh` файлов не в `.gitignore`. Стоит проверить, не закоммичены ли они.
2. **Пустые директории** — resolved: legacy-placeholder каталоги удалены, а `make check-empty-dirs` предотвращает их возврат.
3. **Jira noop fallback** — если Jira не настроен, используется `jira.NewNoop()`, который возвращает пустой статус. Это корректно, но неочевидно из конфига.
4. **Playwright auto-bootstrap** — при первом запуске может потребоваться установка driver; UX зависит от наличия прав и сетевого доступа.
5. **Locking** — `git.Client` использует per-path мьютексы для защиты от concurrent-операций; это важно для сценариев с несколькими репозиториями.
6. **Script generation** — скрипт создаётся в CWD (`os.Getwd()`), а не в state-dir; это сознательное решение, но может быть неочевидным.
