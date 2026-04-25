package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

func (m *Model) startLoadActiveRepo() tea.Cmd {
	repo := m.cfg.Repos[m.repoIdx]
	return m.startLoadRepo(repo, true, false)
}

func (m *Model) startLoadSelectedRepo() tea.Cmd {
	if len(m.cfg.Repos) == 0 || m.repoIdx < 0 || m.repoIdx >= len(m.cfg.Repos) {
		return nil
	}

	return m.startLoadRepo(m.cfg.Repos[m.repoIdx], true, false)
}

func (m *Model) startLoadRepo(repo config.RepoConfig, manual, startup bool) tea.Cmd {
	m.repoLoadReq[repo.Name]++
	requestID := m.repoLoadReq[repo.Name]
	actionKey := actionKeyLoadRepo(repo.Name)
	ctx, actionID := m.beginAction(actionKey)
	m.repoLoading[repo.Name] = true
	m.err = nil

	if startup {
		m.setStartupStage(repo.Name, "загрузка веток")
		m.markStartupActivity()
		m.setStartupProgressStatus()
	} else if manual {
		m.statusLine = fmt.Sprintf("Обновление %q...", repo.Name)
		m.refreshLocked = true
		m.refreshAll = false
		for repoName := range m.refreshPending {
			delete(m.refreshPending, repoName)
		}
		m.refreshRepo = repo.Name
		m.refreshReqID = requestID
	} else {
		m.statusLine = fmt.Sprintf("Загрузка %q...", repo.Name)
	}

	if repo.Name == m.selectedRepoName() {
		m.activeRepo = model.RepoBranches{RepoName: repo.Name, RepoSource: repo.SourceType()}
		return tea.Batch(m.spinner.Tick, loadRepoBranchesCmd(ctx, m.clean, repo, requestID, startup, actionKey, actionID))
	}

	return loadRepoBranchesCmd(ctx, m.clean, repo, requestID, startup, actionKey, actionID)
}

func (m *Model) startCreateLocalCopyFromCurrentRemoteBranch() tea.Cmd {
	if !m.canCreateLocalFromRemote() {
		if !m.canUseF7ForActiveRepo() {
			m.statusLine = "Создание локальной копии доступно только для репозиториев с path"
		} else {
			m.statusLine = "Создание локальной копии доступно только для удаленной ветки"
		}
		return nil
	}

	repo, ok := m.cfg.RepoByName(m.activeRepo.RepoName)
	if !ok {
		m.err = errors.New("репозиторий не найден в конфигурации")
		return nil
	}

	branch := m.currentBranch()
	if branch == nil {
		m.statusLine = "Ветка не выбрана"
		return nil
	}

	m.repoLoading[repo.Name] = true
	m.statusLine = fmt.Sprintf("Создание локальной копии %s...", branch.Name)
	actionKey := actionKeyLocalCopy(repo.Name)
	ctx, actionID := m.beginAction(actionKey)

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		err := m.clean.CreateLocalTrackingBranch(ctx, repo, branch.Name, branch.QualifiedName)
		return localCopyCompletedMsg{actionKey: actionKey, actionID: actionID, repoName: repo.Name, branch: branch.Name, err: err}
	})
}

func (m *Model) startFetchAndPullActiveRepo() tea.Cmd {
	if len(m.cfg.Repos) == 0 || m.repoIdx < 0 || m.repoIdx >= len(m.cfg.Repos) {
		m.statusLine = "Нет активного репозитория"
		return nil
	}

	repo := m.cfg.Repos[m.repoIdx]
	m.repoLoading[repo.Name] = true
	m.err = nil
	m.statusLine = fmt.Sprintf("Обновление %q через fetch + pull...", repo.Name)
	m.refreshLocked = true
	m.refreshAll = false
	for repoName := range m.refreshPending {
		delete(m.refreshPending, repoName)
	}
	m.refreshRepo = repo.Name
	m.refreshReqID = 0
	actionKey := actionKeyFetchPull(repo.Name)
	ctx, actionID := m.beginAction(actionKey)

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		err := m.clean.FetchAndPullRepo(ctx, repo)
		return repoFetchPullCompletedMsg{actionKey: actionKey, actionID: actionID, repoName: repo.Name, err: err}
	})
}

func (m *Model) startLoadReleaseOptions() tea.Cmd {
	if m.releaseLoading {
		return nil
	}
	if m.focus != focusBranches {
		return nil
	}
	if m.activeRepo.RepoName == "" {
		m.statusLine = "Сначала откройте ветки репозитория"
		return nil
	}

	repo, ok := m.cfg.RepoByName(m.activeRepo.RepoName)
	if !ok {
		m.err = errors.New("репозиторий не найден в конфигурации")
		return nil
	}

	m.releaseLoading = true
	m.err = nil
	m.statusLine = "Загрузка Jira releases..."
	actionKey := actionKeyReleaseOptions(repo.Name)
	ctx, actionID := m.beginAction(actionKey)
	branchesSnapshot := slices.Clone(m.activeRepo.Branches)

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		options, err := m.clean.ListRepoReleasedFixVersions(ctx, repo, branchesSnapshot)
		return releaseOptionsLoadedMsg{actionKey: actionKey, actionID: actionID, repoName: repo.Name, options: options, err: err}
	})
}

func (m *Model) startApplyReleaseAutocheck(choice usecase.RepoRelease) tea.Cmd {
	if m.releaseLoading {
		return nil
	}

	repo, ok := m.cfg.RepoByName(m.activeRepo.RepoName)
	if !ok {
		m.err = errors.New("репозиторий не найден в конфигурации")
		return nil
	}

	m.releaseLoading = true
	m.confirmType = confirmNone
	m.err = nil
	m.statusLine = "Применение release-driven автопометки..."
	actionKey := actionKeyReleaseApply(repo.Name)
	ctx, actionID := m.beginAction(actionKey)
	branchesSnapshot := slices.Clone(m.activeRepo.Branches)

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		summary, branches, err := m.clean.BuildReleaseAutocheckCandidates(ctx, repo, branchesSnapshot, choice.Group, choice.Version.ID)
		if err != nil {
			return releaseAutocheckAppliedMsg{actionKey: actionKey, actionID: actionID, repoName: repo.Name, summary: summary, err: err}
		}

		selectedID := strings.TrimSpace(choice.Version.ID)
		return releaseAutocheckAppliedMsg{
			actionKey:  actionKey,
			actionID:   actionID,
			repoName:   repo.Name,
			summary:    summary,
			branches:   branches,
			selectedID: selectedID,
		}
	})
}

func (m *Model) openConfirmIfPossible() bool {
	if len(m.selectedBranches(m.activeRepo.RepoName)) == 0 {
		return false
	}
	m.confirmType = confirmGenerate
	return true
}

func (m *Model) activateSelectedRepoFromCache() {
	repoName := m.selectedRepoName()
	if repoName == "" {
		return
	}

	rb, ok := m.repoData[repoName]
	if !ok {
		m.activeRepo = model.RepoBranches{RepoName: repoName}
		return
	}

	m.activeRepo = rb
	m.ensureRepoState(repoName)
	m.clampBranchCursor(repoName)
	m.ensureBranchCursorVisible(repoName)
}

func (m Model) hasLoadedBranches(repoName string) bool {
	_, ok := m.repoData[repoName]
	return ok
}

func (m Model) loadingSelectedRepo() bool {
	repoName := m.selectedRepoName()
	if repoName == "" {
		return false
	}

	return m.repoLoading[repoName]
}

func (m Model) canUseF7ForActiveRepo() bool {
	repo, ok := m.cfg.RepoByName(m.activeRepo.RepoName)
	if !ok {
		return false
	}
	return strings.TrimSpace(repo.Path) != ""
}

func (m *Model) ensureAppContext() {
	if m.appCtx != nil {
		return
	}

	m.appCtx, m.appCancel = context.WithCancel(context.Background())
}

func (m *Model) beginAction(actionKey string) (context.Context, int) {
	m.ensureAppContext()
	if actionKey == "" {
		m.actionSeq++
		return m.appCtx, m.actionSeq
	}

	if ref, ok := m.actionCancels[actionKey]; ok && ref.cancel != nil {
		ref.cancel()
		delete(m.actionCancels, actionKey)
	}

	m.actionSeq++
	actionID := m.actionSeq
	ctx, cancel := context.WithCancel(m.appCtx)
	m.actionCancels[actionKey] = actionCancelRef{id: actionID, cancel: cancel}
	return ctx, actionID
}

func (m *Model) finishAction(actionKey string, actionID int) {
	if actionKey == "" {
		return
	}

	ref, ok := m.actionCancels[actionKey]
	if !ok || ref.id != actionID {
		return
	}

	if ref.cancel != nil {
		ref.cancel()
	}
	delete(m.actionCancels, actionKey)
}

func (m *Model) cancelAllOperations() {
	for key, ref := range m.actionCancels {
		if ref.cancel != nil {
			ref.cancel()
		}
		delete(m.actionCancels, key)
	}

	if m.appCancel != nil {
		m.appCancel()
	}
	m.appCtx = nil
	m.appCancel = nil
}

func actionKeyLoadRepo(repoName string) string {
	return "repo.load." + strings.TrimSpace(repoName)
}

func actionKeyRepoStat(repoName string) string {
	return "repo.stat." + strings.TrimSpace(repoName)
}

func actionKeyCheckout(repoName string) string {
	return "repo.checkout." + strings.TrimSpace(repoName)
}

func actionKeyLocalCopy(repoName string) string {
	return "repo.localcopy." + strings.TrimSpace(repoName)
}

func actionKeyFetchPull(repoName string) string {
	return "repo.fetchpull." + strings.TrimSpace(repoName)
}

func actionKeyReleaseOptions(repoName string) string {
	return "repo.release.options." + strings.TrimSpace(repoName)
}

func actionKeyReleaseApply(repoName string) string {
	return "repo.release.apply." + strings.TrimSpace(repoName)
}
