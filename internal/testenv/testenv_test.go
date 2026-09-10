package testenv

import (
	"os"
	"strings"
	"testing"
)

func TestGitSanitizedEnvRemovesAllGitVariables(t *testing.T) {
	// t.Setenv несовместим с t.Parallel: тест меняет окружение процесса.
	t.Setenv("GIT_DIR", "/tmp/some-foreign-git-dir")
	t.Setenv("GIT_WORK_TREE", "/tmp/some-foreign-worktree")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.bare'='true'")

	env := GitSanitizedEnv()
	for _, entry := range env {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "GIT_") {
			t.Fatalf("expected no GIT_* variables after sanitization, got %q", entry)
		}
	}
	if len(env) == 0 {
		t.Fatal("sanitized env must keep non-git variables")
	}
	if os.Getenv("GIT_DIR") == "" {
		t.Fatal("sanitization must not mutate the process environment itself")
	}
}
