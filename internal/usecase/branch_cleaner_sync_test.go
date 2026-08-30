package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

type fakeGitClient struct {
	resolveRepoPathFn     func(ctx context.Context, repoName, repoURL, localPath string) (string, error)
	managedRepoPathFn     func(repoName, repoURL string) string
	ensureManagedCloneFn  func(ctx context.Context, repoName, repoURL string) (string, error)
	fetchAndPullFn        func(ctx context.Context, repoPath, repoURL string) error
	detectDefaultBranchFn func(ctx context.Context, repoPath, currentBranch string) (string, error)
	listBranchesFn        func(ctx context.Context, repoPath string) ([]model.BranchInfo, error)
	currentBranchFn       func(ctx context.Context, repoPath string) (string, error)
	branchMetadataFn      func(ctx context.Context, repoPath, branch, defaultBranch string) (model.MergeStatus, string, error)
	getDirtyStatsFn       func(ctx context.Context, repoPath string) (model.DirtyStats, error)
	getRepoStatFn         func(ctx context.Context, repoPath string) (model.RepoStat, error)
	updateOpensourceFn    func(ctx context.Context, url, targetPath, branch string) error
	createTrackingFn      func(ctx context.Context, repoPath, localBranch, remoteBranch string) error
}

type fakeStatusResolver struct {
	resolveFn          func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult
	prefetchFn         func(ctx context.Context, requests []jira.StatusBatchRequest)
	prefetchProgressFn func(ctx context.Context, requests []jira.StatusBatchRequest, onProgress jira.PrefetchProgressCallback) []jira.PrefetchBatchProgress
}

func (f fakeStatusResolver) ResolveStatus(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
	if f.resolveFn != nil {
		return f.resolveFn(group, ticketURL, jiraBaseURL, key)
	}

	return jira.StatusResult{Status: "-", State: jira.StatusStateUnmapped, Reason: jira.StatusReasonNoMapping}
}

func (f fakeStatusResolver) PrefetchStatuses(ctx context.Context, requests []jira.StatusBatchRequest) {
	if f.prefetchFn != nil {
		f.prefetchFn(ctx, requests)
	}
}

func (f fakeStatusResolver) PrefetchStatusesWithProgress(ctx context.Context, requests []jira.StatusBatchRequest, onProgress jira.PrefetchProgressCallback) []jira.PrefetchBatchProgress {
	if f.prefetchProgressFn != nil {
		return f.prefetchProgressFn(ctx, requests, onProgress)
	}

	if f.prefetchFn != nil {
		f.prefetchFn(ctx, requests)
	}

	return nil
}

func (f *fakeGitClient) ResolveRepoPath(ctx context.Context, repoName, repoURL, localPath string) (string, error) {
	if f.resolveRepoPathFn != nil {
		return f.resolveRepoPathFn(ctx, repoName, repoURL, localPath)
	}
	return "", nil
}

func (f *fakeGitClient) ManagedRepoPath(repoName, repoURL string) string {
	if f.managedRepoPathFn != nil {
		return f.managedRepoPathFn(repoName, repoURL)
	}
	return ""
}

func (f *fakeGitClient) EnsureManagedClone(ctx context.Context, repoName, repoURL string) (string, error) {
	if f.ensureManagedCloneFn != nil {
		return f.ensureManagedCloneFn(ctx, repoName, repoURL)
	}
	return "", nil
}

func (f *fakeGitClient) FetchAndPull(ctx context.Context, repoPath, repoURL string) error {
	if f.fetchAndPullFn != nil {
		return f.fetchAndPullFn(ctx, repoPath, repoURL)
	}
	return nil
}

func (f *fakeGitClient) DetectDefaultBranch(ctx context.Context, repoPath, currentBranch string) (string, error) {
	if f.detectDefaultBranchFn != nil {
		return f.detectDefaultBranchFn(ctx, repoPath, currentBranch)
	}
	return "", nil
}

func (f *fakeGitClient) ListBranches(ctx context.Context, repoPath string) ([]model.BranchInfo, error) {
	if f.listBranchesFn != nil {
		return f.listBranchesFn(ctx, repoPath)
	}
	return nil, nil
}

func (f *fakeGitClient) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	if f.currentBranchFn != nil {
		return f.currentBranchFn(ctx, repoPath)
	}
	return "", nil
}

func (f *fakeGitClient) BranchMetadata(ctx context.Context, repoPath, branch, defaultBranch string) (model.MergeStatus, string, error) {
	if f.branchMetadataFn != nil {
		return f.branchMetadataFn(ctx, repoPath, branch, defaultBranch)
	}
	return model.MergeStatusUnknown, "-", nil
}

func (f *fakeGitClient) GetDirtyStats(ctx context.Context, repoPath string) (model.DirtyStats, error) {
	if f.getDirtyStatsFn != nil {
		return f.getDirtyStatsFn(ctx, repoPath)
	}
	return model.DirtyStats{}, nil
}

func (f *fakeGitClient) GetRepoStat(ctx context.Context, repoPath string) (model.RepoStat, error) {
	if f.getRepoStatFn != nil {
		return f.getRepoStatFn(ctx, repoPath)
	}
	return model.RepoStat{}, nil
}

func (f *fakeGitClient) UpdateOpensourceRepo(ctx context.Context, url, targetPath, branch string) error {
	if f.updateOpensourceFn != nil {
		return f.updateOpensourceFn(ctx, url, targetPath, branch)
	}
	return nil
}

func (f *fakeGitClient) ForceCheckout(_ context.Context, repoPath, branch string) error {
	return nil
}

func (f *fakeGitClient) CreateTrackingBranchAndCheckout(ctx context.Context, repoPath, localBranch, remoteBranch string) error {
	if f.createTrackingFn != nil {
		return f.createTrackingFn(ctx, repoPath, localBranch, remoteBranch)
	}
	return nil
}

func TestLoadRepoBranchesOpensourceClonesMissingPathViaUpdateFlow(t *testing.T) {
	t.Parallel()

	updateCalled := false
	resolveCalled := false

	git := &fakeGitClient{
		updateOpensourceFn: func(_ context.Context, url, targetPath, branch string) error {
			updateCalled = true
			if url != "ssh://git.example/repo.git" {
				t.Fatalf("unexpected opensource url: %q", url)
			}
			if targetPath != "/tmp/bff" {
				t.Fatalf("unexpected opensource path: %q", targetPath)
			}
			if branch != "develop" {
				t.Fatalf("unexpected autoswitch branch: %q", branch)
			}
			return nil
		},
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			resolveCalled = true
			if repoURL != "" {
				t.Fatalf("unexpected repoURL in ResolveRepoPath: %q", repoURL)
			}
			return localPath, nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/local-bff",
				QualifiedName: "feature/local-bff",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{
		Name: "bff",
		URL:  "ssh://git.example/repo.git",
		Path: "/tmp/bff",
		Branch: config.Branch{
			Autoswitch: "develop",
		},
	}

	rb, err := cleaner.LoadRepoBranches(t.Context(), repo)
	if err != nil {
		t.Fatalf("expected opensource update success, got error: %v", err)
	}
	if !updateCalled {
		t.Fatal("expected opensource update to be called")
	}
	if !resolveCalled {
		t.Fatal("expected path resolution after opensource update")
	}
	if rb.RepoPath != repo.Path {
		t.Fatalf("expected local repo path %q, got %q", repo.Path, rb.RepoPath)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected local branches to be loaded, got %d", len(rb.Branches))
	}
	if rb.Branches[0].Name != "feature/local-bff" {
		t.Fatalf("unexpected branch loaded: %q", rb.Branches[0].Name)
	}
	if rb.SyncWarning != "" {
		t.Fatalf("expected no sync warning after successful opensource update, got %q", rb.SyncWarning)
	}
	if rb.DefaultBranch != "main" {
		t.Fatalf("expected default branch main, got %q", rb.DefaultBranch)
	}
}

func TestLoadRepoBranchesOpensourceKeepsLocalDataAndReturnsSyncWarning(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		updateOpensourceFn: func(_ context.Context, url, targetPath, branch string) error {
			return errors.New("connection refused")
		},
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			if repoURL != "" {
				t.Fatalf("unexpected repoURL in ResolveRepoPath: %q", repoURL)
			}
			return localPath, nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/local-bff",
				QualifiedName: "feature/local-bff",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "bff", URL: "ssh://git.example/repo.git", Path: "/tmp/bff"}

	rb, err := cleaner.LoadRepoBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected local fallback with warning, got error: %v", err)
	}
	if rb.RepoPath != repo.Path {
		t.Fatalf("expected local repo path %q, got %q", repo.Path, rb.RepoPath)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected local branches to be loaded, got %d", len(rb.Branches))
	}
	if rb.Branches[0].Name != "feature/local-bff" {
		t.Fatalf("unexpected branch loaded: %q", rb.Branches[0].Name)
	}
	if rb.SyncWarning == "" {
		t.Fatal("expected sync warning for opensource repo")
	}
	if !strings.Contains(rb.SyncWarning, "синхронизация remote не выполнена") {
		t.Fatalf("unexpected warning: %q", rb.SyncWarning)
	}
}

func TestLoadRepoBranchesOpensourceReturnsErrorWhenUpdateFailsAndNoLocalRepo(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		updateOpensourceFn: func(_ context.Context, url, targetPath, branch string) error {
			return errors.New("connection refused")
		},
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "", errors.New("local path is not a git repository")
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "bff", URL: "ssh://git.example/repo.git", Path: "/tmp/missing"}

	_, err := cleaner.LoadRepoBranches(context.Background(), repo)
	if err == nil {
		t.Fatal("expected hard error when opensource update fails and fallback repo is unavailable")
	}
	if !strings.Contains(err.Error(), "подготовить репозиторий") {
		t.Fatalf("expected prepare repository error wrapper, got: %v", err)
	}
}

func TestLoadRepoStatURLFallsBackToManagedCacheOnSyncFailure(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		ensureManagedCloneFn: func(_ context.Context, repoName, repoURL string) (string, error) {
			return "", errors.New("no route to host")
		},
		managedRepoPathFn: func(repoName, repoURL string) string {
			return "/tmp/state/workspace/repo"
		},
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			if localPath == "" {
				t.Fatal("expected fallback local path")
			}
			return localPath, nil
		},
		getRepoStatFn: func(_ context.Context, repoPath string) (model.RepoStat, error) {
			return model.RepoStat{CurrentBranch: "main", Loaded: true}, nil
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "simplewine", URL: "ssh://git.example/simplewine.git"}

	stat, err := cleaner.LoadRepoStat(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected local managed fallback, got error: %v", err)
	}
	if stat.SyncWarning == "" {
		t.Fatal("expected sync warning when managed sync fails but cache is available")
	}
	if !strings.Contains(stat.SyncWarning, "синхронизация remote не выполнена") {
		t.Fatalf("unexpected warning: %q", stat.SyncWarning)
	}
}

func TestLoadRepoStatURLReturnsHardErrorWhenNoLocalCache(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		ensureManagedCloneFn: func(_ context.Context, repoName, repoURL string) (string, error) {
			return "", errors.New("connection refused")
		},
		managedRepoPathFn: func(repoName, repoURL string) string {
			return "/tmp/state/workspace/missing"
		},
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "", errors.New("local path is not a git repository")
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "simplewine", URL: "ssh://git.example/simplewine.git"}

	_, err := cleaner.LoadRepoStat(context.Background(), repo)
	if err == nil {
		t.Fatal("expected hard error when both sync and local cache are unavailable")
	}
	if !strings.Contains(err.Error(), "подготовить репозиторий") {
		t.Fatalf("expected prepare repository error wrapper, got: %v", err)
	}
}

func TestResolveRepoForReadURLFallbackIgnoresParentCancelForLocalCacheCheck(t *testing.T) {
	t.Parallel()

	resolveCalled := false
	git := &fakeGitClient{
		ensureManagedCloneFn: func(_ context.Context, repoName, repoURL string) (string, error) {
			return "", errors.New("operation timed out")
		},
		managedRepoPathFn: func(repoName, repoURL string) string {
			return "/tmp/state/workspace/repo"
		},
		resolveRepoPathFn: func(ctx context.Context, repoName, repoURL, localPath string) (string, error) {
			resolveCalled = true
			if ctx.Err() != nil {
				t.Fatalf("expected fallback cache check without canceled context, got: %v", ctx.Err())
			}
			return localPath, nil
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "demo", URL: "ssh://git.example/demo.git"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repoPath, syncWarning, err := cleaner.resolveRepoForRead(ctx, repo)
	if err != nil {
		t.Fatalf("expected fallback to local cache, got error: %v", err)
	}
	if !resolveCalled {
		t.Fatal("expected local cache path resolution to be called")
	}
	if repoPath != "/tmp/state/workspace/repo" {
		t.Fatalf("unexpected fallback path: %q", repoPath)
	}
	if syncWarning.Message == "" {
		t.Fatal("expected sync warning when remote sync fails")
	}
}

func TestFetchAndPullRepoPathSourceUsesLocalPath(t *testing.T) {
	t.Parallel()

	called := false
	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			if repoName != "demo" {
				t.Fatalf("unexpected repo name: %q", repoName)
			}
			if repoURL != "" {
				t.Fatalf("unexpected repoURL for path source: %q", repoURL)
			}
			if localPath != "/tmp/demo" {
				t.Fatalf("unexpected local path: %q", localPath)
			}
			return localPath, nil
		},
		fetchAndPullFn: func(_ context.Context, repoPath, repoURL string) error {
			called = true
			if repoPath != "/tmp/demo" {
				t.Fatalf("unexpected repo path: %q", repoPath)
			}
			if repoURL != "" {
				t.Fatalf("unexpected repo URL for path source: %q", repoURL)
			}
			return nil
		},
	}

	cleaner := NewCleaner(git)
	err := cleaner.FetchAndPullRepo(context.Background(), config.RepoConfig{Name: "demo", Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected fetch+pull call")
	}
}

func TestFetchAndPullRepoURLSourceUsesManagedClone(t *testing.T) {
	t.Parallel()

	called := false
	git := &fakeGitClient{
		ensureManagedCloneFn: func(_ context.Context, repoName, repoURL string) (string, error) {
			if repoName != "demo" {
				t.Fatalf("unexpected repo name: %q", repoName)
			}
			if repoURL != "ssh://git.example/demo.git" {
				t.Fatalf("unexpected repo URL: %q", repoURL)
			}
			return "/tmp/state/workspace/demo", nil
		},
		fetchAndPullFn: func(_ context.Context, repoPath, repoURL string) error {
			called = true
			if repoPath != "/tmp/state/workspace/demo" {
				t.Fatalf("unexpected repo path: %q", repoPath)
			}
			if repoURL != "ssh://git.example/demo.git" {
				t.Fatalf("unexpected repo URL: %q", repoURL)
			}
			return nil
		},
	}

	cleaner := NewCleaner(git)
	err := cleaner.FetchAndPullRepo(context.Background(), config.RepoConfig{Name: "demo", URL: "ssh://git.example/demo.git"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected fetch+pull call")
	}
}

func TestCreateLocalTrackingBranchResolvesRepoAndCallsGit(t *testing.T) {
	t.Parallel()

	called := false
	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			if repoName != "demo" {
				t.Fatalf("unexpected repo name: %q", repoName)
			}
			return "/tmp/demo", nil
		},
		createTrackingFn: func(_ context.Context, repoPath, localBranch, remoteBranch string) error {
			called = true
			if repoPath != "/tmp/demo" {
				t.Fatalf("unexpected repoPath: %q", repoPath)
			}
			if localBranch != "feature/new" {
				t.Fatalf("unexpected localBranch: %q", localBranch)
			}
			if remoteBranch != "origin/feature/new" {
				t.Fatalf("unexpected remoteBranch: %q", remoteBranch)
			}
			return nil
		},
	}

	cleaner := NewCleaner(git)
	err := cleaner.CreateLocalTrackingBranch(context.Background(), config.RepoConfig{Name: "demo", Path: "/tmp/demo"}, "feature/new", "origin/feature/new")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !called {
		t.Fatal("expected tracking branch creation call")
	}
}

func TestLoadRepoBranchesProtectsRemoteBranchWithUnknownRemoteName(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/remote",
				Scope:         model.BranchScopeRemote,
				QualifiedName: "feature/remote",
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), config.RepoConfig{Name: "demo", Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}
	if !rb.Branches[0].Protected {
		t.Fatal("expected branch to be protected")
	}
	if rb.Branches[0].Reason != "имя remote неизвестно" {
		t.Fatalf("unexpected protection reason: %q", rb.Branches[0].Reason)
	}
}

func TestLoadRepoBranchesSetsJiraLinkFieldsFromNamedGroup(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/OPS-101",
				QualifiedName: "feature/OPS-101",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{{
		"group":      "TASKS",
		"url":        "https://tasks.example.org",
		"playwright": false,
	}})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{"^feature/(?P<TASKS>[A-Z]+-\\d+)$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}

	branch := rb.Branches[0]
	if branch.JiraKey != "OPS-101" {
		t.Fatalf("expected jira key OPS-101, got %q", branch.JiraKey)
	}
	if branch.JiraGroup != "TASKS" {
		t.Fatalf("expected jira group TASKS, got %q", branch.JiraGroup)
	}
	if branch.JiraURL != "https://tasks.example.org" {
		t.Fatalf("expected jira url, got %q", branch.JiraURL)
	}
	if branch.JiraTicketURL != "https://tasks.example.org/browse/OPS-101" {
		t.Fatalf("unexpected jira ticket url: %q", branch.JiraTicketURL)
	}
}

func TestLoadRepoBranchesResolvesJiraStatusForMappedTicket(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/OPS-500",
				QualifiedName: "feature/OPS-500",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{{
		"group": "TASKS",
		"url":   "https://tasks.example.org",
	}})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{"^feature/(?P<TASKS>[A-Z]+-\\d+)$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git, WithJiraStatusResolver(fakeStatusResolver{resolveFn: func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
		if group != "TASKS" {
			t.Fatalf("unexpected jira group: %q", group)
		}
		if ticketURL != "https://tasks.example.org/browse/OPS-500" {
			t.Fatalf("unexpected ticket url: %q", ticketURL)
		}
		if jiraBaseURL != "https://tasks.example.org" {
			t.Fatalf("unexpected jira base url: %q", jiraBaseURL)
		}
		if key != "OPS-500" {
			t.Fatalf("unexpected jira key: %q", key)
		}

		return jira.StatusResult{Status: "In Progress", State: jira.StatusStateReady, Reason: jira.StatusReasonNone}
	}}))

	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}

	if rb.Branches[0].JiraStatus != "In Progress" {
		t.Fatalf("expected jira status to be resolved, got %q", rb.Branches[0].JiraStatus)
	}
	if rb.Branches[0].JiraState != model.JiraStatusStateReady {
		t.Fatalf("expected jira state ready, got %q", rb.Branches[0].JiraState)
	}
	if rb.Branches[0].JiraReason != model.JiraStatusReasonNone {
		t.Fatalf("expected jira reason none, got %q", rb.Branches[0].JiraReason)
	}
}

func TestLoadRepoBranchesMapsJiraAuthState(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/OPS-777",
				QualifiedName: "feature/OPS-777",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{{
		"group": "TASKS",
		"url":   "https://tasks.example.org",
	}})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{"^feature/(?P<TASKS>[A-Z]+-\\d+)$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git, WithJiraStatusResolver(fakeStatusResolver{resolveFn: func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
		return jira.StatusResult{Status: "-", State: jira.StatusStateAuth, Reason: jira.StatusReasonAuthRequired}
	}}))

	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if rb.Branches[0].JiraState != model.JiraStatusStateAuth {
		t.Fatalf("expected jira state auth, got %q", rb.Branches[0].JiraState)
	}
	if rb.Branches[0].JiraReason != model.JiraStatusReasonAuthRequired {
		t.Fatalf("expected jira reason auth_required, got %q", rb.Branches[0].JiraReason)
	}
}

func TestLoadRepoBranchesPrefetchesJiraStatusesBeforeResolve(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{
				{Name: "OPS-101", QualifiedName: "OPS-101", Scope: model.BranchScopeLocal},
				{Name: "IDEA-7", QualifiedName: "IDEA-7", Scope: model.BranchScopeLocal},
				{Name: "misc/no-jira", QualifiedName: "misc/no-jira", Scope: model.BranchScopeLocal},
			}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{
		{"group": "TASKS", "url": "https://tasks.example.org"},
		{"group": "IDEA", "url": "https://idea.example.org"},
	})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{`^(?P<TASKS>OPS-\d+)$`, `^(?P<IDEA>IDEA-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	prefetchCalls := 0
	prefetched := make([]jira.StatusBatchRequest, 0)
	cleaner := NewCleaner(git, WithJiraStatusResolver(fakeStatusResolver{
		prefetchFn: func(_ context.Context, requests []jira.StatusBatchRequest) {
			prefetchCalls++
			prefetched = append(prefetched, requests...)
		},
		resolveFn: func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
			return jira.StatusResult{Status: "Ready", State: jira.StatusStateReady, Reason: jira.StatusReasonNone}
		},
	}))

	_, err = cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	if prefetchCalls != 1 {
		t.Fatalf("expected single prefetch call, got %d", prefetchCalls)
	}
	if len(prefetched) != 2 {
		t.Fatalf("expected two mapped jira requests in prefetch, got %d", len(prefetched))
	}
}

func TestLoadRepoBranchesWithSummaryReturnsBatchProgress(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "OPS-101",
				QualifiedName: "OPS-101",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{{
		"group": "TASKS",
		"url":   "https://tasks.example.org",
	}})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{`^(?P<TASKS>OPS-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git, WithJiraStatusResolver(fakeStatusResolver{
		prefetchProgressFn: func(_ context.Context, requests []jira.StatusBatchRequest, _ jira.PrefetchProgressCallback) []jira.PrefetchBatchProgress {
			if len(requests) != 1 {
				t.Fatalf("expected one prefetch request, got %d", len(requests))
			}
			return []jira.PrefetchBatchProgress{{
				BatchIndex: 1,
				BatchTotal: 1,
				BatchSize:  1,
				Processed:  1,
				Total:      1,
			}}
		},
		resolveFn: func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
			return jira.StatusResult{Status: "Done", State: jira.StatusStateReady, Reason: jira.StatusReasonNone}
		},
	}))

	_, summary, err := cleaner.LoadRepoBranchesWithSummary(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches with summary: %v", err)
	}

	if len(summary.JiraBatchProgress) != 1 {
		t.Fatalf("expected one jira progress item, got %d", len(summary.JiraBatchProgress))
	}
	if summary.JiraBatchProgress[0].Processed != 1 || summary.JiraBatchProgress[0].Total != 1 {
		t.Fatalf("unexpected progress payload: %+v", summary.JiraBatchProgress[0])
	}
}

func TestLoadRepoBranchesWithProgressStreamsViaJiraLayer(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "OPS-101",
				QualifiedName: "OPS-101",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("jira", []map[string]any{{
		"group": "TASKS",
		"url":   "https://tasks.example.org",
	}})
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{`^(?P<TASKS>OPS-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	progressObservedInCallback := false
	progressObservedInPrefetch := false

	cleaner := NewCleaner(git, WithJiraStatusResolver(fakeStatusResolver{
		prefetchProgressFn: func(_ context.Context, requests []jira.StatusBatchRequest, onProgress jira.PrefetchProgressCallback) []jira.PrefetchBatchProgress {
			if len(requests) != 1 {
				t.Fatalf("expected one prefetch request, got %d", len(requests))
			}
			if onProgress == nil {
				t.Fatal("expected non-nil onProgress callback in jira prefetcher")
			}

			item := jira.PrefetchBatchProgress{BatchIndex: 1, BatchTotal: 1, BatchSize: 1, Processed: 1, Total: 1}
			onProgress(item)
			if !progressObservedInCallback {
				t.Fatal("expected onProgress to be called during prefetch execution")
			}
			progressObservedInPrefetch = true
			return []jira.PrefetchBatchProgress{item}
		},
		resolveFn: func(group, ticketURL, jiraBaseURL, key string) jira.StatusResult {
			return jira.StatusResult{Status: "Done", State: jira.StatusStateReady, Reason: jira.StatusReasonNone}
		},
	}))

	_, summary, err := cleaner.LoadRepoBranchesWithProgress(context.Background(), cfg.Repos[0], func(progress RepoLoadProgress) {
		if progress.BatchIndex == 1 && progress.Processed == 1 {
			progressObservedInCallback = true
		}
	})
	if err != nil {
		t.Fatalf("load repo branches with progress: %v", err)
	}
	if !progressObservedInPrefetch {
		t.Fatal("expected prefetch function to confirm in-flight progress callback")
	}
	if !summary.JiraProgressStreamed {
		t.Fatal("expected JiraProgressStreamed=true for real-time callback mode")
	}
	if len(summary.JiraBatchProgress) != 1 {
		t.Fatalf("expected one progress item in summary, got %d", len(summary.JiraBatchProgress))
	}
}

func TestLoadRepoBranchesSetsNoGroupConfigReasonForNamedGroupWithoutTopLevelConfig(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "BFF-1004",
				QualifiedName: "BFF-1004",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{`^(?P<SIMPLEWINE>(WEB|MOBI|BFF)-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	if rb.Branches[0].JiraKey != "BFF-1004" {
		t.Fatalf("expected jira key BFF-1004, got %q", rb.Branches[0].JiraKey)
	}
	if rb.Branches[0].JiraReason != model.JiraStatusReasonNoGroupConfig {
		t.Fatalf("expected jira reason no_group_config, got %q", rb.Branches[0].JiraReason)
	}
}

func TestLoadRepoBranchesSetsRegexKeyOnlyReasonForFallbackJIRA(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/OPS-321",
				QualifiedName: "feature/OPS-321",
				Scope:         model.BranchScopeLocal,
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"jira": []string{`^feature/(?P<JIRA>[A-Z]+-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	if rb.Branches[0].JiraKey != "OPS-321" {
		t.Fatalf("expected jira key OPS-321, got %q", rb.Branches[0].JiraKey)
	}
	if rb.Branches[0].JiraReason != model.JiraStatusReasonRegexKeyOnly {
		t.Fatalf("expected jira reason regex_key_only, got %q", rb.Branches[0].JiraReason)
	}
}

func TestLoadRepoBranchesProtectsRemoteBranchByKeepPattern(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "release/1.0",
				Scope:         model.BranchScopeRemote,
				RemoteName:    "origin",
				QualifiedName: "origin/release/1.0",
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"keep": []string{"^release/.*$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}
	if !rb.Branches[0].Protected {
		t.Fatal("expected branch to be protected")
	}
	if !strings.Contains(rb.Branches[0].Reason, "совпадение с keep pattern") {
		t.Fatalf("unexpected protection reason: %q", rb.Branches[0].Reason)
	}
}

func TestLoadRepoBranchesRemoteWithFullRefIsSelectable(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "feature/remote-ok",
				Scope:         model.BranchScopeRemote,
				RemoteName:    "origin",
				QualifiedName: "refs/remotes/origin/feature/remote-ok",
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), config.RepoConfig{Name: "demo", Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}
	if rb.Branches[0].Protected {
		t.Fatalf("expected remote branch to be selectable, got protected reason: %q", rb.Branches[0].Reason)
	}
}

func TestLoadRepoBranchesProtectsAndSkipsRemoteDefaultBranch(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{{
				Name:          "main",
				Scope:         model.BranchScopeRemote,
				RemoteName:    "origin",
				QualifiedName: "origin/main",
			}}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "feature/local", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	cleaner := NewCleaner(git)
	repo := config.RepoConfig{Name: "demo", Path: "/tmp/demo"}

	rb, err := cleaner.LoadRepoBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}
	if len(rb.Branches) != 1 {
		t.Fatalf("expected one branch, got %d", len(rb.Branches))
	}
	if !rb.Branches[0].Protected {
		t.Fatal("expected remote default branch to be protected")
	}
	if rb.Branches[0].Reason != "ветка по умолчанию" {
		t.Fatalf("unexpected protection reason: %q", rb.Branches[0].Reason)
	}

	_, err = cleaner.GenerateDeleteScript(repo, rb.RepoPath, rb.Branches, model.ScriptFormatSH)
	if err == nil {
		t.Fatal("expected script generation to skip protected remote default branch")
	}
	if !strings.Contains(err.Error(), "подходящие ветки не выбраны") {
		t.Fatalf("unexpected generate script error: %v", err)
	}
}

func TestLoadRepoBranchesSetsAutocheckForMatchingBranches(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{
				{Name: "feature/task-1", QualifiedName: "feature/task-1", Scope: model.BranchScopeLocal},
				{Name: "bugfix/fix-2", QualifiedName: "bugfix/fix-2", Scope: model.BranchScopeLocal},
				{Name: "hotfix/urgent", QualifiedName: "hotfix/urgent", Scope: model.BranchScopeLocal},
			}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"autocheck": []string{"^feature/.*$", "^bugfix/.*$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	byName := make(map[string]model.BranchInfo, len(rb.Branches))
	for _, b := range rb.Branches {
		byName[b.Name] = b
	}

	if !byName["feature/task-1"].Autocheck {
		t.Fatal("feature/task-1 must be autocheck-selected")
	}
	if !byName["bugfix/fix-2"].Autocheck {
		t.Fatal("bugfix/fix-2 must be autocheck-selected")
	}
	if byName["hotfix/urgent"].Autocheck {
		t.Fatal("hotfix/urgent must not be autocheck-selected (no regex match)")
	}
}

func TestLoadRepoBranchesAutocheckSkipsProtectedBranches(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{
				{Name: "feature/task", QualifiedName: "feature/task", Scope: model.BranchScopeLocal},
				{Name: "release/1.0", QualifiedName: "release/1.0", Scope: model.BranchScopeLocal},
			}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"keep":      []string{"^release/.*$"},
			"autocheck": []string{"^feature/.*$", "^release/.*$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	byName := make(map[string]model.BranchInfo, len(rb.Branches))
	for _, b := range rb.Branches {
		byName[b.Name] = b
	}

	// feature/task: matches autocheck, not protected -> autocheck=true
	if !byName["feature/task"].Autocheck {
		t.Fatal("feature/task must be autocheck-selected")
	}
	// release/1.0: matches autocheck regex BUT protected by keep -> autocheck=false
	if byName["release/1.0"].Autocheck {
		t.Fatal("release/1.0 must not be autocheck-selected (protected by keep)")
	}
	if !byName["release/1.0"].Protected {
		t.Fatal("release/1.0 must be protected")
	}
}

func TestLoadRepoBranchesAutocheckSkipsCurrentBranch(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{
				{Name: "feature/active-work", QualifiedName: "feature/active-work", Scope: model.BranchScopeLocal},
				{Name: "feature/other", QualifiedName: "feature/other", Scope: model.BranchScopeLocal},
			}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "feature/active-work", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "demo",
		"path": "/tmp/demo",
		"branch": map[string]any{
			"autocheck": []string{"^feature/.*$"},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), cfg.Repos[0])
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	byName := make(map[string]model.BranchInfo, len(rb.Branches))
	for _, b := range rb.Branches {
		byName[b.Name] = b
	}

	// Current branch must NOT be autocheck-selected even if it matches regex
	if byName["feature/active-work"].Autocheck {
		t.Fatal("current branch must not be autocheck-selected")
	}
	// Other matching branch must be autocheck-selected
	if !byName["feature/other"].Autocheck {
		t.Fatal("feature/other must be autocheck-selected")
	}
}

func TestLoadRepoBranchesAutocheckIsNoOpWhenEmpty(t *testing.T) {
	t.Parallel()

	git := &fakeGitClient{
		resolveRepoPathFn: func(_ context.Context, repoName, repoURL, localPath string) (string, error) {
			return "/tmp/demo", nil
		},
		listBranchesFn: func(_ context.Context, repoPath string) ([]model.BranchInfo, error) {
			return []model.BranchInfo{
				{Name: "feature/task", QualifiedName: "feature/task", Scope: model.BranchScopeLocal},
			}, nil
		},
		currentBranchFn: func(_ context.Context, repoPath string) (string, error) {
			return "main", nil
		},
		detectDefaultBranchFn: func(_ context.Context, repoPath, currentBranch string) (string, error) {
			return "main", nil
		},
	}

	repo := config.RepoConfig{Name: "demo", Path: "/tmp/demo"}
	// No autocheck configured

	cleaner := NewCleaner(git)
	rb, err := cleaner.LoadRepoBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("load repo branches: %v", err)
	}

	if rb.Branches[0].Autocheck {
		t.Fatal("branch must not be autocheck-selected when autocheck is empty")
	}
}
