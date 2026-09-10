package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// startInitialLoads запускает первую загрузку репозиториев при старте TUI.
func (m *Model) startInitialLoads() tea.Cmd {
	return m.startPreloadPass(true, false)
}

// startRescanAllRepos перезапускает загрузку всех репозиториев, сохраняя выбор.
func (m *Model) startRescanAllRepos() tea.Cmd {
	return m.startPreloadPass(false, true)
}

// startPreloadPass запускает параллельную загрузку репозиториев при старте или пересканировании.
func (m *Model) startPreloadPass(startup bool, keepSelection bool) tea.Cmd {
	if len(m.cfg.Repos) == 0 {
		return nil
	}

	if !keepSelection {
		m.selectFirstVisibleRepo()
	}

	m.startupLoading = startup
	m.startupPending = 0
	m.startupTotal = 0
	m.startupURLTotal = 0
	m.startupURLDone = 0
	m.startupRepoTotal = len(m.cfg.Repos)
	m.startupRepoDone = 0
	m.startupCurrentRepo = ""
	m.startupCurrentStage = ""
	m.startupCurrentOp = "Подготовка startup-задач"
	m.startupStartedAt = time.Now()
	m.startupElapsed = 0
	m.startupStageStartedAt = m.startupStartedAt
	m.startupStageElapsed = 0
	for repoName := range m.startupRepoDoneSet {
		delete(m.startupRepoDoneSet, repoName)
	}

	if !startup {
		m.startupCurrentOp = ""
		m.startupCurrentRepo = ""
		m.startupCurrentStage = ""
		m.startupStartedAt = time.Time{}
		m.startupElapsed = 0
		m.startupStageStartedAt = time.Time{}
		m.startupStageElapsed = 0
		m.startupTotal = 0
		m.refreshLocked = true
		m.refreshAll = true
		m.refreshRepo = ""
		m.refreshReqID = 0
		m.err = nil
		for repoName := range m.refreshPending {
			delete(m.refreshPending, repoName)
		}
	}

	var cmds []tea.Cmd

	if startup && m.startupPlaywrightStartFn != nil && !m.startupPlaywrightScheduled {
		m.setStartupStage("", "инициализация Playwright")
		cmds = append(cmds, tea.Batch(
			func() tea.Msg { return startupLogMsg{"[СТАРТ] Playwright: запускаю runtime"} },
			func() tea.Msg {
				return playwrightStartupCompletedMsg{err: m.startupPlaywrightStartFn()}
			},
		))
		m.startupPending++
		m.startupPlaywrightScheduled = true
		m.startupPlaywrightState = startupPlaywrightPending
	}

	for idx, repo := range m.cfg.Repos {
		if !startup {
			m.refreshPending[repo.Name] = true
		}
		switch repo.SourceType() {
		case "url":
			m.startupURLTotal++
			m.setStartupStage(repo.Name, "подготовка загрузки веток")
			cmds = append(cmds, m.startLoadRepo(repo, false, startup))
			m.startupPending++
		case "opensource":
			m.startupURLTotal++
			m.setStartupStage(repo.Name, "подготовка загрузки веток")
			cmds = append(cmds, m.startLoadRepo(repo, false, startup))
			m.startupPending++
		case "path":
			if idx == m.repoIdx {
				m.setStartupStage(repo.Name, "подготовка загрузки веток")
				cmds = append(cmds, m.startLoadRepo(repo, false, startup))
				m.startupPending++
			} else {
				m.setStartupStage(repo.Name, "подготовка статуса Git")
				actionKey := actionKeyRepoStat(repo.Name)
				ctx, actionID := m.beginAction(actionKey)
				cmds = append(cmds, loadRepoStatCmd(ctx, m.clean, repo, startup, actionKey, actionID))
				m.startupPending++
			}
		}
	}

	if !startup {
		m.statusLine = "Пересканирование всех репозиториев..."
	} else {
		m.startupTotal = m.startupPending
		m.setStartupStage("", "выполнение startup-задач")
		m.setStartupProgressStatus()
	}

	m.activateSelectedRepoFromCache()
	if m.startupPending == 0 {
		m.startupLoading = false
		m.startupCurrentOp = ""
		m.resetRefreshLock()
		return nil
	}

	if m.loadingSelectedRepo() || m.startupLoading || m.refreshLocked {
		cmds = append(cmds, m.spinner.Tick)
	}
	if startup {
		cmds = append(cmds, startupTimerTickCmd())
	}
	return tea.Batch(cmds...)
}

// finishStartupTaskIfNeeded уменьшает счетчик startup-задач и показывает сводку по завершении.
func (m *Model) finishStartupTaskIfNeeded(startup bool) {
	if !startup || !m.startupLoading {
		return
	}
	if m.startupPending > 0 {
		m.startupPending--
	}
	if m.startupPending == 0 {
		m.startupLoading = false
		m.startupCurrentOp = "Инициализация завершена"
		m.startupCurrentRepo = ""
		m.startupCurrentStage = "завершено"
		m.startupStageElapsed = 0
		m.applyStartupCompletionSummary()
	}
}

// applyStartupCompletionSummary записывает итог загрузки в статус/лог и переходит к проблемному репо.
func (m *Model) applyStartupCompletionSummary() {
	summary := m.startupCompletionSummary()
	m.statusLine = summary.text
	if summary.logLine != "" {
		m.pushLog(summary.logLine)
	}
	for _, line := range summary.problemLines {
		m.pushLog(line)
	}
	if summary.firstProblem != "" {
		m.selectRepoByName(summary.firstProblem)
	}
}

// startupCompletionSummary описывает итог первичной синхронизации для статус-строки и лога.
type startupCompletionSummary struct {
	text         string
	logLine      string
	problemLines []string
	firstProblem string
}

// startupCompletionSummary считает успешные, кэш, сетевые и прочие ошибки загрузки, клоны без checkout.
func (m Model) startupCompletionSummary() startupCompletionSummary {
	total := len(m.cfg.Repos)
	synced := 0
	var (
		networkFailed []string
		loadFailed    []string
		cache         []string
		emptyClones   []string
	)
	for _, repo := range m.cfg.Repos {
		stat := m.repoStats[repo.Name]
		switch {
		case stat.HasError():
			if stat.LoadErrorKind == model.RepoLoadErrorKindNetwork {
				networkFailed = append(networkFailed, repo.Name)
			} else {
				loadFailed = append(loadFailed, repo.Name)
			}
		case stat.HasEmptyCloneWarning():
			emptyClones = append(emptyClones, repo.Name)
		case stat.HasSyncWarning():
			cache = append(cache, repo.Name)
		case stat.Loaded:
			synced++
		}
	}

	parts := []string{fmt.Sprintf("синхронизировано %d/%d", synced, total)}
	if len(networkFailed) > 0 {
		parts = append(parts, fmt.Sprintf("%s (сеть)", ruCount(len(networkFailed), "ошибка", "ошибки", "ошибок")))
	}
	if len(loadFailed) > 0 {
		parts = append(parts, fmt.Sprintf("%s загрузки", ruCount(len(loadFailed), "ошибка", "ошибки", "ошибок")))
	}
	if len(cache) > 0 {
		parts = append(parts, fmt.Sprintf("%s из кэша", ruCount(len(cache), "репозиторий", "репозитория", "репозиториев")))
	}
	if len(emptyClones) > 0 {
		parts = append(parts, fmt.Sprintf("%s без checkout", ruCount(len(emptyClones), "репозиторий", "репозитория", "репозиториев")))
	}

	summary := startupCompletionSummary{
		text: "Первичная синхронизация завершена: " + strings.Join(parts, ", "),
	}
	if len(networkFailed)+len(loadFailed)+len(cache)+len(emptyClones) == 0 {
		return summary
	}

	summary.logLine = "[СВОДКА] " + summary.text
	for _, name := range networkFailed {
		summary.problemLines = append(summary.problemLines, "[ERR] "+name+": "+loadErrorSummaryLabel(model.RepoLoadErrorKindNetwork))
	}
	for _, name := range loadFailed {
		summary.problemLines = append(summary.problemLines, "[ERR] "+name+": "+loadErrorSummaryLabel(m.repoStats[name].LoadErrorKind))
	}
	for _, name := range cache {
		summary.problemLines = append(summary.problemLines, "[WARN] "+name+": используется кэш, remote не обновлен")
	}
	for _, name := range emptyClones {
		summary.problemLines = append(summary.problemLines, "[WARN] "+name+": репозиторий-оболочка без checkout")
	}
	switch {
	case len(networkFailed) > 0:
		summary.firstProblem = networkFailed[0]
	case len(loadFailed) > 0:
		summary.firstProblem = loadFailed[0]
	case len(emptyClones) > 0:
		summary.firstProblem = emptyClones[0]
	case len(cache) > 0:
		summary.firstProblem = cache[0]
	}
	return summary
}

// ruCount форматирует число с русской формой существительного (1/2-4/5+).
func ruCount(n int, one, few, many string) string {
	nAbs := n % 100
	n1 := nAbs % 10
	word := many
	switch {
	case nAbs >= 11 && nAbs <= 14:
		word = many
	case n1 == 1:
		word = one
	case n1 >= 2 && n1 <= 4:
		word = few
	}
	return fmt.Sprintf("%d %s", n, word)
}

// finishStartupURLTaskIfNeeded учитывает завершение синхронизации URL/opensource-репозитория.
func (m *Model) finishStartupURLTaskIfNeeded(repoName string, startup bool) {
	if !startup {
		return
	}

	repo, ok := m.cfg.RepoByName(repoName)
	if !ok {
		return
	}

	source := repo.SourceType()
	if source != "url" && source != "opensource" {
		return
	}

	if m.startupURLDone < m.startupURLTotal {
		m.startupURLDone++
		m.setStartupStage(repoName, "синхронизация URL-репозитория")
		m.pushLog(fmt.Sprintf("[OK] %s: синхронизация URL %d/%d", repoName, m.startupURLDone, m.startupURLTotal))
	}
}

func (m *Model) setStartupProgressStatus() {
	if m.startupURLTotal <= 0 {
		m.statusLine = "Первичная загрузка репозиториев..."
		return
	}

	m.statusLine = fmt.Sprintf("Первичная синхронизация URL-репозиториев: %d/%d", m.startupURLDone, m.startupURLTotal)
}

func (m *Model) markStartupRepoDone(repoName string) {
	repoName = strings.TrimSpace(repoName)
	if repoName == "" {
		return
	}
	if m.startupRepoDoneSet[repoName] {
		return
	}
	m.startupRepoDoneSet[repoName] = true
	if m.startupRepoDone < m.startupRepoTotal {
		m.startupRepoDone++
	}
}

func (m *Model) finishRefreshIfMatched(repoName string, requestID int) {
	if !m.refreshLocked {
		return
	}
	if m.refreshAll {
		return
	}
	if m.refreshRepo != repoName || m.refreshReqID != requestID {
		return
	}
	m.resetRefreshLock()
}

func (m *Model) releaseRefreshLock(repoName string) {
	if !m.refreshLocked {
		return
	}
	if m.refreshAll {
		return
	}
	if m.refreshRepo != repoName {
		return
	}
	m.resetRefreshLock()
}

func (m *Model) finishRefreshPendingIfNeeded(repoName string) {
	if !m.refreshLocked || !m.refreshAll {
		return
	}
	if !m.refreshPending[repoName] {
		return
	}
	delete(m.refreshPending, repoName)
	if len(m.refreshPending) > 0 {
		return
	}
	m.statusLine = "Пересканирование всех репозиториев завершено"
	m.resetRefreshLock()
}

func (m *Model) resetRefreshLock() {
	m.refreshLocked = false
	m.refreshAll = false
	m.refreshRepo = ""
	m.refreshReqID = 0
	for repoName := range m.refreshPending {
		delete(m.refreshPending, repoName)
	}
}

const maxEventLog = 50

func (m *Model) pushLog(msg string) {
	if msg == "" {
		return
	}
	m.eventLog = append(m.eventLog, msg)
	if len(m.eventLog) > maxEventLog {
		m.eventLog = m.eventLog[len(m.eventLog)-maxEventLog:]
	}
}
func (m *Model) setStartupStage(repoName, stage string) {
	if !m.startupLoading {
		return
	}
	stage = strings.TrimSpace(stage)
	repoName = strings.TrimSpace(repoName)
	stageChanged := m.startupCurrentRepo != repoName || m.startupCurrentStage != stage
	m.startupCurrentRepo = repoName
	m.startupCurrentStage = stage
	if stageChanged {
		m.startupStageStartedAt = time.Now()
		m.startupStageElapsed = 0
	}
	if repoName != "" && stage != "" {
		m.startupCurrentOp = fmt.Sprintf("%s: %s", repoName, stage)
		return
	}
	if stage != "" {
		m.startupCurrentOp = stage
		return
	}
	m.startupCurrentOp = repoName
}

func startupTimerTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return startupTimerTickMsg{at: t}
	})
}

func (m *Model) updateStartupElapsed(now time.Time) {
	if !m.startupLoading {
		return
	}
	if m.startupStartedAt.IsZero() {
		m.startupStartedAt = now
	}
	m.startupElapsed = now.Sub(m.startupStartedAt).Round(time.Second)
	if !m.startupStageStartedAt.IsZero() {
		m.startupStageElapsed = now.Sub(m.startupStageStartedAt).Round(time.Second)
	}
}

func formatSeconds(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%dс", int(d.Round(time.Second)/time.Second))
}

func (m *Model) updateStartupCurrentOpFromLog(entry string) {
	if !m.startupLoading {
		return
	}
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return
	}
	if strings.HasPrefix(entry, "[") {
		if idx := strings.Index(entry, "]"); idx >= 0 && idx+1 < len(entry) {
			entry = strings.TrimSpace(entry[idx+1:])
		}
	}
	if before, after, ok := strings.Cut(entry, ":"); ok {
		m.setStartupStage(before, after)
		return
	}
	m.setStartupStage("", entry)
}

// viewStartupScreen рисует экран инициализации со спиннером, прогрессом и логом.
func (m Model) viewStartupScreen() string {
	usableW := max(40, m.width-4)
	usableH := max(12, m.height-2)

	progHeader := titleStyle.Width(usableW).Render("  go-repo-orchestrator  ")
	spinLine := fmt.Sprintf("  %s Инициализация репозиториев  ", m.spinner.View())

	var progressLines []string
	if m.startupURLTotal > 0 {
		bar := startupProgressBar(m.startupURLDone, m.startupURLTotal, usableW-30)
		progressLines = append(progressLines, fmt.Sprintf("  Синхронизация URL-репо: %s %d/%d  ", bar, m.startupURLDone, m.startupURLTotal))
	}
	if m.startupRepoTotal > 0 {
		bar := startupProgressBar(m.startupRepoDone, m.startupRepoTotal, usableW-30)
		progressLines = append(progressLines, fmt.Sprintf("  Репозитории: %s %d/%d  ", bar, m.startupRepoDone, m.startupRepoTotal))
	}
	if m.startupTotal > 0 {
		startupDone := max(0, m.startupTotal-m.startupPending)
		bar := startupProgressBar(startupDone, m.startupTotal, usableW-30)
		progressLines = append(progressLines, fmt.Sprintf("  Задачи startup: %s %d/%d  ", bar, startupDone, m.startupTotal))
	}
	if m.startupCurrentRepo != "" {
		progressLines = append(progressLines, fmt.Sprintf("  Текущий репозиторий: %s  ", m.startupCurrentRepo))
	}
	if m.startupCurrentStage != "" {
		if m.startupStageElapsed > 0 {
			progressLines = append(progressLines, fmt.Sprintf("  Этап: %s (%s)  ", m.startupCurrentStage, formatSeconds(m.startupStageElapsed)))
		} else {
			progressLines = append(progressLines, fmt.Sprintf("  Этап: %s  ", m.startupCurrentStage))
		}
	}
	if m.startupElapsed > 0 {
		progressLines = append(progressLines, fmt.Sprintf("  Прошло: %s  ", formatSeconds(m.startupElapsed)))
	}
	if m.startupCurrentOp != "" {
		progressLines = append(progressLines, fmt.Sprintf("  Операция: %s  ", m.startupCurrentOp))
	}
	if m.startupPlaywrightStartFn != nil {
		playwrightLine := "  Playwright: ожидание...  "
		switch m.startupPlaywrightState {
		case startupPlaywrightReady:
			playwrightLine = "  Playwright: готов  "
		case startupPlaywrightFailed:
			playwrightLine = "  Playwright: недоступен (HTTP fallback)  "
		case startupPlaywrightPending:
			playwrightLine = "  Playwright: запуск...  "
		}
		progressLines = append(progressLines, playwrightLine)
	}
	if m.startupWarn != "" {
		progressLines = append(progressLines, warnStyle.Render("  "+m.startupWarn+"  "))
	}

	headerBlock := lipgloss.JoinVertical(lipgloss.Left,
		progHeader,
		topMenuStyle.Width(usableW).Render(spinLine),
	)
	for _, l := range progressLines {
		headerBlock = lipgloss.JoinVertical(lipgloss.Left, headerBlock,
			statusStyle.Width(usableW).Render(l))
	}

	headerLines := lipgloss.Height(headerBlock)

	logH := max(4, usableH-headerLines-2)
	logBlock := m.viewStartupLogPanel(usableW, logH)

	full := lipgloss.JoinVertical(lipgloss.Left, headerBlock, logBlock)
	return appStyle.Width(m.width).Height(m.height).Render(
		lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, full),
	)
}

// startupProgressBar рисует полосу прогресса startup-задач.
func startupProgressBar(done, total, width int) string {
	if width <= 0 || total <= 0 {
		return ""
	}
	width = min(40, max(10, width))
	filled := done * width / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return lipgloss.NewStyle().Foreground(mcBrightCyan).Render(bar)
}

// viewStartupLogPanel рисует прокручиваемый лог загрузки с переносом длинных ошибок.
func (m Model) viewStartupLogPanel(width, height int) string {
	logBg := lipgloss.Color("17")
	logFg := lipgloss.Color("252")
	dimFg := lipgloss.Color("240")
	borderFg := lipgloss.Color("27")

	st := lipgloss.NewStyle().
		Background(logBg).
		Foreground(logFg).
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderFg).
		Width(width).
		Height(height)

	headerSt := lipgloss.NewStyle().
		Background(lipgloss.Color("20")).
		Foreground(mcWhite).
		Bold(true).
		Width(width-2).
		Padding(0, 1)

	innerW := max(10, width-4)
	innerH := max(1, height-4)

	var lines []string
	lines = append(lines, headerSt.Render(" ЛОГ СОБЫТИЙ ЗАГРУЗКИ "))

	if len(m.eventLog) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(dimFg).Background(logBg).Render("  (ожидание событий...)"))
	} else {
		var rendered []string
		for _, entry := range m.eventLog {
			var entryFg lipgloss.Color
			switch {
			case strings.HasPrefix(entry, "[WARN]") || strings.HasPrefix(entry, "[ERR]") || strings.HasPrefix(entry, "[СВОДКА]"):
				entryFg = mcYellow
			case strings.HasPrefix(entry, "[OK]"), strings.HasPrefix(entry, "[СКРИПТ]"):
				entryFg = lipgloss.Color("46")
			default:
				entryFg = logFg
			}
			for _, wrapped := range wrapPrefixed(entry, "  ", innerW) {
				rendered = append(rendered, lipgloss.NewStyle().Foreground(entryFg).Background(logBg).Render(wrapped))
			}
		}
		start := 0
		if len(rendered) > innerH {
			start = len(rendered) - innerH
		}
		lines = append(lines, rendered[start:]...)
	}

	return st.Render(strings.Join(lines, "\n"))
}
