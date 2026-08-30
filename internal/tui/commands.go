package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

func waitRepoLoadJiraProgressCmd(repoName string, startup bool, stream <-chan usecase.RepoLoadProgress) tea.Cmd {
	if stream == nil {
		return nil
	}

	return func() tea.Msg {
		progress, ok := <-stream
		if !ok {
			return nil
		}
		return repoLoadJiraProgressMsg{
			repoName: repoName,
			startup:  startup,
			progress: progress,
			stream:   stream,
		}
	}
}

// loadRepoBranchesCmd запускает загрузку веток репозитория и отправляет поэтапные события в лог.
func loadRepoBranchesCmd(ctx context.Context, cleaner cleanerPort, repo config.RepoConfig, requestID int, startup bool, actionKey string, actionID int) tea.Cmd {
	progressStream := make(chan usecase.RepoLoadProgress, 2048)
	progressCmd := waitRepoLoadJiraProgressCmd(repo.Name, startup, progressStream)

	if !startup {
		return tea.Batch(progressCmd, func() tea.Msg {
			rb, summary, err := cleaner.LoadRepoBranchesWithProgress(ctx, repo, func(item usecase.RepoLoadProgress) {
				select {
				case progressStream <- item:
				case <-ctx.Done():
				}
			})
			close(progressStream)
			return branchesLoadedMsg{
				requestID:            requestID,
				actionKey:            actionKey,
				actionID:             actionID,
				repoName:             repo.Name,
				rb:                   rb,
				err:                  err,
				startup:              false,
				jiraBatchProgress:    summary.JiraBatchProgress,
				jiraProgressStreamed: summary.JiraProgressStreamed,
			}
		})
	}

	stageGit := func() tea.Msg {
		return startupLogMsg{"[СТАРТ] " + repo.Name + ": начинаю синхронизацию"}
	}
	loadAndReport := func() tea.Msg {
		rb, summary, err := cleaner.LoadRepoBranchesWithProgress(ctx, repo, func(item usecase.RepoLoadProgress) {
			select {
			case progressStream <- item:
			case <-ctx.Done():
			}
		})
		close(progressStream)
		if err != nil {
			return branchesLoadedMsg{requestID: requestID, actionKey: actionKey, actionID: actionID, repoName: repo.Name, rb: rb, err: err, startup: true}
		}
		jiraResolved := 0
		for _, b := range rb.Branches {
			if b.JiraKey != "-" && b.JiraKey != "" && b.JiraStatus != "-" && b.JiraStatus != "" {
				jiraResolved++
			}
		}
		syncNote := ""
		if rb.SyncWarning != "" {
			syncNote = " [из кэша]"
		}
		return branchesLoadedMsg{
			requestID:            requestID,
			actionKey:            actionKey,
			actionID:             actionID,
			repoName:             repo.Name,
			rb:                   rb,
			err:                  nil,
			startup:              true,
			jiraResolved:         jiraResolved,
			jiraBatchProgress:    summary.JiraBatchProgress,
			jiraProgressStreamed: summary.JiraProgressStreamed,
			syncNote:             syncNote,
		}
	}
	return tea.Batch(stageGit, progressCmd, loadAndReport)
}

func loadRepoStatCmd(ctx context.Context, cleaner cleanerPort, repo config.RepoConfig, startup bool, actionKey string, actionID int) tea.Cmd {
	return func() tea.Msg {
		stat, err := cleaner.LoadRepoStat(ctx, repo)
		return repoStatLoadedMsg{actionKey: actionKey, actionID: actionID, repoName: repo.Name, stat: stat, err: err, startup: startup}
	}
}

func generateScriptCmd(cleaner cleanerPort, repo config.RepoConfig, repoPath string, branches []model.BranchInfo, format model.ScriptFormat) tea.Cmd {
	return func() tea.Msg {
		result, err := cleaner.GenerateDeleteScript(repo, repoPath, branches, format)
		return scriptGeneratedMsg{result: result, err: err}
	}
}
