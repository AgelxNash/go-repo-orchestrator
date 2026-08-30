package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func TestResolveRepoPathRejectsNestedSubdir(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")

	runCmd(t, dir, "git", "init", repoPath)
	runCmd(t, repoPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoPath, "git", "config", "user.name", "tester")
	writeFile(t, filepath.Join(repoPath, "README.md"), "init\n")
	runCmd(t, repoPath, "git", "add", "README.md")
	runCmd(t, repoPath, "git", "commit", "-m", "init")

	// Создаём вложенную подпапку без собственного .git
	nestedPath := filepath.Join(repoPath, "common", "static")
	if err := os.MkdirAll(nestedPath, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	_, err := client.ResolveRepoPath(t.Context(), "local", "", nestedPath)
	if err == nil {
		t.Fatal("expected error for nested subdir, got nil")
	}
	if !strings.Contains(err.Error(), "не является корнем git-репозитория") {
		t.Fatalf("expected root mismatch error, got: %v", err)
	}
}

func TestResolveRepoPathAcceptsSeparateNestedRepo(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	parentRepoPath := filepath.Join(dir, "parent-repo")
	nestedRepoPath := filepath.Join(parentRepoPath, "nested-repo")

	// Создаём родительский репозиторий
	runCmd(t, dir, "git", "init", parentRepoPath)
	runCmd(t, parentRepoPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, parentRepoPath, "git", "config", "user.name", "tester")
	writeFile(t, filepath.Join(parentRepoPath, "README.md"), "parent\n")
	runCmd(t, parentRepoPath, "git", "add", "README.md")
	runCmd(t, parentRepoPath, "git", "commit", "-m", "parent init")

	// Создаём отдельный вложенный репозиторий с собственным .git
	runCmd(t, parentRepoPath, "git", "init", nestedRepoPath)
	runCmd(t, nestedRepoPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, nestedRepoPath, "git", "config", "user.name", "tester")
	writeFile(t, filepath.Join(nestedRepoPath, "NESTED.md"), "nested\n")
	runCmd(t, nestedRepoPath, "git", "add", "NESTED.md")
	runCmd(t, nestedRepoPath, "git", "commit", "-m", "nested init")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	resolved, err := client.ResolveRepoPath(t.Context(), "local", "", nestedRepoPath)
	if err != nil {
		t.Fatalf("expected nested repo to be accepted, got error: %v", err)
	}

	absNestedPath, err := filepath.Abs(nestedRepoPath)
	if err != nil {
		t.Fatalf("resolve absolute nested path: %v", err)
	}
	if resolved != absNestedPath {
		t.Fatalf("expected resolved path %s, got %s", absNestedPath, resolved)
	}
}

func TestResolveRepoPathAcceptsGitWorktree(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	mainRepoPath := filepath.Join(dir, "repo")
	worktreePath := filepath.Join(dir, "repo-worktree")

	runCmd(t, dir, "git", "init", mainRepoPath)
	runCmd(t, mainRepoPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, mainRepoPath, "git", "config", "user.name", "tester")
	writeFile(t, filepath.Join(mainRepoPath, "README.md"), "init\n")
	runCmd(t, mainRepoPath, "git", "add", "README.md")
	runCmd(t, mainRepoPath, "git", "commit", "-m", "init")
	runCmd(t, mainRepoPath, "git", "checkout", "-b", "feature/worktree")
	runCmd(t, mainRepoPath, "git", "checkout", "master")
	runCmd(t, mainRepoPath, "git", "worktree", "add", worktreePath, "feature/worktree")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	resolved, err := client.ResolveRepoPath(t.Context(), "local", "", worktreePath)
	if err != nil {
		t.Fatalf("resolve worktree repo path: %v", err)
	}

	absWorktreePath, err := filepath.Abs(worktreePath)
	if err != nil {
		t.Fatalf("resolve absolute worktree path: %v", err)
	}
	if resolved != absWorktreePath {
		t.Fatalf("expected resolved path %s, got %s", absWorktreePath, resolved)
	}
}

func TestListBranchesIncludesRemoteBranchesWithUniqueKeys(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	clonePath := filepath.Join(dir, "clone")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")

	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, seedPath, "git", "checkout", "-b", "feature/only-remote")
	writeFile(t, filepath.Join(seedPath, "feature.txt"), "feature\n")
	runCmd(t, seedPath, "git", "add", "feature.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "feature")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "feature/only-remote")

	runCmd(t, seedPath, "git", "checkout", "main")
	runCmd(t, seedPath, "git", "branch", "-D", "feature/only-remote")

	runCmd(t, dir, "git", "clone", remotePath, clonePath)
	runCmd(t, clonePath, "git", "fetch", "--prune", "origin")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	branches, err := client.ListBranches(context.Background(), clonePath)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}

	var hasLocalMain bool
	var hasRemoteFeature bool
	keys := make(map[string]struct{}, len(branches))
	for _, br := range branches {
		if _, exists := keys[br.Key]; exists {
			t.Fatalf("duplicate branch key detected: %s", br.Key)
		}
		keys[br.Key] = struct{}{}

		if br.Scope == model.BranchScopeLocal && br.Name == "main" {
			hasLocalMain = true
		}
		if br.Scope == model.BranchScopeRemote && br.Name == "feature/only-remote" {
			hasRemoteFeature = true
			if br.RemoteName != "origin" {
				t.Fatalf("expected remote name origin, got %q", br.RemoteName)
			}
			if !strings.HasPrefix(br.FullRef, "refs/remotes/") {
				t.Fatalf("expected full remote ref, got %q", br.FullRef)
			}
		}
	}

	if !hasLocalMain {
		t.Fatal("expected local main branch in list")
	}
	if !hasRemoteFeature {
		t.Fatal("expected remote feature branch in list")
	}
}

func TestCreateTrackingBranchAndCheckoutFromRemote(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	clonePath := filepath.Join(dir, "clone")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")

	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, seedPath, "git", "checkout", "-b", "feature/tracking")
	writeFile(t, filepath.Join(seedPath, "tracking.txt"), "tracking\n")
	runCmd(t, seedPath, "git", "add", "tracking.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "tracking")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "feature/tracking")

	runCmd(t, dir, "git", "clone", remotePath, clonePath)
	runCmd(t, clonePath, "git", "fetch", "--prune", "origin")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	err := client.CreateTrackingBranchAndCheckout(context.Background(), clonePath, "feature/tracking", "origin/feature/tracking")
	if err != nil {
		t.Fatalf("create tracking branch: %v", err)
	}

	out, err := exec.Command("git", "-C", clonePath, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve HEAD after tracking branch create: %v (%s)", err, string(out))
	}
	if strings.TrimSpace(string(out)) != "feature/tracking" {
		t.Fatalf("expected HEAD to be feature/tracking, got %q", strings.TrimSpace(string(out)))
	}
}

func TestUpdateOpensourceRepoClonesWhenPathMissing(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	targetPath := filepath.Join(dir, "opensource")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")
	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	if err := client.UpdateOpensourceRepo(context.Background(), remotePath, targetPath, ""); err != nil {
		t.Fatalf("update opensource repo: %v", err)
	}

	if !isGitRepo(context.Background(), targetPath) {
		t.Fatalf("expected git repository at %s after update", targetPath)
	}
}

func TestUpdateOpensourceRepoFetchesExistingRepoWithoutResetWhenAutoswitchEmpty(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	targetPath := filepath.Join(dir, "opensource")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")
	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, dir, "git", "clone", remotePath, targetPath)
	writeFile(t, filepath.Join(targetPath, "README.md"), "dirty\n")

	runCmd(t, seedPath, "git", "checkout", "-b", "feature/new-remote")
	writeFile(t, filepath.Join(seedPath, "feature.txt"), "feature\n")
	runCmd(t, seedPath, "git", "add", "feature.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "feature")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "feature/new-remote")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	if err := client.UpdateOpensourceRepo(context.Background(), remotePath, targetPath, ""); err != nil {
		t.Fatalf("update opensource repo: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(targetPath, "README.md"))
	if err != nil {
		t.Fatalf("read working tree file: %v", err)
	}
	if strings.TrimSpace(string(content)) != "dirty" {
		t.Fatalf("expected working tree changes to be preserved, got %q", strings.TrimSpace(string(content)))
	}

	if err := exec.Command("git", "-C", targetPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/new-remote").Run(); err != nil {
		t.Fatalf("expected new remote branch to be fetched: %v", err)
	}
}

func TestUpdateOpensourceRepoAutoswitchResetsAndChecksOutBranch(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	targetPath := filepath.Join(dir, "opensource")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")
	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, seedPath, "git", "checkout", "-b", "develop")
	writeFile(t, filepath.Join(seedPath, "develop.txt"), "develop\n")
	runCmd(t, seedPath, "git", "add", "develop.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "develop")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "develop")

	runCmd(t, dir, "git", "clone", remotePath, targetPath)
	writeFile(t, filepath.Join(targetPath, "README.md"), "dirty\n")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	if err := client.UpdateOpensourceRepo(context.Background(), remotePath, targetPath, "develop"); err != nil {
		t.Fatalf("update opensource repo with autoswitch: %v", err)
	}

	branchOut, err := exec.Command("git", "-C", targetPath, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve current branch: %v (%s)", err, string(branchOut))
	}
	if strings.TrimSpace(string(branchOut)) != "develop" {
		t.Fatalf("expected current branch develop, got %q", strings.TrimSpace(string(branchOut)))
	}

	statusOut, err := exec.Command("git", "-C", targetPath, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("read git status: %v (%s)", err, string(statusOut))
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		t.Fatalf("expected clean working tree after autoswitch, got %q", strings.TrimSpace(string(statusOut)))
	}
}

func TestFetchAndPullFastForwardUpdatesCurrentBranch(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	clonePath := filepath.Join(dir, "clone")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")

	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "v1\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, dir, "git", "clone", remotePath, clonePath)

	writeFile(t, filepath.Join(seedPath, "README.md"), "v2\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "update")
	runCmd(t, seedPath, "git", "push", "origin", "main")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	if err := client.FetchAndPull(context.Background(), clonePath, remotePath); err != nil {
		t.Fatalf("fetch and pull: %v", err)
	}

	out, err := exec.Command("git", "-C", clonePath, "show", "-s", "--format=%s", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("read head commit message: %v (%s)", err, string(out))
	}
	if strings.TrimSpace(string(out)) != "update" {
		t.Fatalf("expected pulled commit message 'update', got %q", strings.TrimSpace(string(out)))
	}
}

func TestFetchAndPullFailsWithoutUpstream(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	repoPath := filepath.Join(dir, "repo")

	runCmd(t, dir, "git", "init", "--bare", remotePath)
	runCmd(t, dir, "git", "clone", remotePath, seedPath)
	runCmd(t, seedPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, seedPath, "git", "config", "user.name", "tester")
	runCmd(t, seedPath, "git", "checkout", "-b", "main")
	writeFile(t, filepath.Join(seedPath, "README.md"), "init\n")
	runCmd(t, seedPath, "git", "add", "README.md")
	runCmd(t, seedPath, "git", "commit", "-m", "init")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "main")
	runCmd(t, dir, "git", "--git-dir", remotePath, "symbolic-ref", "HEAD", "refs/heads/main")

	runCmd(t, dir, "git", "clone", remotePath, repoPath)
	runCmd(t, repoPath, "git", "checkout", "-b", "feature/no-upstream")

	client := NewClient(5*time.Second, filepath.Join(dir, "workspace"))
	err := client.FetchAndPull(context.Background(), repoPath, "")
	if err == nil {
		t.Fatal("expected error for branch without upstream")
	}
	if !strings.Contains(err.Error(), "не настроен upstream") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGitRespectsCanceledParentContext(t *testing.T) {
	t.Parallel()

	client := NewClient(5*time.Second, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.runGit(ctx, "", "version")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !strings.Contains(err.Error(), "отменен") {
		t.Fatalf("expected cancellation error text, got %v", err)
	}
}

func TestLockForPathWaitCanBeCanceledByContext(t *testing.T) {
	t.Parallel()

	client := NewClient(5*time.Second, t.TempDir())
	path := filepath.Join(t.TempDir(), "repo")

	unlock, err := client.lockForPath(context.Background(), path)
	if err != nil {
		t.Fatalf("acquire initial lock: %v", err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = client.lockForPath(ctx, path)
	if err == nil {
		t.Fatal("expected lock wait cancellation error")
	}
	if !strings.Contains(err.Error(), "ожидание блокировки отменено") {
		t.Fatalf("unexpected lock wait error: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("lock wait cancellation took too long: %s", time.Since(start))
	}
}

func runCmd(t *testing.T, workdir string, command string, args ...string) {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s\n%v", command, args, string(out), err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func TestValidateRepoRootRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	clientDir := t.TempDir()
	// Используем существующую директорию, но с отмененным контекстом
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := validateRepoRoot(ctx, clientDir)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got: %v", err)
	}
}

func TestManagedRepoPathDeterministic(t *testing.T) {
	t.Parallel()

	c := NewClient(time.Second, t.TempDir())
	a := c.ManagedRepoPath("demo", "https://github.com/AgelxNash/go-repo-orchestrator.git")
	b := c.ManagedRepoPath("demo", "https://github.com/AgelxNash/go-repo-orchestrator.git")
	if a == "" || a != b {
		t.Fatalf("expected stable non-empty path, got %q vs %q", a, b)
	}
}

func TestManagedRepoPathDifferentURLsDifferentKeys(t *testing.T) {
	t.Parallel()

	c := NewClient(time.Second, t.TempDir())
	a := c.ManagedRepoPath("demo", "https://github.com/owner/repo-A.git")
	b := c.ManagedRepoPath("demo", "https://github.com/owner/repo-B.git")
	if a == b {
		t.Fatalf("expected different keys for different urls, got %q", a)
	}
}

func TestManagedRepoPathEmptyNameFallback(t *testing.T) {
	t.Parallel()

	c := NewClient(time.Second, t.TempDir())
	got := c.ManagedRepoPath("   ", "https://github.com/owner/repo.git")
	if got == "" {
		t.Fatal("expected non-empty fallback path")
	}
}

func TestResolveRepoPathPathSourceResolvesAbsolute(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "src")
	runCmd(t, dir, "git", "init", repoPath)

	c := NewClient(time.Second, dir)
	got, err := c.ResolveRepoPath(context.Background(), "name", "", repoPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	absWant, _ := filepath.Abs(repoPath)
	if got != absWant {
		t.Fatalf("expected absolute %q, got %q", absWant, got)
	}
}

func TestResolveRepoPathRequiresURLOrPath(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	c := NewClient(time.Second, t.TempDir())
	if _, err := c.ResolveRepoPath(context.Background(), "name", "", ""); err == nil {
		t.Fatal("expected error for empty url and path")
	}
}

func TestCurrentBranchAndDirtyStats(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	runCmd(t, dir, "git", "init", repo)
	runCmd(t, repo, "git", "config", "user.email", "test@example.com")
	runCmd(t, repo, "git", "config", "user.name", "tester")
	runCmd(t, repo, "git", "config", "commit.gpgsign", "false")
	runCmd(t, repo, "git", "checkout", "-b", "feature/test")
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	runCmd(t, repo, "git", "add", "README.md")
	runCmd(t, repo, "git", "commit", "-m", "init")
	// Untracked файл появляется после коммита — git status --porcelain
	// покажет его как ??, что увидит наш `GetDirtyStats`.
	writeFile(t, filepath.Join(repo, "UNTRACKED.md"), "u\n")

	c := NewClient(time.Second, dir)
	branch, err := c.CurrentBranch(context.Background(), repo)
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if branch != "feature/test" {
		t.Fatalf("expected feature/test, got %q", branch)
	}

	stats, err := c.GetDirtyStats(context.Background(), repo)
	if err != nil {
		t.Fatalf("dirty stats: %v", err)
	}
	if len(stats.Untracked) != 1 {
		t.Fatalf("expected 1 untracked file, got %+v", stats)
	}
}

func TestListBranchesLocalAndRemote(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	runCmd(t, dir, "git", "init", repo)
	runCmd(t, repo, "git", "config", "user.email", "test@example.com")
	runCmd(t, repo, "git", "config", "user.name", "tester")
	runCmd(t, repo, "git", "config", "commit.gpgsign", "false")
	runCmd(t, repo, "git", "checkout", "-b", "feature/local")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n")
	runCmd(t, repo, "git", "add", "a.txt")
	runCmd(t, repo, "git", "commit", "-m", "a")
	runCmd(t, repo, "git", "checkout", "-b", "feature/remote")
	runCmd(t, repo, "git", "checkout", "feature/local")
	// create remote via local bare clone
	bare := filepath.Join(dir, "bare.git")
	runCmd(t, dir, "git", "clone", "--bare", repo, bare)
	runCmd(t, repo, "git", "remote", "add", "origin", bare)
	runCmd(t, repo, "git", "push", "-u", "origin", "feature/local")
	runCmd(t, repo, "git", "push", "origin", "feature/remote")

	c := NewClient(time.Second, dir)
	branches, err := c.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}

	var hasLocalFeature, hasRemoteFeature bool
	for _, b := range branches {
		if b.Name == "feature/local" && b.Scope == model.BranchScopeLocal {
			hasLocalFeature = true
		}
		if strings.HasPrefix(b.QualifiedName, "origin/feature/") {
			hasRemoteFeature = true
		}
	}
	if !hasLocalFeature {
		t.Fatalf("expected local feature/local in %+v", branches)
	}
	if !hasRemoteFeature {
		t.Fatalf("expected remote-tracking branches in %+v", branches)
	}
}
