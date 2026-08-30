# 2026-08-30__dependabot-security-update.md

## Общая информация
- **Дата:** 2026-08-30
- **Контекст:** Закрытие 16 открытых Dependabot alerts в разделе `Security and quality → Dependabot` репозитория.
- **Инструменты:** Playwright (чтение приватной страницы alerts), `go get`, `govulncheck` (x/vuln latest, собран под Go 1.26).

## Исходное состояние (16 alerts)

| Пакет | Алерты | Severity | CVE (примеры) | Затронутые версии | Patched | Тип зависимости |
|---|---|---|---|---|---|---|
| `golang.org/x/crypto` | #13, #14, #15, #16, #17, #18, #19, #20, #21, #22, #23, #24, #25 (13 шт.) | 7 Critical, 2 High, 4 Moderate | CVE-2026-42508 и др. | < 0.52.0 | 0.52.0 | indirect (транзитивная через go-git) |
| `github.com/go-git/go-git/v5` | #26, #27 | 1 High, 1 Moderate | CVE-2026-71556, CVE-2026-71557 | <= 5.19.1 | 5.19.2 | direct |
| `golang.org/x/net` | #12 | Moderate | CVE-2026-25680 | < 0.55.0 | 0.55.0 | indirect |

Суть ключевых уязвимостей:
- **x/crypto (пакет ssh):** пачка фиксов одного релиза v0.52.0 — auth bypass через незаenforced `@revoked` статус, обход key constraints агента, bypass FIDO/U2F physical presence check, deadlock сервера, infinite loop на больших channel writes, DoS на патологических RSA/DSA параметрах, memory leak при reject каналов, panic сервера в CheckHostKey/Authenticate.
- **go-git #26:** symlink traversal в `worktreeFilesystem` — операции worktree могут модифицировать файлы за пределами intended worktree path.
- **go-git #27:** malicious reference names могут изменять файлы за пределами reference storage.
- **x/net #12:** DoS в `golang.org/x/net/html` парсере.

Причина, почему Dependabot не мог обновить сам: go-git v5.19.2 требует `x/crypto >= 0.53.0` и `x/net >= 0.56.0`, что выше минимальных patched-версий алертов («Dependabot cannot update to the required version: One or more other dependencies require a version that is incompatible»).

## Внесённые изменения
Обновлены `go.mod` и `go.sum`; код проекта не тронут. Go baseline поднят с 1.25.0 до 1.26.6. CI и release уже используют `go-version-file: go.mod`, поэтому обе цепочки собирают проект одной и той же patched-версией Go.

| Модуль | Было | Стало |
|---|---|---|
| `github.com/go-git/go-git/v5` | v5.19.1 | **v5.19.2** |
| `golang.org/x/crypto` (indirect) | v0.50.0 | **v0.55.0** |
| `golang.org/x/net` (indirect) | v0.53.0 | **v0.57.0** |
| `golang.org/x/sys` (indirect) | v0.43.0 | v0.47.0 (сопутствующая, требование совместимости) |
| `golang.org/x/text` (indirect) | v0.36.0 | v0.41.0 (сопутствующая) |
| `golang.org/x/term` (indirect) | — | v0.45.0 (сопутствующая) |

Промежуточные версии: сначала подняты ровно patched-минимумы (5.19.2 / 0.52.0 / 0.55.0), затем `x/crypto` поднят до 0.55.0 для закрытия GO-2026-6303 (fixed в 0.55.0), обнаруженной govulncheck сверх Dependabot-списка.

## Проверки и верификация
- `make check` (fmt-check + test + vet + build + golangci-lint) — **успешно, exit 0, 0 issues**; все 11 пакетов с тестами — `ok`.
- `govulncheck ./...` после обновления:
  - Module-level уязвимости, вызываемые кодом: **0** (было: весь Dependabot-набор).
  - Осталась одна module-level GO-2026-5932 (`x/crypto@v0.55.0`, **Fixed in: N/A** — исправления upstream пока нет), код проекта её не вызывает.
  - Stdlib: 7 уязвимостей go1.26.3 (см. «Остаточный риск»).

## Анализ безопасности (Security Triage)
- **go-git #26/#27 (прямая зависимость, реальный usage):** проект клонирует репозитории из workspace-конфига (`internal/git`), т.е. парсит reference names и пишет в worktree потенциально недоверенного происхождения — обе уязвимости **применимы по сценарию**, обновление до v5.19.2 обязательно. Наивысший приоритет из всего набора.
- **x/crypto (13 алертов):** все — в пакете `ssh` (SSH-агенты, FIDO/U2F, SSH-сервер). Проект использует go-git для `git@...` SSH-remote; SSH-агент/сервер/keysign API напрямую не вызываются, поэтому фактическая поверхность минимальна — но транзитивное обновление всё равно необходимо (требование go-git 5.19.2).
- **x/net #12:** HTML-парсер (`x/net/html`) в коде проекта напрямую не используется (транзитивно через playwright-go/bubbletea), DoS-сценарий неприменим напрямую; закрыто попутно.

## Остаточный риск
1. **GO-2026-5932** (`x/crypto` v0.55.0, fixed: N/A) — ждём upstream-релиз; не вызывается кодом. Отслеживать в следующем цикле обновлений.
2. **Go standard library:** baseline поднят до Go 1.26.6, которая содержит фиксы для ранее найденных уязвимостей GO-2026-6218, -6090, -5972, -5856, -5039, -5037 и -5026. Требуется повторный прогон `govulncheck` после обновления toolchain.
3. Dependabot alerts на GitHub закроются автоматически после попадания обновлённого `go.mod` в `main` (Dependabot scans default branch).

## Ограничения
- Операции `git commit`, `git push` и `git merge` не выполнялись на момент первоначальной версии этой заметки. Актуальный статус фиксируется в итоговом отчёте security-работ.
