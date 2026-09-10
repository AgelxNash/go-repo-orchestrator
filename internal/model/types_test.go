package model

import "testing"

func TestBranchInfoIsRemote(t *testing.T) {
	t.Parallel()

	if !(BranchInfo{Scope: BranchScopeRemote}).IsRemote() {
		t.Fatal("expected remote branch to be remote")
	}
	if (BranchInfo{Scope: BranchScopeRemote}).IsLocal() {
		t.Fatal("remote branch must not be local")
	}
}

func TestBranchInfoIsLocal(t *testing.T) {
	t.Parallel()

	if !(BranchInfo{Scope: BranchScopeLocal}).IsLocal() {
		t.Fatal("expected local branch to be local")
	}
	if (BranchInfo{Scope: BranchScopeLocal}).IsRemote() {
		t.Fatal("local branch must not be remote")
	}
}

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
