package model

import (
	"encoding/json"
	"testing"
)

// TestBranchInfoIsRemote проверяет, что remote scope считается удалённой веткой.
func TestBranchInfoIsRemote(t *testing.T) {
	t.Parallel()

	if !(BranchInfo{Scope: BranchScopeRemote}).IsRemote() {
		t.Fatal("expected remote branch to be remote")
	}
	if (BranchInfo{Scope: BranchScopeRemote}).IsLocal() {
		t.Fatal("remote branch must not be local")
	}
}

// TestBranchInfoIsLocal проверяет, что локальный scope не считается remote.
func TestBranchInfoIsLocal(t *testing.T) {
	t.Parallel()

	if !(BranchInfo{Scope: BranchScopeLocal}).IsLocal() {
		t.Fatal("expected local branch to be local")
	}
	if (BranchInfo{Scope: BranchScopeLocal}).IsRemote() {
		t.Fatal("local branch must not be remote")
	}
}

// TestHasSyncWarningIgnoresEmptyClone проверяет, что оболочка без checkout не считается сбоем remote.
func TestHasSyncWarningIgnoresEmptyClone(t *testing.T) {
	t.Parallel()

	stat := RepoStat{
		Warning: RepoWarning{Code: RepoWarningEmptyClone, Message: "оболочка"},
		Loaded:  true,
	}
	if stat.HasSyncWarning() {
		t.Fatal("empty clone warning must not be treated as remote sync warning")
	}
	if !stat.HasEmptyCloneWarning() {
		t.Fatal("expected empty clone warning")
	}
}

// TestHasSyncWarningDetectsRemoteCode проверяет предупреждение remote_sync_failed.
func TestHasSyncWarningDetectsRemoteCode(t *testing.T) {
	t.Parallel()

	stat := RepoStat{
		Warning: RepoWarning{Code: RepoWarningRemoteSyncFailed, Message: "сеть"},
		Loaded:  true,
	}
	if !stat.HasSyncWarning() {
		t.Fatal("expected remote sync warning")
	}
}

// TestStatusReportJSONSerialization проверяет контракт сериализации отчета статуса в JSON.
func TestStatusReportJSONSerialization(t *testing.T) {
	t.Parallel()

	// 1. Пустой список repos сериализуется как [] (не null).
	emptyReport := StatusReport{
		Total:  0,
		Clean:  0,
		Dirty:  0,
		Errors: 0,
		Repos:  make([]RepoStatusDTO, 0),
	}
	data, err := json.Marshal(emptyReport)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	expectedEmpty := `{"total":0,"clean":0,"dirty":0,"errors":0,"repos":[]}`
	if string(data) != expectedEmpty {
		t.Fatalf("expected empty json %s, got %s", expectedEmpty, string(data))
	}

	// 2. Dirty всегда выводится (даже если false), пустые sync_warning и error опускаются.
	cleanRepo := RepoStatusDTO{
		Name:   "repo-clean",
		Path:   "/path/clean",
		Branch: "main",
		Dirty:  false,
	}
	cleanData, err := json.Marshal(cleanRepo)
	if err != nil {
		t.Fatalf("json.Marshal clean repo failed: %v", err)
	}
	expectedClean := `{"name":"repo-clean","path":"/path/clean","branch":"main","dirty":false}`
	if string(cleanData) != expectedClean {
		t.Fatalf("expected clean repo json %s, got %s", expectedClean, string(cleanData))
	}

	// 3. Dirty = true, заполненные sync_warning и error присутствуют.
	dirtyRepo := RepoStatusDTO{
		Name:        "repo-dirty",
		Path:        "/path/dirty",
		Branch:      "feature",
		Dirty:       true,
		SyncWarning: "sync issue",
		Error:       "some error",
	}
	dirtyData, err := json.Marshal(dirtyRepo)
	if err != nil {
		t.Fatalf("json.Marshal dirty repo failed: %v", err)
	}
	expectedDirty := `{"name":"repo-dirty","path":"/path/dirty","branch":"feature","dirty":true,"sync_warning":"sync issue","error":"some error"}`
	if string(dirtyData) != expectedDirty {
		t.Fatalf("expected dirty repo json %s, got %s", expectedDirty, string(dirtyData))
	}
}

// TestExecReportJSONSerialization проверяет контракт сериализации отчета exec в JSON.
func TestExecReportJSONSerialization(t *testing.T) {
	t.Parallel()

	// 1. Пустой список results сериализуется как [] (не null).
	emptyReport := ExecReport{
		Total:   0,
		Success: 0,
		Failed:  0,
		Results: make([]ExecResultDTO, 0),
	}
	data, err := json.Marshal(emptyReport)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	expectedEmpty := `{"total":0,"success":0,"failed":0,"results":[]}`
	if string(data) != expectedEmpty {
		t.Fatalf("expected empty json %s, got %s", expectedEmpty, string(data))
	}

	// 2. Успешный результат: error опускается (omitempty).
	successDTO := ExecResultDTO{
		Repo:       "repo-1",
		Path:       "/path/to/repo-1",
		ExitCode:   0,
		Stdout:     "hello world\n",
		Stderr:     "",
		DurationMS: 42,
	}
	successData, err := json.Marshal(successDTO)
	if err != nil {
		t.Fatalf("json.Marshal success DTO failed: %v", err)
	}
	expectedSuccess := `{"repo":"repo-1","path":"/path/to/repo-1","exit_code":0,"stdout":"hello world\n","stderr":"","duration_ms":42}`
	if string(successData) != expectedSuccess {
		t.Fatalf("expected success json %s, got %s", expectedSuccess, string(successData))
	}

	// 3. Результат с ошибкой: error присутствует.
	failDTO := ExecResultDTO{
		Repo:       "repo-2",
		Path:       "/path/to/repo-2",
		ExitCode:   1,
		Stdout:     "",
		Stderr:     "fatal error\n",
		DurationMS: 15,
		Error:      "exit status 1",
	}
	failData, err := json.Marshal(failDTO)
	if err != nil {
		t.Fatalf("json.Marshal fail DTO failed: %v", err)
	}
	expectedFail := `{"repo":"repo-2","path":"/path/to/repo-2","exit_code":1,"stdout":"","stderr":"fatal error\n","duration_ms":15,"error":"exit status 1"}`
	if string(failData) != expectedFail {
		t.Fatalf("expected fail json %s, got %s", expectedFail, string(failData))
	}
}
