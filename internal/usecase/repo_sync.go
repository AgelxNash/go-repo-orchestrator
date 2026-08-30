package usecase

import (
	"context"
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
	stat.SyncWarning = syncWarning

	return stat, nil
}

func (c *Cleaner) resolveRepoForRead(ctx context.Context, repo config.RepoConfig) (string, string, error) {
	switch repo.SourceType() {
	case "url":
		managedPath, err := c.git.EnsureManagedClone(ctx, repo.Name, repo.URL)
		if err == nil {
			return managedPath, "", nil
		}

		fallbackPath := c.git.ManagedRepoPath(repo.Name, repo.URL)
		fallbackCtx := context.WithoutCancel(ctx)
		if _, fallbackErr := c.git.ResolveRepoPath(fallbackCtx, repo.Name, "", fallbackPath); fallbackErr != nil {
			return "", "", fmt.Errorf("подготовить репозиторий: %w", err)
		}

		return fallbackPath, fmt.Sprintf("синхронизация remote не выполнена: %v", err), nil

	case "opensource":
		if updateErr := c.git.UpdateOpensourceRepo(ctx, repo.URL, repo.Path, repo.Branch.Autoswitch); updateErr != nil {
			localPath, fallbackErr := c.git.ResolveRepoPath(ctx, repo.Name, "", repo.Path)
			if fallbackErr != nil {
				return "", "", fmt.Errorf("подготовить репозиторий: %w", updateErr)
			}

			return localPath, fmt.Sprintf("синхронизация remote не выполнена: %v", updateErr), nil
		}

		localPath, err := c.git.ResolveRepoPath(ctx, repo.Name, "", repo.Path)
		if err != nil {
			return "", "", fmt.Errorf("подготовить репозиторий: %w", err)
		}

		return localPath, "", nil

	default:
		managedPath, err := c.git.ResolveRepoPath(ctx, repo.Name, repo.URL, repo.Path)
		if err != nil {
			return "", "", fmt.Errorf("подготовить репозиторий: %w", err)
		}

		return managedPath, "", nil
	}
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

	if err := c.git.FetchAndPull(ctx, repoPath, repo.URL); err != nil {
		return fmt.Errorf("выполнить fetch и pull: %w", err)
	}

	return nil
}
