package tui

import (
	"context"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

type cleanerPort interface {
	LoadRepoBranchesWithProgress(ctx context.Context, repo config.RepoConfig, onProgress usecase.RepoLoadProgressCallback) (model.RepoBranches, usecase.RepoLoadSummary, error)
	LoadRepoStat(ctx context.Context, repo config.RepoConfig) (model.RepoStat, error)
	GenerateDeleteScript(repo config.RepoConfig, repoPath string, branches []model.BranchInfo, format model.ScriptFormat) (model.ScriptResult, error)
	ForceCheckoutLocalBranch(ctx context.Context, repo config.RepoConfig, branch string) error
	CreateLocalTrackingBranch(ctx context.Context, repo config.RepoConfig, localBranch, remoteBranch string) error
	FetchAndPullRepo(ctx context.Context, repo config.RepoConfig) error
	ListRepoReleasedFixVersions(ctx context.Context, repo config.RepoConfig, branches []model.BranchInfo) ([]usecase.RepoRelease, error)
	BuildReleaseAutocheckCandidates(ctx context.Context, repo config.RepoConfig, branches []model.BranchInfo, group, releaseID string) (usecase.ReleaseAutocheckResult, []model.BranchInfo, error)
}
