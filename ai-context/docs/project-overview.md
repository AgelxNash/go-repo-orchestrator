# Обзор проекта go-repo-orchestrator

## Назначение

Локальная TUI-утилита на Go для безопасной подготовки удаления Git-веток. Приложение **не удаляет ветки напрямую** — оно генерирует исполняемые скрипты (`.sh` / `.bat`), которые пользователь запускает самостоятельно.

## Ключевые сценарии

1. **Обзор репозиториев** — загрузка списка репозиториев из YAML-конфига, отображение текущей ветки, статуса dirty/clean.
2. **Анализ веток** — загрузка локальных и удалённых веток, определение merge-статуса, извлечение Jira-ключей из имён веток.
3. **Выбор и фильтрация** — пользователь выбирает ветки для удаления через TUI (Space/Insert), защищённые ветки автоматически исключены.
4. **Генерация скрипта** — по F8/g формируется `.sh` или `.bat` с командами `git branch -d/-D` (локальные) и `git push --delete` (удалённые).
5. **Jira-интеграция** — опциональное получение статуса Jira-тикетов через REST API или Playwright-browser transport.

## Режимы репозиториев

| Режим | Конфиг | Поведение |
|-------|--------|-----------|
| `url` | только `url` | Managed clone в `<state-dir>/workspace/<name>__<url-hash>/` |
| `path` | только `path` | Работа с локальным репозиторием напрямую |
| `opensource` (url+path) | `url` + `path` | Clone/fetch в указанную `path`, опциональный autoswitch |

## Безопасные ограничения

- **Protected ветки** — определяются regex-правилами `branch.keep` в конфиге; не могут быть выбраны для удаления.
- **Current branch** — текущая ветка репозитория всегда защищена.
- **Default branch** — определяется автоматически (`origin/HEAD` → `main` → `master` → current); защищена.
- **Remote default** — для удалённых веток default-ветка тоже защищена.
- **Ambiguous remote ref** — удалённая ветка с неоднозначным qualified-name не допускается к удалению.

## CLI

- `go-repo-orchestrator --config <path>` — запуск TUI (основной режим).
- `go-repo-orchestrator generate --config <path>` — сканирование текущей директории и генерация YAML-конфига.
- Параметр `--config` **обязателен** для всех команд.
- ENV override: префикс `GBC_` (например, `GBC_STATE_DIR`).

## Runtime зависимости

- Go 1.24+
- `git` в PATH
- Опционально: Playwright (авто-bootstrap при первом запуске, если хотя бы одна Jira-группа требует `playwright: true`)

## State directory

- Linux/macOS: `$HOME/.local/state/go-repo-orchestrator`
- Fallback: `.go-repo-orchestrator-state`
- Managed workspace: `<state-dir>/workspace/`
- Playwright driver: `<state-dir>/playwright/driver/<version>/`

## Генерируемые скрипты

Имя файла: `go-repo-orchestrator-<repo>-delete-<session>-<timestamp>.{sh,bat}`

Скрипт создаётся в текущей рабочей директории (`os.Getwd()`).

## Non-goals

- Прямое удаление веток из TUI (только генерация скриптов).
- Управление remote-репозиториями (нет push/pull для URL-only режима, кроме fetch+pull по F7).
- Runtime-логгирование в stdout (намеренно отключено, чтобы не ломать TUI).
