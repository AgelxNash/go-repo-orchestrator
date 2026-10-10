package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func writeStatusTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runStatus(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := NewRootCommand("dev", "none", "unknown", zap.NewNop())
	cmd.SetOut(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func createStatusTestRepo(t *testing.T, dir, branch string, makeDirty bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runShell(t, dir, fmt.Sprintf("git init -q -b %s .", branch))
	runShell(t, dir, "git config user.email test@example.com")
	runShell(t, dir, "git config user.name tester")
	runShell(t, dir, "git commit -q --allow-empty -m init")
	if makeDirty {
		dirtyFile := filepath.Join(dir, "dirty.txt")
		if err := os.WriteFile(dirtyFile, []byte("uncommitted change"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStatusCommandMissingConfig(t *testing.T) {
	t.Parallel()

	_, err := runStatus(t, "status")
	if err == nil {
		t.Fatal("expected missing --config error")
	}
	if !strings.Contains(err.Error(), "параметр --config обязателен") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusCommandUnexpectedArgs(t *testing.T) {
	t.Parallel()

	cfgPath := writeStatusTestConfig(t, "repos: []\n")
	_, err := runStatus(t, "status", "foo", "bar", "--config", cfgPath)
	if err == nil {
		t.Fatal("expected unexpected args error")
	}
	if !strings.Contains(err.Error(), "неожиданные аргументы: foo bar") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusCommandInvalidFilter(t *testing.T) {
	t.Parallel()

	cfgPath := writeStatusTestConfig(t, "repos: []\n")
	_, err := runStatus(t, "status", "--filter", "invalid_value", "--config", cfgPath)
	if err == nil {
		t.Fatal("expected invalid filter error")
	}
	expected := `недопустимое значение фильтра: "invalid_value" (допустимы: all, dirty, clean, error)`
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got: %v", expected, err)
	}
}

func TestStatusCommandConsoleTableOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cleanDir := filepath.Join(root, "repo-clean")
	dirtyDir := filepath.Join(root, "repo-dirty")

	createStatusTestRepo(t, cleanDir, "main", false)
	createStatusTestRepo(t, dirtyDir, "dev", true)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-clean
    path: %s
  - name: repo-dirty
    path: %s
`, cleanDir, dirtyDir)
	cfgPath := writeStatusTestConfig(t, cfgBody)

	rendered, err := runStatus(t, "status", "--config", cfgPath)
	if err != nil {
		t.Fatalf("unexpected status command error: %v", err)
	}

	for _, fragment := range []string{
		"РЕПОЗИТОРИЙ", "ВЕТКА", "СОСТОЯНИЕ", "СИНХРОНИЗАЦИЯ", "ОШИБКА",
		"repo-clean", "main", "чистый",
		"repo-dirty", "dev", "с изменениями",
		"Всего: 2 | Чистых: 1 | С изменениями: 1 | Ошибок: 0",
	} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, rendered)
		}
	}
}

func TestStatusCommandJSONOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cleanDir := filepath.Join(root, "repo-clean")
	dirtyDir := filepath.Join(root, "repo-dirty")

	createStatusTestRepo(t, cleanDir, "main", false)
	createStatusTestRepo(t, dirtyDir, "feature", true)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-clean
    path: %s
  - name: repo-dirty
    path: %s
`, cleanDir, dirtyDir)
	cfgPath := writeStatusTestConfig(t, cfgBody)

	rendered, err := runStatus(t, "status", "--json", "--config", cfgPath)
	if err != nil {
		t.Fatalf("unexpected status command error: %v", err)
	}

	var report model.StatusReport
	if err := json.Unmarshal([]byte(rendered), &report); err != nil {
		t.Fatalf("failed to parse json output: %v\noutput:\n%s", err, rendered)
	}

	if report.Total != 2 {
		t.Errorf("expected Total 2, got %d", report.Total)
	}
	if report.Clean != 1 {
		t.Errorf("expected Clean 1, got %d", report.Clean)
	}
	if report.Dirty != 1 {
		t.Errorf("expected Dirty 1, got %d", report.Dirty)
	}
	if report.Errors != 0 {
		t.Errorf("expected Errors 0, got %d", report.Errors)
	}
	if len(report.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(report.Repos))
	}

	r0 := report.Repos[0]
	if r0.Name != "repo-clean" || r0.Branch != "main" || r0.Dirty || r0.SyncWarning != "" || r0.Error != "" {
		t.Errorf("unexpected repo-clean DTO: %+v", r0)
	}

	r1 := report.Repos[1]
	if r1.Name != "repo-dirty" || r1.Branch != "feature" || !r1.Dirty || r1.SyncWarning != "" || r1.Error != "" {
		t.Errorf("unexpected repo-dirty DTO: %+v", r1)
	}

	// Проверяем, что dirty: false сериализуется (не omitempty), а пустые sync_warning и error отсутствуют
	if !strings.Contains(rendered, `"dirty": false`) {
		t.Error("expected 'dirty: false' in JSON output")
	}
	if strings.Contains(rendered, `"sync_warning"`) {
		t.Error("empty sync_warning should be omitted from JSON")
	}
	if strings.Contains(rendered, `"error"`) {
		t.Error("empty error should be omitted from JSON")
	}
}

func TestStatusCommandFilterScenarios(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cleanDir := filepath.Join(root, "repo-clean")
	dirtyDir := filepath.Join(root, "repo-dirty")
	missingDir := filepath.Join(root, "repo-missing")

	createStatusTestRepo(t, cleanDir, "main", false)
	createStatusTestRepo(t, dirtyDir, "dev", true)

	cfgBody := fmt.Sprintf(`repos:
  - name: repo-clean
    path: %s
  - name: repo-dirty
    path: %s
  - name: repo-err
    path: %s
`, cleanDir, dirtyDir, missingDir)
	cfgPath := writeStatusTestConfig(t, cfgBody)

	// Filter: all
	{
		rendered, err := runStatus(t, "status", "--filter", "all", "--ignore-errors", "--json", "--config", cfgPath)
		if err != nil {
			t.Fatalf("filter all failed: %v", err)
		}
		var report model.StatusReport
		if err := json.Unmarshal([]byte(rendered), &report); err != nil {
			t.Fatal(err)
		}
		if report.Total != 3 || report.Clean != 1 || report.Dirty != 1 || report.Errors != 1 {
			t.Errorf("filter all: unexpected counters %+v", report)
		}
		if len(report.Repos) != 3 {
			t.Errorf("filter all: expected 3 repos, got %d", len(report.Repos))
		}
	}

	// Filter: dirty
	{
		rendered, err := runStatus(t, "status", "--filter", "dirty", "--ignore-errors", "--json", "--config", cfgPath)
		if err != nil {
			t.Fatalf("filter dirty failed: %v", err)
		}
		var report model.StatusReport
		if err := json.Unmarshal([]byte(rendered), &report); err != nil {
			t.Fatal(err)
		}
		if report.Total != 3 || report.Clean != 1 || report.Dirty != 1 || report.Errors != 1 {
			t.Errorf("filter dirty: counters must reflect all repos: %+v", report)
		}
		if len(report.Repos) != 1 || report.Repos[0].Name != "repo-dirty" {
			t.Errorf("filter dirty: expected only repo-dirty, got %+v", report.Repos)
		}
	}

	// Filter: clean
	{
		rendered, err := runStatus(t, "status", "--filter", "clean", "--ignore-errors", "--json", "--config", cfgPath)
		if err != nil {
			t.Fatalf("filter clean failed: %v", err)
		}
		var report model.StatusReport
		if err := json.Unmarshal([]byte(rendered), &report); err != nil {
			t.Fatal(err)
		}
		if report.Total != 3 {
			t.Errorf("filter clean: counters must reflect all repos: %+v", report)
		}
		if len(report.Repos) != 1 || report.Repos[0].Name != "repo-clean" {
			t.Errorf("filter clean: expected only repo-clean, got %+v", report.Repos)
		}
	}

	// Filter: error
	{
		rendered, err := runStatus(t, "status", "--filter", "error", "--ignore-errors", "--json", "--config", cfgPath)
		if err != nil {
			t.Fatalf("filter error failed: %v", err)
		}
		var report model.StatusReport
		if err := json.Unmarshal([]byte(rendered), &report); err != nil {
			t.Fatal(err)
		}
		if report.Total != 3 {
			t.Errorf("filter error: counters must reflect all repos: %+v", report)
		}
		if len(report.Repos) != 1 || report.Repos[0].Name != "repo-err" {
			t.Errorf("filter error: expected only repo-err, got %+v", report.Repos)
		}
	}

	// Filter: error when no errors exist — verify repos is [] not null
	{
		cleanOnlyCfg := fmt.Sprintf(`repos:
  - name: repo-clean
    path: %s
`, cleanDir)
		cleanOnlyPath := writeStatusTestConfig(t, cleanOnlyCfg)
		rendered, err := runStatus(t, "status", "--filter", "error", "--json", "--config", cleanOnlyPath)
		if err != nil {
			t.Fatalf("filter error cleanOnly failed: %v", err)
		}
		if !strings.Contains(rendered, `"repos": []`) {
			t.Errorf("expected empty repos list to serialize as '[]', got:\n%s", rendered)
		}
	}
}

func TestStatusCommandErrorsAndIgnoreErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missingDir := filepath.Join(root, "non-existent-repo")

	cfgBody := fmt.Sprintf(`repos:
  - name: broken-repo
    path: %s
`, missingDir)
	cfgPath := writeStatusTestConfig(t, cfgBody)

	// Без --ignore-errors: должна быть ошибка
	_, err := runStatus(t, "status", "--config", cfgPath)
	if err == nil {
		t.Fatal("expected error when repo is broken and --ignore-errors is not set")
	}
	expected := "обнаружены ошибки в 1 репозиториях"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got: %v", expected, err)
	}

	// С --ignore-errors: ошибки выполнения быть не должно
	rendered, err := runStatus(t, "status", "--ignore-errors", "--config", cfgPath)
	if err != nil {
		t.Fatalf("expected no error with --ignore-errors, got: %v", err)
	}
	if !strings.Contains(rendered, "broken-repo") || !strings.Contains(rendered, "Ошибок: 1") {
		t.Fatalf("expected rendered table with error repo and counter 1, got:\n%s", rendered)
	}
}

func TestStatusCommandStreamIsolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	missingDir := filepath.Join(root, "broken")

	cfgBody := fmt.Sprintf(`repos:
  - name: broken-repo
    path: %s
`, missingDir)
	cfgPath := writeStatusTestConfig(t, cfgBody)

	rendered, err := runStatus(t, "status", "--json", "--config", cfgPath)
	// Команда должна вернуть ошибку из-за broken repo
	if err == nil {
		t.Fatal("expected error without --ignore-errors")
	}

	// В stdout должен быть валидный JSON, который успешно парсится
	var report model.StatusReport
	if jsonErr := json.Unmarshal([]byte(rendered), &report); jsonErr != nil {
		t.Fatalf("stdout does not contain valid JSON despite stream isolation: %v\noutput:\n%s", jsonErr, rendered)
	}

	if report.Errors != 1 {
		t.Errorf("expected Errors 1, got %d", report.Errors)
	}
	if len(report.Repos) != 1 || report.Repos[0].Error == "" {
		t.Errorf("expected repo with error filled, got %+v", report.Repos)
	}
}

func TestStatusCommandDeterministicOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	numRepos := 12
	var repoConfigs []string
	expectedNames := make([]string, numRepos)

	for i := range numRepos {
		name := fmt.Sprintf("repo-%02d", i)
		expectedNames[i] = name
		dir := filepath.Join(root, name)
		createStatusTestRepo(t, dir, "main", i%2 == 0)
		repoConfigs = append(repoConfigs, fmt.Sprintf("  - name: %s\n    path: %s", name, dir))
	}

	cfgBody := "repos:\n" + strings.Join(repoConfigs, "\n") + "\n"
	cfgPath := writeStatusTestConfig(t, cfgBody)

	rendered, err := runStatus(t, "status", "--json", "--config", cfgPath)
	if err != nil {
		t.Fatalf("status command error: %v", err)
	}

	var report model.StatusReport
	if err := json.Unmarshal([]byte(rendered), &report); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}

	if len(report.Repos) != numRepos {
		t.Fatalf("expected %d repos, got %d", numRepos, len(report.Repos))
	}

	for i, repo := range report.Repos {
		if repo.Name != expectedNames[i] {
			t.Errorf("index %d: expected name %q, got %q (deterministic order violated)", i, expectedNames[i], repo.Name)
		}
	}
}
