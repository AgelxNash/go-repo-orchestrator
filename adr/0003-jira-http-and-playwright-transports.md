# 0003. Поддерживать Jira через HTTP и Playwright/CDP транспорты

- **Status:** accepted
- **Date:** 2026-08-30

## Context

Jira-интеграция нужна для сопоставления веток с задачами, статусов задач и release-driven autocheck. В корпоративных окружениях Jira может быть доступна разными путями: стандартный REST API с Bearer/Basic auth, Server/DC с mTLS/self-signed CA, SSO/Cloudflare/Captcha-сценарии, где прямой HTTP-запрос не проходит.

Альтернативы: поддерживать только HTTP REST API, поддерживать только browser automation, требовать отдельный proxy/token extractor.

## Decision

Поддерживать два транспорта:

- HTTP transport по умолчанию: Jira REST API, Bearer/Basic auth, per-group TLS/mTLS/CA настройки;
- Browser transport через Playwright/CDP: для SSO/сложных корпоративных сценариев, с HTTP fallback при недоступности browser runtime.

Группа Jira (`jira[].group`) управляет URL, auth и transport. Named capture groups в `branch.jira` связывают ветки с конкретной Jira-группой.

## Consequences

Плюсы:

- один инструмент покрывает обычные и сложные корпоративные Jira-сценарии;
- HTTP transport остаётся быстрым и простым для стандартных инстансов;
- Playwright/CDP не требует хранения SSO-секретов в конфиге приложения;
- per-group настройки позволяют пережить миграции между Jira-инстансами.

Минусы:

- больше attack surface: CDP endpoint и redirect-политики требуют отдельного hardening;
- browser runtime сложнее тестировать и диагностировать;
- fallback-логика должна быть явной, чтобы пользователь понимал источник статусов.

Security backlog по этому решению: CDP URL allowlist и Jira CheckRedirect остаются отдельными задачами hardening.
