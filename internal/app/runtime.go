package app

import (
	"fmt"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/browser"
	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/git"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"

	"go.uber.org/zap"
)

// Runtime объединяет рабочие зависимости приложения.
type Runtime struct {
	Git        *git.Client
	Cleaner    *usecase.Cleaner
	Playwright *browser.PlaywrightRuntime
}

// jiraHTTPTimeout — таймаут HTTP-запросов к Jira (общий и per-group mTLS-клиенты).
const jiraHTTPTimeout = 5 * time.Second

// NewRuntime собирает зависимости use case слоя.
func NewRuntime(stateDir, workspaceDir string, gitTimeout time.Duration, browserCDPURL string, jiraGroups []config.JiraConfig, logger *zap.Logger) (*Runtime, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	gitClient := git.NewClient(gitTimeout, workspaceDir)
	playwrightRuntime := browser.NewPlaywrightRuntime(
		browserCDPURL,
		browser.WithStateDir(stateDir),
		browser.WithLogger(logger),
	)
	groupConfigs, err := jira.WithGroupConfigs(jiraHTTPTimeout, jiraGroups)
	if err != nil {
		return nil, fmt.Errorf("настроить jira-подключения: %w", err)
	}
	statusService := jira.NewStatusService(
		jiraHTTPTimeout,
		groupConfigs,
		jira.WithBrowserRuntime(playwrightRuntime),
		jira.WithLogger(logger),
	)
	cleaner := usecase.NewCleaner(
		gitClient,
		usecase.WithJiraStatusResolver(statusService),
		usecase.WithJiraReleaseService(statusService),
		usecase.WithLogger(logger),
	)

	return &Runtime{
		Git:        gitClient,
		Cleaner:    cleaner,
		Playwright: playwrightRuntime,
	}, nil
}

func (r *Runtime) StartPlaywright() error {
	if r == nil || r.Playwright == nil {
		return nil
	}

	return r.Playwright.Start()
}

func (r *Runtime) Close() error {
	if r == nil || r.Playwright == nil {
		return nil
	}

	return r.Playwright.Close()
}

// NewRuntimeFromOptions собирает runtime с безопасными значениями по умолчанию.
func NewRuntimeFromOptions(opts RuntimeOptions) (*Runtime, error) {
	if opts.StateDir == "" {
		opts.StateDir = DefaultStateDir()
	}

	if opts.GitTimeout <= 0 {
		opts.GitTimeout = DefaultGitTimeout
	}

	if opts.WorkspaceDir == "" {
		opts.WorkspaceDir = DefaultWorkspaceDir(opts.StateDir)
	}

	return NewRuntime(opts.StateDir, opts.WorkspaceDir, opts.GitTimeout, opts.BrowserCDPURL, opts.JiraGroups, opts.Logger)
}
