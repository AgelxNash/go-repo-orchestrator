# Hardening: shell-escape и sanitizeBranchName

- **Дата:** 2026-08-30
- **Ветка:** `feat/hardening-branch-quoting`
- **Контекст:** по итогам CTF-аудита (см. `2026-08-30__security-audit-runbook.md`) выделен MEDIUM-риск BAT-экранирования: `escapeForBat` удваивал только `"`, что открывало поверхность для `cmd.exe` injection через имена веток и удалённых ref'ов.

## Что сделано

1. `internal/usecase/shellescape.go` — новый модуль:
   - `sanitizeBranchName(name) (string, error)` — fail-fast проверка: пустые, control-символы (CR/LF/NUL/TAB/DEL/0x7F/vertical-tab/form-feed), `%%` (cmd.exe expansion), backtick, длина > 200.
   - `escapeForBat(value) string` — безопасное cmd.exe-escaping: удваивает `"`, `^`, `%`; префиксует `!`, `&`, `|`, `<`, `>` кареткой.
   - `quoteForBat(value) string` — обёртка с кавычками.

2. `internal/usecase/branch_cleaner.go`:
   - `buildScriptContent` и `buildDeleteCommandBAT` используют `quoteForBat`.
   - Перед генерацией команды вызывается `branchPassesSanityCheck` — небезопасные имена отбрасываются.
   - `escapeForBat` оставлен как низкоуровневая функция, но больше не вызывается в BAT-командах напрямую.

3. Тесты — `internal/usecase/shellescape_test.go`:
   - 5 safe-кейсов, 11 unsafe-кейсов, проверка trim, 8 кейсов `escapeForBat`, `quoteForBat`, отказ `buildDeleteCommandBAT` для небезопасных имён.

4. `internal/jira/parser_test.go`:
   - 9 кейсов для `buildSearchStatusURL` и `parseSearchStatuses` (валидные/невалидные URL, JQL-кодирование, фильтрация пустых ключей, dedup по последнему, malformed JSON).

5. Документация — `ai-context/docs/architecture-map.md` дополнен разделом «Безопасность» с обзором shell-escape, TLS и текущего attack surface.

## Покрытие

- `internal/usecase`: 72.7% → 73.9%.
- `internal/jira`: 73.8% → 76.4%.

## Что не входит в hardening

- CDPUrl-allowlist (`127.0.0.1`/`::1`) — отдельная задача, требует изменения конфигурации.
- Jira `CheckRedirect` — требует поведенческих изменений, оставлено на следующий цикл.
- Workspace boundary для `repo.path` — требует более глубокого рефакторинга.
