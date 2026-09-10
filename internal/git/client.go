package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/workdir"
)

const (
	defaultNetworkConcurrency  = 6
	defaultNetworkMaxRetries   = 2
	defaultNetworkRetryBackoff = 300 * time.Millisecond
)

// ClientOption настраивает параметры работы Git-клиента.
type ClientOption func(*Client)

// WithNetworkConcurrency задает лимит одновременных сетевых операций (fetch/clone/pull).
func WithNetworkConcurrency(limit int) ClientOption {
	return func(c *Client) {
		if limit > 0 {
			c.networkSem = make(chan struct{}, limit)
		}
	}
}

// WithNetworkRetry настраивает количество повторных попыток и базовую задержку для transient сетевых сбоев.
func WithNetworkRetry(maxRetries int, backoff time.Duration) ClientOption {
	return func(c *Client) {
		if maxRetries >= 0 {
			c.maxRetries = maxRetries
		}
		if backoff > 0 {
			c.retryBackoff = backoff
		}
	}
}

// Client выполняет git-операции: через go-git по умолчанию и git CLI для bundle/clone/fetch-сценариев.
type Client struct {
	timeout      time.Duration
	workspaceDir string
	// networkSem ограничивает одновременные clone/fetch/pull (bounded parallelism),
	// чтобы mass-startup не упирался в sshd MaxStartups. Размер буфера = лимит (по умолчанию 6).
	networkSem   chan struct{}
	maxRetries   int
	retryBackoff time.Duration

	lockMu sync.Mutex
	locks  map[string]*pathLock
}

// ResolveRepoPath возвращает рабочий путь репозитория по настройкам источника.
func (c *Client) ResolveRepoPath(ctx context.Context, repoName, repoURL, localPath string) (string, error) {
	localPath = strings.TrimSpace(localPath)
	repoURL = strings.TrimSpace(repoURL)

	if localPath != "" {
		return validateRepoRoot(ctx, localPath)
	}

	if repoURL == "" {
		return "", errors.New("для управляемого клона требуется адрес репозитория")
	}

	return c.EnsureManagedClone(ctx, repoName, repoURL)
}

// NewClient создает Git-клиент с таймаутом на каждую команду.
func NewClient(timeout time.Duration, workspaceDir string, opts ...ClientOption) *Client {
	c := &Client{
		timeout:      timeout,
		workspaceDir: workspaceDir,
		networkSem:   make(chan struct{}, defaultNetworkConcurrency),
		maxRetries:   defaultNetworkMaxRetries,
		retryBackoff: defaultNetworkRetryBackoff,
		locks:        make(map[string]*pathLock),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// EnsureManagedClone гарантирует наличие managed clone и актуализирует remote refs.
func (c *Client) EnsureManagedClone(ctx context.Context, repoName, repoURL string) (string, error) {
	if c.workspaceDir == "" {
		return "", errors.New("требуется рабочая директория")
	}

	managedPath := filepath.Join(c.workspaceDir, safeManagedRepoDir(repoName, repoURL))
	unlock, err := c.lockForPath(ctx, managedPath)
	if err != nil {
		return "", err
	}
	defer unlock()

	if err := ensureDir(c.workspaceDir); err != nil {
		return "", err
	}

	if !isGitRepo(ctx, managedPath) {
		if err := c.cloneRepo(ctx, repoURL, managedPath); err != nil {
			return "", err
		}
	}

	if err := c.ensureOriginURL(ctx, managedPath, repoURL); err != nil {
		return "", err
	}

	if err := c.fetchPrune(ctx, managedPath); err != nil {
		return "", err
	}

	if err := c.seedLocalBranchesFromOrigin(ctx, managedPath); err != nil {
		return "", err
	}

	return managedPath, nil
}

// UpdateOpensourceRepo реализует логику обновления opensource-репозитория (аналог скрипта update-opensource.sh).
func (c *Client) UpdateOpensourceRepo(ctx context.Context, url, targetPath, branch string) error {
	targetPath = strings.TrimSpace(targetPath)
	branch = strings.TrimSpace(branch)
	if targetPath == "" {
		return errors.New("требуется целевой путь")
	}

	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("определение целевого пути: %w", err)
	}

	unlock, err := c.lockForPath(ctx, absPath)
	if err != nil {
		return err
	}
	defer unlock()

	if !isGitRepo(ctx, absPath) {
		if err := ensureDir(filepath.Dir(absPath)); err != nil {
			return err
		}
		if err := c.cloneRepo(ctx, url, absPath); err != nil {
			return err
		}
	} else {
		// Проверяем, что путь является корнем git-репозитория, а не вложенной подпапкой.
		if _, err := validateRepoRoot(ctx, absPath); err != nil {
			return fmt.Errorf("валидация корня opensource-репозитория: %w", err)
		}
		if err := c.ensureOriginURL(ctx, absPath, url); err != nil {
			return err
		}
	}

	if err := c.fetchPrune(ctx, absPath); err != nil {
		return err
	}

	if branch == "" {
		return nil
	}

	if _, err := c.runGit(ctx, absPath, "reset", "--hard", "HEAD", "--quiet"); err != nil {
		return fmt.Errorf("ошибка git reset: %w", err)
	}
	if _, err := c.runGit(ctx, absPath, "clean", "-fd", "--quiet"); err != nil {
		return fmt.Errorf("ошибка git clean: %w", err)
	}

	existsLocal, err := c.BranchExists(ctx, absPath, branch)
	if err != nil {
		return err
	}
	if existsLocal {
		if _, err := c.runGit(ctx, absPath, "checkout", "--quiet", branch); err != nil {
			return fmt.Errorf("ошибка checkout %q: %w", branch, err)
		}
	} else {
		if err := c.runGitStatusOnly(ctx, absPath, "show-ref", "--verify", "--quiet", "refs/tags/"+branch); err == nil {
			if _, err := c.runGit(ctx, absPath, "checkout", "--quiet", branch); err != nil {
				return fmt.Errorf("ошибка checkout тега %q: %w", branch, err)
			}
		} else {
			if _, err := c.runGit(ctx, absPath, "checkout", "-B", branch, "origin/"+branch, "--quiet"); err != nil {
				return fmt.Errorf("ошибка checkout -B %q origin/%q: %w", branch, branch, err)
			}
		}
	}

	if err := c.runGitStatusOnly(ctx, absPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
		if _, resetErr := c.runGit(ctx, absPath, "reset", "--hard", "origin/"+branch, "--quiet"); resetErr != nil {
			return fmt.Errorf("ошибка reset к origin/%q: %w", branch, resetErr)
		}
	}

	return nil
}

// FetchAndPull выполняет безопасный fetch + pull для текущей ветки.
func (c *Client) FetchAndPull(ctx context.Context, repoPath, repoURL string) error {
	unlock, err := c.lockForPath(ctx, repoPath)
	if err != nil {
		return err
	}
	defer unlock()

	if strings.TrimSpace(repoURL) != "" {
		if err := c.ensureOriginURL(ctx, repoPath, repoURL); err != nil {
			return err
		}
	}

	if err := c.fetchPrune(ctx, repoPath); err != nil {
		return err
	}

	statusOut, err := c.runGit(ctx, repoPath, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("проверка рабочего дерева: %w", err)
	}
	if strings.TrimSpace(statusOut) != "" {
		return errors.New("рабочее дерево содержит незакоммиченные изменения; pull отменен")
	}

	currentBranchOut, err := c.runGit(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("определение текущей ветки: %w", err)
	}
	currentBranch := strings.TrimSpace(currentBranchOut)
	if currentBranch == "" || currentBranch == "HEAD" {
		return errors.New("pull недоступен: репозиторий в состоянии detached HEAD")
	}

	if err := c.runGitStatusOnly(ctx, repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		return fmt.Errorf("pull недоступен: для ветки %q не настроен upstream", currentBranch)
	}

	if err := c.withNetworkRetry(ctx, func(ctx context.Context) error {
		if _, err := c.runGit(ctx, repoPath, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("git pull --ff-only: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// ForceCheckout принудительно переключает репозиторий на указанную ветку, сбрасывая все изменения.
func (c *Client) ForceCheckout(ctx context.Context, repoPath, branch string) error {
	unlock, err := c.lockForPath(ctx, repoPath)
	if err != nil {
		return err
	}
	defer unlock()

	_, err = c.runGit(ctx, repoPath, "checkout", "-f", branch)
	if err != nil {
		return fmt.Errorf("ошибка git checkout -f %s: %w", branch, err)
	}
	return nil
}

// CreateTrackingBranchAndCheckout создает локальную tracking-ветку из remote и переключается на нее.
func (c *Client) CreateTrackingBranchAndCheckout(ctx context.Context, repoPath, localBranch, remoteBranch string) error {
	unlock, err := c.lockForPath(ctx, repoPath)
	if err != nil {
		return err
	}
	defer unlock()

	exists, err := c.BranchExists(ctx, repoPath, localBranch)
	if err != nil {
		return fmt.Errorf("проверка существования локальной ветки %q: %w", localBranch, err)
	}

	if exists {
		if _, err := c.runGit(ctx, repoPath, "checkout", localBranch); err != nil {
			return fmt.Errorf("ошибка checkout существующей ветки %q: %w", localBranch, err)
		}
		return nil
	}

	if _, err := c.runGit(ctx, repoPath, "checkout", "--track", "-b", localBranch, remoteBranch); err != nil {
		return fmt.Errorf("ошибка создания ветки отслеживания %q из %q: %w", localBranch, remoteBranch, err)
	}

	return nil
}

// DetectDefaultBranch определяет default branch:
// origin/HEAD -> main -> master -> current branch.
func (c *Client) DetectDefaultBranch(ctx context.Context, repoPath, currentBranch string) (string, error) {
	stdout, err := c.runGit(ctx, repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(stdout)
		if strings.HasPrefix(ref, "origin/") && len(ref) > len("origin/") {
			return strings.TrimPrefix(ref, "origin/"), nil
		}
	}

	for _, fallback := range []string{"main", "master"} {
		exists, checkErr := c.BranchExists(ctx, repoPath, fallback)
		if checkErr != nil {
			return "", checkErr
		}
		if exists {
			return fallback, nil
		}
	}

	if currentBranch != "" {
		return currentBranch, nil
	}

	return "", nil
}

// BranchMetadata возвращает merged-статус и базовую ветку для branch.
func (c *Client) BranchMetadata(ctx context.Context, repoPath, branch, defaultBranch string) (model.MergeStatus, string, error) {
	mergeStatus, err := c.mergeStatus(ctx, repoPath, branch, defaultBranch)
	if err != nil {
		return model.MergeStatusUnknown, "-", err
	}

	baseBranch := c.resolveBaseBranch(ctx, repoPath, branch)
	if baseBranch == "" {
		baseBranch = "-"
	}

	return mergeStatus, baseBranch, nil
}

// ManagedRepoPath вычисляет путь managed clone для репозитория.
func (c *Client) ManagedRepoPath(repoName, repoURL string) string {
	return filepath.Join(c.workspaceDir, safeManagedRepoDir(repoName, repoURL))
}

// ListBranches возвращает локальные и удаленные ветки, отсортированные по дате последнего коммита.
func (c *Client) ListBranches(ctx context.Context, repoPath string) ([]model.BranchInfo, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}

	repo, err := c.open(repoPath)
	if err != nil {
		return nil, err
	}

	localBranches, err := c.listLocalBranches(repo)
	if err != nil {
		return nil, err
	}

	remoteBranches, err := c.listRemoteBranches(repo)
	if err != nil {
		return nil, err
	}

	result := slices.Concat(localBranches, remoteBranches)

	slices.SortFunc(result, func(a, b model.BranchInfo) int {
		return b.LastCommitAt.Compare(a.LastCommitAt)
	})

	return result, nil
}

func (c *Client) listLocalBranches(repo *git.Repository) ([]model.BranchInfo, error) {
	branches, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("получение списка локальных веток: %w", err)
	}

	var result []model.BranchInfo
	err = branches.ForEach(func(ref *plumbing.Reference) error {
		commit, err := commitForRef(repo, ref)
		if err != nil {
			return fmt.Errorf("определение коммита для ветки %q: %w", ref.Name().Short(), err)
		}

		name := ref.Name().Short()
		result = append(result, model.BranchInfo{
			Key:           ref.Name().String(),
			Name:          name,
			QualifiedName: name,
			FullRef:       ref.Name().String(),
			Scope:         model.BranchScopeLocal,
			LastCommitAt:  commit.Committer.When,
			LastCommitSHA: ref.Hash().String(),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) listRemoteBranches(repo *git.Repository) ([]model.BranchInfo, error) {
	refs, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("получение списка ссылок: %w", err)
	}

	var result []model.BranchInfo
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsRemote() {
			return nil
		}

		if ref.Name().Short() == "origin/HEAD" || strings.HasSuffix(ref.Name().String(), "/HEAD") {
			return nil
		}

		short := ref.Name().Short()
		parts := strings.SplitN(short, "/", 2)
		if len(parts) != 2 {
			return nil
		}

		commit, err := commitForRef(repo, ref)
		if err != nil {
			return fmt.Errorf("определение коммита для удаленной ветки %q: %w", short, err)
		}

		remoteName := parts[0]
		name := parts[1]
		result = append(result, model.BranchInfo{
			Key:           ref.Name().String(),
			Name:          name,
			QualifiedName: short,
			FullRef:       ref.Name().String(),
			Scope:         model.BranchScopeRemote,
			RemoteName:    remoteName,
			LastCommitAt:  commit.Committer.When,
			LastCommitSHA: ref.Hash().String(),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// CurrentBranch возвращает имя текущей ветки в репозитории.
func (c *Client) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	if err := contextErr(ctx); err != nil {
		return "", err
	}

	repo, err := c.open(repoPath)
	if err != nil {
		return "", err
	}

	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) && isEmptyCloneRepo(repo) {
			return "", newEmptyCloneError()
		}
		return "", fmt.Errorf("определение HEAD: %w", err)
	}

	return head.Name().Short(), nil
}

// BranchExists проверяет существование локальной ветки по имени.
func (c *Client) BranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	if err := contextErr(ctx); err != nil {
		return false, err
	}

	repo, err := c.open(repoPath)
	if err != nil {
		return false, err
	}

	_, err = repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return false, nil
	}

	return false, fmt.Errorf("определение ветки %q: %w", branch, err)
}

// GetDirtyStats проверяет наличие измененных или неотслеживаемых файлов в рабочей директории и подсчитывает их.
func (c *Client) GetDirtyStats(ctx context.Context, repoPath string) (model.DirtyStats, error) {
	stdout, err := c.runGit(ctx, repoPath, "status", "--porcelain")
	if err != nil {
		return model.DirtyStats{}, fmt.Errorf("git status --porcelain: %w", err)
	}

	var stats model.DirtyStats
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}
		x := line[0]
		y := line[1]
		file := strings.TrimSpace(line[2:])

		if x == '?' && y == '?' {
			stats.Untracked = append(stats.Untracked, file)
			continue
		}

		if x == 'M' || y == 'M' {
			stats.Modified = append(stats.Modified, file)
		}
		if x == 'A' || y == 'A' {
			stats.Added = append(stats.Added, file)
		}
		if x == 'D' || y == 'D' {
			stats.Deleted = append(stats.Deleted, file)
		}
	}

	return stats, nil
}

// GetRepoStat собирает CurrentBranch и IsDirty для репозитория.
func (c *Client) GetRepoStat(ctx context.Context, repoPath string) (model.RepoStat, error) {
	branch, err := c.CurrentBranch(ctx, repoPath)
	if err != nil {
		if errors.Is(err, ErrEmptyClone) {
			return model.RepoStat{
				Loaded: true,
				Warning: model.RepoWarning{
					Code:    model.RepoWarningEmptyClone,
					Message: err.Error(),
				},
			}, nil
		}
		return model.RepoStat{}, err
	}

	dirty, err := c.GetDirtyStats(ctx, repoPath)
	if err != nil {
		return model.RepoStat{}, err
	}

	return model.RepoStat{
		CurrentBranch: branch,
		DirtyStats:    dirty,
		Loaded:        true,
	}, nil
}

// cloneRepo клонирует репозиторий в managedPath с ограничением параллелизма и retry.
func (c *Client) cloneRepo(ctx context.Context, repoURL, managedPath string) error {
	if err := ensureDir(filepath.Dir(managedPath)); err != nil {
		return err
	}

	return c.withNetworkRetry(ctx, func(ctx context.Context) error {
		_, err := c.runGit(ctx, "", "clone", repoURL, managedPath)
		if err != nil {
			if !isGitRepo(ctx, managedPath) {
				_ = os.RemoveAll(managedPath)
			}
			return fmt.Errorf("ошибка клонирования репозитория %q в %q: %w", repoURL, managedPath, err)
		}
		return nil
	})
}

// ensureOriginURL записывает URL origin, если текущий адрес отличается.
func (c *Client) ensureOriginURL(ctx context.Context, repoPath, repoURL string) error {
	stdout, err := c.runGit(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("чтение адреса origin: %w", err)
	}

	currentURL := strings.TrimSpace(stdout)
	if currentURL == repoURL {
		return nil
	}

	if _, err := c.runGit(ctx, repoPath, "remote", "set-url", "origin", repoURL); err != nil {
		return fmt.Errorf("установка адреса origin: %w", err)
	}

	return nil
}

// fetchPrune выполняет git fetch --prune origin с семафором и retry для transient-сбоев.
func (c *Client) fetchPrune(ctx context.Context, repoPath string) error {
	return c.withNetworkRetry(ctx, func(ctx context.Context) error {
		if _, err := c.runGit(ctx, repoPath, "fetch", "--prune", "origin"); err != nil {
			return fmt.Errorf("ошибка fetch --prune origin: %w", err)
		}
		return nil
	})
}

func (c *Client) seedLocalBranchesFromOrigin(ctx context.Context, repoPath string) error {
	stdout, err := c.runGit(ctx, repoPath, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin")
	if err != nil {
		return fmt.Errorf("получение списка удаленных refs: %w", err)
	}

	for _, remoteBranch := range strings.Split(strings.TrimSpace(stdout), "\n") {
		remoteBranch = strings.TrimSpace(remoteBranch)
		if remoteBranch == "" || remoteBranch == "origin/HEAD" {
			continue
		}
		if !strings.HasPrefix(remoteBranch, "origin/") {
			continue
		}

		branchName := strings.TrimPrefix(remoteBranch, "origin/")
		exists, checkErr := c.BranchExists(ctx, repoPath, branchName)
		if checkErr != nil {
			return checkErr
		}
		if exists {
			continue
		}

		if _, err := c.runGit(ctx, repoPath, "branch", "--track", branchName, remoteBranch); err != nil {
			return fmt.Errorf("ошибка создания локальной ветки %q из %q: %w", branchName, remoteBranch, err)
		}
	}

	return nil
}

// open открывает локальный git-репозиторий по пути.
func (c *Client) open(repoPath string) (*git.Repository, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("открытие репозитория %s: %w", repoPath, err)
	}

	return repo, nil
}

// commitForRef возвращает объект коммита, на который указывает ссылка.
func commitForRef(repo *git.Repository, ref *plumbing.Reference) (*object.Commit, error) {
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}

	return commit, nil
}

// runGit запускает git-команду в repoPath и классифицирует ошибку CLI.
func (c *Client) runGit(parentCtx context.Context, repoPath string, args ...string) (string, error) {
	execCtx, cancel := c.withCommandTimeout(parentCtx)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", c.classifyGitCommandError(execCtx, args[0], stderr.String(), err)
	}

	return stdout.String(), nil
}

// classifyGitCommandError оборачивает сбой git CLI и помечает transient сетевые ошибки.
func (c *Client) classifyGitCommandError(execCtx context.Context, op, stderr string, runErr error) error {
	if errors.Is(execCtx.Err(), context.Canceled) {
		return fmt.Errorf("git %s отменен: %w", op, runErr)
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: таймаут git %s после %s: %w", ErrTransientNetwork, op, c.timeout, runErr)
	}

	stderrStr := strings.TrimSpace(stderr)
	if stderrStr == "" && runErr != nil {
		stderrStr = runErr.Error()
	}
	wrapped := fmt.Errorf("ошибка git %s: %s (%w)", op, stderrStr, runErr)
	if isTransientGitOutput(stderrStr, runErr) {
		return fmt.Errorf("%w: %w", ErrTransientNetwork, wrapped)
	}
	return wrapped
}

// mergeStatus определяет, влита ли ветка в базовую.
func (c *Client) mergeStatus(ctx context.Context, repoPath, branch, defaultBranch string) (model.MergeStatus, error) {
	if strings.TrimSpace(defaultBranch) == "" || branch == defaultBranch {
		return model.MergeStatusUnknown, nil
	}

	err := c.runGitStatusOnly(ctx, repoPath, "merge-base", "--is-ancestor", branch, defaultBranch)
	if err == nil {
		return model.MergeStatusMerged, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.ExitStatus() == 1 {
			return model.MergeStatusUnmerged, nil
		}
	}

	return model.MergeStatusUnknown, nil
}

// resolveBaseBranch возвращает upstream ветки или пустую строку.
func (c *Client) resolveBaseBranch(ctx context.Context, repoPath, branch string) string {
	stdout, err := c.runGit(ctx, repoPath, "rev-parse", "--abbrev-ref", branch+"@{upstream}")
	if err != nil {
		return ""
	}

	upstream := strings.TrimSpace(stdout)
	if upstream == "" {
		return ""
	}

	return upstream
}

// runGitStatusOnly запускает git-команду без захвата stdout и классифицирует ошибку CLI.
func (c *Client) runGitStatusOnly(parentCtx context.Context, repoPath string, args ...string) error {
	execCtx, cancel := c.withCommandTimeout(parentCtx)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return c.classifyGitCommandError(execCtx, args[0], stderr.String(), err)
	}

	return nil
}

func (c *Client) lockForPath(ctx context.Context, path string) (func(), error) {
	c.lockMu.Lock()
	mu, ok := c.locks[path]
	if !ok {
		mu = newPathLock()
		c.locks[path] = mu
	}
	c.lockMu.Unlock()

	if err := mu.lock(ctx); err != nil {
		return nil, err
	}

	return mu.unlock, nil
}

func safeManagedRepoDir(repoName, repoURL string) string {
	return workdir.ManagedRepoDirKey(repoName, repoURL)
}

func isGitRepo(ctx context.Context, path string) bool {
	if path == "" {
		return false
	}

	cmd := exec.CommandContext(ctxOrBackground(ctx), "git", "-C", path, "rev-parse", "--is-inside-work-tree")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}

	insideWorkTree := strings.TrimSpace(stdout.String())
	if insideWorkTree != "true" {
		return false
	}

	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

var ErrNotGitRepo = errors.New("путь не является git-репозиторием")

// resolveGitRoot возвращает корень git-репозитория для path, если path находится внутри git-репозитория.
// Возвращает пустую строку, если path не внутри git-репозитория.
func resolveGitRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctxOrBackground(ctx), "git", "-C", path, "rev-parse", "--show-toplevel")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", nil // not inside a git repo
	}
	return strings.TrimSpace(stdout.String()), nil
}

// normalizePath нормализует путь: разрешает symlinks и очищает от лишних элементов.
func normalizePath(path string) (string, error) {
	evalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(evalPath), nil
}

// validateRepoRoot проверяет, что path является корнем git-репозитория (или worktree).
// Возвращает нормализованный путь или ошибку.
// Если path не находится внутри git-репозитория, возвращает ErrNotGitRepo.
func validateRepoRoot(ctx context.Context, path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("определение абсолютного пути: %w", err)
	}

	gitRoot, err := resolveGitRoot(ctx, absPath)
	if err != nil {
		return "", fmt.Errorf("поиск корня репозитория: %w", err)
	}
	if gitRoot == "" {
		return "", ErrNotGitRepo
	}

	normalizedPath, err := normalizePath(absPath)
	if err != nil {
		return "", fmt.Errorf("нормализация пути: %w", err)
	}
	normalizedRoot, err := normalizePath(gitRoot)
	if err != nil {
		return "", fmt.Errorf("нормализация корня: %w", err)
	}

	if normalizedPath != normalizedRoot {
		return "", fmt.Errorf("путь не является корнем git-репозитория: %s (фактический корень: %s)", normalizedPath, normalizedRoot)
	}

	return normalizedPath, nil
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

type pathLock struct {
	token chan struct{}
}

func newPathLock() *pathLock {
	lock := &pathLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	return lock
}

func (l *pathLock) lock(ctx context.Context) error {
	ctx = ctxOrBackground(ctx)
	select {
	case <-ctx.Done():
		return fmt.Errorf("ожидание блокировки отменено: %w", ctx.Err())
	case <-l.token:
		return nil
	}
}

func (l *pathLock) unlock() {
	select {
	case l.token <- struct{}{}:
	default:
	}
}

func (c *Client) withCommandTimeout(parentCtx context.Context) (context.Context, context.CancelFunc) {
	parentCtx = ctxOrBackground(parentCtx)
	if c.timeout <= 0 {
		return parentCtx, func() {}
	}

	return context.WithTimeout(parentCtx, c.timeout)
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	return ctx.Err()
}
