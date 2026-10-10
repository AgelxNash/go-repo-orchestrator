package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// LoadRepoStat быстро (без загрузки веток) получает текущую ветку и статус (dirty/clean).
func (c *Cleaner) LoadRepoStat(ctx context.Context, repo config.RepoConfig) (model.RepoStat, error) {
	managedPath, syncWarning, err := c.resolveRepoForRead(ctx, repo)
	if err != nil {
		return model.RepoStat{}, err
	}

	stat, err := c.git.GetRepoStat(ctx, managedPath)
	if err != nil {
		return model.RepoStat{}, fmt.Errorf("получить статус репозитория: %w", err)
	}
	if syncWarning.Code != "" {
		stat.SyncWarning = syncWarning.Text()
		if stat.Warning.Code != model.RepoWarningEmptyClone {
			stat.Warning = syncWarning
		}
	}

	return stat, nil
}

const repoWarningRemoteSyncFailed = model.RepoWarningRemoteSyncFailed
const repoWarningEmptyClone = model.RepoWarningEmptyClone

// newRemoteSyncWarning собирает предупреждение о неудачной синхронизации remote.
func newRemoteSyncWarning(err error) model.RepoWarning {
	return model.RepoWarning{
		Code:    repoWarningRemoteSyncFailed,
		Message: fmt.Sprintf("синхронизация remote не выполнена: %v", err),
	}
}

func (c *Cleaner) resolveRepoForRead(ctx context.Context, repo config.RepoConfig) (string, model.RepoWarning, error) {
	var (
		managedPath string
		warning     model.RepoWarning
		err         error
	)

	switch repo.SourceType() {
	case "url":
		var cloneErr error
		managedPath, cloneErr = c.git.EnsureManagedClone(ctx, repo.Name, repo.URL)
		if cloneErr != nil {
			fallbackPath := c.git.ManagedRepoPath(repo.Name, repo.URL)
			fallbackCtx := context.WithoutCancel(ctx)
			if _, fallbackErr := c.git.ResolveRepoPath(fallbackCtx, repo.Name, "", fallbackPath); fallbackErr != nil {
				return "", model.RepoWarning{}, fmt.Errorf("подготовить репозиторий: %w", cloneErr)
			}

			managedPath = fallbackPath
			warning = newRemoteSyncWarning(cloneErr)
		}

	case "opensource":
		if updateErr := c.git.UpdateOpensourceRepo(ctx, repo.URL, repo.Path, repo.Branch.Autoswitch); updateErr != nil {
			localPath, fallbackErr := c.git.ResolveRepoPath(ctx, repo.Name, "", repo.Path)
			if fallbackErr != nil {
				return "", model.RepoWarning{}, fmt.Errorf("подготовить репозиторий: %w", updateErr)
			}

			managedPath = localPath
			warning = newRemoteSyncWarning(updateErr)
		} else {
			managedPath, err = c.git.ResolveRepoPath(ctx, repo.Name, "", repo.Path)
			if err != nil {
				return "", model.RepoWarning{}, fmt.Errorf("подготовить репозиторий: %w", err)
			}
		}

	default:
		managedPath, err = c.git.ResolveRepoPath(ctx, repo.Name, repo.URL, repo.Path)
		if err != nil {
			return "", model.RepoWarning{}, fmt.Errorf("подготовить репозиторий: %w", err)
		}
	}

	if repo.Upstream != "" && managedPath != "" {
		_ = c.git.EnsureRemote(ctx, managedPath, "upstream", repo.Upstream)
	}

	return managedPath, warning, nil
}

// ForceCheckoutLocalBranch проксирует вызов к git для принудительного переключения ветки
func (c *Cleaner) ForceCheckoutLocalBranch(ctx context.Context, repo config.RepoConfig, branch string) error {
	managedPath, err := c.git.ResolveRepoPath(ctx, repo.Name, repo.URL, repo.Path)
	if err != nil {
		return fmt.Errorf("подготовить репозиторий: %w", err)
	}

	return c.git.ForceCheckout(ctx, managedPath, branch)
}

// CreateLocalTrackingBranch создает локальную tracking-ветку из remote и переключается на нее.
func (c *Cleaner) CreateLocalTrackingBranch(ctx context.Context, repo config.RepoConfig, localBranch, remoteBranch string) error {
	managedPath, err := c.git.ResolveRepoPath(ctx, repo.Name, repo.URL, repo.Path)
	if err != nil {
		return fmt.Errorf("подготовить репозиторий: %w", err)
	}

	return c.git.CreateTrackingBranchAndCheckout(ctx, managedPath, localBranch, remoteBranch)
}

// FetchAndPullRepo выполняет безопасное обновление выбранного репозитория через fetch + pull.
func (c *Cleaner) FetchAndPullRepo(ctx context.Context, repo config.RepoConfig) error {
	var (
		repoPath string
		err      error
	)

	switch repo.SourceType() {
	case "url":
		repoPath, err = c.git.EnsureManagedClone(ctx, repo.Name, repo.URL)
		if err != nil {
			return fmt.Errorf("подготовить репозиторий: %w", err)
		}
	default:
		repoPath, err = c.git.ResolveRepoPath(ctx, repo.Name, "", repo.Path)
		if err != nil {
			return fmt.Errorf("подготовить репозиторий: %w", err)
		}
	}

	var upstreamErr error
	if repo.Upstream != "" {
		if err := c.git.EnsureRemote(ctx, repoPath, "upstream", repo.Upstream); err != nil {
			return fmt.Errorf("настроить remote upstream: %w", err)
		}
		if err := c.git.FetchRemote(ctx, repoPath, "upstream"); err != nil {
			upstreamErr = fmt.Errorf("fetch remote upstream: %w", err)
		}
	}

	pullErr := c.git.FetchAndPull(ctx, repoPath, repo.URL)
	if pullErr != nil {
		pullErr = fmt.Errorf("выполнить fetch и pull: %w", pullErr)
	}

	if upstreamErr != nil || pullErr != nil {
		return errors.Join(upstreamErr, pullErr)
	}

	return nil
}
