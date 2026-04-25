package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/viper"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

func TestAdjustedViewportOffsetKeepsCursorVisible(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		cursor int
		total  int
		rows   int
		want   int
	}{
		{name: "empty list", offset: 3, cursor: 0, total: 0, rows: 5, want: 0},
		{name: "fits in viewport", offset: 2, cursor: 1, total: 3, rows: 5, want: 0},
		{name: "scroll down when cursor below", offset: 0, cursor: 6, total: 20, rows: 5, want: 2},
		{name: "scroll up when cursor above", offset: 7, cursor: 3, total: 20, rows: 5, want: 3},
		{name: "clamp to max offset", offset: 100, cursor: 19, total: 20, rows: 5, want: 15},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := adjustedViewportOffset(tc.offset, tc.cursor, tc.total, tc.rows)
			if got != tc.want {
				t.Fatalf("expected offset %d, got %d", tc.want, got)
			}
		})
	}
}

func TestRepoListStateShowsErrorForBrokenRepo(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "broken-repo", Path: "/tmp/repo"}},
	}, nil, false)

	m.repoStats["broken-repo"] = model.RepoStat{
		CurrentBranch: "-",
		LoadError:     "resolve head: reference not found",
		Loaded:        true,
	}

	branch, status := m.repoListState("broken-repo")
	if branch != "-" {
		t.Fatalf("expected branch '-', got %q", branch)
	}
	if status != "Ошибка" {
		t.Fatalf("expected status 'Ошибка', got %q", status)
	}
}

func TestCanActivateBranchesBlockedByRepoError(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "broken-repo", Path: "/tmp/repo"}},
	}, nil, false)

	m.activeRepo = model.RepoBranches{RepoName: "broken-repo"}
	m.repoStats["broken-repo"] = model.RepoStat{
		LoadError: "resolve head: reference not found",
		Loaded:    true,
	}

	ok, _ := m.canActivateBranches()
	if ok {
		t.Fatal("expected branches panel to be blocked for repo with load error")
	}
}

func TestRepoListStateShowsCurrentBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.repoStats["repo-a"] = model.RepoStat{
		CurrentBranch: "feature/ABC-123",
		Loaded:        true,
	}

	branch, status := m.repoListState("repo-a")
	if branch != "feature/ABC-123" {
		t.Fatalf("expected current branch to be visible, got %q", branch)
	}
	if status != "Чисто" {
		t.Fatalf("expected status 'Чисто', got %q", status)
	}
}

func TestRepoListStateShowsWarningForUnsyncedRepo(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/repo-a.git"}},
	}, nil, false)

	m.repoStats["repo-a"] = model.RepoStat{
		CurrentBranch: "main",
		SyncWarning:   "таймаут обращения к удаленному репозиторию",
		Loaded:        true,
	}

	branch, status := m.repoListState("repo-a")
	if branch != "main" {
		t.Fatalf("expected current branch main, got %q", branch)
	}
	if status != "Предупреждение" {
		t.Fatalf("expected warning status, got %q", status)
	}
}

func TestCanActivateBranchesAllowsSyncWarning(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/repo-a.git"}},
	}, nil, false)

	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{
		CurrentBranch: "main",
		SyncWarning:   "таймаут обращения к удаленному репозиторию",
		Loaded:        true,
	}

	ok, reason := m.canActivateBranches()
	if !ok {
		t.Fatalf("expected warning state to allow branches panel, got reason: %q", reason)
	}
}

func TestRepoListStateDoesNotLeakActiveRepoBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", Path: "/tmp/repo-a"},
			{Name: "repo-b", Path: "/tmp/repo-b"},
		},
	}, nil, false)

	m.repoIdx = 0
	m.activeRepo = model.RepoBranches{
		RepoName:      "repo-a",
		CurrentBranch: "main",
	}

	branch, status := m.repoListState("repo-b")
	if branch != "-" {
		t.Fatalf("expected repo-b branch '-', got %q", branch)
	}
	if status != "Не загружен" {
		t.Fatalf("expected repo-b status 'Не загружен', got %q", status)
	}
}

func TestRepoLoadErrorDoesNotLeakToOtherReposContext(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-bad", URL: "https://example.invalid/repo-bad.git"},
			{Name: "repo-good", Path: "/tmp/repo-good"},
		},
	}, nil, false)

	m.repoLoadReq["repo-bad"] = 1
	updated, _ := m.Update(branchesLoadedMsg{
		requestID: 1,
		repoName:  "repo-bad",
		err:       fmt.Errorf("connection refused"),
	})
	m = updated.(Model)

	m.repoStats["repo-good"] = model.RepoStat{CurrentBranch: "main", Loaded: true}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	ctx := m.viewContextLine(240)

	if strings.Contains(ctx, "соединение отклонено") {
		t.Fatalf("expected repo-bad error to stay isolated, got context: %q", ctx)
	}
	if !strings.Contains(ctx, "repo-good") {
		t.Fatalf("expected repo-good context, got %q", ctx)
	}
	if !strings.Contains(ctx, "Статус: Чисто") {
		t.Fatalf("expected clean status for repo-good, got %q", ctx)
	}
}

func TestBackgroundRepoLoadErrorDoesNotForceFocusToRepos(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", Path: "/tmp/repo-a"},
			{Name: "repo-b", URL: "https://example.invalid/repo-b.git"},
		},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.repoLoadReq["repo-b"] = 1

	updated, _ := m.Update(branchesLoadedMsg{requestID: 1, repoName: "repo-b", err: fmt.Errorf("connection refused")})
	next := updated.(Model)

	if next.focus != focusBranches {
		t.Fatalf("expected focus to stay on branches, got %v", next.focus)
	}
}

func TestF2TogglesInfoPanel(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	if !m.showInfo {
		t.Fatal("expected info panel to be enabled by default")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next := updated.(Model)
	if next.showInfo {
		t.Fatal("expected F2 to disable info panel")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF2})
	next = updated.(Model)
	if !next.showInfo {
		t.Fatal("expected second F2 to enable info panel")
	}
}

func TestF8OpensGenerateConfirm(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{Name: "feature/test"}},
	}
	m.selected["repo-a"] = map[string]bool{"feature/test": true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF8})
	next := updated.(Model)
	if next.confirmType != confirmGenerate {
		t.Fatalf("expected confirmGenerate after F8, got %v", next.confirmType)
	}
}

func TestF2TogglesInfoInBranchesTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.showInfo = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	next := updated.(Model)

	if next.showInfo {
		t.Fatal("expected F2 to disable info panel in branches tab")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF2})
	next = updated.(Model)
	if !next.showInfo {
		t.Fatal("expected second F2 to enable info panel in branches tab")
	}

	if !strings.Contains(next.statusLine, "Инфо-панель") {
		t.Fatalf("expected info panel status message, got %q", next.statusLine)
	}
}

func TestF4SwitchesScopeInBranchesTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF4})
	next := updated.(Model)
	if next.branchScope != branchScopeLocal {
		t.Fatalf("expected local scope after first F4, got %v", next.branchScope)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF4})
	next = updated.(Model)
	if next.branchScope != branchScopeRemote {
		t.Fatalf("expected remote scope after second F4, got %v", next.branchScope)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF4})
	next = updated.(Model)
	if next.branchScope != branchScopeAll {
		t.Fatalf("expected all scope after third F4, got %v", next.branchScope)
	}
}

func TestF4IgnoredInReposTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF4})
	next := updated.(Model)

	if next.branchScope != branchScopeAll {
		t.Fatalf("expected scope to stay unchanged in repos tab, got %v", next.branchScope)
	}

	if strings.Contains(next.statusLine, "Scope веток") {
		t.Fatalf("did not expect scope status in repos tab, got %q", next.statusLine)
	}
}

func TestF9TogglesHiddenProtectedOnlyInBranchesTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF9})
	next := updated.(Model)
	if next.hideProtected {
		t.Fatal("expected F9 to be ignored in repos tab")
	}

	next.focus = focusBranches
	next.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	next.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF9})
	next = updated.(Model)
	if !next.hideProtected {
		t.Fatal("expected F9 to enable hidden protected in branches tab")
	}
	if !strings.Contains(next.statusLine, "Скрытое") {
		t.Fatalf("expected hidden toggle status line, got %q", next.statusLine)
	}
}

func TestEnterOpensBranchesTabFromRepos(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := updated.(Model)

	if next.focus != focusBranches {
		t.Fatalf("expected focusBranches after Enter, got %v", next.focus)
	}
}

func TestTabIsDisabled(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	next := updated.(Model)

	if next.focus != focusRepos {
		t.Fatalf("expected focusRepos after Tab, got %v", next.focus)
	}
}

func TestF3EnablesSearchMode(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	if m.searchMode {
		t.Fatal("expected search mode to be disabled by default")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF3})
	next := updated.(Model)

	if !next.searchMode {
		t.Fatal("expected search mode to be enabled by F3")
	}
}

func TestF5StartsGlobalRescanInReposTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", Path: "/tmp/repo-a"},
			{Name: "repo-b", URL: "https://example.com/repo-b.git"},
		},
	}, nil, false)

	m.focus = focusRepos

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after F5 refresh")
	}
	if !next.loadingSelectedRepo() {
		t.Fatal("expected loading=true after F5 refresh")
	}
	if !next.refreshLocked || !next.refreshAll {
		t.Fatal("expected global refresh lock after F5 in repos tab")
	}
	if !next.refreshPending["repo-a"] || !next.refreshPending["repo-b"] {
		t.Fatalf("expected refresh pending for all repos, got: %#v", next.refreshPending)
	}
	if !strings.Contains(next.statusLine, "Пересканирование всех репозиториев") {
		t.Fatalf("expected global rescan status line, got %q", next.statusLine)
	}
}

func TestF7StartsFetchPullInReposTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after F7 fetch+pull")
	}
	if !next.loadingSelectedRepo() {
		t.Fatal("expected loading=true after F7 fetch+pull")
	}
	if !strings.Contains(next.statusLine, "fetch + pull") {
		t.Fatalf("expected fetch+pull status line, got %q", next.statusLine)
	}
}

func TestF7HotkeyShownInReposBar(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	bar := m.viewHotkeyBar(240)
	if !strings.Contains(bar, "Fetch+Pull") {
		t.Fatalf("expected repos hotkey bar to include F7 Fetch+Pull, got %q", bar)
	}
}

func TestF5HotkeyShownAsRescanInReposBar(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	bar := m.viewHotkeyBar(240)
	if !strings.Contains(bar, "Рескан") {
		t.Fatalf("expected repos hotkey bar to include F5 as rescan, got %q", bar)
	}
}

func TestGlobalRescanBlocksNavigationUntilAllReposComplete(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", Path: "/tmp/repo-a"},
			{Name: "repo-b", Path: "/tmp/repo-b"},
		},
	}, nil, false)

	m.focus = focusRepos
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	next := updated.(Model)

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	blocked := updated.(Model)
	if blocked.repoIdx != 0 {
		t.Fatalf("expected repo index to stay at 0 while refresh is locked, got %d", blocked.repoIdx)
	}

	updated, _ = blocked.Update(branchesLoadedMsg{
		requestID: blocked.repoLoadReq["repo-a"],
		repoName:  "repo-a",
		rb:        model.RepoBranches{RepoName: "repo-a"},
	})
	stillLocked := updated.(Model)

	updated, _ = stillLocked.Update(tea.KeyMsg{Type: tea.KeyDown})
	stillBlocked := updated.(Model)
	if stillBlocked.repoIdx != 0 {
		t.Fatalf("expected repo index to stay at 0 until all repos complete, got %d", stillBlocked.repoIdx)
	}

	updated, _ = stillBlocked.Update(repoStatLoadedMsg{repoName: "repo-b", stat: model.RepoStat{Loaded: true}})
	unlocked := updated.(Model)

	updated, _ = unlocked.Update(tea.KeyMsg{Type: tea.KeyDown})
	after := updated.(Model)
	if after.repoIdx != 1 {
		t.Fatalf("expected repo index to move after refresh completion, got %d", after.repoIdx)
	}
}

func TestStartupLoadingBlocksInteractiveInputAndShowsInitScreen(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", URL: "https://example.com/a.git"},
			{Name: "repo-b", URL: "https://example.com/b.git"},
		},
	}, nil, false)

	m.width = 120
	m.height = 30

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	if !next.startupLoading {
		t.Fatal("expected startup loading mode to be active")
	}
	if !strings.Contains(next.View(), "Инициализация репозиториев") {
		t.Fatalf("expected startup init screen, got %q", next.View())
	}

	next.focus = focusRepos
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	blocked := updated.(Model)
	if blocked.focus != focusRepos {
		t.Fatalf("expected Enter to be ignored during startup loading, got focus=%v", blocked.focus)
	}
}

func TestStartupLoadingFinishesAfterInitialMessages(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", URL: "https://example.com/a.git"},
			{Name: "repo-b", URL: "https://example.com/b.git"},
		},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-a"], repoName: "repo-a", startup: true})
	next = updated.(Model)
	if !next.startupLoading {
		t.Fatal("expected startup loading to remain active until all tasks finish")
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-b"], repoName: "repo-b", startup: true})
	next = updated.(Model)
	if next.startupLoading {
		t.Fatal("expected startup loading to be disabled after all startup tasks complete")
	}
}

func TestStartupStatusShowsURLProgressAndDoesNotSwitchToRepoStatusUntilComplete(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", URL: "https://example.com/a.git"},
			{Name: "repo-b", URL: "https://example.com/b.git"},
		},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	if !strings.Contains(next.statusLine, "Первичная синхронизация URL-репозиториев: 0/2") {
		t.Fatalf("expected startup progress 0/2, got %q", next.statusLine)
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-a"], repoName: "repo-a", startup: true, rb: model.RepoBranches{RepoName: "repo-a"}})
	next = updated.(Model)

	if !strings.Contains(next.statusLine, "Первичная синхронизация URL-репозиториев: 1/2") {
		t.Fatalf("expected startup progress 1/2, got %q", next.statusLine)
	}
	if strings.Contains(next.statusLine, "синхронизирован") {
		t.Fatalf("did not expect per-repo sync status during startup preload, got %q", next.statusLine)
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-b"], repoName: "repo-b", startup: true, rb: model.RepoBranches{RepoName: "repo-b"}})
	next = updated.(Model)

	if next.startupLoading {
		t.Fatal("expected startup loading to be disabled after all startup tasks complete")
	}
	if !strings.Contains(next.statusLine, "Первичная синхронизация URL-репозиториев завершена: 2/2") {
		t.Fatalf("expected startup completion summary, got %q", next.statusLine)
	}
}

func TestStartupLoadsOpensourceRepoBranchesEvenWhenNotSelected(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-path", Path: "/tmp/repo-path"},
			{Name: "repo-open", URL: "https://example.com/open.git", Path: "/tmp/repo-open"},
		},
	}, nil, false)

	m.repoIdx = 0
	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	if !next.repoLoading["repo-open"] {
		t.Fatal("expected opensource repo to start branch loading during startup")
	}
	if next.repoLoadReq["repo-open"] == 0 {
		t.Fatal("expected branch load request id for opensource repo")
	}
}

func TestStartupDefersPlaywrightStartUntilStartupTask(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	called := false
	m.SetPlaywrightStartupStartFn(func() error {
		called = true
		return nil
	})

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	if called {
		t.Fatal("expected playwright start to be deferred until async startup task execution")
	}
	if !next.startupLoading {
		t.Fatal("expected startup loading mode to stay active")
	}
	if next.startupPending != 2 {
		t.Fatalf("expected 2 startup tasks (repo + playwright), got %d", next.startupPending)
	}
}

func TestStartupPlaywrightFailureSetsWarningAndDoesNotBlockTUI(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	m.SetPlaywrightStartupStartFn(func() error { return nil })

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(playwrightStartupCompletedMsg{err: fmt.Errorf("driver bootstrap failed")})
	next = updated.(Model)

	if next.startupWarn == "" {
		t.Fatal("expected startup warning after playwright failure")
	}
	if !strings.Contains(next.startupWarn, "браузер Playwright не запущен") {
		t.Fatalf("unexpected startup warning: %q", next.startupWarn)
	}
	if !next.startupLoading {
		t.Fatal("expected startup loading to continue until repo preload finishes")
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-a"], repoName: "repo-a", startup: true, rb: model.RepoBranches{RepoName: "repo-a"}})
	next = updated.(Model)

	if next.startupLoading {
		t.Fatal("expected startup loading to finish after remaining preload tasks")
	}
}

func TestStartupPlaywrightSuccessMarksReadyInProgressScreen(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	m.SetPlaywrightStartupStartFn(func() error { return nil })

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(playwrightStartupCompletedMsg{})
	next = updated.(Model)

	if next.startupPlaywrightState != startupPlaywrightReady {
		t.Fatalf("expected playwright ready state, got %d", next.startupPlaywrightState)
	}
	if strings.Contains(next.viewStartupScreen(), "Playwright: запуск") {
		t.Fatalf("expected startup screen to stop showing running state, got %q", next.viewStartupScreen())
	}
}

func TestStartupTracksOverallProgressAcrossAllTasks(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-path", Path: "/tmp/repo-path"},
			{Name: "repo-url", URL: "https://example.com/repo-url.git"},
		},
	}, nil, false)

	m.SetPlaywrightStartupStartFn(func() error { return nil })

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	if next.startupTotal != 3 {
		t.Fatalf("expected startupTotal=3 (path+url+playwright), got %d", next.startupTotal)
	}
	if next.startupPending != 3 {
		t.Fatalf("expected startupPending=3 at start, got %d", next.startupPending)
	}
	if !strings.Contains(next.viewStartupScreen(), "Задачи startup") {
		t.Fatalf("expected startup screen to show overall progress line, got %q", next.viewStartupScreen())
	}

	updated, _ = next.Update(playwrightStartupCompletedMsg{})
	next = updated.(Model)
	if got := next.startupTotal - next.startupPending; got != 1 {
		t.Fatalf("expected completed startup tasks=1 after playwright, got %d", got)
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-url"], repoName: "repo-url", startup: true, rb: model.RepoBranches{RepoName: "repo-url"}})
	next = updated.(Model)
	if got := next.startupTotal - next.startupPending; got != 2 {
		t.Fatalf("expected completed startup tasks=2 after url repo, got %d", got)
	}

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-path"], repoName: "repo-path", startup: true, rb: model.RepoBranches{RepoName: "repo-path"}})
	next = updated.(Model)
	if got := next.startupTotal - next.startupPending; got != 3 {
		t.Fatalf("expected completed startup tasks=3 after all tasks, got %d", got)
	}
}

func TestStartupViewShowsRepoAndStageWithoutAbstractHints(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	m.width = 120
	m.height = 30
	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	next.setStartupStage("repo-a", "загрузка веток")
	view := next.viewStartupScreen()

	if !strings.Contains(view, "Репозитории:") {
		t.Fatalf("expected startup view to show repo progress, got %q", view)
	}
	if !strings.Contains(view, "Текущий репозиторий: repo-a") {
		t.Fatalf("expected startup view to show current repo, got %q", view)
	}
	if !strings.Contains(view, "Этап: загрузка веток") {
		t.Fatalf("expected startup view to show current stage, got %q", view)
	}
	if strings.Contains(view, "Heartbeat:") {
		t.Fatalf("did not expect heartbeat line in startup view, got %q", view)
	}
}

func TestStartupViewShowsElapsedAndStageElapsed(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	m.width = 120
	m.height = 30
	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	start := time.Now().Add(-14 * time.Second)
	next.setStartupStage("repo-a", "проверка Jira")
	next.startupStartedAt = start
	next.startupStageStartedAt = start

	updated, _ = next.Update(startupTimerTickMsg{at: start.Add(14 * time.Second)})
	next = updated.(Model)
	view := next.viewStartupScreen()

	if !strings.Contains(view, "Прошло: 14с") {
		t.Fatalf("expected startup elapsed timer in view, got %q", view)
	}
	if !strings.Contains(view, "Этап: проверка Jira") {
		t.Fatalf("expected stage line in view, got %q", view)
	}
	if !strings.Contains(view, "(14с)") {
		t.Fatalf("expected stage elapsed timer in view, got %q", view)
	}
}

func TestStartupViewDoesNotShowLongRunningHint(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)
	view := next.viewStartupScreen()
	if strings.Contains(view, "Startup занимает больше обычного") {
		t.Fatalf("did not expect long-running hint in startup view, got %q", view)
	}
}

func TestStartupViewDoesNotShowLastActivity(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	m.width = 120
	m.height = 30
	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	view := next.viewStartupScreen()

	if strings.Contains(view, "Последняя активность") {
		t.Fatalf("did not expect last activity line in startup view, got %q", view)
	}
}

func TestStartupEventAfterCompletionDoesNotResumeCycle(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-a"], repoName: "repo-a", startup: true, rb: model.RepoBranches{RepoName: "repo-a"}})
	next = updated.(Model)

	if next.startupLoading {
		t.Fatal("expected startup loading to be disabled after completion")
	}
	if next.startupCurrentOp != "Инициализация завершена" {
		t.Fatalf("expected final startup operation, got %q", next.startupCurrentOp)
	}

	finalOp := next.startupCurrentOp
	updated, cmd := next.Update(startupLogMsg{text: "[GIT] repo-a: позднее событие"})
	after := updated.(Model)

	if cmd != nil {
		t.Fatal("expected no follow-up command after startup completion")
	}
	if after.startupLoading {
		t.Fatal("expected startup loading to remain disabled")
	}
	if after.startupCurrentOp != finalOp {
		t.Fatalf("expected final startup operation to stay unchanged, got %q", after.startupCurrentOp)
	}
}

func TestStartupLateMessagesDoNotOverrideFinalCurrentOperation(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(branchesLoadedMsg{requestID: next.repoLoadReq["repo-a"], repoName: "repo-a", startup: true, rb: model.RepoBranches{RepoName: "repo-a"}})
	next = updated.(Model)

	if next.startupCurrentOp != "Инициализация завершена" {
		t.Fatalf("expected final startup operation, got %q", next.startupCurrentOp)
	}

	updated, _ = next.Update(repoStatLoadedMsg{repoName: "repo-a", startup: true, stat: model.RepoStat{Loaded: true}})
	after := updated.(Model)

	if after.startupCurrentOp != "Инициализация завершена" {
		t.Fatalf("expected late startup message not to override final operation, got %q", after.startupCurrentOp)
	}
}

func TestStartupEventLogIncludesJiraBatchProgress(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(branchesLoadedMsg{
		requestID: next.repoLoadReq["repo-a"],
		repoName:  "repo-a",
		startup:   true,
		rb:        model.RepoBranches{RepoName: "repo-a", Branches: make([]model.BranchInfo, 3)},
		jiraBatchProgress: []usecase.RepoLoadProgress{
			{BatchIndex: 1, BatchTotal: 2, Processed: 2, Total: 3},
			{BatchIndex: 2, BatchTotal: 2, Processed: 3, Total: 3},
		},
	})
	next = updated.(Model)

	log := strings.Join(next.eventLog, "\n")
	if !strings.Contains(log, "[JIRA] repo-a: проверена пачка 1/2") {
		t.Fatalf("expected first batch progress in startup log, got %q", log)
	}
	if !strings.Contains(log, "[JIRA] repo-a: обработано 3/3 веток") {
		t.Fatalf("expected cumulative branch progress in startup log, got %q", log)
	}
}

func TestStartupEventLogSkipsDuplicatedBatchProgressWhenStreamed(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	updated, _ := m.Update(initialLoadMsg{})
	next := updated.(Model)

	updated, _ = next.Update(repoLoadJiraProgressMsg{
		repoName: "repo-a",
		startup:  true,
		progress: usecase.RepoLoadProgress{BatchIndex: 1, BatchTotal: 2, Processed: 2, Total: 3},
	})
	next = updated.(Model)

	updated, _ = next.Update(branchesLoadedMsg{
		requestID:            next.repoLoadReq["repo-a"],
		repoName:             "repo-a",
		startup:              true,
		rb:                   model.RepoBranches{RepoName: "repo-a", Branches: make([]model.BranchInfo, 3)},
		jiraProgressStreamed: true,
		jiraBatchProgress: []usecase.RepoLoadProgress{
			{BatchIndex: 1, BatchTotal: 2, Processed: 2, Total: 3},
			{BatchIndex: 2, BatchTotal: 2, Processed: 3, Total: 3},
		},
	})
	next = updated.(Model)

	countBatchLogs := 0
	for _, line := range next.eventLog {
		if strings.Contains(line, "проверена пачка") {
			countBatchLogs++
		}
	}
	if countBatchLogs != 1 {
		t.Fatalf("expected one streamed batch log without duplication, got %d; log=%q", countBatchLogs, strings.Join(next.eventLog, "\n"))
	}
}

func TestRepoLoadLogIncludesJiraBatchSummaryOutsideStartup(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/a.git"}},
	}, nil, false)

	reqID := 1
	m.repoLoadReq["repo-a"] = reqID
	m.repoLoading["repo-a"] = true
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}

	updated, _ := m.Update(branchesLoadedMsg{
		requestID: reqID,
		repoName:  "repo-a",
		startup:   false,
		rb:        model.RepoBranches{RepoName: "repo-a", CurrentBranch: "main"},
		jiraBatchProgress: []usecase.RepoLoadProgress{
			{BatchIndex: 1, BatchTotal: 2, Processed: 20, Total: 87},
			{BatchIndex: 2, BatchTotal: 2, Processed: 40, Total: 87},
		},
	})
	next := updated.(Model)

	log := strings.Join(next.eventLog, "\n")
	if !strings.Contains(log, "[JIRA] repo-a: обновлено 40/87") {
		t.Fatalf("expected jira batch summary in repo load log, got %q", log)
	}
}

func TestRepoCursorMoveDoesNotAutoRefresh(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-a", URL: "https://example.com/a.git"},
			{Name: "repo-b", URL: "https://example.com/b.git"},
		},
	}, nil, false)

	m.focus = focusRepos

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := updated.(Model)

	if cmd != nil {
		t.Fatal("expected no refresh command on cursor move")
	}
	if next.loadingSelectedRepo() {
		t.Fatal("expected selected repo to stay non-loading on cursor move")
	}
}

func TestTopMenuEnterHintIsContextual(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusRepos
	reposMenu := m.viewTopMenu(120)
	if !strings.Contains(reposMenu, "Enter - открыть репозиторий") {
		t.Fatalf("expected repos hint in top menu, got %q", reposMenu)
	}

	m.focus = focusBranches
	branchesMenu := m.viewTopMenu(120)
	if !strings.Contains(branchesMenu, "Enter - checkout ветки") {
		t.Fatalf("expected branches hint in top menu, got %q", branchesMenu)
	}
	if strings.Contains(branchesMenu, "Enter - открыть репозиторий") {
		t.Fatalf("did not expect repos hint in branches menu, got %q", branchesMenu)
	}
}

func TestBranchesPanelLoadHintShowsF5(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-b"}

	panel := m.viewBranchesPanel(100, 20)
	if !strings.Contains(panel, "Нажмите F5 или r для загрузки") {
		t.Fatalf("expected F5 load hint in branches panel, got %q", panel)
	}
}

func TestScopeLabelVisibleInHotkeyBarAndTopMenu(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	bar := m.viewHotkeyBar(240)
	if !strings.Contains(bar, "Scope: all") {
		t.Fatalf("expected default scope label in hotkey bar, got %q", bar)
	}

	menu := m.viewTopMenu(240)
	if !strings.Contains(menu, "F4 Scope: all") {
		t.Fatalf("expected scope label in top menu, got %q", menu)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF4})
	next := updated.(Model)

	bar = next.viewHotkeyBar(240)
	if !strings.Contains(bar, "Scope: local") {
		t.Fatalf("expected switched scope label in hotkey bar, got %q", bar)
	}

	menu = next.viewTopMenu(240)
	if !strings.Contains(menu, "F4 Scope: local") {
		t.Fatalf("expected switched scope label in top menu, got %q", menu)
	}
}

func TestVisibleBranchesRespectScopeFilter(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/local", Scope: model.BranchScopeLocal},
			{Name: "feature/remote", Scope: model.BranchScopeRemote, RemoteName: "origin", QualifiedName: "origin/feature/remote"},
		},
	}

	m.branchScope = branchScopeLocal
	if got := len(m.visibleBranches()); got != 1 {
		t.Fatalf("expected 1 local branch, got %d", got)
	}

	m.branchScope = branchScopeRemote
	if got := len(m.visibleBranches()); got != 1 {
		t.Fatalf("expected 1 remote branch, got %d", got)
	}

	m.branchScope = branchScopeAll
	if got := len(m.visibleBranches()); got != 2 {
		t.Fatalf("expected 2 branches for all scope, got %d", got)
	}
}

func TestSpaceSelectsRemoteBranchForDeleteScript(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeRemote
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:          "feature/remote",
			QualifiedName: "origin/feature/remote",
			FullRef:       "refs/remotes/origin/feature/remote",
			Key:           "refs/remotes/origin/feature/remote",
			Scope:         model.BranchScopeRemote,
			RemoteName:    "origin",
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	next := updated.(Model)

	selected := next.selected[next.activeRepo.RepoName]
	if !selected["refs/remotes/origin/feature/remote"] {
		t.Fatal("expected remote branch to be selected")
	}
}

func TestInsertSelectsLocalBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeLocal
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:  "feature/local",
			Key:   "refs/heads/feature/local",
			Scope: model.BranchScopeLocal,
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyInsert})
	next := updated.(Model)

	selected := next.selected[next.activeRepo.RepoName]
	if !selected["refs/heads/feature/local"] {
		t.Fatal("expected Insert to select local branch")
	}
}

func TestF7StartsRemoteLocalCopyFlow(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:          "feature/remote",
			QualifiedName: "origin/feature/remote",
			Scope:         model.BranchScopeRemote,
			RemoteName:    "origin",
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command for remote local-copy flow")
	}
	if !next.repoLoading["repo-a"] {
		t.Fatal("expected repo loading=true during local copy flow")
	}
	if !strings.Contains(next.statusLine, "Создание локальной копии") {
		t.Fatalf("expected local-copy status line, got %q", next.statusLine)
	}
}

func TestF7DisabledForLocalBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:  "feature/local",
			Scope: model.BranchScopeLocal,
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	next := updated.(Model)

	if cmd != nil {
		t.Fatal("expected nil command for local branch on F7")
	}
	if next.repoLoading["repo-a"] {
		t.Fatal("did not expect loading for local branch on F7")
	}
	if !strings.Contains(next.statusLine, "только для удаленной ветки") {
		t.Fatalf("expected disabled status line, got %q", next.statusLine)
	}
}

func TestF7DisabledForURLRepoRemoteBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", URL: "https://example.com/repo-a.git"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:          "feature/remote",
			QualifiedName: "origin/feature/remote",
			Scope:         model.BranchScopeRemote,
			RemoteName:    "origin",
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	if m.canCreateLocalFromRemote() {
		t.Fatal("expected F7 to be unavailable for URL-only repo")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	next := updated.(Model)
	if cmd != nil {
		t.Fatal("expected nil command for URL-only repo on F7")
	}
	if !strings.Contains(next.statusLine, "только для репозиториев с path") {
		t.Fatalf("expected path-only status line, got %q", next.statusLine)
	}
}

func TestProtectedBranchIsMarkedWithStar(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:      "feature/protected",
			Scope:     model.BranchScopeLocal,
			Protected: true,
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	panel := m.viewBranchesPanel(120, 16)
	if !strings.Contains(panel, "feature/protected*") {
		t.Fatalf("expected protected marker '*' in branches panel, got %q", panel)
	}
}

func TestSelectionUsesBranchKeyForDuplicateNames(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeAll
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/same", Key: "refs/heads/feature/same", Scope: model.BranchScopeLocal},
			{Name: "feature/same", Key: "refs/remotes/origin/feature/same", Scope: model.BranchScopeRemote, RemoteName: "origin", QualifiedName: "origin/feature/same"},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	next := updated.(Model)
	selected := next.selected[next.activeRepo.RepoName]

	if !selected["refs/heads/feature/same"] {
		t.Fatal("expected local branch key to be selected")
	}
	if selected["refs/remotes/origin/feature/same"] {
		t.Fatal("did not expect remote branch key to be selected")
	}
}

func TestPlusInvertsOnlyVisibleSelectableBranches(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeAll
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/local-a", Key: "refs/heads/feature/local-a", Scope: model.BranchScopeLocal},
			{Name: "feature/local-protected", Key: "refs/heads/feature/local-protected", Scope: model.BranchScopeLocal, Protected: true},
			{Name: "feature/remote", Key: "refs/remotes/origin/feature/remote", QualifiedName: "origin/feature/remote", Scope: model.BranchScopeRemote, RemoteName: "origin"},
			{Name: "feature/local-b", Key: "refs/heads/feature/local-b", Scope: model.BranchScopeLocal},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.selected["repo-a"] = map[string]bool{
		"refs/heads/feature/local-a":         true,
		"refs/heads/feature/local-protected": true,
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	next := updated.(Model)
	selected := next.selected["repo-a"]

	if selected["refs/heads/feature/local-a"] {
		t.Fatal("expected selected local branch to be unselected after inversion")
	}
	if !selected["refs/heads/feature/local-b"] {
		t.Fatal("expected unselected local branch to be selected after inversion")
	}
	if !selected["refs/heads/feature/local-protected"] {
		t.Fatal("expected protected branch selection to stay unchanged")
	}
	if !selected["refs/remotes/origin/feature/remote"] {
		t.Fatal("expected remote branch to be selected after inversion")
	}
	if !strings.Contains(next.statusLine, "Инверсия выбора: 3") {
		t.Fatalf("expected inversion status message, got %q", next.statusLine)
	}
}

func TestNumpadPlusInvertsSelection(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeAll
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:       "feature/remote",
			Key:        "refs/remotes/origin/feature/remote",
			Scope:      model.BranchScopeRemote,
			RemoteName: "origin",
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	next := updated.(Model)
	if !next.selected["repo-a"]["refs/remotes/origin/feature/remote"] {
		t.Fatal("expected '+' inversion to select remote branch")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	next = updated.(Model)
	if next.selected["repo-a"]["refs/remotes/origin/feature/remote"] {
		t.Fatal("expected second '+' inversion to unselect remote branch")
	}
}

func TestRepoViewportScrollKeepsActiveRowVisible(t *testing.T) {
	repos := make([]config.RepoConfig, 0, 14)
	for i := 0; i < 14; i++ {
		repos = append(repos, config.RepoConfig{Name: "repo-" + string(rune('a'+i)), Path: "/tmp/repo"})
	}

	m := NewModel(&config.Config{Repos: repos}, nil, false)
	m.height = 20

	for i := 0; i < 9; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}

	indices := m.visibleRepoIndices()
	pos := m.repoVisiblePosition(indices)
	rows := m.repoViewportRows()
	if rows <= 0 {
		t.Fatalf("expected positive repo viewport rows, got %d", rows)
	}
	if pos < m.repoOffset || pos >= m.repoOffset+rows {
		t.Fatalf("active repo index %d is out of visible window [%d,%d)", pos, m.repoOffset, m.repoOffset+rows)
	}
	if m.repoOffset == 0 {
		t.Fatal("expected non-zero repo scroll offset after moving down")
	}
}

func TestBranchViewportScrollKeepsCursorVisible(t *testing.T) {
	branches := make([]model.BranchInfo, 0, 16)
	for i := 0; i < 16; i++ {
		branches = append(branches, model.BranchInfo{
			Name:         "feature/test-" + string(rune('a'+i)),
			Scope:        model.BranchScopeLocal,
			LastCommitAt: time.Now(),
		})
	}

	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)
	m.height = 20
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: branches}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	for i := 0; i < 9; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}

	visible := m.visibleBranches()
	cursor := m.currentCursor("repo-a")
	rows := m.branchViewportRows()
	offset := m.branchOffset["repo-a"]
	if rows <= 0 {
		t.Fatalf("expected positive branch viewport rows, got %d", rows)
	}
	if len(visible) == 0 {
		t.Fatal("expected non-empty branches list")
	}
	if cursor < offset || cursor >= offset+rows {
		t.Fatalf("cursor %d is out of visible window [%d,%d)", cursor, offset, offset+rows)
	}
	if offset == 0 {
		t.Fatal("expected non-zero branch scroll offset after moving down")
	}
}

func TestRepoViewportScrollMovesBackUp(t *testing.T) {
	repos := make([]config.RepoConfig, 0, 18)
	for i := 0; i < 18; i++ {
		repos = append(repos, config.RepoConfig{Name: "repo-" + string(rune('a'+i)), Path: "/tmp/repo"})
	}

	m := NewModel(&config.Config{Repos: repos}, nil, false)
	m.height = 20

	for i := 0; i < 12; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if m.repoOffset == 0 {
		t.Fatal("expected non-zero repo offset after scrolling down")
	}

	for i := 0; i < 12; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(Model)
	}

	if m.repoIdx != 0 {
		t.Fatalf("expected repo index to return to 0, got %d", m.repoIdx)
	}
	if m.repoOffset != 0 {
		t.Fatalf("expected repo offset to return to 0, got %d", m.repoOffset)
	}
}

func TestBranchViewportScrollMovesBackUp(t *testing.T) {
	branches := make([]model.BranchInfo, 0, 20)
	for i := 0; i < 20; i++ {
		branches = append(branches, model.BranchInfo{
			Name:         "feature/test-" + string(rune('a'+i)),
			Scope:        model.BranchScopeLocal,
			LastCommitAt: time.Now(),
		})
	}

	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)
	m.height = 20
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: branches}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	for i := 0; i < 12; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if m.branchOffset["repo-a"] == 0 {
		t.Fatal("expected non-zero branch offset after scrolling down")
	}

	for i := 0; i < 12; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(Model)
	}

	if got := m.currentCursor("repo-a"); got != 0 {
		t.Fatalf("expected branch cursor to return to 0, got %d", got)
	}
	if got := m.branchOffset["repo-a"]; got != 0 {
		t.Fatalf("expected branch offset to return to 0, got %d", got)
	}
}

func TestEnsureVisibleResetsOffsetWhenCursorAtStart(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)
	m.height = 20

	m.repoOffset = 5
	m.repoIdx = 0
	m.ensureRepoCursorVisible()
	if m.repoOffset != 0 {
		t.Fatalf("expected repo offset to reset to 0, got %d", m.repoOffset)
	}

	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/a", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
			{Name: "feature/b", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
			{Name: "feature/c", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
			{Name: "feature/d", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
			{Name: "feature/e", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
			{Name: "feature/f", Scope: model.BranchScopeLocal, LastCommitAt: time.Now()},
		},
	}
	m.branchCursor["repo-a"] = 0
	m.branchOffset["repo-a"] = 4
	m.ensureBranchCursorVisible("repo-a")
	if got := m.branchOffset["repo-a"]; got != 0 {
		t.Fatalf("expected branch offset to reset to 0, got %d", got)
	}
}

func TestRemoteScopeViewportStaysVisibleAfterScrollDownAndUp(t *testing.T) {
	const (
		localCount  = 8
		remoteCount = 518
	)

	branches := make([]model.BranchInfo, 0, localCount+remoteCount)
	now := time.Now()
	for i := 0; i < localCount; i++ {
		branches = append(branches, model.BranchInfo{
			Name:         fmt.Sprintf("feature/local-%03d", i),
			Scope:        model.BranchScopeLocal,
			LastCommitAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < remoteCount; i++ {
		branches = append(branches, model.BranchInfo{
			Name:          fmt.Sprintf("feature/remote-%03d", i),
			QualifiedName: fmt.Sprintf("origin/feature/remote-%03d", i),
			FullRef:       fmt.Sprintf("refs/remotes/origin/feature/remote-%03d", i),
			Key:           fmt.Sprintf("refs/remotes/origin/feature/remote-%03d", i),
			Scope:         model.BranchScopeRemote,
			RemoteName:    "origin",
			LastCommitAt:  now.Add(-time.Duration(localCount+i) * time.Minute),
		})
	}

	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "bff", Path: "/tmp/bff"}},
	}, nil, false)
	m.height = 24
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "bff", Branches: branches}
	m.repoStats["bff"] = model.RepoStat{Loaded: true}

	runScenario := func(name string, scopeSwitches int, expectedVisible int) {
		t.Helper()

		caseModel := m
		var updated tea.Model
		for i := 0; i < scopeSwitches; i++ {
			updated, _ = caseModel.Update(tea.KeyMsg{Type: tea.KeyF4})
			caseModel = updated.(Model)
		}
		if got := len(caseModel.visibleBranches()); got != expectedVisible {
			t.Fatalf("%s: expected %d visible branches, got %d", name, expectedVisible, got)
		}

		rows := caseModel.branchViewportRows()
		if rows <= 0 {
			t.Fatalf("%s: expected positive rows, got %d", name, rows)
		}
		for i := 0; i < rows+120; i++ {
			updated, _ = caseModel.Update(tea.KeyMsg{Type: tea.KeyDown})
			caseModel = updated.(Model)
		}
		if caseModel.branchOffset["bff"] == 0 {
			t.Fatalf("%s: expected non-zero offset after scrolling down", name)
		}

		contentHeight := max(8, caseModel.height-4)
		infoHeight := min(14, max(8, contentHeight/3))
		branchesHeight := max(7, contentHeight-infoHeight-1)
		movedUp := false

		for i := 0; i < rows+80; i++ {
			prevOffset := caseModel.branchOffset["bff"]
			updated, _ = caseModel.Update(tea.KeyMsg{Type: tea.KeyUp})
			caseModel = updated.(Model)
			if caseModel.branchOffset["bff"] < prevOffset {
				movedUp = true
			}

			current := caseModel.currentBranch()
			if current == nil {
				t.Fatalf("%s: expected current branch while scrolling", name)
				return // unreachable, satisfies staticcheck SA5011
			}
			panel := caseModel.viewBranchesPanel(120, branchesHeight)
			display := caseModel.branchDisplayName(*current)
			line, ok := lineContaining(panel, display)
			if !ok {
				t.Fatalf("%s: current branch %q is not visible while scrolling up (cursor=%d offset=%d)", name, display, caseModel.currentCursor("bff"), caseModel.branchOffset["bff"])
			}
			if !strings.Contains(line, ">") {
				t.Fatalf("%s: active row for branch %q has no cursor marker while scrolling up", name, display)
			}
			if got := strings.Count(line, "\x1b[0m"); got > 2 {
				t.Fatalf("%s: active row style is fragmented (%d resets) for branch %q", name, got, display)
			}
		}
		if !movedUp {
			t.Fatalf("%s: expected viewport offset to move up while scrolling", name)
		}
	}

	t.Run("remote", func(t *testing.T) {
		runScenario("remote", 2, remoteCount)
	})

	t.Run("all", func(t *testing.T) {
		runScenario("all", 0, localCount+remoteCount)
	})
}

func lineContaining(text, token string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, token) {
			return line, true
		}
	}
	return "", false
}

func TestEscFromBranchesResetsSelectionForRepo(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:  "feature/local",
			Key:   "refs/heads/feature/local",
			Scope: model.BranchScopeLocal,
		}},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.selected["repo-a"] = map[string]bool{"refs/heads/feature/local": true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.focus != focusRepos {
		t.Fatalf("expected focusRepos after Esc, got %v", next.focus)
	}
	if len(next.selected["repo-a"]) != 0 {
		t.Fatalf("expected selection reset for repo-a, got %#v", next.selected["repo-a"])
	}
}

func TestSelectBranchMovesCursorDownWhenPossible(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchScope = branchScopeLocal
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/a", Key: "refs/heads/feature/a", Scope: model.BranchScopeLocal},
			{Name: "feature/b", Key: "refs/heads/feature/b", Scope: model.BranchScopeLocal},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.branchCursor["repo-a"] = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	next := updated.(Model)

	if !next.selected["repo-a"]["refs/heads/feature/a"] {
		t.Fatal("expected first branch to be selected")
	}
	if got := next.currentCursor("repo-a"); got != 1 {
		t.Fatalf("expected cursor to move to next row, got %d", got)
	}
}

func TestF6CyclesSortModes(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	if m.repoSort != repoSortByName {
		t.Fatalf("expected default repo sort by name, got %v", m.repoSort)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF6})
	next := updated.(Model)
	if next.repoSort != repoSortByActiveBranch {
		t.Fatalf("expected repo sort by active branch, got %v", next.repoSort)
	}

	next.focus = focusBranches
	next.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	next.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	if next.branchSort != branchSortByCommitDate {
		t.Fatalf("expected branch sort by commit date, got %v", next.branchSort)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	if next.branchSort != branchSortByMergeStatus {
		t.Fatalf("expected branch sort by merge status, got %v", next.branchSort)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	if next.branchSort != branchSortByJiraStatus {
		t.Fatalf("expected branch sort by jira status, got %v", next.branchSort)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	if next.branchSort != branchSortByName {
		t.Fatalf("expected branch sort by name after cycle, got %v", next.branchSort)
	}
}

func TestRepoSortByActiveBranchInReposTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-b", Path: "/tmp/repo-b"},
			{Name: "repo-a", Path: "/tmp/repo-a"},
		},
	}, nil, false)

	m.repoStats["repo-a"] = model.RepoStat{CurrentBranch: "zzz", Loaded: true}
	m.repoStats["repo-b"] = model.RepoStat{CurrentBranch: "aaa", Loaded: true}

	if got := m.visibleRepoIndices(); len(got) != 2 || got[0] != 1 {
		t.Fatalf("expected name sort first repo-a index=1, got %v", got)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF6})
	next := updated.(Model)

	got := next.visibleRepoIndices()
	if len(got) != 2 || got[0] != 0 {
		t.Fatalf("expected branch sort first repo-b index=0, got %v", got)
	}
	if next.repoIdx != 0 {
		t.Fatalf("expected cursor to move to first visible sorted repo index=0, got %d", next.repoIdx)
	}
}

func TestStartInitialLoadsSelectsFirstRepoByActiveSort(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{
			{Name: "repo-c", Path: "/tmp/repo-c"},
			{Name: "repo-a", Path: "/tmp/repo-a"},
			{Name: "repo-b", Path: "/tmp/repo-b"},
		},
	}, nil, false)

	m.startInitialLoads()

	if m.repoIdx != 1 {
		t.Fatalf("expected startup cursor on first sorted repo index=1, got %d", m.repoIdx)
	}
}

func TestHomeNavigationKeyAliases(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want bool
	}{
		{name: "native home key type", msg: tea.KeyMsg{Type: tea.KeyHome}, want: true},
		{name: "ctrl+home key type", msg: tea.KeyMsg{Type: tea.KeyCtrlHome}, want: true},
		{name: "shift+home key type", msg: tea.KeyMsg{Type: tea.KeyShiftHome}, want: true},
		{name: "ctrl+shift+home key type", msg: tea.KeyMsg{Type: tea.KeyCtrlShiftHome}, want: true},
		{name: "ctrl+a key type", msg: tea.KeyMsg{Type: tea.KeyCtrlA}, want: true},
		{name: "alt+home alias", msg: tea.KeyMsg{Type: tea.KeyHome, Alt: true}, want: true},
		{name: "find alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("find")}, want: true},
		{name: "kp_home alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kp_home")}, want: true},
		{name: "kp-7 alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kp-7")}, want: true},
		{name: "trimmed home alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  home  ")}, want: true},
		{name: "numpad7 alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("numpad7")}, want: true},
		{name: "xterm csi home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[1~")}, want: true},
		{name: "xterm ss3 home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1bOH")}, want: true},
		{name: "rxvt home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[7~")}, want: true},
		{name: "bracket home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[H")}, want: true},
		{name: "tabby escaped home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc[1~")}, want: true},
		{name: "caret escaped home sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("^[[1~")}, want: true},
		{name: "unknown key", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("foo")}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHomeNavigationKey(tc.msg); got != tc.want {
				t.Fatalf("expected %v, got %v for key %q", tc.want, got, tc.msg.String())
			}
		})
	}
}

func TestEndNavigationKeyAliases(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want bool
	}{
		{name: "native end key type", msg: tea.KeyMsg{Type: tea.KeyEnd}, want: true},
		{name: "ctrl+end key type", msg: tea.KeyMsg{Type: tea.KeyCtrlEnd}, want: true},
		{name: "shift+end key type", msg: tea.KeyMsg{Type: tea.KeyShiftEnd}, want: true},
		{name: "ctrl+shift+end key type", msg: tea.KeyMsg{Type: tea.KeyCtrlShiftEnd}, want: true},
		{name: "ctrl+e key type", msg: tea.KeyMsg{Type: tea.KeyCtrlE}, want: true},
		{name: "alt+end alias", msg: tea.KeyMsg{Type: tea.KeyEnd, Alt: true}, want: true},
		{name: "select alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("select")}, want: true},
		{name: "kp_end alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kp_end")}, want: true},
		{name: "kp-1 alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kp-1")}, want: true},
		{name: "trimmed end alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  end  ")}, want: true},
		{name: "numpad1 alias", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("numpad1")}, want: true},
		{name: "xterm csi end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[4~")}, want: true},
		{name: "xterm ss3 end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1bOF")}, want: true},
		{name: "rxvt end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[8~")}, want: true},
		{name: "bracket end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[F")}, want: true},
		{name: "tabby escaped end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc[4~")}, want: true},
		{name: "caret escaped end sequence", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("^[[4~")}, want: true},
		{name: "unknown key", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bar")}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEndNavigationKey(tc.msg); got != tc.want {
				t.Fatalf("expected %v, got %v for key %q", tc.want, got, tc.msg.String())
			}
		})
	}
}

func TestBeginActionCancelsPreviousActionWithSameKey(t *testing.T) {
	t.Parallel()

	m := NewModel(&config.Config{Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}}}, nil, false)
	ctx1, _ := m.beginAction(actionKeyLoadRepo("repo-a"))
	ctx2, _ := m.beginAction(actionKeyLoadRepo("repo-a"))

	select {
	case <-ctx1.Done():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected previous action context to be canceled")
	}

	select {
	case <-ctx2.Done():
		t.Fatal("expected current action context to stay active")
	default:
	}
}

func TestQuitCancelsInflightActions(t *testing.T) {
	t.Parallel()

	m := NewModel(&config.Config{Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}}}, nil, false)
	ctxLoad, _ := m.beginAction(actionKeyLoadRepo("repo-a"))
	ctxStat, _ := m.beginAction(actionKeyRepoStat("repo-a"))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next := updated.(Model)

	select {
	case <-ctxLoad.Done():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected load action context to be canceled on quit")
	}

	select {
	case <-ctxStat.Done():
		// expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected stat action context to be canceled on quit")
	}

	if len(next.actionCancels) != 0 {
		t.Fatalf("expected no active action cancels after quit, got %d", len(next.actionCancels))
	}
}

func TestRepoNavigationPageKeys(t *testing.T) {
	repos := make([]config.RepoConfig, 12)
	for i := range repos {
		repos[i] = config.RepoConfig{Name: fmt.Sprintf("repo-%02d", i), Path: fmt.Sprintf("/tmp/repo-%02d", i)}
	}
	m := NewModel(&config.Config{Repos: repos}, nil, false)
	m.showInfo = false
	m.height = 14
	m.selectFirstVisibleRepo()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	next := updated.(Model)
	if next.repoIdx != 6 {
		t.Fatalf("expected PgDown to move repo cursor to index=6, got %d", next.repoIdx)
	}
	if next.repoOffset == 0 {
		t.Fatal("expected repo offset to move after PgDown")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnd})
	next = updated.(Model)
	if next.repoIdx != len(repos)-1 {
		t.Fatalf("expected End to move repo cursor to last index=%d, got %d", len(repos)-1, next.repoIdx)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyHome})
	next = updated.(Model)
	if next.repoIdx != 0 {
		t.Fatalf("expected Home to move repo cursor to first index=0, got %d", next.repoIdx)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	next = updated.(Model)
	if next.repoIdx != len(repos)-1 {
		t.Fatalf("expected Ctrl+E End fallback to move repo cursor to last index=%d, got %d", len(repos)-1, next.repoIdx)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	next = updated.(Model)
	if next.repoIdx != 0 {
		t.Fatalf("expected Ctrl+A Home fallback to move repo cursor to first index=0, got %d", next.repoIdx)
	}
}

func TestBranchNavigationPageKeys(t *testing.T) {
	branches := make([]model.BranchInfo, 12)
	for i := range branches {
		branches[i] = model.BranchInfo{
			Name:  fmt.Sprintf("feature/%02d", i),
			Key:   fmt.Sprintf("refs/heads/feature/%02d", i),
			Scope: model.BranchScopeLocal,
		}
	}

	m := NewModel(&config.Config{Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}}}, nil, false)
	m.focus = focusBranches
	m.showInfo = false
	m.height = 14
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: branches}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	next := updated.(Model)
	if got := next.currentCursor("repo-a"); got != 6 {
		t.Fatalf("expected PgDown to move branch cursor to index=6, got %d", got)
	}
	if next.branchOffset["repo-a"] == 0 {
		t.Fatal("expected branch offset to move after PgDown")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnd})
	next = updated.(Model)
	if got := next.currentCursor("repo-a"); got != len(branches)-1 {
		t.Fatalf("expected End to move branch cursor to last index=%d, got %d", len(branches)-1, got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	next = updated.(Model)
	if got := next.currentCursor("repo-a"); got != 5 {
		t.Fatalf("expected PgUp to move branch cursor up by one page to index=5, got %d", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyHome})
	next = updated.(Model)
	if got := next.currentCursor("repo-a"); got != 0 {
		t.Fatalf("expected Home to move branch cursor to first index=0, got %d", got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	next = updated.(Model)
	if got := next.currentCursor("repo-a"); got != len(branches)-1 {
		t.Fatalf("expected Ctrl+E End fallback to move branch cursor to last index=%d, got %d", len(branches)-1, got)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	next = updated.(Model)
	if got := next.currentCursor("repo-a"); got != 0 {
		t.Fatalf("expected Ctrl+A Home fallback to move branch cursor to first index=0, got %d", got)
	}
}

func TestBranchSortModesAffectVisibleOrder(t *testing.T) {
	now := time.Now()
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/b", Scope: model.BranchScopeLocal, LastCommitAt: now.Add(-time.Hour), MergeStatus: model.MergeStatusUnmerged, JiraStatus: "Open"},
			{Name: "feature/a", Scope: model.BranchScopeLocal, LastCommitAt: now, MergeStatus: model.MergeStatusMerged, JiraStatus: "Done"},
			{Name: "feature/c", Scope: model.BranchScopeLocal, LastCommitAt: now.Add(-2 * time.Hour), MergeStatus: model.MergeStatusUnknown, JiraStatus: "-"},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	visible := m.visibleBranches()
	if visible[0].Name != "feature/a" {
		t.Fatalf("expected name sort to start with feature/a, got %s", visible[0].Name)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF6})
	next := updated.(Model)
	visible = next.visibleBranches()
	if visible[0].Name != "feature/a" {
		t.Fatalf("expected date sort to start with newest feature/a, got %s", visible[0].Name)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	visible = next.visibleBranches()
	if visible[0].MergeStatus != model.MergeStatusMerged {
		t.Fatalf("expected merge-status sort to place merged first, got %s", visible[0].MergeStatus)
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyF6})
	next = updated.(Model)
	visible = next.visibleBranches()
	if visible[0].JiraStatus != "Done" {
		t.Fatalf("expected jira-status sort to place done first, got %s", visible[0].JiraStatus)
	}
}

func TestJiraStatusSortOrderByBuckets(t *testing.T) {
	now := time.Now()
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.branchSort = branchSortByJiraStatus
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "feature/a", Scope: model.BranchScopeLocal, JiraStatus: "Open", LastCommitAt: now.Add(-5 * time.Minute)},
			{Name: "feature/b", Scope: model.BranchScopeLocal, JiraStatus: "In Progress", LastCommitAt: now.Add(-4 * time.Minute)},
			{Name: "feature/c", Scope: model.BranchScopeLocal, JiraStatus: "Done", LastCommitAt: now.Add(-3 * time.Minute)},
			{Name: "feature/d", Scope: model.BranchScopeLocal, JiraStatus: "QA", LastCommitAt: now.Add(-2 * time.Minute)},
			{Name: "feature/e", Scope: model.BranchScopeLocal, JiraStatus: "-", LastCommitAt: now.Add(-time.Minute)},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	visible := m.visibleBranches()
	if len(visible) != 5 {
		t.Fatalf("expected 5 branches, got %d", len(visible))
	}

	got := []string{visible[0].JiraStatus, visible[1].JiraStatus, visible[2].JiraStatus, visible[3].JiraStatus, visible[4].JiraStatus}
	want := []string{"Done", "QA", "In Progress", "Open", "-"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected jira sort order at %d: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestInfoPanelShowsJiraStatusFallbackDash(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:          "feature/no-jira",
			Scope:         model.BranchScopeLocal,
			JiraKey:       "-",
			JiraStatus:    "-",
			JiraTicketURL: "-",
		}},
	}
	m.repoData["repo-a"] = m.activeRepo
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	panel := m.viewStatsPanel(180, 16)
	if !strings.Contains(panel, "Статус Jira: -") {
		t.Fatalf("expected jira status fallback in info panel, got %q", panel)
	}
	if !strings.Contains(panel, "Jira-состояние: нет mapping") {
		t.Fatalf("expected jira state explanation in info panel, got %q", panel)
	}
}

func TestContextLineShowsJiraIndicatorAndState(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{{
			Name:       "feature/OPS-77",
			Scope:      model.BranchScopeLocal,
			JiraKey:    "OPS-77",
			JiraStatus: "-",
			JiraState:  model.JiraStatusStateAuth,
			JiraReason: model.JiraStatusReasonAuthRequired,
		}},
	}
	m.repoData["repo-a"] = m.activeRepo
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	ctx := m.viewContextLine(240)
	if !strings.Contains(ctx, "Jira-статус: auth [A]") {
		t.Fatalf("expected auth indicator in context line, got %q", ctx)
	}
	if !strings.Contains(ctx, "Jira-состояние: нужна авторизация") {
		t.Fatalf("expected jira state label in context line, got %q", ctx)
	}
}

func TestJiraStateTextDifferentiatesMappingReasons(t *testing.T) {
	m := NewModel(&config.Config{}, nil, false)

	groupMissing := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateUnmapped,
		JiraReason: model.JiraStatusReasonNoGroupConfig,
	})
	if !strings.Contains(groupMissing, "group не настроена") {
		t.Fatalf("expected no-group-config hint, got %q", groupMissing)
	}

	regexOnly := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateUnmapped,
		JiraReason: model.JiraStatusReasonRegexKeyOnly,
	})
	if !strings.Contains(regexOnly, "ключ извлечен только из regex") {
		t.Fatalf("expected regex-only hint, got %q", regexOnly)
	}

	browserFallback := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateReady,
		JiraReason: model.JiraStatusReasonBrowserUnavailableHTTPFallback,
	})
	if !strings.Contains(browserFallback, "browser недоступен, использован HTTP fallback") {
		t.Fatalf("expected browser fallback hint, got %q", browserFallback)
	}

	authViaFallback := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateAuth,
		JiraReason: model.JiraStatusReasonBrowserUnavailableHTTPAuthRequired,
	})
	if !strings.Contains(authViaFallback, "browser недоступен, HTTP требует авторизацию") {
		t.Fatalf("expected browser+http auth hint, got %q", authViaFallback)
	}
}

func TestJiraStateTextShowsSpecificAuthAndHTTPReasons(t *testing.T) {
	m := NewModel(&config.Config{}, nil, false)

	forbidden := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateAuth,
		JiraReason: model.JiraStatusReasonForbidden,
	})
	if !strings.Contains(forbidden, "нет доступа") || !strings.Contains(forbidden, "403") {
		t.Fatalf("expected forbidden jira hint, got %q", forbidden)
	}

	loginRequired := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateAuth,
		JiraReason: model.JiraStatusReasonLoginRequired,
	})
	if !strings.Contains(loginRequired, "требуется вход") {
		t.Fatalf("expected login-required jira hint, got %q", loginRequired)
	}

	notFound := m.jiraStateText(model.BranchInfo{
		JiraState:  model.JiraStatusStateError,
		JiraReason: model.JiraStatusReasonIssueNotFound,
	})
	if !strings.Contains(notFound, "тикет не найден") || !strings.Contains(notFound, "404") {
		t.Fatalf("expected issue-not-found jira hint, got %q", notFound)
	}
}

func TestF5RefreshesFromBranchesTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after F5 refresh in branches tab")
	}
	if !next.loadingSelectedRepo() {
		t.Fatal("expected loading=true after F5 refresh in branches tab")
	}
}

func TestF6HintVisibleInTopMenuAndHotkeyBar(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	top := m.viewTopMenu(240)
	if !strings.Contains(top, "F6 Сорт") {
		t.Fatalf("expected F6 sort hint in repos top menu, got %q", top)
	}
	bar := m.viewHotkeyBar(240)
	if !strings.Contains(bar, "F6") {
		t.Fatalf("expected F6 in repos hotkey bar, got %q", bar)
	}

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	top = m.viewTopMenu(240)
	if !strings.Contains(top, "F6 Сорт") {
		t.Fatalf("expected F6 sort hint in branches top menu, got %q", top)
	}
	bar = m.viewHotkeyBar(240)
	if !strings.Contains(bar, "F6") {
		t.Fatalf("expected F6 in branches hotkey bar, got %q", bar)
	}
}

func TestApplyAutocheckSelectionMarksEligibleBranches(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	branches := []model.BranchInfo{
		{Name: "feature/task", Key: "refs/heads/feature/task", Scope: model.BranchScopeLocal, Autocheck: true},
		{Name: "bugfix/fix", Key: "refs/heads/bugfix/fix", Scope: model.BranchScopeLocal, Autocheck: true},
		{Name: "hotfix/urgent", Key: "refs/heads/hotfix/urgent", Scope: model.BranchScopeLocal, Autocheck: false},
		{Name: "release/1.0", Key: "refs/heads/release/1.0", Scope: model.BranchScopeLocal, Autocheck: true, Protected: true},
	}

	m.applyAutocheckSelection("repo-a", branches)

	selected := m.selected["repo-a"]
	if !selected["refs/heads/feature/task"] {
		t.Fatal("expected feature/task to be auto-selected")
	}
	if !selected["refs/heads/bugfix/fix"] {
		t.Fatal("expected bugfix/fix to be auto-selected")
	}
	if selected["refs/heads/hotfix/urgent"] {
		t.Fatal("expected hotfix/urgent to NOT be auto-selected (Autocheck=false)")
	}
	if selected["refs/heads/release/1.0"] {
		t.Fatal("expected release/1.0 to NOT be auto-selected (Protected=true)")
	}
}

func TestManualOverrideCanUnselectAutocheckBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	branches := []model.BranchInfo{
		{Name: "feature/task", Key: "refs/heads/feature/task", Scope: model.BranchScopeLocal, Autocheck: true},
	}

	// Simulate autocheck selection
	m.applyAutocheckSelection("repo-a", branches)

	// Verify initial state
	if !m.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected feature/task to be auto-selected initially")
	}

	// Simulate manual override: user presses Space on the auto-selected branch
	m.focus = focusBranches
	m.branchScope = branchScopeLocal
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: branches,
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.branchCursor["repo-a"] = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	next := updated.(Model)

	// Manual toggle should unselect the auto-selected branch
	if next.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected manual Space to unselect auto-selected branch")
	}
}

func TestManualOverrideCanSelectNonAutocheckBranch(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	branches := []model.BranchInfo{
		{Name: "feature/task", Key: "refs/heads/feature/task", Scope: model.BranchScopeLocal, Autocheck: true},
		{Name: "hotfix/urgent", Key: "refs/heads/hotfix/urgent", Scope: model.BranchScopeLocal, Autocheck: false},
	}

	m.applyAutocheckSelection("repo-a", branches)

	// hotfix/urgent should NOT be auto-selected
	if m.selected["repo-a"]["refs/heads/hotfix/urgent"] {
		t.Fatal("expected hotfix/urgent to NOT be auto-selected")
	}

	// Navigate to hotfix/urgent and select manually
	m.focus = focusBranches
	m.branchScope = branchScopeLocal
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: branches,
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.branchCursor["repo-a"] = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	next := updated.(Model)

	if !next.selected["repo-a"]["refs/heads/hotfix/urgent"] {
		t.Fatal("expected manual Space to select non-autocheck branch")
	}
}

func TestAutocheckSelectionIsIdempotent(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	branches := []model.BranchInfo{
		{Name: "feature/task", Key: "refs/heads/feature/task", Scope: model.BranchScopeLocal, Autocheck: true},
	}

	m.applyAutocheckSelection("repo-a", branches)
	m.applyAutocheckSelection("repo-a", branches)

	if len(m.selected["repo-a"]) != 1 {
		t.Fatalf("expected exactly 1 selected branch after double apply, got %d", len(m.selected["repo-a"]))
	}
	if !m.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected feature/task to remain selected")
	}
}

func TestManualOverridePersistsAfterRefresh(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	branches := []model.BranchInfo{
		{Name: "feature/task", Key: "refs/heads/feature/task", Scope: model.BranchScopeLocal, Autocheck: true},
	}

	// 1. Initial autocheck
	m.applyAutocheckSelection("repo-a", branches)
	if !m.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected initial auto-selection")
	}

	// 2. Manual unselect via Update
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: branches}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.branchCursor["repo-a"] = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)

	if m.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected manual unselect to work")
	}

	// 3. Refresh (re-apply autocheck) - should NOT re-select
	m.applyAutocheckSelection("repo-a", branches)

	if m.selected["repo-a"]["refs/heads/feature/task"] {
		t.Fatal("expected manual unselect to persist after re-applying autocheck")
	}
}

func TestStarStartsReleaseOptionsLoadOnlyInBranchesTab(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: []model.BranchInfo{{Name: "OPS-1", Scope: model.BranchScopeLocal}}}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected non-nil command after '*' in branches tab")
	}
	if !next.releaseLoading {
		t.Fatal("expected releaseLoading=true after '*'")
	}

	m.focus = focusRepos
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	next = updated.(Model)
	if cmd != nil {
		t.Fatal("expected nil command after '*' in repos tab")
	}
	if next.releaseLoading {
		t.Fatal("expected releaseLoading=false in repos tab")
	}
}

func TestRefreshDoesNotStartReleaseFlow(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: []model.BranchInfo{{Name: "OPS-1", Scope: model.BranchScopeLocal}}}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("expected refresh command after F5")
	}
	if next.releaseLoading {
		t.Fatal("expected release flow to stay idle on refresh")
	}
	if next.confirmType == confirmReleaseSelect {
		t.Fatal("expected refresh to not open release modal")
	}
}

func TestReleaseAutocheckRespectsManualOverride(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "OPS-1", Key: "refs/heads/OPS-1", Scope: model.BranchScopeLocal},
			{Name: "OPS-2", Key: "refs/heads/OPS-2", Scope: model.BranchScopeLocal},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.selected["repo-a"] = map[string]bool{
		"refs/heads/OPS-1": false, // manual override: user explicitly unselected
	}

	updated, _ := m.Update(releaseAutocheckAppliedMsg{
		repoName: "repo-a",
		summary:  usecase.ReleaseAutocheckResult{Group: "TASKS", ReleaseID: "42"},
		branches: []model.BranchInfo{
			{Name: "OPS-1", Key: "refs/heads/OPS-1", Scope: model.BranchScopeLocal},
			{Name: "OPS-2", Key: "refs/heads/OPS-2", Scope: model.BranchScopeLocal},
		},
		selectedID: "42",
	})
	next := updated.(Model)

	if next.selected["repo-a"]["refs/heads/OPS-1"] {
		t.Fatal("expected manual unselect to remain stronger than release auto-selection")
	}
	if !next.selected["repo-a"]["refs/heads/OPS-2"] {
		t.Fatal("expected non-overridden branch to be selected by release auto-check")
	}
}

func TestReleaseModalCancelClosesWithoutSelectionChanges(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}
	m.confirmType = confirmReleaseSelect
	m.releaseOptions = []usecase.RepoRelease{{Group: "TASKS", Version: jira.ReleaseVersion{ID: "42", Name: "R42"}}}
	m.selected["repo-a"] = map[string]bool{"refs/heads/OPS-1": true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.confirmType != confirmNone {
		t.Fatalf("expected release modal to close on Esc, got %v", next.confirmType)
	}
	if !next.selected["repo-a"]["refs/heads/OPS-1"] {
		t.Fatal("expected selection to remain unchanged after cancel")
	}
	if !strings.Contains(next.statusLine, "отменена") {
		t.Fatalf("expected cancel status line, got %q", next.statusLine)
	}
}

func TestReleaseModalIgnoresFormatToggleKeys(t *testing.T) {
	m := NewModel(&config.Config{
		Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}},
	}, nil, false)

	m.focus = focusBranches
	m.confirmType = confirmReleaseSelect
	m.scriptFormat = model.ScriptFormatSH

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	next := updated.(Model)
	if next.scriptFormat != model.ScriptFormatSH {
		t.Fatal("expected right key to not toggle script format in release modal")
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	next = updated.(Model)
	if next.scriptFormat != model.ScriptFormatSH {
		t.Fatal("expected t key to not toggle script format in release modal")
	}
}

func TestReleaseFlowAcceptanceStarLoadSelectApplyAndSummary(t *testing.T) {
	cfg := &config.Config{Repos: []config.RepoConfig{{Name: "repo-a", Path: "/tmp/repo-a"}}}
	m := NewModel(cfg, nil, false)

	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a", Branches: []model.BranchInfo{{Name: "OPS-1", Key: "refs/heads/OPS-1", Scope: model.BranchScopeLocal}, {Name: "OPS-2", Key: "refs/heads/OPS-2", Scope: model.BranchScopeLocal}}}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	next := updated.(Model)
	if cmd == nil || !next.releaseLoading {
		t.Fatal("expected '*' to initiate release loading")
	}

	options := []usecase.RepoRelease{{Group: "TASKS", Version: jira.ReleaseVersion{ID: "42", Name: "Release 42", ReleaseDate: "2026-04-24"}}}
	updated, _ = next.Update(releaseOptionsLoadedMsg{repoName: "repo-a", options: options})
	next = updated.(Model)
	if next.confirmType != confirmReleaseSelect {
		t.Fatalf("expected release select modal, got %v", next.confirmType)
	}

	updated, _ = next.Update(releaseAutocheckAppliedMsg{
		repoName: "repo-a",
		summary:  usecase.ReleaseAutocheckResult{Group: "TASKS", ReleaseID: "42", IssueKeysTotal: 2, BranchMatches: 2, BranchSkippedProtect: 0, BranchSkippedNoJira: 0},
		branches: []model.BranchInfo{
			{Name: "OPS-1", Key: "refs/heads/OPS-1", Scope: model.BranchScopeLocal},
			{Name: "OPS-2", Key: "refs/heads/OPS-2", Scope: model.BranchScopeLocal},
		},
		selectedID: "42",
	})
	next = updated.(Model)

	if !next.selected["repo-a"]["refs/heads/OPS-1"] || !next.selected["repo-a"]["refs/heads/OPS-2"] {
		t.Fatal("expected release apply to select matching branches")
	}
	if !strings.Contains(next.statusLine, "Release 42") || !strings.Contains(next.statusLine, "issues=2") {
		t.Fatalf("expected summary status line after release apply, got %q", next.statusLine)
	}
}

type fakeReleaseServiceTUI struct {
	listVersionsFn  func(ctx context.Context, group string) ([]jira.ReleaseVersion, error)
	listIssueKeysFn func(ctx context.Context, group, releaseID string) ([]string, error)
}

func (f fakeReleaseServiceTUI) ListReleasedFixVersions(ctx context.Context, group string) ([]jira.ReleaseVersion, error) {
	if f.listVersionsFn != nil {
		return f.listVersionsFn(ctx, group)
	}
	return nil, nil
}

func (f fakeReleaseServiceTUI) ListDoneIssueKeysByRelease(ctx context.Context, group, releaseID string) ([]string, error) {
	if f.listIssueKeysFn != nil {
		return f.listIssueKeysFn(ctx, group, releaseID)
	}
	return nil, nil
}

func TestReleaseApplyUsesBranchesSnapshotNotLiveState(t *testing.T) {
	v := viper.New()
	v.Set("jira", []map[string]any{{"group": "TASKS", "url": "https://tasks.example.org"}})
	v.Set("repos", []map[string]any{{
		"name": "repo-a",
		"path": "/tmp/repo-a",
		"branch": map[string]any{
			"jira": []string{`^(?P<TASKS>OPS-\d+)$`},
		},
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	cleaner := usecase.NewCleaner(nil, usecase.WithJiraReleaseService(fakeReleaseServiceTUI{
		listIssueKeysFn: func(_ context.Context, group, releaseID string) ([]string, error) {
			if group != "TASKS" || releaseID != "42" {
				t.Fatalf("unexpected release params: group=%s releaseID=%s", group, releaseID)
			}
			return []string{"OPS-1"}, nil
		},
	}))

	m := NewModel(cfg, cleaner, false)
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{
		RepoName: "repo-a",
		Branches: []model.BranchInfo{
			{Name: "OPS-1", Key: "refs/heads/OPS-1", Scope: model.BranchScopeLocal},
			{Name: "OPS-2", Key: "refs/heads/OPS-2", Scope: model.BranchScopeLocal},
		},
	}
	m.repoStats["repo-a"] = model.RepoStat{Loaded: true}

	choice := usecase.RepoRelease{Group: "TASKS", Version: jira.ReleaseVersion{ID: "42", Name: "R42"}}
	cmd := m.startApplyReleaseAutocheck(choice)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Mutate live state after command creation.
	m.activeRepo.Branches = []model.BranchInfo{{Name: "OTHER-1", Key: "refs/heads/OTHER-1", Scope: model.BranchScopeLocal}}

	msg := cmd()
	applyMsg, ok := msg.(releaseAutocheckAppliedMsg)
	if !ok {
		batch, batchOK := msg.(tea.BatchMsg)
		if !batchOK {
			t.Fatalf("expected releaseAutocheckAppliedMsg or tea.BatchMsg, got %T", msg)
		}

		found := false
		for _, batchCmd := range batch {
			if batchCmd == nil {
				continue
			}
			inner := batchCmd()
			candidate, innerOK := inner.(releaseAutocheckAppliedMsg)
			if !innerOK {
				continue
			}
			applyMsg = candidate
			found = true
			break
		}
		if !found {
			t.Fatalf("expected releaseAutocheckAppliedMsg inside tea.BatchMsg, got %T", msg)
		}
	}
	if applyMsg.err != nil {
		t.Fatalf("unexpected error: %v", applyMsg.err)
	}
	if len(applyMsg.branches) != 1 || applyMsg.branches[0].Name != "OPS-1" {
		t.Fatalf("expected snapshot-based match OPS-1, got %#v", applyMsg.branches)
	}
}

func TestStartApplyReleaseAutocheckReturnsNilWhenReleaseLoading(t *testing.T) {
	v := viper.New()
	v.Set("repos", []map[string]any{{
		"name": "repo-a",
		"path": "/tmp/repo-a",
	}})

	cfg, err := config.LoadFromViper(v)
	if err != nil {
		t.Fatalf("load config from viper: %v", err)
	}

	m := NewModel(cfg, nil, false)
	m.focus = focusBranches
	m.activeRepo = model.RepoBranches{RepoName: "repo-a"}
	m.releaseLoading = true

	cmd := m.startApplyReleaseAutocheck(usecase.RepoRelease{Group: "TASKS", Version: jira.ReleaseVersion{ID: "42"}})
	if cmd != nil {
		t.Fatal("expected nil cmd when releaseLoading=true to prevent duplicate apply")
	}
}
