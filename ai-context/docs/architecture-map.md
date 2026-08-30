# Карта архитектуры go-repo-orchestrator

## Структура директорий

```
cmd/
  go-repo-orchestrator/     — точка входа (main.go)
  git-branch-cleaner/       — пустая директория (legacy/placeholder)
internal/
  app/                      — bootstrap, runtime, production logger
  cli/                      — Cobra-команды (root, generate)
  config/                   — YAML-парсинг, валидация, regex-компиляция
  model/                    — доменные типы (BranchInfo, RepoBranches, ScriptResult и др.)
  filter/                   — правила защиты веток (current/default/keep)
  git/                      — Git-клиент (go-git + CLI fallback)
  usecase/                  — BranchCleaner: загрузка веток, генерация скриптов
  jira/                     — Jira StatusService (HTTP + Playwright browser transport)
  browser/                  — Playwright runtime (CDP/launch, auto-bootstrap)
  tui/                      — BubbleTea модель (двухпанельный интерфейс)
  backup/                   — пустая директория (placeholder)
```

## Поток данных (runtime flow)

```
main.go
  → app.NewProductionLogger()     // zap.NewNop() — без stdout-вывода
  → cli.NewRootCommand()
      → PersistentPreRunE: валидация --config, загрузка YAML
      → config.LoadFromViper()
          → compilePatterns(branch.keep, branch.jira)
          → validateRepoIdentityConflicts()
      → app.NewRuntimeFromOptions()
          → git.NewClient(timeout, workspaceDir)
          → browser.NewPlaywrightRuntime(cdpURL, ...)
          → jira.NewStatusService(timeout, groups, browser, logger)
          → usecase.NewCleaner(gitClient, jiraStatusResolver, logger)
      → tui.NewModel(cfg, cleaner)
      → tea.NewProgram(model).Run()
```

## TUI → usecase → git/jira

```
tui.Model.Update()
  ├── initialLoadMsg → startPreloadPass()
  │     └── loadRepoBranchesCmd(cleaner, repo)
  │           └── cleaner.LoadRepoBranches(repo)
  │                 ├── git.ResolveRepoPath() / EnsureManagedClone()
  │                 ├── git.ListBranches()         → []model.BranchInfo
  │                 ├── git.CurrentBranch()
  │                 ├── git.DetectDefaultBranch()
  │                 ├── git.GetDirtyStats()
  │                 ├── jira.PrefetchStatuses()     → batch Jira API
  │                 │     └── StatusService.fetchAndStoreBatch()
  │                 │           ├── HTTP transport
  │                 │           └── Browser transport (Playwright)
  │                 ├── filter.Evaluate() / repo.IsProtected()
  │                 └── git.BranchMetadata()        → merge status
  │
  ├── F8/g → openConfirmIfPossible()
  │     └── generateScriptCmd(cleaner, repo, branches, format)
  │           └── cleaner.GenerateDeleteScript()
  │                 └── buildScriptContent() → .sh/.bat
  │
  ├── F7 (branches) → CreateLocalTrackingBranch()
  ├── F7 (repos)    → FetchAndPullRepo()
  ├── * (branches) → Release-aware autocheck pipeline
  │     ├── release selection modal (Jira API v2: released versions)
  │     └── autocheck pipeline: selected release -> issue keys (JQL) -> branch match -> protection filter -> selection state
  ├── + (branches) → Invert selection state
  ├── Enter (repos) → focusBranches
  └── Enter (branches) → ForceCheckoutLocalBranch()
```

## Ключевые интерфейсы

### usecase.gitClient (internal/usecase/branch_cleaner.go)

Интерфейс, который `Cleaner` использует для взаимодействия с Git. Реализация: `git.Client`.

Ключевые методы:
- `ResolveRepoPath(repoName, repoURL, localPath)` — определяет рабочий путь
- `EnsureManagedClone(repoName, repoURL)` — гарантирует managed clone
- `ListBranches(repoPath)` — локальные + удалённые ветки
- `BranchMetadata(repoPath, branch, defaultBranch)` — merge status + base branch
- `GetDirtyStats(repoPath)` — modified/added/deleted/untracked

### jira.StatusResolver (internal/jira/status.go)

Интерфейс для получения статуса Jira-тикетов. Реализация: `StatusService`, fallback: `jira.Noop`.

- `ResolveStatus(group, ticketURL, jiraBaseURL, key)` — одиночный запрос
- `PrefetchStatuses(requests)` — batch-запрос (до 500 ключей за раз)

## Конфигурация (YAML)

```yaml
browser:
  cdp_url: 'http://127.0.0.1:9222'   # опционально

jira:
  - group: 'EXAMPLE'
    url: https://jira.example.com
    type: 'login' | 'token'
    playwright: false                  # true → browser transport
    login: { username, password }
    token: '...'

repos:
  - name: 'unique-name'               # обязательно, уникально
    url: git@github.com:...            # опционально
    path: ./local/path                 # опционально
    branch:
      autoswitch: 'main'              # только для opensource
      keep: ['^(main|develop)$']      # regex-защита
      jira: ['(?P<GROUP>KEY-\d+)']    # named-group → jira.group mapping
```

## Jira key extraction (branch.jira regex)

Приоритет извлечения Jira-ключа из имени ветки:
1. **Named group** с именем, совпадающим с `jira.group` → прямой mapping + ticket URL
2. **Named group** без совпадения → key extracted, но без URL
3. **Fallback `JIRA` group** → key из `(?P<JIRA>...)`
4. **Full match fallback** → вся совпавшая строка как key

## Playwright runtime

- Включается только если `config.PlaywrightEnabled()` (хотя бы одна Jira-группа с `playwright: true`)
- Режимы: `launch` (headless chromium) или `cdp` (подключение к внешнему браузеру)
- Auto-bootstrap: если driver отсутствует, автоматически устанавливается
- Driver directory: `<state-dir>/playwright/driver/<version>/`

## Безопасность

### Shell-экранирование

- `internal/usecase/quoteForPOSIX` — single-quote POSIX escaping для `.sh` скриптов.
- `internal/usecase/quoteForBat` (комбинирует `escapeForBat` + кавычки) — cmd.exe-safe escaping:
  удваивает `"`, `^`, `%`; префиксует `!`, `&`, `|`, `<`, `>` кареткой.
- `internal/usecase/sanitizeBranchName` — fail-fast валидация перед встраиванием в команды:
  отклоняет пустые, control-символы, DEL (0x7F), `%var%`, backtick, длину > 200.

### TLS/Jira transport

- `jira[].ssl` (PR #33): per-group http.Client с `tls.Config`; combined PEM / отдельные cert+key; традиционно зашифрованный PEM-ключ; `ca_cert`; `verify: false` (только для тестовых сред).
- Fail-fast: ошибки загрузки сертификатов останавливают приложение на старте.

### Текущий attack surface

- `browser.cdp_url` — высокорисковый вектор: доверяет произвольному CDP endpoint.
- Jira redirect boundary — без `CheckRedirect` клиент не ограничен исходным origin.
- Browser→HTTP fallback — меняет модель аутентификации; рекомендуется opt-in.
- `repo.path` — destructive Git по произвольному абсолютному пути.
- `escapeForBat` — ранее имел недостаточное экранирование; исправлено в PR по hardening.

Подробный runbook: `ai-context/notes/2026-08-30__security-audit-runbook.md`.
