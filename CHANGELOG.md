# Changelog

Все заметные изменения проекта фиксируются в этом файле.

Формат основан на [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии проекта следуют [Semantic Versioning](https://semver.org/lang/ru/).

## [Unreleased]

### Added

- CI: `make test-race`, coverage-профиль и artifact `coverage-profile` в GitHub Actions.
- CI: `make vulncheck` и `govulncheck` в `ci / go-checks`.
- CI: расширенный набор `golangci-lint` (`gosec`, `errorlint`, `bodyclose`, `copyloopvar`, `misspell`, `nolintlint`, `revive`).
- Модель `RepoWarning{Code, Message}` для non-fatal предупреждений репозитория.
- ADR-каталог с ретроспективной фиксацией ключевых архитектурных решений.
- `config.schema.json` для editor-autocomplete и ранней проверки структуры `config.yaml`.

### Changed

- Jira status prefetch стал context-aware: отмена операции из TUI доходит до batch-запросов статусов.
- `internal/jira/status.go` разделён на сфокусированные файлы без изменения поведения.
- `internal/usecase/branch_cleaner.go` разделён на сфокусированные файлы без изменения поведения.
- `Model.Update` разделён на message handlers; `model.go` уменьшен, поведение TUI сохранено.
- TUI зависит от узкого `cleanerPort` вместо конкретного `*usecase.Cleaner`.

### Security

- `govulncheck` добавлен в основной CI quality gate.
- Race detector добавлен в основной CI test-run.

## История релизов

Подробные release notes публикуются в [GitHub Releases](https://github.com/AgelxNash/go-repo-orchestrator/releases). Начиная с этого файла, новые заметные изменения дополнительно фиксируются в секции `Unreleased` и переносятся в версионную секцию при релизе.
