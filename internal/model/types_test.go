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

func TestBranchInfoEmptyScopeIsNeither(t *testing.T) {
	t.Parallel()

	var empty BranchInfo
	if empty.IsRemote() || empty.IsLocal() {
		t.Fatalf("empty scope should be neither local nor remote, got %+v", empty)
	}
}
