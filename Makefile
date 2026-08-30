GO_DIRS := ./cmd ./internal
BIN_DIR := ./bin
BIN_NAME := go-repo-orchestrator
MAIN_PKG := ./cmd/go-repo-orchestrator
BIN_PATH := $(BIN_DIR)/$(BIN_NAME)
GO_LIST_PATTERNS := ./cmd/... ./internal/... $(if $(wildcard ./pkg),./pkg/...)
GO_PACKAGES := $(shell go list $(GO_LIST_PATTERNS))
GOLANGCI_LINT := $(or $(shell command -v golangci-lint 2>/dev/null),$(shell go env GOPATH)/bin/golangci-lint)
GORELEASER := $(or $(shell command -v goreleaser 2>/dev/null),$(shell go env GOPATH)/bin/goreleaser)
COMMITLINT := $(or $(shell command -v commitlint 2>/dev/null),$(shell go env GOPATH)/bin/commitlint)

.PHONY: test lint build check fmt-check vet test-race coverage vulncheck

test:
	go test $(GO_PACKAGES)

test-race:
	go test -race $(GO_PACKAGES)

coverage:
	mkdir -p tmp
	go test -coverprofile=tmp/coverage.out $(GO_PACKAGES)
	go tool cover -func=tmp/coverage.out
	go tool cover -html=tmp/coverage.out -o tmp/coverage.html
	@echo "Coverage report: tmp/coverage.html"

lint:
	@test -x "$(GOLANGCI_LINT)" || { \
		echo "golangci-lint не найден. Установите его локально или запустите lint в CI."; \
		exit 1; \
	}
	"$(GOLANGCI_LINT)" run --timeout=5m $(GO_LIST_PATTERNS)

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BIN_PATH) $(MAIN_PKG)

fmt-check:
	@unformatted="$$(gofmt -l $(GO_DIRS))"; \
	if [ -n "$$unformatted" ]; then \
		echo "Found unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet $(GO_PACKAGES)

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: fmt-check test vet build lint

.PHONY: release-snapshot release-check goreleaser-install release-dry release-tag commitlint-install golangci-lint-install

commitlint-install:
	@test -x "$(COMMITLINT)" || go install github.com/conventionalcommit/commitlint@latest

golangci-lint-install:
	@test -x "$(GOLANGCI_LINT)" || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

goreleaser-install:
	@test -x "$(GORELEASER)" || go install github.com/goreleaser/goreleaser/v2@latest

release-check: goreleaser-install
	"$(GORELEASER)" check

release-snapshot: goreleaser-install
	"$(GORELEASER)" release --snapshot --clean --skip=publish

release-dry: goreleaser-install
	"$(GORELEASER)" release --clean --skip=publish

release-tag:
	@set -eu; \
	version='$(strip $(VERSION))'; \
	message='$(subst ','\'',$(strip $(MESSAGE)))'; \
	if [ -z "$$version" ]; then \
		echo "VERSION is required."; \
		echo "Usage: make release-tag VERSION=v0.1.0 MESSAGE=\"First public release\""; \
		exit 1; \
	fi; \
	if [ -z "$$message" ]; then \
		echo "MESSAGE is required."; \
		echo "Usage: make release-tag VERSION=v0.1.0 MESSAGE=\"First public release\""; \
		exit 1; \
	fi; \
	if git rev-parse --verify --quiet "refs/tags/$$version" >/dev/null; then \
		echo "Tag '$$version' already exists. Choose another VERSION or delete existing tag."; \
		exit 1; \
	fi; \
	msg_file=$$(mktemp); \
	trap 'rm -f "$$msg_file"' EXIT INT TERM; \
	printf '%s\n' "$$message" > "$$msg_file"; \
	git tag -a "$$version" -F "$$msg_file"; \
	git push origin "$$version"

.PHONY: setup-hooks

setup-hooks: commitlint-install golangci-lint-install
	git config core.hooksPath .githooks
	chmod +x .githooks/commit-msg
	chmod +x .githooks/pre-commit
	chmod +x .githooks/pre-push
