package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func writeExecTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runExec(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func createExecTestRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runShell(t, dir, "git init -q -b main .")
	runShell(t, dir, "git config user.email test@example.com")
	runShell(t, dir, "git config user.name tester")
	runShell(t, dir, "git commit -q --allow-empty -m init")
}

func TestExecCommandMissingCommandAfterDash(t *testing.T) {
	t.Parallel()

	cfgPath := writeExecTestConfig(t, "repos: []\n")

	testCases := []struct {
		name      string
		args      []string
		errSubstr string
	}{
		{
			name:      "no dash and no command",
			args:      []string{"exec", "--config", cfgPath},
			errSubstr: "команда не указана: укажите команду после разделителя '--'",
		},
		{
			name:      "dash without command",
			args:      []string{"exec", "--config", cfgPath, "--"},
			errSubstr: "команда не указана: укажите команду после разделителя '--'",
		},
		{
			name:      "command without dash separator",
			args:      []string{"exec", "--config", cfgPath, "git", "status"},
			errSubstr: "команда не указана: укажите команду после разделителя '--'",
		},
		{
			name:      "unexpected args before dash",
			args:      []string{"exec", "--config", cfgPath, "extra", "--", "git", "status"},
			errSubstr: "неожиданные аргументы перед '--': extra",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := runExec(t, tc.args...)
			if err == nil {
				t.Fatalf("expected error for case %q, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("expected error containing %q, got: %v", tc.errSubstr, err)
			}
		})
	}
}

func TestExecCommandMissingConfig(t *testing.T) {
	t.Parallel()

	_, _, err := runExec(t, "exec", "--", "git", "status")
	if err == nil {
		t.Fatal("expected missing --config error")
	}
	if !strings.Contains(err.Error(), "параметр --config обязателен") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecCommandInvalidFilter(t *testing.T) {
	t.Parallel()

	cfgPath := writeExecTestConfig(t, "repos: []\n")
	_, _, err := runExec(t, "exec", "--filter", "[invalid-regex", "--config", cfgPath, "--", "echo", "hello")
	if err == nil {
		t.Fatal("expected invalid filter regex error")
	}
	if !strings.Contains(err.Error(), "невалидный regex для --filter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecCommandInvalidParallel(t *testing.T) {
	t.Parallel()

	cfgPath := writeExecTestConfig(t, "repos: []\n")

	for _, invalidParallel := range []string{"0", "-1", "-5"} {
		_, _, err := runExec(t, "exec", "--parallel", invalidParallel, "--config", cfgPath, "--", "echo", "hello")
		if err == nil {
			t.Fatalf("expected invalid parallel error for %s", invalidParallel)
		}
		if !strings.Contains(err.Error(), "недопустимое значение --parallel") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestExecCommandInvalidTimeout(t *testing.T) {
	t.Parallel()

	cfgPath := writeExecTestConfig(t, "repos: []\n")

	for _, invalidTimeout := range []string{"0s", "-1s"} {
		_, _, err := runExec(t, "exec", "--timeout", invalidTimeout, "--config", cfgPath, "--", "echo", "hello")
		if err == nil {
			t.Fatalf("expected invalid timeout error for %s", invalidTimeout)
		}
		if !strings.Contains(err.Error(), "недопустимое значение --timeout") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestExecCommandSuccessConsole(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")

	createExecTestRepo(t, repoA)
	createExecTestRepo(t, repoB)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-a
    path: %s
  - name: repo-b
    path: %s
`, repoA, repoB)
	cfgPath := writeExecTestConfig(t, cfgBody)

	stdout, _, err := runExec(t, "exec", "--config", cfgPath, "--", "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("unexpected exec command error: %v", err)
	}

	for _, expected := range []string{
		"[repo-a]",
		"true",
		"[repo-b]",
		"Всего: 2 | Успешно: 2 | С ошибками: 0",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("expected console output to contain %q, got:\n%s", expected, stdout)
		}
	}
}

func TestExecCommandFilter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	apiDir := filepath.Join(root, "backend-api")
	authDir := filepath.Join(root, "backend-auth")
	uiDir := filepath.Join(root, "frontend-ui")

	createExecTestRepo(t, apiDir)
	createExecTestRepo(t, authDir)
	createExecTestRepo(t, uiDir)

	cfgBody := fmt.Sprintf(`repos:
  - name: backend-api
    path: %s
  - name: backend-auth
    path: %s
  - name: frontend-ui
    path: %s
`, apiDir, authDir, uiDir)
	cfgPath := writeExecTestConfig(t, cfgBody)

	// Фильтр по ^backend-.*
	stdout, _, err := runExec(t, "exec", "--filter", "^backend-.*", "--json", "--config", cfgPath, "--", "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("unexpected exec error: %v", err)
	}

	var report model.ExecReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("failed to parse json: %v\noutput:\n%s", err, stdout)
	}

	if report.Total != 2 {
		t.Fatalf("expected total 2, got %d", report.Total)
	}
	if report.Success != 2 {
		t.Fatalf("expected success 2, got %d", report.Success)
	}
	if report.Failed != 0 {
		t.Fatalf("expected failed 0, got %d", report.Failed)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}
	if report.Results[0].Repo != "backend-api" || report.Results[1].Repo != "backend-auth" {
		t.Fatalf("unexpected repo names in results: %+v", report.Results)
	}

	// Фильтр, под который ничего не попадает
	stdoutEmpty, _, err := runExec(t, "exec", "--filter", "^unknown-.*", "--json", "--config", cfgPath, "--", "echo", "test")
	if err != nil {
		t.Fatalf("unexpected exec error with empty filter: %v", err)
	}

	var emptyReport model.ExecReport
	if err := json.Unmarshal([]byte(stdoutEmpty), &emptyReport); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}
	if emptyReport.Total != 0 || emptyReport.Success != 0 || emptyReport.Failed != 0 {
		t.Fatalf("expected empty report counters, got %+v", emptyReport)
	}
	if !strings.Contains(stdoutEmpty, `"results": []`) {
		t.Fatalf("expected '\"results\": []' in json, got:\n%s", stdoutEmpty)
	}
}

func TestExecCommandFailedExitCode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")

	createExecTestRepo(t, repoA)
	createExecTestRepo(t, repoB)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-a
    path: %s
  - name: repo-b
    path: %s
`, repoA, repoB)
	cfgPath := writeExecTestConfig(t, cfgBody)

	// Запускаем команду, возвращающую код ошибки (checkout несуществующей ветки)
	stdout, _, err := runExec(t, "exec", "--config", cfgPath, "--", "git", "checkout", "non-existent-branch-xyz")
	if err == nil {
		t.Fatal("expected exec command to fail when child command returns non-zero exit code")
	}

	if !strings.Contains(err.Error(), "выполнение завершилось с ошибками в 2 репозиториях") {
		t.Fatalf("unexpected error message: %v", err)
	}

	for _, expected := range []string{
		"[repo-a]",
		"Ошибка:",
		"[repo-b]",
		"Всего: 2 | Успешно: 0 | С ошибками: 2",
	} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("expected console output to contain %q, got:\n%s", expected, stdout)
		}
	}
}

func TestExecCommandJSONOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")

	createExecTestRepo(t, repoA)
	createExecTestRepo(t, repoB)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-a
    path: %s
  - name: repo-b
    path: %s
`, repoA, repoB)
	cfgPath := writeExecTestConfig(t, cfgBody)

	stdout, stderr, err := runExec(t, "exec", "--json", "--config", cfgPath, "--", "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("unexpected exec command error: %v", err)
	}

	if stderr != "" {
		t.Fatalf("expected empty stderr on success, got: %s", stderr)
	}

	var report model.ExecReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("failed to parse json output: %v\noutput:\n%s", err, stdout)
	}

	if report.Total != 2 {
		t.Errorf("expected total 2, got %d", report.Total)
	}
	if report.Success != 2 {
		t.Errorf("expected success 2, got %d", report.Success)
	}
	if report.Failed != 0 {
		t.Errorf("expected failed 0, got %d", report.Failed)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}

	for i, res := range report.Results {
		if res.Repo == "" {
			t.Errorf("result %d: expected non-empty repo", i)
		}
		if res.Path == "" {
			t.Errorf("result %d: expected non-empty path", i)
		}
		if res.ExitCode != 0 {
			t.Errorf("result %d: expected exit_code 0, got %d", i, res.ExitCode)
		}
		if strings.TrimSpace(res.Stdout) != "true" {
			t.Errorf("result %d: expected stdout 'true', got %q", i, res.Stdout)
		}
		if res.DurationMS <= 0 {
			t.Errorf("result %d: expected duration_ms > 0, got %d", i, res.DurationMS)
		}
		if res.Error != "" {
			t.Errorf("result %d: expected empty error, got %q", i, res.Error)
		}
	}

	// Поле error должно отсутствовать при успешном выполнении благодаря omitempty
	if strings.Contains(stdout, `"error"`) {
		t.Errorf("expected 'error' field to be omitted on success, got:\n%s", stdout)
	}
}

func TestExecCommandFailFast(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoBroken := filepath.Join(root, "repo-broken")
	repoSlow := filepath.Join(root, "repo-slow")

	// repoBroken не создаем на диске (путь не существует), чтобы команда упала сразу
	createExecTestRepo(t, repoSlow)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-broken
    path: %s
  - name: repo-slow
    path: %s
`, repoBroken, repoSlow)
	cfgPath := writeExecTestConfig(t, cfgBody)

	// Запускаем с --parallel 1 и --fail-fast.
	// Первый репозиторий падает мгновенно, второй не должен запускаться.
	start := time.Now()
	stdout, _, err := runExec(t, "exec", "--parallel", "1", "--fail-fast", "--json", "--config", cfgPath, "--", "sleep", "3")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected exec error with fail-fast")
	}

	// Тест должен завершиться быстро (намного быстрее чем sleep 3)
	if duration >= 2500*time.Millisecond {
		t.Fatalf("fail-fast did not cancel execution in time: took %v", duration)
	}

	var report model.ExecReport
	if jsonErr := json.Unmarshal([]byte(stdout), &report); jsonErr != nil {
		t.Fatalf("failed to parse json output: %v\noutput:\n%s", jsonErr, stdout)
	}

	if report.Total != 2 {
		t.Fatalf("expected total 2, got %d", report.Total)
	}
	if report.Failed != 2 {
		t.Fatalf("expected failed 2, got %d", report.Failed)
	}
	if len(report.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(report.Results))
	}

	// Первый упал из-за ошибки валидации/отсутствия директории
	if !strings.Contains(report.Results[0].Error, "no such file or directory") &&
		!strings.Contains(report.Results[0].Error, "ошибка валидации репозитория") &&
		!strings.Contains(report.Results[0].Error, "путь не является git-репозиторием") {
		t.Errorf("expected broken repo error, got: %s", report.Results[0].Error)
	}

	// Второй был отменен из-за fail-fast
	if !strings.Contains(report.Results[1].Error, "--fail-fast") {
		t.Errorf("expected fail-fast cancellation error in second repo, got: %s", report.Results[1].Error)
	}
}

func TestExecCommandStreamIsolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoDir := filepath.Join(root, "repo-fail")
	createExecTestRepo(t, repoDir)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-fail
    path: %s
`, repoDir)
	cfgPath := writeExecTestConfig(t, cfgBody)

	stdout, stderr, err := runExec(t, "exec", "--json", "--config", cfgPath, "--", "git", "checkout", "nonexistent-ref")
	if err == nil {
		t.Fatal("expected error due to non-zero exit code")
	}

	// В stdout должен быть строго валидный JSON
	var report model.ExecReport
	if jsonErr := json.Unmarshal([]byte(stdout), &report); jsonErr != nil {
		t.Fatalf("stdout contains invalid JSON: %v\noutput:\n%s", jsonErr, stdout)
	}

	if report.Total != 1 || report.Failed != 1 {
		t.Fatalf("unexpected report counters: %+v", report)
	}
	if report.Results[0].ExitCode == 0 {
		t.Errorf("expected non-zero exit code, got %d", report.Results[0].ExitCode)
	}
	if report.Results[0].Error == "" {
		t.Error("expected non-empty error field in DTO")
	}

	// stderr не должен портить stdout
	_ = stderr
}

func TestExecCommandDeterministicOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	numRepos := 10
	var repoConfigs []string
	expectedNames := make([]string, numRepos)

	for i := range numRepos {
		name := fmt.Sprintf("repo-%02d", i)
		expectedNames[i] = name
		dir := filepath.Join(root, name)
		createExecTestRepo(t, dir)
		repoConfigs = append(repoConfigs, fmt.Sprintf("  - name: %s\n    path: %s", name, dir))
	}

	cfgBody := "repos:\n" + strings.Join(repoConfigs, "\n") + "\n"
	cfgPath := writeExecTestConfig(t, cfgBody)

	stdout, _, err := runExec(t, "exec", "--parallel", "4", "--json", "--config", cfgPath, "--", "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("exec command error: %v", err)
	}

	var report model.ExecReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	if len(report.Results) != numRepos {
		t.Fatalf("expected %d results, got %d", numRepos, len(report.Results))
	}

	for i, res := range report.Results {
		if res.Repo != expectedNames[i] {
			t.Errorf("index %d: expected name %q, got %q (deterministic order violated)", i, expectedNames[i], res.Repo)
		}
	}
}

func TestExecCommandStdinDiscarded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoDir := filepath.Join(root, "repo-stdin")
	createExecTestRepo(t, repoDir)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-stdin
    path: %s
`, repoDir)
	cfgPath := writeExecTestConfig(t, cfgBody)

	// Команда cat без аргументов ожидает ввод из stdin.
	// Если stdin перенаправлен в os.DevNull/empty reader, cat завершится немедленно со статусом 0.
	start := time.Now()
	_, _, err := runExec(t, "exec", "--timeout", "5s", "--config", cfgPath, "--", "cat")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("cat command failed: %v", err)
	}

	if duration >= 4*time.Second {
		t.Fatalf("cat took too long (%v), stdin may not be disconnected", duration)
	}
}

func TestExecCommandTimeout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repoDir := filepath.Join(root, "repo-timeout")
	createExecTestRepo(t, repoDir)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-timeout
    path: %s
`, repoDir)
	cfgPath := writeExecTestConfig(t, cfgBody)

	// Запускаем sleep 2 с таймаутом 200ms
	start := time.Now()
	stdout, _, err := runExec(t, "exec", "--timeout", "200ms", "--json", "--config", cfgPath, "--", "sleep", "2")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}

	if duration >= 1800*time.Millisecond {
		t.Fatalf("timeout was not respected: took %v", duration)
	}

	var report model.ExecReport
	if jsonErr := json.Unmarshal([]byte(stdout), &report); jsonErr != nil {
		t.Fatalf("failed to parse json: %v\noutput:\n%s", jsonErr, stdout)
	}

	if report.Failed != 1 {
		t.Fatalf("expected failed 1, got %d", report.Failed)
	}
	if !strings.Contains(report.Results[0].Error, "превышен таймаут выполнения") && !strings.Contains(report.Results[0].Error, "signal: killed") {
		t.Fatalf("expected timeout error message, got: %s", report.Results[0].Error)
	}
}

func TestExecCommandRejectsNonGitDirectory(t *testing.T) {
	t.Parallel()

	// Обычная папка без .git
	nonGitDir := t.TempDir()

	cfgBody := fmt.Sprintf(`repos:
  - name: not-a-repo
    path: %s
`, nonGitDir)
	cfgPath := writeExecTestConfig(t, cfgBody)

	stdout, _, err := runExec(t, "exec", "--json", "--config", cfgPath, "--", "ls")
	if err == nil {
		t.Fatal("expected error when target repo is not a git directory")
	}

	var report model.ExecReport
	if jsonErr := json.Unmarshal([]byte(stdout), &report); jsonErr != nil {
		t.Fatalf("failed to parse json: %v\noutput:\n%s", jsonErr, stdout)
	}

	if report.Failed != 1 {
		t.Fatalf("expected failed 1, got %d", report.Failed)
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(report.Results))
	}
	if !strings.Contains(report.Results[0].Error, "ошибка валидации репозитория") && !strings.Contains(report.Results[0].Error, "путь не является git-репозиторием") {
		t.Fatalf("expected git validation error, got: %s", report.Results[0].Error)
	}
}
