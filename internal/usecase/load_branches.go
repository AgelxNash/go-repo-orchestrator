package usecase

import (
	"context"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// LoadRepoBranches загружает ветки и помечает их по правилам доступности к удалению.
func (c *Cleaner) LoadRepoBranches(ctx context.Context, repo config.RepoConfig) (model.RepoBranches, error) {
	rb, _, err := c.loadRepoBranchesDetailed(ctx, repo, nil)
	return rb, err
}

func (c *Cleaner) LoadRepoBranchesWithSummary(ctx context.Context, repo config.RepoConfig) (model.RepoBranches, RepoLoadSummary, error) {
	return c.loadRepoBranchesDetailed(ctx, repo, nil)
}

func (c *Cleaner) LoadRepoBranchesWithProgress(ctx context.Context, repo config.RepoConfig, onProgress RepoLoadProgressCallback) (model.RepoBranches, RepoLoadSummary, error) {
	return c.loadRepoBranchesDetailed(ctx, repo, onProgress)
}

func (c *Cleaner) loadRepoBranchesDetailed(ctx context.Context, repo config.RepoConfig, onProgress RepoLoadProgressCallback) (model.RepoBranches, RepoLoadSummary, error) {
	managedPath, syncWarning, err := c.resolveRepoForRead(ctx, repo)
	if err != nil {
		return model.RepoBranches{}, RepoLoadSummary{}, err
	}

	allBranches, err := c.git.ListBranches(ctx, managedPath)
	if err != nil {
		return model.RepoBranches{}, RepoLoadSummary{}, err
	}

	currentBranch, err := c.git.CurrentBranch(ctx, managedPath)
	if err != nil {
		return model.RepoBranches{}, RepoLoadSummary{}, err
	}

	defaultBranch, err := c.git.DetectDefaultBranch(ctx, managedPath, currentBranch)
	if err != nil {
		return model.RepoBranches{}, RepoLoadSummary{}, err
	}

	dirtyStats, err := c.git.GetDirtyStats(ctx, managedPath)
	if err != nil {
		dirtyStats = model.DirtyStats{}
	}

	filtered := make([]model.BranchInfo, 0, len(allBranches))
	mappingStats := newJiraMappingStats()
	requests := collectJiraStatusRequests(repo, allBranches)
	loadSummary := RepoLoadSummary{}
	if prefetcher, ok := c.jira.(jiraStatusPrefetcherWithProgress); ok {
		batchProgress := prefetcher.PrefetchStatusesWithProgress(requests, func(item jira.PrefetchBatchProgress) {
			if onProgress == nil {
				return
			}
			onProgress(RepoLoadProgress{
				BatchIndex: item.BatchIndex,
				BatchTotal: item.BatchTotal,
				BatchSize:  item.BatchSize,
				Processed:  item.Processed,
				Total:      item.Total,
			})
		})
		if len(batchProgress) > 0 {
			loadSummary.JiraProgressStreamed = onProgress != nil
			loadSummary.JiraBatchProgress = make([]RepoLoadProgress, 0, len(batchProgress))
			for _, item := range batchProgress {
				progressItem := RepoLoadProgress{
					BatchIndex: item.BatchIndex,
					BatchTotal: item.BatchTotal,
					BatchSize:  item.BatchSize,
					Processed:  item.Processed,
					Total:      item.Total,
				}
				loadSummary.JiraBatchProgress = append(loadSummary.JiraBatchProgress, progressItem)
			}
		}
	} else if prefetcher, ok := c.jira.(jiraStatusPrefetcher); ok {
		prefetcher.PrefetchStatuses(requests)
	}

	for _, branch := range allBranches {
		allowed, reason := evaluateBranchProtection(repo, branch, currentBranch, defaultBranch)
		jiraMatch, ok, jiraDiag := repo.ExtractJiraMatchDetailed(branch.Name)
		mappingStats.add(jiraDiag)
		if ok {
			branch.JiraKey = jiraMatch.Key
			branch.JiraGroup = valueOrDash(jiraMatch.Group)
			branch.JiraURL = valueOrDash(jiraMatch.URL)
			branch.JiraTicketURL = valueOrDash(jiraMatch.TicketURL)
			if jiraMatch.Group != "" && jiraMatch.TicketURL != "" {
				result := c.jira.ResolveStatus(jiraMatch.Group, jiraMatch.TicketURL, jiraMatch.URL, jiraMatch.Key)
				branch.JiraStatus = valueOrDash(result.StatusOrDash())
				branch.JiraState = mapJiraStatusState(result.State)
				branch.JiraReason = mapJiraStatusReason(result.Reason)
			} else {
				branch.JiraStatus = "-"
				branch.JiraState = model.JiraStatusStateUnmapped
				switch jiraDiag.Reason {
				case config.JiraMatchReasonNamedGroupNoGroup:
					branch.JiraReason = model.JiraStatusReasonNoGroupConfig
				case config.JiraMatchReasonFallbackJIRA, config.JiraMatchReasonFallbackFullMatch:
					branch.JiraReason = model.JiraStatusReasonRegexKeyOnly
				default:
					branch.JiraReason = model.JiraStatusReasonNoMapping
				}
			}
		} else {
			branch.JiraKey = "-"
			branch.JiraGroup = "-"
			branch.JiraURL = "-"
			branch.JiraTicketURL = "-"
			branch.JiraStatus = "-"
			branch.JiraState = model.JiraStatusStateUnmapped
			branch.JiraReason = model.JiraStatusReasonNoRegexMatch
		}

		mergeStatus, baseBranch, metaErr := c.git.BranchMetadata(ctx, managedPath, branch.QualifiedName, defaultBranch)
		if metaErr != nil {
			mergeStatus = model.MergeStatusUnknown
			baseBranch = "-"
		}
		if branch.IsRemote() {
			baseBranch = "-"
		}
		branch.MergeStatus = mergeStatus
		branch.BaseBranch = baseBranch
		branch.Protected = !allowed
		branch.Reason = reason
		branch.Autocheck = !branch.Protected && branch.Name != currentBranch && repo.MatchesAutocheck(branch.Name)
		filtered = append(filtered, branch)
	}

	c.logJiraMappingSummary(repo.Name, mappingStats)

	return model.RepoBranches{
		RepoName:      repo.Name,
		RepoURL:       repo.URL,
		RepoSource:    repo.SourceType(),
		RepoPath:      managedPath,
		SyncWarning:   syncWarning,
		DefaultBranch: defaultBranch,
		CurrentBranch: currentBranch,
		DirtyStats:    dirtyStats,
		Branches:      filtered,
	}, loadSummary, nil
}
