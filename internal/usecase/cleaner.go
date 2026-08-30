package usecase

import (
	"context"

	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"

	"go.uber.org/zap"
)

type gitClient interface {
	ResolveRepoPath(ctx context.Context, repoName, repoURL, localPath string) (string, error)
	ManagedRepoPath(repoName, repoURL string) string
	EnsureManagedClone(ctx context.Context, repoName, repoURL string) (string, error)
	FetchAndPull(ctx context.Context, repoPath, repoURL string) error
	DetectDefaultBranch(ctx context.Context, repoPath, currentBranch string) (string, error)
	ListBranches(ctx context.Context, repoPath string) ([]model.BranchInfo, error)
	CurrentBranch(ctx context.Context, repoPath string) (string, error)
	BranchMetadata(ctx context.Context, repoPath, branch, defaultBranch string) (model.MergeStatus, string, error)
	GetDirtyStats(ctx context.Context, repoPath string) (model.DirtyStats, error)
	GetRepoStat(ctx context.Context, repoPath string) (model.RepoStat, error)
	UpdateOpensourceRepo(ctx context.Context, url, targetPath, branch string) error
	ForceCheckout(ctx context.Context, repoPath, branch string) error
	CreateTrackingBranchAndCheckout(ctx context.Context, repoPath, localBranch, remoteBranch string) error
}

// Cleaner координирует загрузку веток и генерацию скрипта удаления.
type Cleaner struct {
	git  gitClient
	jira jira.StatusResolver
	rel  jira.ReleaseService
	log  *zap.Logger
}

type jiraStatusPrefetcher interface {
	PrefetchStatuses(requests []jira.StatusBatchRequest)
}

type jiraStatusPrefetcherWithProgress interface {
	PrefetchStatusesWithProgress(requests []jira.StatusBatchRequest, onProgress jira.PrefetchProgressCallback) []jira.PrefetchBatchProgress
}

// RepoLoadProgress описывает прогресс пакетной загрузки Jira-статусов для репозитория.
type RepoLoadProgress struct {
	BatchIndex int
	BatchTotal int
	BatchSize  int
	Processed  int
	Total      int
}

// RepoLoadSummary содержит агрегированную информацию о загрузке статусов Jira.
type RepoLoadSummary struct {
	JiraBatchProgress    []RepoLoadProgress
	JiraProgressStreamed bool
}

// RepoLoadProgressCallback вызывается при поступлении промежуточного прогресса загрузки.
type RepoLoadProgressCallback func(RepoLoadProgress)

type CleanerOption func(*Cleaner)

// NewCleaner собирает основной use case очистки веток.
func NewCleaner(git gitClient, opts ...CleanerOption) *Cleaner {
	cleaner := &Cleaner{
		git:  git,
		jira: jira.NewNoop(),
		rel:  jira.NewNoop(),
		log:  zap.NewNop(),
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(cleaner)
	}

	if cleaner.jira == nil {
		cleaner.jira = jira.NewNoop()
	}
	if cleaner.rel == nil {
		cleaner.rel = jira.NewNoop()
	}
	if cleaner.log == nil {
		cleaner.log = zap.NewNop()
	}

	return cleaner
}

func WithJiraStatusResolver(resolver jira.StatusResolver) CleanerOption {
	return func(cleaner *Cleaner) {
		cleaner.jira = resolver
	}
}

func WithJiraReleaseService(service jira.ReleaseService) CleanerOption {
	return func(cleaner *Cleaner) {
		cleaner.rel = service
	}
}

func WithLogger(logger *zap.Logger) CleanerOption {
	return func(cleaner *Cleaner) {
		cleaner.log = logger
	}
}
