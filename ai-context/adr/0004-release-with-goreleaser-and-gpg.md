# 0004. Выпускать релизы через GoReleaser с GPG-подписанными checksums

- **Status:** accepted
- **Date:** 2026-08-30

## Context

Проект распространяется как Go CLI/TUI-утилита для Linux, macOS и Windows. Пользователям нужны готовые бинарные артефакты, повторяемая сборка и возможность проверить целостность скачанных файлов.

Альтернативы: ручная сборка архивов, `go install` как единственный способ установки, неподписанные checksums.

## Decision

Использовать GoReleaser для tag-driven релизов:

- релиз запускается по тегам `v*`;
- сборки выполняются для Linux/macOS/Windows и amd64/arm64/386 (кроме darwin/386);
- архивы содержат бинарь, README и LICENSE;
- генерируется `checksums.txt` с SHA-256;
- checksum-файл подписывается GPG-ключом через GitHub Actions secrets;
- release workflow выполняет preflight: `go test`, `go vet`, `goreleaser check`, проверка наличия и fingerprint GPG-ключа.

## Consequences

Плюсы:

- релизы повторяемы и автоматизированы;
- пользователи могут проверять `checksums.txt.sig` и SHA-256;
- release notes генерируются из GitHub changelog/commits;
- ручной риск ошибок в архивах ниже.

Минусы:

- релиз зависит от корректной настройки GitHub Actions secrets;
- GPG lifecycle (ротация/отзыв ключа) нужно сопровождать отдельно;
- workflow должен поддерживаться при обновлениях GoReleaser и GitHub Actions.

Дополнение: проект ведёт `CHANGELOG.md` для human-readable истории заметных изменений, а GitHub Releases остаются источником подробных release notes.
