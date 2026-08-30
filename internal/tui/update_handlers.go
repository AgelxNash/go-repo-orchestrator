package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.ensureRepoCursorVisible()
	m.ensureBranchCursorVisible(m.activeRepo.RepoName)
	return m, nil
}

func (m Model) handleStartupLog(msg startupLogMsg) (tea.Model, tea.Cmd) {
	m.pushLog(msg.text)
	m.updateStartupCurrentOpFromLog(msg.text)
	return m, nil
}

func (m Model) handleStartupTimerTick(msg startupTimerTickMsg) (tea.Model, tea.Cmd) {
	if !m.startupLoading {
		return m, nil
	}
	m.updateStartupElapsed(msg.at)
	return m, startupTimerTickCmd()
}

func (m Model) handleRepoLoadJiraProgress(msg repoLoadJiraProgressMsg) (tea.Model, tea.Cmd) {
	if msg.startup && m.startupLoading {
		m.setStartupStage(msg.repoName, "проверка Jira")
	}
	if msg.progress.BatchTotal > 0 {
		m.pushLog(fmt.Sprintf("[JIRA] %s: проверена пачка %d/%d", msg.repoName, msg.progress.BatchIndex, msg.progress.BatchTotal))
	}
	if msg.progress.Total > 0 {
		m.pushLog(fmt.Sprintf("[JIRA] %s: обработано %d/%d веток", msg.repoName, msg.progress.Processed, msg.progress.Total))
	}
	return m, waitRepoLoadJiraProgressCmd(msg.repoName, msg.startup, msg.stream)
}

func (m Model) handlePlaywrightStartupCompleted(msg playwrightStartupCompletedMsg) (tea.Model, tea.Cmd) {
	m.finishStartupTaskIfNeeded(true)
	if msg.err != nil {
		warn := "Предупреждение: браузер Playwright не запущен: " + msg.err.Error()
		m.SetStartupWarning(warn)
		m.startupPlaywrightState = startupPlaywrightFailed
		m.pushLog("[WARN] Playwright: runtime недоступен, использую HTTP fallback: " + msg.err.Error())
		if m.startupLoading {
			m.setStartupProgressStatus()
		}
		return m, nil
	}

	m.startupPlaywrightState = startupPlaywrightReady
	m.setStartupStage("", "инициализация Playwright")
	m.pushLog("[OK] Playwright: runtime готов")
	if m.startupLoading {
		m.setStartupProgressStatus()
	}
	return m, nil
}

func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.loadingSelectedRepo() && !m.startupLoading && !m.refreshLocked && !m.releaseLoading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m Model) handleBranchesLoaded(msg branchesLoadedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	if expectedReqID := m.repoLoadReq[msg.repoName]; expectedReqID != msg.requestID {
		return m, nil
	}
	if msg.startup && m.startupLoading {
		m.setStartupStage(msg.repoName, "получение результата загрузки веток")
	}
	startupInProgress := m.startupLoading
	m.repoLoading[msg.repoName] = false
	m.finishRefreshIfMatched(msg.repoName, msg.requestID)
	m.finishRefreshPendingIfNeeded(msg.repoName)
	m.finishStartupURLTaskIfNeeded(msg.repoName, msg.startup)
	if msg.startup && startupInProgress {
		m.markStartupRepoDone(msg.repoName)
	}
	m.finishStartupTaskIfNeeded(msg.startup)
	if msg.startup && startupInProgress && m.startupLoading {
		m.setStartupProgressStatus()
	}
	friendlyErr := userFacingError(msg.err)
	if msg.err != nil {
		stat := model.RepoStat{Loaded: true}
		if friendlyErr != nil {
			stat.LoadError = friendlyErr.Error()
		}
		m.repoStats[msg.repoName] = stat
		delete(m.repoData, msg.repoName)
		if msg.repoName == m.selectedRepoName() && (!msg.startup || !startupInProgress) {
			m.statusLine = fmt.Sprintf("Не удалось загрузить %q: %s", msg.repoName, stat.LoadError)
		}
		if msg.startup {
			m.pushLog(fmt.Sprintf("[ERR] %s: загрузка веток не удалась: %s", msg.repoName, stat.LoadError))
		}
		m.activateSelectedRepoFromCache()
		return m, nil
	}
	m.repoData[msg.repoName] = msg.rb
	m.applyAutocheckSelection(msg.repoName, msg.rb.Branches)
	syncWarning := strings.TrimSpace(msg.rb.SyncWarning)
	if syncWarning != "" {
		if friendly := userFacingError(errors.New(syncWarning)); friendly != nil {
			syncWarning = friendly.Error()
		}
	}
	m.repoStats[msg.repoName] = model.RepoStat{
		CurrentBranch: msg.rb.CurrentBranch,
		DirtyStats:    msg.rb.DirtyStats,
		LoadError:     "",
		SyncWarning:   syncWarning,
		Loaded:        true,
	}
	m.ensureRepoState(msg.repoName)
	m.activateSelectedRepoFromCache()
	if msg.repoName == m.selectedRepoName() && (!msg.startup || !startupInProgress) {
		if syncWarning != "" {
			m.statusLine = fmt.Sprintf("Репозиторий %q загружен из локальных данных: %s", msg.repoName, syncWarning)
		} else {
			m.statusLine = fmt.Sprintf("Репозиторий %q синхронизирован", msg.repoName)
		}
	}
	if msg.startup {
		m.pushLog(fmt.Sprintf("[GIT] %s: загружено %d веток", msg.repoName, len(msg.rb.Branches)))
		if !msg.jiraProgressStreamed {
			for _, batch := range msg.jiraBatchProgress {
				if batch.BatchTotal > 0 {
					m.pushLog(fmt.Sprintf("[JIRA] %s: проверена пачка %d/%d", msg.repoName, batch.BatchIndex, batch.BatchTotal))
				}
				if batch.Total > 0 {
					m.pushLog(fmt.Sprintf("[JIRA] %s: обработано %d/%d веток", msg.repoName, batch.Processed, batch.Total))
				}
			}
		}
		if msg.jiraResolved > 0 {
			m.pushLog(fmt.Sprintf("[JIRA] %s: проверены статусы для %d веток", msg.repoName, msg.jiraResolved))
		}
		if syncWarning != "" {
			m.pushLog(fmt.Sprintf("[WARN] %s: использую кэш: %s", msg.repoName, syncWarning))
		}
		m.pushLog(fmt.Sprintf("[OK] %s: синхронизация завершена%s", msg.repoName, msg.syncNote))
	} else {
		if len(msg.jiraBatchProgress) > 0 && !msg.jiraProgressStreamed {
			last := msg.jiraBatchProgress[len(msg.jiraBatchProgress)-1]
			if last.Total > 0 {
				m.pushLog(fmt.Sprintf("[JIRA] %s: обновлено %d/%d", msg.repoName, last.Processed, last.Total))
			}
		}
		if syncWarning != "" {
			m.pushLog(fmt.Sprintf("[WARN] %s: %s", msg.repoName, syncWarning))
		} else {
			m.pushLog(fmt.Sprintf("[OK] %s синхронизирован (%s)", msg.repoName, valueOrDash(msg.rb.CurrentBranch)))
		}
	}
	m.clampBranchCursor(msg.repoName)
	m.ensureRepoCursorVisible()
	m.ensureBranchCursorVisible(msg.repoName)
	return m, nil
}

func (m Model) handleInitialLoad(msg initialLoadMsg) (tea.Model, tea.Cmd) {
	return m, m.startInitialLoads()
}

func (m Model) handleScriptGenerated(msg scriptGeneratedMsg) (tea.Model, tea.Cmd) {
	m.confirmType = confirmNone
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = "Ошибка генерации скрипта"
		return m, nil
	}
	m.lastGenerated = &msg.result
	m.statusLine = fmt.Sprintf("Скрипт создан: %s", filepath.Base(msg.result.ScriptPath))
	m.pushLog(fmt.Sprintf("[СКРИПТ] создан: %s", filepath.Base(msg.result.ScriptPath)))
	return m, nil
}

func (m Model) handleRepoStatLoaded(msg repoStatLoadedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.finishRefreshPendingIfNeeded(msg.repoName)
	startupInProgress := m.startupLoading
	if msg.startup && startupInProgress {
		m.setStartupStage(msg.repoName, "получение результата статуса Git")
	}
	if msg.startup && startupInProgress {
		m.markStartupRepoDone(msg.repoName)
	}
	m.finishStartupTaskIfNeeded(msg.startup)
	stat := msg.stat
	stat.Loaded = true
	if strings.TrimSpace(stat.SyncWarning) != "" {
		if friendly := userFacingError(errors.New(stat.SyncWarning)); friendly != nil {
			stat.SyncWarning = friendly.Error()
		}
	}
	if msg.err != nil {
		friendly := userFacingError(msg.err)
		if friendly != nil {
			stat.LoadError = friendly.Error()
		}
	}
	m.repoStats[msg.repoName] = stat
	if msg.startup && startupInProgress {
		if stat.LoadError != "" {
			m.pushLog(fmt.Sprintf("[ERR] %s: статус Git не получен: %s", msg.repoName, stat.LoadError))
		} else if strings.TrimSpace(stat.SyncWarning) != "" {
			m.pushLog(fmt.Sprintf("[WARN] %s: статус Git из локальных данных: %s", msg.repoName, stat.SyncWarning))
			m.pushLog(fmt.Sprintf("[OK] %s: синхронизация завершена", msg.repoName))
		} else {
			m.pushLog(fmt.Sprintf("[GIT] %s: статус получен (ветка: %s)", msg.repoName, valueOrDash(stat.CurrentBranch)))
			m.pushLog(fmt.Sprintf("[OK] %s: синхронизация завершена", msg.repoName))
		}
	}
	return m, nil
}

func (m Model) handleCheckoutCompleted(msg checkoutCompletedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.repoLoading[msg.repoName] = false
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = fmt.Sprintf("Не удалось переключиться на ветку в %q: %s", msg.repoName, m.err.Error())
		return m, nil
	}

	m.statusLine = fmt.Sprintf("Ветка в %q переключена", msg.repoName)
	if m.activeRepo.RepoName == msg.repoName {
		return m, m.startLoadSelectedRepo()
	}

	repo, ok := m.cfg.RepoByName(msg.repoName)
	if ok {
		actionKey := actionKeyRepoStat(repo.Name)
		ctx, actionID := m.beginAction(actionKey)
		return m, loadRepoStatCmd(ctx, m.clean, repo, false, actionKey, actionID)
	}
	return m, nil
}

func (m Model) handleLocalCopyCompleted(msg localCopyCompletedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.repoLoading[msg.repoName] = false
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = fmt.Sprintf("Не удалось создать локальную копию в %q: %s", msg.repoName, m.err.Error())
		return m, nil
	}

	m.statusLine = fmt.Sprintf("Создана и активирована локальная ветка %q", msg.branch)
	if m.activeRepo.RepoName == msg.repoName {
		return m, m.startLoadSelectedRepo()
	}

	repo, ok := m.cfg.RepoByName(msg.repoName)
	if ok {
		actionKey := actionKeyRepoStat(repo.Name)
		ctx, actionID := m.beginAction(actionKey)
		return m, loadRepoStatCmd(ctx, m.clean, repo, false, actionKey, actionID)
	}
	return m, nil
}

func (m Model) handleRepoFetchPullCompleted(msg repoFetchPullCompletedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.repoLoading[msg.repoName] = false
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = fmt.Sprintf("Не удалось выполнить fetch + pull для %q: %s", msg.repoName, m.err.Error())
		m.releaseRefreshLock(msg.repoName)
		return m, nil
	}

	m.statusLine = fmt.Sprintf("Репозиторий %q обновлен (fetch + pull)", msg.repoName)
	if msg.repoName == m.selectedRepoName() {
		return m, m.startLoadSelectedRepo()
	}

	repo, ok := m.cfg.RepoByName(msg.repoName)
	if ok {
		m.releaseRefreshLock(msg.repoName)
		actionKey := actionKeyRepoStat(repo.Name)
		ctx, actionID := m.beginAction(actionKey)
		return m, loadRepoStatCmd(ctx, m.clean, repo, false, actionKey, actionID)
	}
	m.releaseRefreshLock(msg.repoName)
	return m, nil
}

func (m Model) handleReleaseOptionsLoaded(msg releaseOptionsLoadedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.releaseLoading = false
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = "Не удалось загрузить Jira releases"
		return m, nil
	}

	m.releaseOptions = slices.Clone(msg.options)
	m.releaseOptionIdx = 0
	if len(m.releaseOptions) == 0 {
		m.statusLine = "Released версии Jira не найдены для веток текущего репозитория"
		return m, nil
	}

	selectedID := strings.TrimSpace(m.releaseSelectionByRepo[msg.repoName])
	if selectedID != "" {
		for idx, option := range m.releaseOptions {
			if strings.TrimSpace(option.Version.ID) == selectedID {
				m.releaseOptionIdx = idx
				break
			}
		}
	}

	m.confirmType = confirmReleaseSelect
	m.statusLine = "Выберите release и нажмите Enter для автопометки"
	return m, nil
}

func (m Model) handleReleaseAutocheckApplied(msg releaseAutocheckAppliedMsg) (tea.Model, tea.Cmd) {
	m.finishAction(msg.actionKey, msg.actionID)
	m.releaseLoading = false
	m.err = userFacingError(msg.err)
	if msg.err != nil {
		m.statusLine = "Release-driven автопометка не выполнена"
		return m, nil
	}

	selected := m.ensureRepoSelection(msg.repoName)
	added := 0
	for _, branch := range msg.branches {
		key := m.branchSelectionKey(branch)
		if _, exists := selected[key]; exists {
			continue
		}
		selected[key] = true
		added++
	}

	if strings.TrimSpace(msg.selectedID) != "" {
		m.releaseSelectionByRepo[msg.repoName] = strings.TrimSpace(msg.selectedID)
	}

	m.statusLine = fmt.Sprintf(
		"Release %s: issues=%d, matches=%d, selected=%d, protected=%d, skippedNoJira=%d",
		valueOrDash(msg.summary.ReleaseID),
		msg.summary.IssueKeysTotal,
		msg.summary.BranchMatches,
		added,
		msg.summary.BranchSkippedProtect,
		msg.summary.BranchSkippedNoJira,
	)
	return m, nil
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		m.cancelAllOperations()
		return m, tea.Quit
	}

	if m.startupLoading {
		return m, nil
	}

	if m.refreshLocked {
		return m, nil
	}

	if m.searchMode {
		return m.handleSearchKeyMsg(msg)
	}

	if m.confirmType != confirmNone {
		return m.updateConfirm(msg)
	}

	return m.handleGlobalKeyMsg(msg)
}

func (m Model) handleSearchKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searchMode = false
		m.searchInput.Blur()
		return m, nil
	default:
		prevRepoIdx := m.repoIdx
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)

		if m.focus == focusRepos {
			indices := m.visibleRepoIndices()
			if len(indices) > 0 {
				found := false
				for _, idx := range indices {
					if idx == m.repoIdx {
						found = true
						break
					}
				}
				if !found {
					m.repoIdx = indices[0]
				}
			}
			if m.repoIdx != prevRepoIdx {
				m.ensureRepoCursorVisible()
				m.activateSelectedRepoFromCache()
				return m, cmd
			}
			m.ensureRepoCursorVisible()
		} else {
			m.clampBranchCursor(m.activeRepo.RepoName)
			m.ensureBranchCursorVisible(m.activeRepo.RepoName)
		}
		return m, cmd
	}
}

func (m Model) handleGlobalKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f2":
		m.showInfo = !m.showInfo
		m.statusLine = fmt.Sprintf("Инфо-панель: %s", onOff(m.showInfo))
		return m, nil
	case "f6":
		if m.focus == focusBranches {
			m.toggleBranchSortMode()
			return m, nil
		}
		m.toggleRepoSortMode()
		return m, nil
	case "tab":
		return m, nil
	case "f4":
		if m.focus != focusBranches {
			return m, nil
		}
		m.toggleBranchScope()
		return m, nil
	case "f9":
		if m.focus != focusBranches {
			return m, nil
		}
		m.toggleProtectedFilter()
		return m, nil
	case "f3":
		m.searchMode = true
		m.searchInput.Focus()
		return m, textinput.Blink
	case "f7":
		if m.focus == focusBranches {
			return m, m.startCreateLocalCopyFromCurrentRemoteBranch()
		}
		if m.focus == focusRepos {
			return m, m.startFetchAndPullActiveRepo()
		}
		return m, nil
	case "g", "f8":
		if !m.openConfirmIfPossible() {
			m.statusLine = "Нет выбранных веток для генерации скрипта"
			return m, nil
		}
		return m, nil
	}

	if isReleaseAutocheckKey(msg) {
		if m.focus != focusBranches {
			return m, nil
		}
		return m, m.startLoadReleaseOptions()
	}

	if m.focus == focusRepos {
		return m.updateReposPanel(msg)
	}
	return m.updateBranchesPanel(msg)
}
