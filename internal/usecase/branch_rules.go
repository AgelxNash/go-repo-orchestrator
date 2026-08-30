package usecase

import (
	"strings"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/filter"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func evaluateBranchProtection(repo config.RepoConfig, branch model.BranchInfo, currentBranch, defaultBranch string) (bool, string) {
	if branch.IsLocal() {
		return filter.Evaluate(repo, branch, currentBranch, defaultBranch)
	}

	return evaluateRemoteBranchProtection(repo, branch, defaultBranch)
}

func evaluateRemoteBranchProtection(repo config.RepoConfig, branch model.BranchInfo, defaultBranch string) (bool, string) {
	if strings.TrimSpace(branch.RemoteName) == "" {
		return false, "имя remote неизвестно"
	}
	if strings.TrimSpace(branch.Name) == "" {
		return false, "имя удаленной ветки неизвестно"
	}
	if isRemoteDefaultBranch(branch, defaultBranch) {
		return false, "ветка по умолчанию"
	}
	if !isRemoteDeleteResolvable(branch) {
		return false, "remote ref неоднозначен для удаления"
	}
	if reason, ok := repo.ProtectedReason(branch.Name); ok {
		return false, reason
	}

	return true, "подходит"
}

func isRemoteDefaultBranch(branch model.BranchInfo, defaultBranch string) bool {
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch == "" {
		return false
	}

	branchName := strings.TrimSpace(branch.Name)
	qualified := strings.TrimSpace(branch.QualifiedName)
	if branchName == defaultBranch || qualified == defaultBranch {
		return true
	}

	if branch.RemoteName != "" {
		fullRef := "refs/remotes/" + branch.RemoteName + "/" + branchName
		if fullRef == defaultBranch {
			return true
		}
	}

	parts := strings.SplitN(defaultBranch, "/", 2)
	if len(parts) == 2 {
		return branchName == strings.TrimSpace(parts[1])
	}

	if strings.HasPrefix(defaultBranch, "refs/remotes/") {
		parts = strings.SplitN(strings.TrimPrefix(defaultBranch, "refs/remotes/"), "/", 2)
		if len(parts) == 2 {
			return branchName == strings.TrimSpace(parts[1])
		}
	}

	return false
}

func isRemoteDeleteResolvable(branch model.BranchInfo) bool {
	if !branch.IsRemote() {
		return true
	}

	remoteName := strings.TrimSpace(branch.RemoteName)
	branchName := strings.TrimSpace(branch.Name)
	if remoteName == "" || branchName == "" {
		return false
	}

	qualified := strings.TrimSpace(branch.QualifiedName)
	if qualified == "" {
		return true
	}

	prefix := remoteName + "/"
	if strings.HasPrefix(qualified, prefix) {
		return strings.TrimPrefix(qualified, prefix) == branchName
	}

	fullRefPrefix := "refs/remotes/" + remoteName + "/"
	if strings.HasPrefix(qualified, fullRefPrefix) {
		return strings.TrimPrefix(qualified, fullRefPrefix) == branchName
	}

	return false
}
