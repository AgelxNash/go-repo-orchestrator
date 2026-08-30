# Упрощение merge-политики и CI

## Цель

Корректные Pull Request должны проходить merge по воспроизводимому техническому критерию, а не блокироваться внешними ботами, временно недоступными ссылками или job, не относящимися к изменению.

## Принятое решение

Обязательный технический merge gate для `main`:

- `ci / go-checks` — форматирование, тесты, `go vet`, сборка и `golangci-lint`.

Лёгкий policy gate:

- `conventional-commits / pr-title`.

Advisory-проверки, не блокирующие merge:

- `conventional-commits / commit-messages`;
- Markdown Link Check;
- YAML Lint;
- CodeQL и Code scanning;
- CodeRabbit;
- Dependabot Auto Merge.

## Причины

- PR #30 с обновлением уязвимых зависимостей был технически корректен, но Markdown Link Check падал на приватной Dependabot URL, недоступной неавторизованному runner.
- PR #26 с одной корректной заменой `actions/setup-node@v6` на `@v7` получил failing Markdown Link Check на push, хотя не менял Markdown.
- CodeRabbit может упираться в лимит review и выдавать advisory-комментарии; его статус не является воспроизводимым quality gate.
- CodeQL и Code scanning поступают из динамической GitHub-конфигурации и могут иметь нестабильные имена и neutral/configuration-not-found состояния.

## Изменения workflow

- Markdown Link Check переведён на еженедельный запуск и ручной `workflow_dispatch`; он не запускается на PR/push.
- Dependabot Auto Merge удалён: обновления зависимостей проверяются CI, но merge остаётся решением владельца.
- Основной `ci / go-checks` не ослабляется.

## Branch protection

Для `main` включены Pull Request before merging и required status checks. Required contexts ограничиваются `ci / go-checks` и `conventional-commits / pr-title`. Актуальность ветки, разрешение всех разговоров и требование статусов внешних ботов не используются как обязательные условия merge.

## Проверка

Политика публикуется отдельным PR. Его техническая корректность проверяется `make check` и YAML lint. После merge Dependabot PR #26/#27 проходят по основному quality gate без ожидания advisory jobs.
