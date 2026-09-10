package usecase

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/git"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/testenv"
)

func TestGenerateScriptFlowWithManagedClone(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	remotePath := filepath.Join(dir, "remote.git")
	seedPath := filepath.Join(dir, "seed")
	stateDir := filepath.Join(dir, "state")
	workspaceDir := filepath.Join(stateDir, "workspace")

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

	runCmd(t, seedPath, "git", "checkout", "-b", "feature/old")
	writeFile(t, filepath.Join(seedPath, "feature.txt"), "old\n")
	runCmd(t, seedPath, "git", "add", "feature.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "feature old")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "feature/old")
	runCmd(t, seedPath, "git", "checkout", "main")
	runCmd(t, seedPath, "git", "merge", "--no-ff", "feature/old", "-m", "merge feature old")
	runCmd(t, seedPath, "git", "push", "origin", "main")

	runCmd(t, seedPath, "git", "checkout", "-b", "feature/unmerged")
	writeFile(t, filepath.Join(seedPath, "feature-unmerged.txt"), "unmerged\n")
	runCmd(t, seedPath, "git", "add", "feature-unmerged.txt")
	runCmd(t, seedPath, "git", "commit", "-m", "feature unmerged")
	runCmd(t, seedPath, "git", "push", "-u", "origin", "feature/unmerged")
	runCmd(t, seedPath, "git", "checkout", "main")

	repoCfg := config.RepoConfig{
		Name: "sample",
		URL:  remotePath,
		Branch: config.Branch{
			Keep: []string{"^(main|master)$"},
		},
	}

	g := git.NewClient(10*time.Second, workspaceDir)
	uc := NewCleaner(g)

	rb, err := uc.LoadRepoBranches(context.Background(), repoCfg)
	if err != nil {
		t.Fatalf("load branches first time: %v", err)
	}
	if rb.RepoPath == "" {
		t.Fatalf("managed repo path must be set")
	}
	if rb.DefaultBranch != "main" {
		t.Fatalf("expected default branch main, got %q", rb.DefaultBranch)
	}

	mergedBranch, found := findBranch(rb.Branches, "feature/old")
	if !found {
		t.Fatalf("expected branch feature/old")
	}
	if mergedBranch.MergeStatus != model.MergeStatusMerged {
		t.Fatalf("expected merged status for feature/old, got %s", mergedBranch.MergeStatus)
	}

	unmergedBranch, found := findBranch(rb.Branches, "feature/unmerged")
	if !found {
		t.Fatalf("expected branch feature/unmerged")
	}
	if unmergedBranch.MergeStatus != model.MergeStatusUnmerged {
		t.Fatalf("expected unmerged status for feature/unmerged, got %s", unmergedBranch.MergeStatus)
	}

	result, err := uc.GenerateDeleteScript(repoCfg, rb.RepoPath, []model.BranchInfo{mergedBranch, unmergedBranch}, model.ScriptFormatSH)
	if err != nil {
		t.Fatalf("generate script: %v", err)
	}
	if result.ScriptPath == "" {
		t.Fatalf("script path must be set")
	}

	raw, err := os.ReadFile(result.ScriptPath)
	if err != nil {
		t.Fatalf("read generated script: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "git branch -d 'feature/old'") {
		t.Fatalf("expected safe delete command for merged branch, got: %s", body)
	}
	if !strings.Contains(body, "git branch -D 'feature/unmerged'") {
		t.Fatalf("expected force delete command for unmerged branch, got: %s", body)
	}
}

func TestLoadRepoBranchesWithLocalPathSource(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary is required")
	}

	dir := t.TempDir()
	repoPath := filepath.Join(dir, "local-repo")
	stateDir := filepath.Join(dir, "state")

	runCmd(t, dir, "git", "init", repoPath)
	runCmd(t, repoPath, "git", "config", "user.email", "test@example.com")
	runCmd(t, repoPath, "git", "config", "user.name", "tester")
	writeFile(t, filepath.Join(repoPath, "README.md"), "init\n")
	runCmd(t, repoPath, "git", "add", "README.md")
	runCmd(t, repoPath, "git", "commit", "-m", "init")

	runCmd(t, repoPath, "git", "checkout", "-b", "feature/local")
	writeFile(t, filepath.Join(repoPath, "feature.txt"), "local\n")
	runCmd(t, repoPath, "git", "add", "feature.txt")
	runCmd(t, repoPath, "git", "commit", "-m", "local feature")
	runCmd(t, repoPath, "git", "checkout", "master")

	repoCfg := config.RepoConfig{
		Name: "local",
		Path: repoPath,
		Branch: config.Branch{
			Keep: []string{"^(main|master)$"},
		},
	}

	g := git.NewClient(10*time.Second, filepath.Join(stateDir, "workspace"))
	uc := NewCleaner(g)

	rb, err := uc.LoadRepoBranches(context.Background(), repoCfg)
	if err != nil {
		t.Fatalf("load local path branches: %v", err)
	}
	if rb.RepoSource != "path" {
		t.Fatalf("expected path source, got %s", rb.RepoSource)
	}
	if rb.RepoPath != repoPath {
		t.Fatalf("expected repo path %s, got %s", repoPath, rb.RepoPath)
	}
	if !containsBranch(rb.Branches, "feature/local") {
		t.Fatalf("expected local branch in list")
	}
}

func containsBranch(branches []model.BranchInfo, name string) bool {
	_, found := findBranch(branches, name)
	return found
}

func findBranch(branches []model.BranchInfo, name string) (model.BranchInfo, bool) {
	for _, br := range branches {
		if br.Name == name {
			return br, true
		}
	}

	return model.BranchInfo{}, false
}

// runCmd выполняет команду в workdir с очищенным от GIT_* окружением
// (см. testenv.GitSanitizedEnv) и падает с выводом при ошибке.
func runCmd(t *testing.T, workdir string, command string, args ...string) {
	t.Helper()
	cmd := exec.Command(command, args...)
	cmd.Dir = workdir
	cmd.Env = testenv.GitSanitizedEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s\n%v", command, args, string(out), err)
	}
}

// writeFile записывает файл с тестовым содержимым.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

// TestRunCmdIgnoresForeignGitDir проверяет, что runCmd выполняет git строго в
// переданном workdir даже при установленной посторонней GIT_DIR: git-команда
// обязана резолвить репозиторий в workdir, а не по унаследованному env
// (регрессия issue #62 — GIT_DIR из pre-push хука worktree).
func TestRunCmdIgnoresForeignGitDir(t *testing.T) {
	dir := t.TempDir()

	runCmd(t, dir, "git", "init", "-q", ".")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "foreign-git-dir"))

	runCmd(t, dir, "sh", "-c", "git rev-parse --absolute-git-dir > gitdir.txt")

	got, err := os.ReadFile(filepath.Join(dir, "gitdir.txt"))
	if err != nil {
		t.Fatalf("read gitdir.txt: %v", err)
	}
	if want := filepath.Join(dir, ".git"); strings.TrimSpace(string(got)) != want {
		t.Fatalf("expected git dir %q, got %q", want, strings.TrimSpace(string(got)))
	}
}
