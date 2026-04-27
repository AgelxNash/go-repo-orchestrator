package usecase

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/filter"
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

func collectJiraStatusRequests(repo config.RepoConfig, branches []model.BranchInfo) []jira.StatusBatchRequest {
	requests := make([]jira.StatusBatchRequest, 0, len(branches))
	for _, branch := range branches {
		jiraMatch, ok, _ := repo.ExtractJiraMatchDetailed(branch.Name)
		if !ok {
			continue
		}
		if strings.TrimSpace(jiraMatch.Group) == "" || strings.TrimSpace(jiraMatch.TicketURL) == "" {
			continue
		}
		requests = append(requests, jira.StatusBatchRequest{
			Group:       jiraMatch.Group,
			TicketURL:   jiraMatch.TicketURL,
			JiraBaseURL: jiraMatch.URL,
			Key:         jiraMatch.Key,
		})
	}

	return requests
}

func mapJiraStatusState(state jira.StatusState) model.JiraStatusState {
	switch state {
	case jira.StatusStateReady:
		return model.JiraStatusStateReady
	case jira.StatusStateLoading:
		return model.JiraStatusStateLoading
	case jira.StatusStateTransient:
		return model.JiraStatusStateTransient
	case jira.StatusStateAuth:
		return model.JiraStatusStateAuth
	case jira.StatusStateUnmapped:
		return model.JiraStatusStateUnmapped
	default:
		return model.JiraStatusStateError
	}
}

func mapJiraStatusReason(reason jira.StatusReason) model.JiraStatusReason {
	switch reason {
	case jira.StatusReasonNone:
		return model.JiraStatusReasonNone
	case jira.StatusReasonNoMapping:
		return model.JiraStatusReasonNoMapping
	case jira.StatusReasonNoGroupConfig:
		return model.JiraStatusReasonNoGroupConfig
	case jira.StatusReasonInvalidRequest:
		return model.JiraStatusReasonInvalidRequest
	case jira.StatusReasonTemporarilyDown:
		return model.JiraStatusReasonTemporarilyDown
	case jira.StatusReasonAuthRequired:
		return model.JiraStatusReasonAuthRequired
	case jira.StatusReasonForbidden:
		return model.JiraStatusReasonForbidden
	case jira.StatusReasonLoginRequired:
		return model.JiraStatusReasonLoginRequired
	case jira.StatusReasonIssueNotFound:
		return model.JiraStatusReasonIssueNotFound
	case jira.StatusReasonClientError:
		return model.JiraStatusReasonClientError
	case jira.StatusReasonHTTPError:
		return model.JiraStatusReasonHTTPError
	case jira.StatusReasonTransportError:
		return model.JiraStatusReasonTransportError
	case jira.StatusReasonResponseParseErr:
		return model.JiraStatusReasonResponseParseErr
	case jira.StatusReasonBrowserUnavailableHTTPFallback:
		return model.JiraStatusReasonBrowserUnavailableHTTPFallback
	case jira.StatusReasonBrowserUnavailableHTTPAuthRequired:
		return model.JiraStatusReasonBrowserUnavailableHTTPAuthRequired
	case jira.StatusReasonBrowserUnavailableHTTPError:
		return model.JiraStatusReasonBrowserUnavailableHTTPError
	default:
		return model.JiraStatusReasonHTTPError
	}
}

func valueOrDash(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}

	return raw
}

type jiraMappingStats struct {
	total  int
	counts map[config.JiraMatchReason]int
}

func newJiraMappingStats() jiraMappingStats {
	return jiraMappingStats{counts: make(map[config.JiraMatchReason]int)}
}

func (s *jiraMappingStats) add(diag config.JiraMatchDiagnostics) {
	if s == nil {
		return
	}
	s.total++
	s.counts[diag.Reason]++
}

func (c *Cleaner) logJiraMappingSummary(repoName string, stats jiraMappingStats) {
	if c == nil || c.log == nil || !c.log.Core().Enabled(zap.DebugLevel) {
		return
	}

	fields := []zap.Field{
		zap.String("repo", strings.TrimSpace(repoName)),
		zap.Int("total_branches", stats.total),
		zap.Int("mapped_named_group", stats.counts[config.JiraMatchReasonMappedNamedGroup]),
		zap.Int("named_group_no_group_config", stats.counts[config.JiraMatchReasonNamedGroupNoGroup]),
		zap.Int("fallback_jira", stats.counts[config.JiraMatchReasonFallbackJIRA]),
		zap.Int("fallback_full_match", stats.counts[config.JiraMatchReasonFallbackFullMatch]),
		zap.Int("no_regex_match", stats.counts[config.JiraMatchReasonNoRegexMatch]),
	}

	c.log.Debug("jira mapping summary", fields...)
}

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

// GenerateDeleteScript формирует shell/cmd скрипт удаления выбранных веток и сохраняет его в репозитории.
func (c *Cleaner) GenerateDeleteScript(repo config.RepoConfig, repoPath string, branches []model.BranchInfo, format model.ScriptFormat) (model.ScriptResult, error) {
	if len(branches) == 0 {
		return model.ScriptResult{}, fmt.Errorf("ветки не выбраны")
	}

	eligible := make([]model.BranchInfo, 0, len(branches))
	for _, branch := range branches {
		if branch.Protected {
			continue
		}
		if branch.IsRemote() && !isRemoteDeleteResolvable(branch) {
			continue
		}
		eligible = append(eligible, branch)
	}
	if len(eligible) == 0 {
		return model.ScriptResult{}, fmt.Errorf("подходящие ветки не выбраны")
	}

	sessionID := time.Now().UTC().Format("20060102T150405Z")
	ext := ".sh"
	if format == model.ScriptFormatBAT {
		ext = ".bat"
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	safeRepoName := strings.ReplaceAll(repo.Name, "/", "_")
	safeRepoName = strings.ReplaceAll(safeRepoName, "\\", "_")
	filePattern := fmt.Sprintf("go-repo-orchestrator-%s-delete-%s-*%s", safeRepoName, sessionID, ext)
	content := buildScriptContent(repoPath, eligible, format)

	scriptFile, err := os.CreateTemp(cwd, filePattern)
	if err != nil {
		return model.ScriptResult{}, fmt.Errorf("создать файл скрипта: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer func() {
		_ = scriptFile.Close()
	}()

	perm := os.FileMode(0o644)
	if format == model.ScriptFormatSH {
		perm = 0o755
	}

	if _, err := scriptFile.WriteString(content); err != nil {
		_ = os.Remove(scriptPath)
		return model.ScriptResult{}, fmt.Errorf("записать скрипт: %w", err)
	}

	if err := scriptFile.Chmod(perm); err != nil {
		_ = os.Remove(scriptPath)
		return model.ScriptResult{}, fmt.Errorf("установить права на скрипт: %w", err)
	}

	return model.ScriptResult{
		RepoName:      repo.Name,
		RepoPath:      repoPath,
		ScriptPath:    scriptPath,
		Format:        format,
		BranchesCount: len(eligible),
	}, nil
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

func evaluateBranchProtection(repo config.RepoConfig, branch model.BranchInfo, currentBranch, defaultBranch string) (bool, string) {
	if branch.IsLocal() {
		return filter.Evaluate(repo, branch, currentBranch, defaultBranch)
	}

	return evaluateRemoteBranchProtection(repo, branch, defaultBranch)
}

func evaluateRemoteBranchProtection(repo config.RepoConfig, branch model.BranchInfo, defaultBranch string) (bool, string) {
	if strings.TrimSpace(branch.RemoteName) == "" {
		return false, "имя remote неизвестно"
	}
	if strings.TrimSpace(branch.Name) == "" {
		return false, "имя удаленной ветки неизвестно"
	}
	if isRemoteDefaultBranch(branch, defaultBranch) {
		return false, "ветка по умолчанию"
	}
	if !isRemoteDeleteResolvable(branch) {
		return false, "remote ref неоднозначен для удаления"
	}
	if reason, ok := repo.ProtectedReason(branch.Name); ok {
		return false, reason
	}

	return true, "подходит"
}

func isRemoteDefaultBranch(branch model.BranchInfo, defaultBranch string) bool {
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" {
		return false
	}

	branchName := strings.TrimSpace(branch.Name)
	qualified := strings.TrimSpace(branch.QualifiedName)
	if branchName == defaultBranch || qualified == defaultBranch {
		return true
	}

	if branch.RemoteName != "" {
		fullRef := "refs/remotes/" + branch.RemoteName + "/" + branchName
		if fullRef == defaultBranch {
			return true
		}
	}

	parts := strings.SplitN(defaultBranch, "/", 2)
	if len(parts) == 2 {
		return branchName == strings.TrimSpace(parts[1])
	}

	if strings.HasPrefix(defaultBranch, "refs/remotes/") {
		parts = strings.SplitN(strings.TrimPrefix(defaultBranch, "refs/remotes/"), "/", 2)
		if len(parts) == 2 {
			return branchName == strings.TrimSpace(parts[1])
		}
	}

	return false
}

func isRemoteDeleteResolvable(branch model.BranchInfo) bool {
	if !branch.IsRemote() {
		return true
	}

	remoteName := strings.TrimSpace(branch.RemoteName)
	branchName := strings.TrimSpace(branch.Name)
	if remoteName == "" || branchName == "" {
		return false
	}

	qualified := strings.TrimSpace(branch.QualifiedName)
	if qualified == "" {
		return true
	}

	prefix := remoteName + "/"
	if strings.HasPrefix(qualified, prefix) {
		return strings.TrimPrefix(qualified, prefix) == branchName
	}

	fullRefPrefix := "refs/remotes/" + remoteName + "/"
	if strings.HasPrefix(qualified, fullRefPrefix) {
		return strings.TrimPrefix(qualified, fullRefPrefix) == branchName
	}

	return false
}

func buildScriptContent(repoPath string, branches []model.BranchInfo, format model.ScriptFormat) string {
	var b strings.Builder
	if format == model.ScriptFormatBAT {
		b.WriteString("@echo off\n")
		b.WriteString("setlocal\n")
		b.WriteString("cd /d \"")
		b.WriteString(escapeForBat(repoPath))
		b.WriteString("\"\n\n")
		for _, branch := range branches {
			command := buildDeleteCommandBAT(branch)
			if command == "" {
				continue
			}
			b.WriteString(command)
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString("#!/usr/bin/env sh\n")
	b.WriteString("set -eu\n")
	b.WriteString("cd ")
	b.WriteString(quoteForPOSIX(repoPath))
	b.WriteString("\n\n")
	for _, branch := range branches {
		command := buildDeleteCommandSH(branch)
		if command == "" {
			continue
		}
		b.WriteString(command)
		b.WriteString("\n")
	}

	return b.String()
}

func buildDeleteCommandSH(branch model.BranchInfo) string {
	if branch.IsRemote() {
		if !isRemoteDeleteResolvable(branch) {
			return ""
		}
		return "git push " + quoteForPOSIX(branch.RemoteName) + " --delete " + quoteForPOSIX(branch.Name)
	}

	flag := "-D"
	if branch.MergeStatus == model.MergeStatusMerged {
		flag = "-d"
	}

	return "git branch " + flag + " " + quoteForPOSIX(branch.Name)
}

func buildDeleteCommandBAT(branch model.BranchInfo) string {
	if branch.IsRemote() {
		if !isRemoteDeleteResolvable(branch) {
			return ""
		}
		return "git push \"" + escapeForBat(branch.RemoteName) + "\" --delete \"" + escapeForBat(branch.Name) + "\""
	}

	flag := "-D"
	if branch.MergeStatus == model.MergeStatusMerged {
		flag = "-d"
	}

	return "git branch " + flag + " \"" + escapeForBat(branch.Name) + "\""
}

func quoteForPOSIX(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func escapeForBat(value string) string {
	return strings.ReplaceAll(value, "\"", "\"\"")
}
