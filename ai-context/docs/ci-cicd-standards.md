# Стандарты CI/CD и merge-политика

Документ определяет воспроизводимую политику GitHub Actions и branch protection для репозитория.

## 1. Версии GitHub Actions

Используйте актуальные major-версии GitHub Actions, совместимые с workflow проекта. На текущем этапе используются:

- `actions/checkout@v7`;
- `actions/setup-go@v6` до merge Dependabot PR с обновлением до v7;
- `actions/setup-node@v6` до merge Dependabot PR с обновлением до v7.

Не понижайте версию action без подтверждённой причины совместимости или безопасности.

## 2. Обязательный merge gate

Корректный PR должен проходить без ожидания внешних ботов и нерелевантных проверок.

Единственный обязательный технический quality gate:

- `ci / go-checks` — форматирование Go-кода, тесты, `go vet`, сборка и `golangci-lint`.

Дополнительный лёгкий policy gate:

- `conventional-commits / pr-title` — формат заголовка PR. Он применяется, если в репозитории поддерживается Conventional Commits.

Branch protection для `main` использует Pull Request before merging и required checks из этого раздела. Не требуется обновлять ветку от `main` или разрешать все разговоры перед merge, если это создаёт только технический шум.

## 3. Advisory-проверки

Следующие инструменты дают полезный сигнал, но не являются merge blockers:

- `conventional-commits / commit-messages`;
- Markdown Link Check;
- YAML Lint;
- CodeQL и Code scanning;
- CodeRabbit;
- Dependabot Auto Merge.

Падение advisory-проверки из-за внешнего сайта, rate limit, ограничения бота или нерелевантного для PR содержимого не является регрессией приложения. Владелец может merge корректный PR при успешном `ci / go-checks` и осознанном review diff.

## 4. Markdown Link Check

Проверка Markdown-ссылок запускается вручную (`workflow_dispatch`) и еженедельно по расписанию. Она не выполняется на каждом push или PR и не добавляется в Required status checks.

Это позволяет находить битые ссылки в документации, не блокируя обновления Go-зависимостей, CI-actions или кодовые изменения из-за временно недоступных внешних ресурсов и приватных URL.

## 5. CodeRabbit и CodeQL

CodeRabbit используется как advisory code review assistant. Его комментарии и статус не добавляются в Required status checks: внешний сервис может не прислать статус, исчерпать лимит или дать рекомендацию, не относящуюся к цели PR.

CodeQL и Code scanning остаются security-сигналом. Без явного стабильного workflow с предсказуемым именем check они не должны быть обязательными для merge.

Требование покрытия docstrings не является CI gate, пока в репозитории не существует воспроизводимой локальной проверки. Экспортируемые Go-сущности следует документировать по принятым правилам кода, но advisory-комментарий не блокирует merge.

## 6. Dependabot

Dependabot создаёт PR для обновления зависимостей, но не выполняет автоматический merge. Решение о merge принимается владельцем после успешного `ci / go-checks` и review diff.

Автоматический merge отключён, чтобы сторонний workflow с write-permissions не вносил изменения в `main` без контроля владельца.

## 7. Релизы

Release workflow запускается только на тегах `v*` и не является PR gate. Его preflight проверяет тесты, `go vet` и конфигурацию GoReleaser перед публикацией релиза.
