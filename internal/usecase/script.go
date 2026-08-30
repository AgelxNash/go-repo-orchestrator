package usecase

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// GenerateDeleteScript формирует shell/cmd скрипт удаления выбранных веток и сохраняет его в репозитории.
func (c *Cleaner) GenerateDeleteScript(repo config.RepoConfig, repoPath string, branches []model.BranchInfo, format model.ScriptFormat) (model.ScriptResult, error) {
	if len(branches) == 0 {
		return model.ScriptResult{}, fmt.Errorf("ветки не выбраны")
	}

	eligible := make([]model.BranchInfo, 0, len(branches))
	for _, branch := range branches {
		if branch.Protected {
			continue
		}
		if branch.IsRemote() && !isRemoteDeleteResolvable(branch) {
			continue
		}
		eligible = append(eligible, branch)
	}
	if len(eligible) == 0 {
		return model.ScriptResult{}, fmt.Errorf("подходящие ветки не выбраны")
	}

	sessionID := time.Now().UTC().Format("20060102T150405Z")
	ext := ".sh"
	if format == model.ScriptFormatBAT {
		ext = ".bat"
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	safeRepoName := strings.ReplaceAll(repo.Name, "/", "_")
	safeRepoName = strings.ReplaceAll(safeRepoName, "\\", "_")
	filePattern := fmt.Sprintf("go-repo-orchestrator-%s-delete-%s-*%s", safeRepoName, sessionID, ext)
	content := buildScriptContent(repoPath, eligible, format)

	scriptFile, err := os.CreateTemp(cwd, filePattern)
	if err != nil {
		return model.ScriptResult{}, fmt.Errorf("создать файл скрипта: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer func() {
		_ = scriptFile.Close()
	}()

	perm := os.FileMode(0o644)
	if format == model.ScriptFormatSH {
		perm = 0o755
	}

	if _, err := scriptFile.WriteString(content); err != nil {
		_ = os.Remove(scriptPath)
		return model.ScriptResult{}, fmt.Errorf("записать скрипт: %w", err)
	}

	if err := scriptFile.Chmod(perm); err != nil {
		_ = os.Remove(scriptPath)
		return model.ScriptResult{}, fmt.Errorf("установить права на скрипт: %w", err)
	}

	return model.ScriptResult{
		RepoName:      repo.Name,
		RepoPath:      repoPath,
		ScriptPath:    scriptPath,
		Format:        format,
		BranchesCount: len(eligible),
	}, nil
}

func buildScriptContent(repoPath string, branches []model.BranchInfo, format model.ScriptFormat) string {
	var b strings.Builder
	if format == model.ScriptFormatBAT {
		b.WriteString("@echo off\n")
		b.WriteString("setlocal\n")
		b.WriteString("cd /d ")
		b.WriteString(quoteForBat(repoPath))
		b.WriteString("\n\n")
		for _, branch := range branches {
			if !branchPassesSanityCheck(branch) {
				continue
			}
			command := buildDeleteCommandBAT(branch)
			if command == "" {
				continue
			}
			b.WriteString(command)
			b.WriteString("\n")
		}
		return b.String()
	}

	b.WriteString("#!/usr/bin/env sh\n")
	b.WriteString("set -eu\n")
	b.WriteString("cd ")
	b.WriteString(quoteForPOSIX(repoPath))
	b.WriteString("\n\n")
	for _, branch := range branches {
		command := buildDeleteCommandSH(branch)
		if command == "" {
			continue
		}
		b.WriteString(command)
		b.WriteString("\n")
	}

	return b.String()
}

func buildDeleteCommandSH(branch model.BranchInfo) string {
	if branch.IsRemote() {
		if !isRemoteDeleteResolvable(branch) {
			return ""
		}
		return "git push " + quoteForPOSIX(branch.RemoteName) + " --delete " + quoteForPOSIX(branch.Name)
	}

	flag := "-D"
	if branch.MergeStatus == model.MergeStatusMerged {
		flag = "-d"
	}

	return "git branch " + flag + " " + quoteForPOSIX(branch.Name)
}

func buildDeleteCommandBAT(branch model.BranchInfo) string {
	if !branchPassesSanityCheck(branch) {
		return ""
	}
	if branch.IsRemote() {
		if !isRemoteDeleteResolvable(branch) {
			return ""
		}
		return "git push " + quoteForBat(branch.RemoteName) + " --delete " + quoteForBat(branch.Name)
	}

	flag := "-D"
	if branch.MergeStatus == model.MergeStatusMerged {
		flag = "-d"
	}

	return "git branch " + flag + " " + quoteForBat(branch.Name)
}

// branchPassesSanityCheck отсекает ветки, не проходящие sanitizeBranchName
// (control-символы, `%%`, недопустимая длина). Имена, прошедшие проверку,
// считаются пригодными для встраивания в команды.
func branchPassesSanityCheck(branch model.BranchInfo) bool {
	if _, err := sanitizeBranchName(branch.Name); err != nil {
		return false
	}
	if _, err := sanitizeBranchName(branch.RemoteName); err != nil {
		return false
	}
	return true
}

func quoteForPOSIX(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
