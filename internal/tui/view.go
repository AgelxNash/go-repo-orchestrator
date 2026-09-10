package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// View формирует экран с табами Репозитории/Ветки.
func (m Model) View() string {
	if len(m.cfg.Repos) == 0 {
		return appStyle.Render("Нет репозиториев в конфиге")
	}

	if m.startupLoading {
		return appStyle.Render(m.viewStartupScreen())
	}

	usableWidth := max(64, m.width-2)
	contentHeight := max(8, m.height-4)

	body := m.viewReposTab(usableWidth, contentHeight)
	if m.focus == focusBranches {
		body = m.viewBranchesTab(usableWidth, contentHeight)
	}

	topMenu := m.viewTopMenu(m.width)
	header := titleStyle.Width(m.width).Render(m.viewTitleLine())
	statusLine := m.viewContextLine(m.width)
	hotkeys := m.viewHotkeyBar(m.width)

	view := lipgloss.JoinVertical(lipgloss.Left, topMenu, header, body, statusLine, hotkeys)
	if m.confirmType != confirmNone {
		view = m.viewConfirmModal(view)
	}

	return appStyle.Render(view)
}

func (m Model) viewReposTab(width, height int) string {
	if !m.showInfo {
		return m.viewReposPanel(width, height)
	}

	infoHeight := min(14, max(8, height/3))
	if stat, ok := m.selectedRepoStat(); ok && (stat.HasError() || stat.HasSyncWarning() || stat.HasEmptyCloneWarning()) {
		infoHeight = min(18, max(10, height/2))
	}
	repoHeight := max(7, height-infoHeight-1)
	ruler := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewReposPanel(width, repoHeight),
		ruler,
		m.viewStatsPanel(width, infoHeight),
	)
}

func (m Model) viewBranchesTab(width, height int) string {
	if !m.showInfo {
		return m.viewBranchesPanel(width, height)
	}

	infoHeight := min(14, max(8, height/3))
	if stat, ok := m.selectedRepoStat(); ok && (stat.HasError() || stat.HasSyncWarning() || stat.HasEmptyCloneWarning()) {
		infoHeight = min(18, max(10, height/2))
	}
	branchesHeight := max(7, height-infoHeight-1)
	ruler := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewBranchesPanel(width, branchesHeight),
		ruler,
		m.viewStatsPanel(width, infoHeight),
	)
}

func (m Model) viewReposPanel(width, height int) string {
	style := panelStyle.Width(width).Height(height)
	if m.focus == focusRepos {
		style = panelFocusedStyle.Width(width).Height(height)
	}

	innerWidth := width - 4
	compact := innerWidth < 40
	const branchWidth = 16
	const typeWidth = 10
	nameWidth := max(8, innerWidth-branchWidth-typeWidth-11)

	var lines []string
	lines = append(lines, panelTitleStyle.Width(innerWidth).Render(" РЕПОЗИТОРИИ "))
	if compact {
		compactNameWidth := max(8, innerWidth-14)
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(fmt.Sprintf(" %-*s %-8s", compactNameWidth, "Репозиторий", "Ветка/ст")))
	} else {
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(fmt.Sprintf(" %-*s %-*s %-*s %s", nameWidth, "Репозиторий", branchWidth, "Активная ветка", typeWidth, "Источник", "Ст")))
	}

	indices := m.visibleRepoIndices()
	rows := m.repoRowsForPanelHeight(height)
	pos := m.repoVisiblePosition(indices)
	offset := adjustedViewportOffset(m.repoOffset, pos, len(indices), rows)
	start := offset
	end := min(len(indices), start+rows)

	for _, idx := range indices[start:end] {
		repo := m.cfg.Repos[idx]
		cursor := " "
		if idx == m.repoIdx {
			cursor = cursorStyle.Render(">")
		}

		source := repoSourceCode(repo)
		branch, status := m.repoListState(repo.Name)
		statusStr := m.renderRepoStatus(status)

		displayBranch := branch
		if displayBranch != "" && displayBranch != "-" {
			displayBranch = "@" + displayBranch
		}

		var rowText string
		if compact {
			compactNameWidth := max(8, width-14)
			padName := fitCell(repo.Name, compactNameWidth)
			branchCell := fitCell(displayBranch+" "+m.repoStatusCode(status), 8)
			rowText = fmt.Sprintf("%s%s %-8s", cursor, padName, branchCell)
		} else {
			padName := fitCell(repo.Name, nameWidth)
			padBranch := fitCell(displayBranch, branchWidth)
			srcShort := fitCell(source, typeWidth)
			rowText = fmt.Sprintf("%s%s %s %-*s %s", cursor, padName, padBranch, typeWidth, srcShort, statusStr)
		}

		if idx == m.repoIdx {
			rowText = selectedStyle.Width(innerWidth).Render(rowText)
		}
		lines = append(lines, rowText)
	}

	if len(indices) == 0 {
		lines = append(lines, mutedStyle.Render("Ничего не найдено"))
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) viewBranchesPanel(width, height int) string {
	style := panelStyle.Width(width).Height(height)
	if m.focus == focusBranches {
		style = panelFocusedStyle.Width(width).Height(height)
	}

	innerWidth := width - 4
	compact := innerWidth < 58
	const dateWidth = 10
	const jiraWidth = 17
	const jiraStatusWidth = 20
	const mergeWidth = 9
	const typeWidth = 2
	const keepWidth = 2
	const fixedCols = 2 + 2 + 1 + dateWidth + 1 + jiraWidth + 1 + jiraStatusWidth + 1 + mergeWidth + 1 + typeWidth + 1 + keepWidth
	branchNameWidth := max(12, innerWidth-fixedCols)
	if compact {
		branchNameWidth = max(10, innerWidth-14)
	}

	var lines []string
	lines = append(lines, panelTitleStyle.Width(innerWidth).Render(" ВЕТКИ "))

	if m.loadingSelectedRepo() {
		lines = append(lines, fmt.Sprintf("%s Загрузка веток...", m.spinner.View()))
		lines = append(lines, mutedStyle.Render("Асинхронная загрузка веток (таймаут контролируется)"))
		return style.Render(strings.Join(lines, "\n"))
	}

	if stat, ok := m.selectedRepoStat(); ok && stat.HasError() {
		lines = append(lines, errorStyle.Render("Ошибка загрузки веток"))
		lines = append(lines, wrapPrefixed(stat.LoadError, "", max(16, width-4))...)
		return style.Render(strings.Join(lines, "\n"))
	}

	if m.activeRepo.RepoName == "" {
		lines = append(lines, mutedStyle.Render("Репозиторий не загружен"))
		return style.Render(strings.Join(lines, "\n"))
	}

	if m.activeRepo.RepoName != m.selectedRepoName() {
		lines = append(lines, mutedStyle.Render("Ветки для выбранного репозитория не загружены"))
		lines = append(lines, mutedStyle.Render("Нажмите F5 или r для загрузки"))
		return style.Render(strings.Join(lines, "\n"))
	}

	hdrBranch := fitCell("Ветка", branchNameWidth)
	if compact {
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(fmt.Sprintf(" S %s %-9s", hdrBranch, "Слияние")))
	} else {
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(fmt.Sprintf(" S %s %-10s %-17s %-20s %-9s %s %s", hdrBranch, "Дата", "JIRA", "Статус", "Слияние", "T", "З")))
	}

	visible := m.visibleBranches()
	repoName := m.activeRepo.RepoName
	cursor := m.currentCursor(repoName)
	rows := m.branchRowsForPanelHeight(height)
	offset := adjustedViewportOffset(m.branchOffset[repoName], cursor, len(visible), rows)
	start := offset
	end := min(len(visible), start+rows)

	for i, br := range visible[start:end] {
		absoluteIdx := start + i
		isActiveRow := absoluteIdx == cursor
		cursorStr := " "
		if isActiveRow {
			cursorStr = ">"
		}

		marker := " "
		if m.isSelected(repoName, m.branchSelectionKey(br)) {
			marker = branchMarkerStyle.Render("✓")
		} else if br.IsRemote() {
			marker = "·"
		}

		merge := m.renderMergeCell(br.MergeStatus, mergeWidth, isActiveRow)
		padName := fitCell(m.branchListName(br), branchNameWidth)
		line := ""
		if compact {
			line = fmt.Sprintf("%s %s %s %s", cursorStr, marker, padName, merge)
		} else {
			jira := valueOrDash(br.JiraKey)
			if jira == "-" {
				jira = "--"
			}
			jiraStatusCell := m.renderJiraStatusCell(br, jiraStatusWidth, isActiveRow)
			date := br.LastCommitAt.Format("2006-01-02")
			padJira := fitCell(jira, jiraWidth)
			keep := "-"
			if br.Protected {
				keep = "K"
			}
			branchType := "L"
			if br.IsRemote() {
				branchType = "R"
			}
			line = fmt.Sprintf("%s %s %s %-10s %s %s %s %-2s %-2s", cursorStr, marker, padName, date, padJira, jiraStatusCell, merge, branchType, keep)
		}

		if isActiveRow {
			line = selectedStyle.Width(innerWidth).Render(line)
		}

		lines = append(lines, line)
	}

	if len(visible) == 0 {
		lines = append(lines, mutedStyle.Render("Нет веток для отображения"))
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) viewStatsPanel(width, height int) string {
	style := infoStyle.Width(width).Height(height)
	innerWidth := width - 4
	contentWidth := max(12, innerWidth-2)
	selectedRepo := m.selectedRepoName()
	selectedRepoCfg, hasSelectedRepoCfg := m.cfg.RepoByName(selectedRepo)
	selectedCount := len(m.selectedBranches(selectedRepo))
	visibleCount := len(m.visibleBranches())

	labels := []struct{ key, val string }{
		{"Репозиториев", fmt.Sprintf("%d", len(m.cfg.Repos))},
		{"Веток", fmt.Sprintf("%d", len(m.activeRepo.Branches))},
		{"Видимых", fmt.Sprintf("%d", visibleCount)},
		{"Выбрано", fmt.Sprintf("%d", selectedCount)},
		{"Scope", m.branchScopeLabel()},
		{"Сорт репо", m.repoSortCodeLabel()},
		{"Сорт веток", m.branchSortCodeLabel()},
	}

	summaryParts := make([]string, 0, len(labels))
	for _, l := range labels {
		summaryParts = append(summaryParts, l.key+": "+l.val)
	}
	summary := strings.Join(summaryParts, "  |  ")

	lines := []string{
		panelTitleStyle.Width(innerWidth).Render(" ИНФО  " + truncate(summary, innerWidth-5)),
		truncate(fmt.Sprintf("Репозиторий: %s", valueOrDash(selectedRepo)), contentWidth),
		truncate(fmt.Sprintf("Источник: %s | Scope: %s | Скрытое: %s | Формат: .%s", valueOrDash(repoSourceLabel(selectedRepoCfg, hasSelectedRepoCfg)), m.branchScopeLabel(), onOff(m.hideProtected), m.scriptFormat), contentWidth),
	}

	if m.focus == focusBranches && m.activeRepo.RepoName == selectedRepo && m.hasLoadedBranches(selectedRepo) {
		current := m.currentBranch()
		if current != nil {
			jiraLine := truncate(fmt.Sprintf("Курсор: %s | Тип: %s | JIRA: %s | Статус Jira: %s | Слияние: %s", m.branchDisplayName(*current), m.branchTypeLabel(*current), valueOrDash(current.JiraKey), m.jiraStatusWithIndicator(*current), m.mergeStatusLabel(current.MergeStatus)), contentWidth)
			lines = append(lines, m.jiraInfoStyle(*current).Render(jiraLine))
			lines = append(lines, truncate(fmt.Sprintf("Jira group: %s | Ссылка: %s", valueOrDash(current.JiraGroup), valueOrDash(current.JiraTicketURL)), contentWidth))
			lines = append(lines, truncate(m.jiraStateText(*current), contentWidth))
		}
	}

	if stat, ok := m.selectedRepoStat(); ok {
		lines = append(lines, "", panelHeaderStyle.Width(innerWidth).Render(" Статус Git "))
		if stat.HasError() {
			lines = append(lines, errorStyle.Render("Ошибка доступа к репозиторию"))
			lines = append(lines, wrapPrefixed(stat.LoadError, "  ", contentWidth)...)
		} else {
			lines = append(lines, truncate(fmt.Sprintf("Текущая ветка: %s", valueOrDash(stat.CurrentBranch)), contentWidth))
			if stat.HasEmptyCloneWarning() {
				lines = append(lines, warnStyle.Render(truncate("Состояние: репозиторий-оболочка без checkout", contentWidth)))
				lines = append(lines, wrapPrefixed(stat.Warning.Text(), "  ", contentWidth)...)
			}
			if stat.HasSyncWarning() {
				lines = append(lines, warnStyle.Render(truncate("Синхронизация: предупреждение", contentWidth)))
				lines = append(lines, wrapPrefixed(stat.SyncWarning, "  ", contentWidth)...)
			}
			st := stat.DirtyStats
			if !st.HasChanges() {
				lines = append(lines, cleanStyle.Render("Рабочее дерево: чисто"))
			} else {
				lines = append(lines, dirtyStyle.Render(truncate("Рабочее дерево: "+dirtySummary(st), contentWidth)))
				var dirtyFiles []string
				for _, f := range st.Modified {
					dirtyFiles = append(dirtyFiles, "  M "+f)
				}
				for _, f := range st.Added {
					dirtyFiles = append(dirtyFiles, "  A "+f)
				}
				for _, f := range st.Deleted {
					dirtyFiles = append(dirtyFiles, "  D "+f)
				}
				for _, f := range st.Untracked {
					dirtyFiles = append(dirtyFiles, "  ? "+f)
				}

				maxToShow := 10
				if len(dirtyFiles) > maxToShow {
					for i := 0; i < maxToShow; i++ {
						lines = append(lines, truncate(dirtyFiles[i], width-4))
					}
					lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ... еще %d файлов", len(dirtyFiles)-maxToShow)))
				} else {
					for _, f := range dirtyFiles {
						lines = append(lines, truncate(f, width-4))
					}
				}
			}
		}
	} else {
		lines = append(lines, "", panelHeaderStyle.Width(innerWidth).Render(" Статус Git "), mutedStyle.Render("Нет данных (ветки не загружены)"))
	}

	if m.loadingSelectedRepo() {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("%s Синхронизация репозитория (при недоступности будет ошибка таймаута)", m.spinner.View()))
	}

	if m.lastGenerated != nil {
		lines = append(lines, "")
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(" Последний скрипт "))
		lines = append(lines, truncate(filepath.Base(m.lastGenerated.ScriptPath), max(10, width-4)))
	}

	if m.err != nil {
		lines = append(lines, "")
		lines = append(lines, panelHeaderStyle.Width(innerWidth).Render(" Ошибка "))
		lines = append(lines, wrapPrefixed(m.err.Error(), "", max(16, width-4))...)
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) viewTopMenu(width int) string {
	reposTab := tabInactiveStyle.Render("[ Репозитории ]")
	branchesTab := tabInactiveStyle.Render("[ Ветки ]")
	if m.focus == focusRepos {
		reposTab = tabActiveStyle.Render("[ Репозитории ]")
	} else {
		branchesTab = tabActiveStyle.Render("[ Ветки ]")
	}

	enterHint := "Enter - открыть репозиторий | F6 Сорт: " + m.repoSortCodeLabel()
	if m.focus == focusBranches {
		enterHint = fmt.Sprintf("Enter - checkout ветки | F4 Scope: %s | F6 Сорт: %s", m.branchScopeCodeLabel(), m.branchSortCodeLabel())
	}

	line := " " + reposTab + " " + branchesTab + "  " + enterHint + " "
	if lipgloss.Width(line) > max(16, width-2) {
		if m.focus == focusRepos {
			line = " [РЕПОЗИТОРИИ] [ветки] "
		} else {
			line = " [репозитории] [ВЕТКИ] "
		}
	}
	return topMenuStyle.Width(width).Render(line)
}

func (m Model) viewTitleLine() string {
	if m.searchMode || m.searchInput.Value() != "" {
		return truncate(fmt.Sprintf(" Поиск (F3) / Фильтр: %s", m.searchInput.View()), max(24, m.width-4))
	}
	if m.loadingSelectedRepo() {
		return truncate(fmt.Sprintf(" go-repo-orchestrator | %s загрузка: %s ", m.spinner.View(), m.cfg.Repos[m.repoIdx].Name), max(24, m.width-4))
	}
	return truncate("Выбор ветки перед формированием скрипта .sh/.bat для удаления", max(24, m.width-4))
}

func (m Model) viewContextLine(width int) string {
	if m.searchMode || m.searchInput.Value() != "" {
		return statusStyle.Width(width).Render(truncate(fmt.Sprintf(" ПОИСК: %s_ | Enter-применить, Esc-отмена", m.searchInput.Value()), width))
	}

	contextText := ""
	contextStyle := statusStyle
	if m.err != nil {
		contextText = "Ошибка: " + m.err.Error()
	} else if m.startupWarn != "" {
		contextText = m.startupWarn
	} else if m.statusLine != "" {
		contextText = m.statusLine
	}

	if m.focus == focusRepos {
		repo := m.cfg.Repos[m.repoIdx]
		branch, status := m.repoListState(repo.Name)
		ctx := fmt.Sprintf("Репозиторий: %s | Источник: %s | Ветка: %s | Статус: %s", repo.Name, repoSourceLabel(repo, true), valueOrDash(branch), status)
		if contextText == "" {
			contextText = ctx
		} else {
			contextText = contextText + " | " + ctx
		}
	} else {
		branch := m.currentBranch()
		if branch == nil {
			if contextText == "" {
				contextText = "Ветка не выбрана"
			}
		} else {
			contextStyle = m.jiraContextStyle(*branch)
			ctx := fmt.Sprintf("Ветка: %s | Тип: %s | JIRA: %s | Jira-статус: %s | Jira-состояние: %s | Ссылка: %s | Слияние: %s", m.branchDisplayName(*branch), m.branchTypeLabel(*branch), valueOrDash(branch.JiraKey), m.jiraStatusWithIndicator(*branch), m.jiraStateLabel(*branch), valueOrDash(branch.JiraTicketURL), m.mergeStatusLabel(branch.MergeStatus))
			if contextText == "" {
				contextText = ctx
			} else {
				contextText = contextText + " | " + ctx
			}
		}
	}

	return contextStyle.Width(width).Render(truncate(contextText, max(24, width-4)))
}

func (m Model) viewHotkeyBar(width int) string {
	repoSelected := len(m.cfg.Repos) > 0
	canCheckout := m.canCheckoutBranch()
	canCreateLocal := m.canCreateLocalFromRemote()

	var items []string
	if m.focus == focusRepos {
		items = []string{
			m.renderHotkeyItem("Enter", "Ветки", repoSelected),
			m.renderHotkeyItem("F2", "Контекст", true),
			m.renderHotkeyItem("F3", "Поиск", true),
			m.renderHotkeyItem("F5/r", "Рескан", repoSelected),
			m.renderHotkeyItem("F6", "Сорт: "+m.repoSortCodeLabel(), true),
			m.renderHotkeyItem("F7", "Fetch+Pull", repoSelected),
			m.renderHotkeyItem("F10/q", "Выход", true),
		}
	} else {
		items = []string{
			m.renderHotkeyItem("Esc", "Репо", true),
			m.renderHotkeyItem("Ins/Space", "Выбор", true),
			m.renderHotkeyItem("Enter", "Чекаут", canCheckout),
			m.renderHotkeyItem("F3", "Поиск", true),
			m.renderHotkeyItem("F4", "Scope: "+m.branchScopeCodeLabel(), true),
			m.renderHotkeyItem("F5/r", "Обновить", true),
			m.renderHotkeyItem("F6", "Сорт: "+m.branchSortCodeLabel(), true),
			m.renderHotkeyItem("F7", "Клонировать", canCreateLocal),
			m.renderHotkeyItem("*", "Release auto", true),
			m.renderHotkeyItem("F8/g", "Скрипт", m.canGenerateScript()),
			m.renderHotkeyItem("F9", "Скрытое", true),
			m.renderHotkeyItem("F10/q", "Выход", true),
		}
	}
	line := ""
	for _, item := range items {
		candidate := line + item
		if lipgloss.Width(candidate) > width {
			break
		}
		line = candidate
	}
	if line == "" {
		line = m.renderHotkeyItem("F10/q", "Выход", true)
	}
	return hotkeyStyle.Width(width).Render(line)
}

func (m Model) renderHotkeyItem(key, text string, active bool) string {
	if !active {
		return hotkeyInactiveNumStyle.Render(key) + hotkeyInactiveTextStyle.Render(text)
	}
	return hotkeyNumStyle.Render(key) + hotkeyTextStyle.Render(text)
}

func (m Model) viewConfirmModal(base string) string {
	var text []string

	switch m.confirmType {
	case confirmGenerate:
		branches := m.selectedBranches(m.activeRepo.RepoName)
		text = []string{
			"[ ПОДТВЕРЖДЕНИЕ ] Генерация скрипта",
			"",
			fmt.Sprintf("Репозиторий: %s", m.activeRepo.RepoName),
			fmt.Sprintf("Путь: %s", m.activeRepo.RepoPath),
			fmt.Sprintf("Выбрано веток: %d", len(branches)),
			fmt.Sprintf("Формат: .%s (t/left/right)", m.scriptFormat),
			"",
			"F8/Enter/y - создать скрипт",
			"Esc/n   - отмена",
		}
	case confirmCheckout:
		text = []string{
			"[ ВНИМАНИЕ ] Принудительное переключение",
			"",
			fmt.Sprintf("Репозиторий: %s", m.activeRepo.RepoName),
			fmt.Sprintf("Целевая ветка: %s", m.checkoutTarget),
			"",
			warnStyle.Render("ВНИМАНИЕ: Все незакоммиченные изменения"),
			warnStyle.Render("будут безвозвратно удалены!"),
			"",
			"Enter/y - выполнить checkout -f",
			"Esc/n   - отмена",
		}
	case confirmReleaseSelect:
		choice := "-"
		if len(m.releaseOptions) > 0 && m.releaseOptionIdx >= 0 && m.releaseOptionIdx < len(m.releaseOptions) {
			opt := m.releaseOptions[m.releaseOptionIdx]
			choice = fmt.Sprintf("%s / %s (id=%s, date=%s)", valueOrDash(opt.Group), valueOrDash(opt.Version.Name), valueOrDash(opt.Version.ID), valueOrDash(opt.Version.ReleaseDate))
		}

		text = []string{
			"[ JIRA RELEASE ] Автопометка кандидатов",
			"",
			fmt.Sprintf("Репозиторий: %s", m.activeRepo.RepoName),
			fmt.Sprintf("Найдено releases: %d", len(m.releaseOptions)),
			fmt.Sprintf("Выбор: %s", choice),
			"",
			"↑/↓ (или j/k) - выбрать release",
			"Enter/y - применить автопометку",
			"Esc/n - отмена",
		}
	}

	modal := modalStyle.Width(min(72, m.width-6)).Render(strings.Join(text, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, base, modal))
}

func (m Model) renderMergeCell(status model.MergeStatus, width int, highlighted bool) string {
	if width < 3 {
		width = 3
	}

	if highlighted {
		return lipgloss.NewStyle().Width(width).Render(m.mergeStatusShort(status))
	}

	switch status {
	case model.MergeStatusMerged:
		return lipgloss.NewStyle().Width(width).Render(mergedStyle.Render("слита"))
	case model.MergeStatusUnmerged:
		return lipgloss.NewStyle().Width(width).Render(unmergedStyle.Render("не слита"))
	default:
		return lipgloss.NewStyle().Width(width).Render(unknownStyle.Render("неизв."))
	}
}

func (m Model) mergeStatusShort(status model.MergeStatus) string {
	switch status {
	case model.MergeStatusMerged:
		return "слита"
	case model.MergeStatusUnmerged:
		return "не слита"
	default:
		return "неизв."
	}
}

func (m Model) mergeStatusLabel(status model.MergeStatus) string {
	switch status {
	case model.MergeStatusMerged:
		return "слита"
	case model.MergeStatusUnmerged:
		return "не слита"
	default:
		return "неизвестно"
	}
}
