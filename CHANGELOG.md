# Changelog

Все заметные изменения проекта фиксируются в этом файле.

Формат основан на [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/), версии проекта следуют [Semantic Versioning](https://semver.org/lang/ru/).

## [Unreleased]

### Added

- Команда `doctor repo <NAME>`: диагностика загрузки одного репозитория (source-тип, абсолютный и относительный путь к каталогу, статус Git: валидность HEAD, ветки, upstream, dirty, предупреждение об `autoswitch`, проба синхронизации `fetch --prune origin` с полной ошибкой и понятный вердикт) — без запуска TUI (#68).

### Fixed

- Mass-fetch на старте TUI больше не открывает все SSH-соединения сразу: сетевые git-операции ограничены семафором (по умолчанию 6) и повторяются при transient-сбоях handshake (`kex_exchange_identification`, `Connection reset by peer`, таймаут). После загрузки показывается сводка «синхронизировано N/M, K ошибок (сеть)» с переходом к проблемному репозиторию; полный текст ошибки git доступен в ИНФО и логе, а клон без checkout отличается от generic-ошибки HEAD. Неудачный `git clone` не удаляет уже существующий целевой каталог, отмена контекста не подменяется предыдущей сетевой ошибкой, а пометка «(сеть)» ставится только сетевым `LoadError` (#66).

## [1.0.0] - 2026-09-10

### Added

- Команда `doctor jira <ISSUE-KEY>`: один запрос статуса Jira-задачи с полным трейсом (транспорт, CDP preflight и выбор контекста для browser-групп, HTTP-статус/content-type/final URL, фрагмент тела, вердикт классификатора auth, факт и причина browser→HTTP fallback) — без запуска TUI и без секретов в выводе.
- Подстановка переменных окружения в значениях YAML-конфига: плейсхолдеры `${VAR}` и `${VAR:-default}` (синтаксис docker-compose) во всех строковых значениях; незаданная переменная без дефолта — понятная ошибка на старте с именем переменной и путём ключа; `$$` экранирует литеральный `$`.
- CI: `make test-race`, coverage-профиль и artifact `coverage-profile` в GitHub Actions.
- CI: `make vulncheck` и `govulncheck` в `ci / go-checks`.
- CI: расширенный набор `golangci-lint` (`gosec`, `errorlint`, `bodyclose`, `copyloopvar`, `misspell`, `nolintlint`, `revive`).
- Модель `RepoWarning{Code, Message}` для non-fatal предупреждений репозитория.
- ADR-каталог с ретроспективной фиксацией ключевых архитектурных решений.
- `config.schema.json` для editor-autocomplete и ранней проверки структуры `config.yaml`.

### Changed

- Удалены legacy-placeholder директории `cmd/git-branch-cleaner/` и `internal/backup/`; `make check` теперь проверяет отсутствие пустых каталогов под `cmd/` и `internal/`.
- Jira status prefetch стал context-aware: отмена операции из TUI доходит до batch-запросов статусов.
- `internal/jira/status.go` разделён на сфокусированные файлы без изменения поведения.
- `internal/usecase/branch_cleaner.go` разделён на сфокусированные файлы без изменения поведения.
- `Model.Update` разделён на message handlers; `model.go` уменьшен, поведение TUI сохранено.
- TUI зависит от узкого `cleanerPort` вместо конкретного `*usecase.Cleaner`.

### Fixed

- Относительные `repos[].path` резолвятся от каталога файла конфигурации, а не от CWD процесса: поведение конфига перестало зависеть от точки запуска, и запуск «над» каталогом конфига больше не уводит opensource-режим на клонирование репозиториев в неожиданное место (#67).
- Изоляция git-окружения в тестах и pre-push хуке: переменные `GIT_*` (в частности `GIT_DIR` из хука worktree) больше не наследуются git-подпроцессами тестов и не уводят их из временных каталогов в реальный репозиторий (#62).
- Опечатка в keep-regex MaxScale в `config.example.yaml` (лишняя `}`): правило, задуманное как защита стабильных веток 22.xx–24.xx, не матчило ни одну реальную ветку (#51); добавлен regression-тест на семантику правила.

### Security

- `govulncheck` добавлен в основной CI quality gate.
- Race detector добавлен в основной CI test-run.
- Jira HTTP-транспорт (общий и mTLS per-group клиенты) запрещает redirect со сменой origin — защита от утечки `Authorization` на посторонний host.
- CDP-подключение ограничено loopback-эндпоинтами: `browser.cdp_url` валидируется на loopback IP, `webSocketDebuggerUrl` из `/json/version` проверяется на loopback и `ws`/`wss`, проваленный preflight блокирует запуск.
- Лимит тела ответа Jira 4 МБ на HTTP- и browser-путях (oversized классифицируется как постоянная ошибка `response_too_large`, не как временный сбой) и guard пагинации релизов (не более 1000 страниц).

## История релизов

Подробные release notes публикуются в [GitHub Releases](https://github.com/AgelxNash/go-repo-orchestrator/releases). Начиная с этого файла, новые заметные изменения дополнительно фиксируются в секции `Unreleased` и переносятся в версионную секцию при релизе.
