<div align="center">
  <h1>🚀 go-repo-orchestrator</h1>
  <p><b>Локальная TUI-утилита для оркестрации репозиториев, аудита задач и безопасной работы с Git-ветками</b></p>
  
  [![Go version](https://img.shields.io/github/go-mod/go-version/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/blob/main/go.mod)
  [![Latest release](https://img.shields.io/github/v/release/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/releases)
  [![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-fe5196.svg)](https://www.conventionalcommits.org/en/v1.0.0/)
  ![Go Report Card](https://goreportcard.com/badge/github.com/AgelxNash/go-repo-orchestrator)
  [![CI](https://img.shields.io/github/actions/workflow/status/AgelxNash/go-repo-orchestrator/ci.yml?branch=main&label=CI)](https://github.com/AgelxNash/go-repo-orchestrator/actions/workflows/ci.yml)
  [![Release workflow](https://img.shields.io/github/actions/workflow/status/AgelxNash/go-repo-orchestrator/release.yaml?label=Release)](https://github.com/AgelxNash/go-repo-orchestrator/actions/workflows/release.yaml)
  [![Go Reference](https://pkg.go.dev/badge/github.com/AgelxNash/go-repo-orchestrator.svg)](https://pkg.go.dev/github.com/AgelxNash/go-repo-orchestrator)
  [![License](https://img.shields.io/github/license/AgelxNash/go-repo-orchestrator)](https://github.com/AgelxNash/go-repo-orchestrator/blob/main/LICENSE)
</div>

<br/>

> **go-repo-orchestrator** — это ваш персональный пульт управления хаосом в микросервисах. Инструмент решает проблемы долгого онбординга, позволяет проводить сквозной поиск по веткам, следить за реальными статусами задач из Jira и генерировать безопасные скрипты для удаления мусорных веток в интерактивном TUI.

<div align="center">
  <br>
  <img src=".github/assets/demo.gif" alt="Демонстрация работы приложения" width="100%">
  <br>
  <i>Демонстрация работы приложения</i>
</div>

---

## 📖 Оглавление

- [🔍 Проблема и Решение](#-проблема-и-решение)
- [✨ Скрытые фичи и возможности](#-скрытые-фичи-и-возможности)
- [🚀 Быстрый старт](#-быстрый-старт)
- [💻 Установка](#-установка)
- [⌨️ Горячие клавиши](#️-горячие-клавиши)
- [🏷️ Jira release-driven autocheck](#️-jira-release-driven-autocheck)
- [⚙️ Конфигурация](#️-конфигурация)
- [🤝 Contributing](#-contributing)

---

## 🔍 Проблема и Решение

При работе с десятками микросервисов разработчик тратит кучу времени на рутину: долгий онбординг, сложные переключения (cd) между папками, потерю контекста статусов из таск-трекера и страх случайно удалить нужную ветку командой `git branch -D`.

**go-repo-orchestrator решает эти боли, объединяя управление всеми репозиториями в одном интерфейсе.**

| Боль | ❌ Обычный подход | ✅ go-repo-orchestrator |
|---|---|---|
| **Долгий онбординг** | Ручное клонирование 50+ репо по папкам | Запуск с общим `config.yaml` автоматически вытягивает и раскладывает все проекты |
| **Поиск и навигация** | Бесконечные `cd`, `git branch`, IDE | Глобальный поиск веток и переход (Checkout) кликом мыши или нажатием `Enter` |
| **Зависающие задачи** | Искать статусы вручную | Сквозной мониторинг статусов из Jira напрямую в TUI (легко найти отмененные задачи) |
| **Смена Jira/Проектов** | Неудобно отслеживать разные домены | Поддержка одновременной работы сразу с **несколькими** инстансами Jira |
| **Опасность `git branch -D`**| Страх удалить чужой код / production | TUI-выбор с предпросмотром, генерацией скрипта и `branch.keep` regex защитой |

### 🔒 Безопасное удаление по умолчанию

Деструктивные команды не выполняются "под капотом". Вы выбираете ветки в TUI, после чего генерируется `.sh` / `.bat` скрипт, который вы сможете проанализировать и запустить вручную. К тому же, утилита аппаратно защищает текущую активную ветку и системные ветки (по умолчанию: `main|master|prod|release`).

**Принципы безопасности:**
- **Assistive-only**: Инструмент только помогает найти кандидатов на удаление, но никогда не принимает окончательное решение сам.
- **Никакого Auto-delete**: Автоматическое удаление веток отсутствует как класс.
- **Приоритет ручного выбора**: Любая автоматическая пометка (regex или Jira autocheck) может быть мгновенно отменена пользователем в TUI.
- **Никакого Silent Auto-run**: Проверки статусов релизов никогда не запускаются скрытно при старте или обновлении (refresh) — только по явному действию пользователя.

---

## ✨ Скрытые фичи и возможности

- 🚀 **Мгновенный онбординг (Workspace-менеджер)**: Поделитесь одним YAML-файлом конфигурации с командой. При запуске оркестратор сам склонирует все недостающие репозитории и разложит их по правильным директориям, экономя часы новичкам.
- 👁️ **Глубокая интеграция с Jira**:
  - Оркестратор мониторит не только факт того, что ветка слита, но и **реальные статусы задач**.
  - Вы сразу поймете, что задача висит "В ревью", даже если Merge Request ещё никто не открыл.
  - Мгновенное выявление заброшенных или отмененных из-за смены приоритетов задач для очистки локального мусора.
  - **Multi-Jira**: Работа с несколькими Jira-инстансами одновременно (идеально для ситуаций, когда часть проектов "переезжает" на новые сервера).
- 🔀 **Удобная навигация и Checkout**: Смена активных веток одним нажатием `Enter`. Вам больше не нужно "прыгать" через терминалы и IDE по папкам.
- 🔎 **Сквозной поиск**: Глобальный поиск нужных веток и репозиториев прямо из TUI. Незаменимая фича, когда в рамках одной задачи вы модифицируете 5-7 разных репозиториев.
- 🗑️ **Генерация команд очистки**: Формирует пачки `git branch -D` для локальных веток и `git push <remote> --delete` для удаленных с учетом строгих правил безопасности.
- 🏷️ **Jira release-driven autocheck**:
  - По нажатию `*` во вкладке веток можно запустить автоматическую пометку кандидатов на удаление на основе статуса релиза в Jira.
  - Оркестратор предложит выбрать один из недавно выпущенных (Released) релизов, найдет все связанные с ним задачи и пометит соответствующие локальные ветки.
  - **Assistive-only**: Автоматика только предлагает выбор, окончательное решение всегда за пользователем (Manual override > Auto-mark).
  - **Session-only**: Выбор релиза сохраняется только в рамках текущей сессии.
  - **Resilience (release-autocheck only)**: В рамках Jira release-driven autocheck поддерживается bounded retry/backoff и заголовок `Retry-After` для обхода лимитов Jira API.
  - **No Silent Auto-run**: Проверка никогда не запускается автоматически при старте или обновлении.
- 🌐 **CDP & Playwright-мосты**: Дополнительный транспорт через Chromium для сложных Jira-групп с защитой (Cloudflare/Captchas).

---

## 🚀 Быстрый старт

**Требования:**

- Go `1.24+`
- Установленный `git` в `$PATH`

Существует два основных сценария работы с конфигурацией оркестратора:

### Сценарий 1: С готовым конфингом (Онбординг)

Идеально, если в вашей команде уже есть подготовленный файл конфигурации. Оркестратор автоматически скачает (через `git clone`) недостающие репозитории по URL в нужную структуру.

```bash
# 1. Скачиваем конфигурацию (в качестве примера возьмем базовый шаблон)
curl -O https://raw.githubusercontent.com/AgelxNash/go-repo-orchestrator/main/config.example.yaml

# 2. Запускаем оркестратор
go run ./cmd/go-repo-orchestrator --config config.example.yaml
```

### Сценарий 2: Генерация нового конфига

Если вы внедряете утилиту в свой проект с нуля, создайте чистый шаблон для заполнения:

```bash
# 1. Генерируем новый файл
go run ./cmd/go-repo-orchestrator generate --config ./my-repo.gbc.yaml

# 2. Отредактируйте my-repo.gbc.yaml, добавив свои пути и настройки.

# 3. Запускаем оркестратор
go run ./cmd/go-repo-orchestrator --config ./my-repo.gbc.yaml
```

Для просмотра полной справки по CLI:

```bash
go run ./cmd/go-repo-orchestrator --help
```

---

## 💻 Установка

**Из исходников:**

```bash
make build
./bin/go-repo-orchestrator --config ./config.example.yaml
```

**Через go install:**

```bash
go install github.com/agelxnash/go-repo-orchestrator/cmd/go-repo-orchestrator@latest
```

*Для скачивания готовых бинарных файлов посетите раздел [GitHub Releases](https://github.com/AgelxNash/go-repo-orchestrator/releases).*

---

## ⌨️ Горячие клавиши

#### Основные

- **`F2`** — Показать/скрыть нижнюю панель информации.
- **`F3`** — Поиск (Глобально по веткам и проектам).
- **`F4`** — Область веток (`Локальные` → `Удаленные` → `Все`).
- **`F5`** (или `r`) — Обновить контекст текущей вкладки.
- **`F6`** — Сортировка.
- **`F8`** (или `g`) — Генерация скрипта.
- **`F10`** (или `q` / `Ctrl+C`) — Выход из приложения.

#### Вкладка «Репозитории»

- **`Enter`**: Открыть ветки для активного репозитория.
- **`F7`**: Выполнить `fetch + pull` для активного репозитория.

#### Вкладка «Ветки»

- **`Enter`**: Сделать `checkout` на выбранную ветку без необходимости открывать терминал.
- **`Пробел`** / **`Insert`**: Отметить ветку для скрипта удаления.
- **`F7`**: Создать локальную tracking-копию удаленной ветки (если репозиторий не в режиме `url`-only).
- **`F9`**: Скрыть/показать защищенные ветки.
- **`*`**: Запуск Jira release-driven autocheck.
- **`+`**: Инверсия выбора веток (выбрать все невыбранные, снять выбор с выбранных).

---

## 🏷️ Jira release-driven autocheck

Функция позволяет быстро найти и пометить ветки, связанные с задачами, которые уже вошли в конкретный релиз Jira.

**Процесс работы:**
1. Нажмите **`*`** во вкладке веток.
2. Оркестратор загрузит список последних релизов (Released) из Jira.
3. Выберите нужный релиз в модальном окне.
4. Инструмент найдет все задачи в статусе "Done", привязанные к этому релизу.
5. Используя механизм `branch.jira` matching, оркестратор автоматически пометит соответствующие локальные ветки как кандидатов на удаление.

**Гарантии и ограничения:**
- **Assistive-only**: Это лишь подсказка. Вы всегда можете снять пометку вручную перед генерацией скрипта.
- **No Auto-delete**: Ветки не удаляются автоматически, только помечаются для включения в скрипт.
- **No Silent Auto-run**: Запросы к Jira API для проверки релизов выполняются только по явному нажатию `*`.
- **Session-only**: Выбранный релиз и результаты проверки хранятся только до закрытия приложения.
- **Resilience**: При получении ошибки 429 (Too Many Requests) оркестратор использует стратегию bounded retry/backoff с учетом заголовка `Retry-After`. Вы можете прервать ожидание в любой момент.

---

## ⚙️ Конфигурация

Все настройки по умолчанию и пример заполнения находятся в шаблоне `config.example.yaml`, который вы можете сгенерировать командой `generate` (см. Быстрый старт). Обязательным требованием является передача файла конфигурации через флаг `--config`.

**Ключевые секции конфига:**

- `repos[].name`, `repos[].url`, `repos[].path` — базовые настройки репозитория (поддержка `url`, `path`, `url+path`).
- `repos[].branch.keep` — regex-выражение для системных/защищенных веток.
- `repos[].branch.jira` — regex-выражение для извлечения Jira ключа (например, `[A-Z]+-\d+`).
- `jira[]` — настройки интеграции с Jira:
  - `group` — группа/проект в Jira (например, `"MYPROJ"`).
  - `url` — базовый URL Jira (например, `"https://company.atlassian.net"`).
  - `playwright` — булево: `true` включает браузерный транспорт через CDP/Chromium; `false` (по умолчанию) использует HTTP-транспорт.
  - `token` — API-токен для Bearer-аутентификации (если указан, добавляется заголовок `Authorization: Bearer <token>`).
  - `login.username` / `login.password` — учетные данные для Basic Auth (используются, если оба поля заданы и `token` пуст).
  - `type` — вспомогательное поле конфигурации, не влияющее на runtime-логику (может быть использовано для документирования или будущих расширений).
- `browser.cdp_url` — URL для подключения к уже запущенному Chromium через CDP (например, `"http://localhost:9222"`). Используется при `playwright: true`.

### Связь Jira-групп с ветками

Оркестратор поддерживает несколько Jira-инстансов одновременно. Чтобы определить, к какой Jira-группе относится та или иная ветка, используется механизм named capture groups в regex из `repos[].branch.jira`. Этот же механизм используется для автоматического сопоставления веток с задачами при выполнении `Jira release-driven autocheck`.

**Как это работает:**

- `jira[].group` — идентификатор Jira-группы/инстанса (например, `"MARIADB"`, `"SIMPLEWINE"`).
- В regex для `branch.jira` можно определить named-group, имя которого **совпадает** с `jira.group`. Когда ключ тикета извлекается из имени ветки, оркестратор находит соответствующую Jira-группу по имени named-group.
- Если в regex используется универсальное имя `(?P<JIRA>...)`, ключ тикета будет искаться во **всех** настроенных Jira-группах (fallback-вариант).

**Примеры:**

```yaml
# Прямой mapping: named-group "SIMPLEWINE" -> jira.group: "SIMPLEWINE"
jira:
  - group: "SIMPLEWINE"
    url: "https://simplewine.atlassian.net"
    token: "..."
repos:
  - name: "My Service"
    path: ./services/my-service
    branch:
      jira:
        - '(?P<SIMPLEWINE>SW-\d+)'  # Ключ "SW-123" найдет группу SIMPLEWINE
```

```yaml
# Fallback: универсальный named-group "JIRA" работает с любой группой
jira:
  - group: "PROJ-A"
    url: "https://proj-a.atlassian.net"
  - group: "PROJ-B"
    url: "https://proj-b.atlassian.net"
repos:
  - name: "Shared Lib"
    path: ./libs/shared
    branch:
      jira:
        - '(?P<JIRA>[A-Z]+-\d+)'  # Ключ "PROJ-123" проверит обе группы
```

**Почему `generate` не заполняет эти поля автоматически:**

Команда `generate` создает базовый шаблон конфигурации, но не может угадать:
- Какие Jira-инстансы использует ваша команда (URL, токены, групповые имена).
- Как именно вы именуете ветки (префиксы, разделители, форматы ключей).
- Какие named-group имена предпочтительны для вашего случая.

Эти параметры требуют ручной настройки под конкретную организацию и workflow.

*Можно переопределять конфигурацию через переменные окружения с префиксом `GBC_` (например, `GBC_STATE_DIR`).*

Для editor-autocomplete и ранней проверки структуры используйте JSON Schema: [`config.schema.json`](config.schema.json). Runtime-загрузчик также работает в strict-режиме: неизвестные YAML-ключи считаются ошибкой конфигурации, чтобы опечатки не игнорировались молча.

### Авторизация Jira

Интеграция с Jira поддерживает два транспортных режима, управляемых полем `playwright`:

- **Browser transport** (`playwright: true`) — для SSO/сложных сценариев с Cloudflare/Captchas. Пользователь авторизуется вручную в открываемом браузере/Chromium. После истечения SSO-сессии требуется повторный вход. Рекомендуется использовать `browser.cdp_url` для подключения к уже запущенному CDP-сеансу.
- **HTTP transport** (`playwright: false` или отсутствие поля) — стандартные HTTP-запросы к Jira REST API.

Для аутентификации в HTTP-режиме используются:
- **Bearer auth** — если задано поле `token`, добавляется заголовок `Authorization: Bearer <token>` (для Jira Server/DC это Personal Access Token).
- **Basic auth** — если заданы `login.username` и `login.password` (и `token` пуст), используется базовая аутентификация.

**mTLS и корпоративные CA (опциональный блок `ssl`):** для Jira Server/DC за mutual TLS или self-signed-прокси каждой группе можно задать клиентский сертификат и доверенный CA:

```yaml
jira:
  - group: "MYPROJ"
    url: "https://jira.corp.local"
    token: "..."                # PAT (Bearer) — комбинируется с mTLS
    ssl:
      client_cert: /etc/ssl/corp/jira-client.pem  # combined PEM (cert+key) или только сертификат
      # client_key: /etc/ssl/corp/jira.key        # отдельный файл ключа (если не combined)
      # client_key_password: "..."                # для традиционно зашифрованного PEM-ключа
      # ca_cert: /etc/ssl/corp/root-ca.pem        # корпоративный CA вместо системного trust store
      # verify: false                             # только для тестовых сред (по умолчанию true)
```

Сертификат участвует в TLS-handshake, а `token`/`login` — в HTTP-заголовках: любые комбинации разрешены. Ошибки загрузки сертификатов приводят к остановке приложения на старте с понятным сообщением. Browser transport (`playwright: true`) использует сертификаты системного хранилища браузера и блоком `ssl` не управляется.

Для запросов в рамках `Jira release-driven autocheck` оркестратор поддерживает стратегию bounded retry/backoff и корректно обрабатывает заголовок `Retry-After` при получении ошибки 429 (Too Many Requests).

Поле `type` является вспомогательным и не влияет на runtime-логику (может использоваться для документирования).

**Fallback и подсказки:** При недоступном browser runtime (например, отсутствует Chromium) для групп с `playwright: true` приложение автоматически делает HTTP fallback. Если для Jira требуется авторизация через браузер, TUI покажет соответствующую подсказку с инструкцией.

#### Примеры конфигурации

**SSO/Playwright (с CDP):**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    playwright: true

browser:
  cdp_url: "http://localhost:9222"
```

**HTTP с Bearer-токеном:**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    token: "your_api_token_here"
```

**HTTP с Basic Auth:**

```yaml
jira:
  - group: "MYPROJ"
    url: "https://company.atlassian.net"
    login:
      username: "user@example.com"
      password: "secret"
```

#### Как запустить браузер с CDP

При использовании `playwright: true` оркестратор подключается к уже запущенному браузеру через CDP. Запустите Chromium/Chrome в отдельном сеансе с включённым remote debugging:

**Linux/macOS:**

```bash
# Создаём отдельный профиль, чтобы не мешать основному браузеру
PROFILE_DIR="$HOME/.config/go-repo-orchestrator-chrome-profile"

# Запускаем Chrome/Chromium с CDP
google-chrome \
  --remote-debugging-port=9222 \
  --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$PROFILE_DIR" \
  --no-first-run \
  --no-default-browser-check \
  "https://company.atlassian.net" &
```

Или для `chromium`:

```bash
chromium-browser \
  --remote-debugging-port=9222 \
  --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$HOME/.config/go-repo-orchestrator-chrome-profile" \
  --no-first-run \
  --no-default-browser-check \
  "https://company.atlassian.net" &
```

**Windows (PowerShell):**

```powershell
# Отдельный профиль
$env:PROFILE_DIR = "$env:APPDATA\go-repo-orchestrator-chrome-profile"

# Запуск Chrome (путь может отличаться в зависимости от системы)
& "C:\Program Files\Google\Chrome\Application\chrome.exe" `
  --remote-debugging-port=9222 `
  --remote-debugging-address=127.0.0.1 `
  --user-data-dir="$env:PROFILE_DIR" `
  --no-first-run `
  --no-default-browser-check `
  "https://company.atlassian.net"
```

**Зачем отдельный `--user-data-dir`:** чтобы запущенный для CDP браузер не конфликтовал с вашим основным профилем Chrome/Chromium (расширения, история, куки). После завершения работы с оркестратором можно безопасно удалить эту папку.

Убедитесь, что `browser.cdp_url` в конфиге указывает на `http://localhost:9222` (значение по умолчанию).

### Каталог состояния

Утилита хранит свое состояние (и выкачанные workspace-репозитории) в:

- **Linux/macOS:** `$HOME/.local/state/go-repo-orchestrator`
- **Fallback:** `.go-repo-orchestrator-state`

Клонированные workspace хранятся по пути: `<state-dir>/workspace/<repo-name>__<url-hash>/`.

### О генерации скриптов очистки

Генерируемые `.sh`/`.bat` создаются в вашей текущей рабочей директории терминала в формате:

- `go-repo-orchestrator-<repo>-delete-<session>-<timestamp>.sh`

Важно понимать, что механизмы автоматической пометки (regex-autocheck и Jira release-driven autocheck) только формируют предварительный выбор кандидатов в TUI. Окончательное решение об удалении всегда остается за пользователем при запуске сгенерированного скрипта.

---

## 🤝 Contributing

Будем рады вашему вкладу! Для ознакомления с правилами *Conventional Commits*, требованиями к *Pull Requests* и обязательными проверками, пожалуйста, прочитайте [CONTRIBUTING.md](CONTRIBUTING.md).

<details>
<summary><b>🛠️ Подготовка окружения (Onboarding)</b></summary>

Быстрый onboarding перед первым коммитом:

```bash
make commitlint-install
make golangci-lint-install
make setup-hooks
```

*Команды установят необходимые утилиты (`commitlint`, `golangci-lint`) и пропишут настройки `core.hooksPath=.githooks` для локальных quality gates (`pre-commit` и `pre-push`).*

**Быстрые локальные проверки (manual call):**

```bash
make fmt-check
make vet
make check
```

</details>

<details>
<summary><b>📦 Информация о релизах (Для Maintainers)</b></summary>

Релизный workflow находится в `.github/workflows/release.yaml`.

- **Триггер:** push тега с префиксом `v*`.
- Используется `GoReleaser` + `GPG signing` checksum-файла.

**Проверка подписи скачанных артефактов в релизе:**

```bash
gpg --verify checksums.txt.sig checksums.txt
sha256sum -c checksums.txt
```

</details>

---

## 📈 Roadmap

- Введение полноценного i18n-слоя для CLI/TUI и пользовательских сообщений.
- Потенциальная опция автооткрытия IDE (VS Code / JetBrains) для выбранного репозитория и/или ветки из интерфейса.

---

## ⭐ Star History

<div align="center">
  <a href="https://www.star-history.com/?repos=AgelxNash%2Fgo-repo-orchestrator&type=date&legend=top-left">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&theme=dark&legend=top-left" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&legend=top-left" />
      <img alt="Star History Chart" src="https://api.star-history.com/image?repos=AgelxNash/go-repo-orchestrator&type=date&legend=top-left" />
    </picture>
  </a>
</div>

## 👥 Contributors

<div align="center">
  <a href="https://github.com/AgelxNash/go-repo-orchestrator/graphs/contributors">
    <img src="https://contrib.rocks/image?repo=AgelxNash/go-repo-orchestrator" alt="Contributors" />
  </a>
  <br/>
  <i>Made with <a href="https://contrib.rocks">contrib.rocks</a>.</i>
</div>

---

## 📄 License

**MIT** © [AgelxNash](https://github.com/AgelxNash)

<div align="center">
  <br/>
  <a href="https://github.com/AgelxNash/go-repo-orchestrator">
    <img src="https://github-view-counter.vercel.app/api?username=AgelxNash/go-repo-orchestrator&label=views&color=0969da&labelColor=555555" alt="Views"/>
  </a>
</div>
