# Contributing

## Conventional Commits

В репозитории используется спецификация [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/).

Формат сообщения коммита:

```text
<type>[optional scope][!]: <description>
```

Примеры валидных сообщений:

- `feat: add Jira status badges`
- `fix(tui): handle empty branch list`
- `refactor(git)!: remove legacy remote sync path`
- `docs: update contributing flow`

Рекомендуемые типы: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf`, `revert`, `style`.

### Локальная автоматическая проверка (Git Hooks)

Чтобы включить локальные проверки до CI, выполните в корне проекта:

```bash
make commitlint-install
make golangci-lint-install
make setup-hooks
```

После этого будут активны хуки:

- `commit-msg` — проверка формата сообщения коммита через `commitlint`.
- `pre-commit` — быстрые проверки (`make fmt-check`, `make vet`).
- `pre-push` — полный quality gate (`make check`).

При необходимости те же проверки можно запускать вручную:

```bash
make fmt-check
make vet
make check
```

## Локализация пользовательских сообщений

- User-facing ошибки, тексты CLI/TUI и оборачивающие сообщения ошибок в проекте должны быть на русском языке.
- Английский допустим только для технических literal-значений, ключей конфигурации, команд, протокольных терминов и внешней диагностики.

## Helper-команда для релизного тега

Для создания и публикации аннотированного тега используйте:

```bash
make release-tag VERSION=v0.1.0 MESSAGE='First public release "stable"'
```

`VERSION` и `MESSAGE` обязательны. Если тег уже существует, команда завершится с ошибкой до попытки `git push`.

## Требование к PR title

Заголовок Pull Request должен соответствовать тому же формату Conventional Commits.

Примеры валидных PR title:

- `feat: add conventional commits validation workflow`
- `ci: enforce golangci and commit format checks`

## Какие проверки блокируют merge

Для Pull Request обязательным техническим quality gate является:

- `ci / go-checks` — проверка форматирования, тестов, `go vet`, сборки и `golangci-lint` (конфиг `.golangci.yml`).

Дополнительно может требоваться `conventional-commits / pr-title`, если для PR используется Conventional Commits.

Следующие проверки являются advisory и не блокируют merge: проверка сообщений отдельных коммитов, Markdown Link Check, YAML Lint, CodeQL, CodeRabbit и Dependabot Auto Merge. Они помогают заметить проблемы, но могут зависеть от внешних сервисов, лимитов или нерелевантных для изменения файлов.

## Branch protection (настройка вручную)

В GitHub settings для целевой ветки включите Require a pull request before merging и Require status checks to pass before merging. Добавьте Required status checks:

- `ci / go-checks`
- `conventional-commits / pr-title` — если нужен контроль формата заголовков

Не добавляйте в Required status checks CodeRabbit, CodeQL, Markdown Link Check, YAML Lint, Dependabot Auto Merge или `commit-messages`. Не включайте обязательную актуальность ветки и обязательное разрешение разговоров, если они создают лишние rebase-циклы или блокировки от advisory-ботов.
