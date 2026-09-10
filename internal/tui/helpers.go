package tui

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/git"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

// ensureRepoSelection гарантирует наличие карты выбранных веток для указанного репозитория.
func (m *Model) ensureRepoSelection(repoName string) map[string]bool {
	if _, ok := m.selected[repoName]; !ok {
		m.selected[repoName] = make(map[string]bool)
	}
	return m.selected[repoName]
}

// ensureRepoState инициализирует состояние (курсор, смещение, выбор) для указанного репозитория, если оно отсутствует.
func (m *Model) ensureRepoState(repoName string) {
	_ = m.ensureRepoSelection(repoName)
	if _, ok := m.branchCursor[repoName]; !ok {
		m.branchCursor[repoName] = 0
	}
	if _, ok := m.branchOffset[repoName]; !ok {
		m.branchOffset[repoName] = 0
	}
}

// isSelected проверяет, выбрана ли указанная ветка в данном репозитории.
func (m Model) isSelected(repoName, key string) bool {
	repoSelected, ok := m.selected[repoName]
	if !ok {
		return false
	}

	return repoSelected[key]
}

// selectedBranches возвращает список всех веток репозитория, выбранных пользователем (исключая защищенные).
func (m Model) selectedBranches(repoName string) []model.BranchInfo {
	repoSelected, ok := m.selected[repoName]
	if !ok {
		return nil
	}

	result := make([]model.BranchInfo, 0, len(repoSelected))
	for _, branch := range m.activeRepo.Branches {
		if !repoSelected[m.branchSelectionKey(branch)] {
			continue
		}
		if branch.Protected {
			continue
		}
		result = append(result, branch)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].LastCommitAt.After(result[j].LastCommitAt)
	})

	return result
}

// visibleBranches возвращает список веток активного репозитория, отфильтрованных по области видимости (Local/Remote/All) и поисковому запросу.
func (m Model) visibleBranches() []model.BranchInfo {
	query := ""
	if m.searchMode || m.searchInput.Value() != "" {
		if m.focus == focusBranches {
			query = strings.ToLower(m.searchInput.Value())
		}
	}

	visible := make([]model.BranchInfo, 0, len(m.activeRepo.Branches))
	for _, branch := range m.activeRepo.Branches {
		if !m.isBranchInScope(branch) {
			continue
		}
		if m.hideProtected && branch.Protected {
			continue
		}
		displayName := strings.ToLower(m.branchDisplayName(branch))
		if query != "" && !strings.Contains(displayName, query) {
			continue
		}
		visible = append(visible, branch)
	}

	slices.SortStableFunc(visible, func(a, b model.BranchInfo) int {
		if m.lessBranch(a, b) {
			return -1
		}
		if m.lessBranch(b, a) {
			return 1
		}

		return 0
	})

	return visible
}

// visibleRepoIndices возвращает индексы репозиториев из конфигурации, подходящих под текущий поисковый фильтр.
func (m Model) visibleRepoIndices() []int {
	query := ""
	if m.searchMode || m.searchInput.Value() != "" {
		if m.focus == focusRepos {
			query = strings.ToLower(m.searchInput.Value())
		}
	}

	var indices []int
	for i, repo := range m.cfg.Repos {
		if query != "" && !strings.Contains(strings.ToLower(repo.Name), query) {
			continue
		}
		indices = append(indices, i)
	}

	slices.SortStableFunc(indices, func(i, j int) int {
		left := m.cfg.Repos[i]
		right := m.cfg.Repos[j]
		if m.lessRepo(left, right) {
			return -1
		}
		if m.lessRepo(right, left) {
			return 1
		}

		return 0
	})

	return indices
}

// currentCursor возвращает позицию курсора в списке веток для указанного репозитория.
func (m Model) currentCursor(repoName string) int {
	cursor, ok := m.branchCursor[repoName]
	if !ok {
		return 0
	}
	return cursor
}

// clampBranchCursor удерживает курсор веток в пределах допустимых границ для указанного репозитория.
func (m *Model) clampBranchCursor(repoName string) {
	branches := m.visibleBranches()
	if len(branches) == 0 {
		m.branchCursor[repoName] = 0
		m.branchOffset[repoName] = 0
		return
	}

	cur := m.currentCursor(repoName)
	if cur >= len(branches) {
		cur = len(branches) - 1
	}
	if cur < 0 {
		cur = 0
	}
	m.branchCursor[repoName] = cur
}

// ensureRepoCursorVisible корректирует смещение вьюпорта репозиториев, чтобы выбранный репозиторий всегда был виден.
func (m *Model) ensureRepoCursorVisible() {
	indices := m.visibleRepoIndices()
	if len(indices) == 0 {
		m.repoOffset = 0
		return
	}

	pos := m.repoVisiblePosition(indices)
	if pos < 0 {
		m.repoIdx = indices[0]
		pos = 0
	}

	rows := m.repoViewportRows()
	m.repoOffset = adjustedViewportOffset(m.repoOffset, pos, len(indices), rows)
}

func (m Model) repoVisiblePosition(indices []int) int {
	for i, idx := range indices {
		if idx == m.repoIdx {
			return i
		}
	}
	return -1
}

// ensureBranchCursorVisible корректирует смещение вьюпорта веток, чтобы выбранная ветка всегда была видна.
func (m *Model) ensureBranchCursorVisible(repoName string) {
	if repoName == "" {
		return
	}

	m.ensureRepoState(repoName)
	branches := m.visibleBranches()
	if len(branches) == 0 {
		m.branchCursor[repoName] = 0
		m.branchOffset[repoName] = 0
		return
	}

	cursor := m.currentCursor(repoName)
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(branches) {
		cursor = len(branches) - 1
	}
	m.branchCursor[repoName] = cursor

	rows := m.branchViewportRows()
	m.branchOffset[repoName] = adjustedViewportOffset(m.branchOffset[repoName], cursor, len(branches), rows)
}

func (m Model) repoViewportRows() int {
	contentHeight := max(8, m.height-4)
	repoHeight := contentHeight
	if m.showInfo {
		infoHeight := min(14, max(8, contentHeight/3))
		repoHeight = max(7, contentHeight-infoHeight-1)
	}
	return m.repoRowsForPanelHeight(repoHeight)
}

func (m Model) branchViewportRows() int {
	contentHeight := max(8, m.height-4)
	branchesHeight := contentHeight
	if m.showInfo {
		infoHeight := min(14, max(8, contentHeight/3))
		branchesHeight = max(7, contentHeight-infoHeight-1)
	}
	return m.branchRowsForPanelHeight(branchesHeight)
}

func (m Model) repoRowsForPanelHeight(panelHeight int) int {
	return max(0, panelHeight-4)
}

func (m Model) branchRowsForPanelHeight(panelHeight int) int {
	return max(0, panelHeight-4)
}

func adjustedViewportOffset(offset, cursor, total, rows int) int {
	if total <= 0 || rows <= 0 {
		return 0
	}

	if total <= rows {
		return 0
	}

	maxOffset := total - rows
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}

	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}

	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}

	if offset < 0 {
		return 0
	}
	if offset > maxOffset {
		return maxOffset
	}

	return offset
}

// currentBranch возвращает информацию о ветке под курсором в активном репозитории.
func (m Model) currentBranch() *model.BranchInfo {
	branches := m.visibleBranches()
	if len(branches) == 0 {
		return nil
	}
	cur := m.currentCursor(m.activeRepo.RepoName)
	if cur < 0 || cur >= len(branches) {
		return nil
	}
	branch := branches[cur]
	return &branch
}

// selectedRepoName возвращает имя репозитория под курсором в панели репозиториев.
func (m Model) selectedRepoName() string {
	if len(m.cfg.Repos) == 0 || m.repoIdx < 0 || m.repoIdx >= len(m.cfg.Repos) {
		return ""
	}
	return m.cfg.Repos[m.repoIdx].Name
}

func (m *Model) selectRepoByName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	for i, repo := range m.cfg.Repos {
		if repo.Name == name {
			m.repoIdx = i
			m.ensureRepoCursorVisible()
			m.activateSelectedRepoFromCache()
			return
		}
	}
}

func (m Model) selectedRepoStat() (model.RepoStat, bool) {
	repoName := m.selectedRepoName()
	if repoName == "" {
		return model.RepoStat{}, false
	}

	if stat, ok := m.repoStats[repoName]; ok && stat.Loaded {
		return stat, true
	}

	if m.activeRepo.RepoName == repoName {
		if !m.hasLoadedBranches(repoName) {
			return model.RepoStat{}, false
		}
		return model.RepoStat{
			CurrentBranch: m.activeRepo.CurrentBranch,
			DirtyStats:    m.activeRepo.DirtyStats,
			Loaded:        true,
		}, true
	}

	return model.RepoStat{}, false
}

func (m Model) repoListState(repoName string) (string, string) {
	branch := "-"
	status := "Не загружен"

	if stat, ok := m.repoStats[repoName]; ok && stat.Loaded {
		if stat.CurrentBranch != "" {
			branch = stat.CurrentBranch
		}
		if stat.HasError() {
			return branch, "Ошибка"
		}
		if stat.HasEmptyCloneWarning() {
			return branch, "Нет HEAD"
		}
		if stat.HasSyncWarning() {
			return branch, "Предупреждение"
		}
		if stat.DirtyStats.HasChanges() {
			return branch, "Изменения"
		}
		return branch, "Чисто"
	}

	if m.repoLoading[repoName] {
		return branch, "Загрузка"
	}

	return branch, status
}

func (m Model) renderRepoStatus(status string) string {
	switch status {
	case "Ошибка":
		return errorStyle.Render("[E]")
	case "Нет HEAD":
		return warnStyle.Render("[H]")
	case "Предупреждение":
		return warnStyle.Render("[W]")
	case "Изменения":
		return dirtyStyle.Render("[D]")
	case "Чисто":
		return cleanStyle.Render("[C]")
	case "Загрузка":
		return warnStyle.Render("[~]")
	default:
		return mutedStyle.Render("[ ]")
	}
}

func (m Model) repoStatusCode(status string) string {
	switch status {
	case "Ошибка":
		return "E"
	case "Нет HEAD":
		return "H"
	case "Предупреждение":
		return "W"
	case "Изменения":
		return "D"
	case "Чисто":
		return "C"
	case "Загрузка":
		return "~"
	default:
		return "-"
	}
}

// branchSelectionKey возвращает уникальный строковый ключ для идентификации ветки в карте выбранных элементов.
func (m Model) branchSelectionKey(branch model.BranchInfo) string {
	if strings.TrimSpace(branch.Key) != "" {
		return branch.Key
	}
	if strings.TrimSpace(branch.FullRef) != "" {
		return branch.FullRef
	}
	if strings.TrimSpace(branch.QualifiedName) != "" {
		return branch.QualifiedName
	}
	return branch.Name
}

// branchDisplayName возвращает форматированное имя ветки для отображения в интерфейсе (с учетом удаленных репозиториев).
func (m Model) branchDisplayName(branch model.BranchInfo) string {
	if branch.IsRemote() && branch.RemoteName != "" {
		return branch.RemoteName + "/" + branch.Name
	}
	if strings.TrimSpace(branch.QualifiedName) != "" {
		return branch.QualifiedName
	}
	return branch.Name
}

// branchListName возвращает имя ветки для вывода в списке, добавляя маркеры защиты (*).
func (m Model) branchListName(branch model.BranchInfo) string {
	name := m.branchDisplayName(branch)
	if branch.Protected {
		return name + "*"
	}
	return name
}

func (m Model) branchTypeLabel(branch model.BranchInfo) string {
	if branch.IsRemote() {
		return "remote"
	}
	return "local"
}

func (m Model) isBranchSelectable(branch model.BranchInfo) bool {
	return !branch.Protected
}

// invertVisibleBranchSelection инвертирует выбор для всех видимых (отфильтрованных) веток, которые не являются защищенными.
func (m *Model) invertVisibleBranchSelection(repoName string) int {
	selected := m.ensureRepoSelection(repoName)
	toggled := 0
	for _, branch := range m.visibleBranches() {
		if !m.isBranchSelectable(branch) {
			continue
		}
		key := m.branchSelectionKey(branch)
		selected[key] = !selected[key]
		toggled++
	}
	return toggled
}

// applyAutocheckSelection помечает ветки для автоматического выбора на основе правил из конфигурации.
// Данная функция вызывается при загрузке списка веток репозитория.
// Она уважает ручной выбор пользователя: если ветка уже присутствует в списке выбранных (или явно снятых),
// значение не перезаписывается.
func (m *Model) applyAutocheckSelection(repoName string, branches []model.BranchInfo) {
	selected := m.ensureRepoSelection(repoName)
	for _, branch := range branches {
		if branch.Autocheck && !branch.Protected {
			key := m.branchSelectionKey(branch)
			// Применяем автоматический выбор только если для этой ветки еще нет сохраненного состояния
			// (пользователь еще не нажимал пробел для этой ветки в текущей сессии).
			if _, exists := selected[key]; !exists {
				selected[key] = true
			}
		}
	}
}

func onOff(v bool) string {
	if v {
		return "вкл"
	}
	return "выкл"
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func repoSourceLabel(repo config.RepoConfig, ok bool) string {
	if !ok {
		return "-"
	}

	switch repo.SourceType() {
	case "path":
		return "локальный путь"
	case "url":
		return "удаленный URL"
	case "opensource":
		return "профиль \"опенсорс\""
	default:
		return repo.SourceType()
	}
}

func repoSourceCode(repo config.RepoConfig) string {
	switch repo.SourceType() {
	case "path":
		return "путь"
	case "url":
		return "URL"
	case "opensource":
		return "опенсорс"
	default:
		return repo.SourceType()
	}
}

func dirtySummary(st model.DirtyStats) string {
	return fmt.Sprintf(
		"изменено:%d добавлено:%d удалено:%d неотслеж:%d",
		len(st.Modified),
		len(st.Added),
		len(st.Deleted),
		len(st.Untracked),
	)
}

func userFacingError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, git.ErrEmptyClone) {
		return err
	}

	msg := strings.TrimSpace(err.Error())
	if errors.Is(err, git.ErrTransientNetwork) {
		details := strings.TrimPrefix(msg, git.ErrTransientNetwork.Error()+": ")
		return errors.New("хостинг сбросил SSH-соединение или соединение нестабильно (лимит одновременных сессий):\n" + strings.TrimSpace(details))
	}

	replacements := []struct {
		from string
		to   string
	}{
		{"prepare repository:", "подготовка репозитория:"},
		{"get repo stat:", "чтение состояния репозитория:"},
		{"read config:", "чтение конфигурации:"},
		{"resolve config path:", "проверка пути к конфигурации:"},
		{"create config directory:", "создание директории конфигурации:"},
		{"scan directory:", "сканирование директории:"},
		{"marshal config:", "формирование конфигурации:"},
		{"write config:", "запись конфигурации:"},
		{"repo not found", "репозиторий не найден"},
	}

	for _, r := range replacements {
		msg = strings.ReplaceAll(msg, r.from, r.to)
	}

	lowerMsg := strings.ToLower(msg)
	switch {
	case strings.Contains(lowerMsg, "kex_exchange_identification") || strings.Contains(lowerMsg, "connection reset by peer"):
		return errors.New("хостинг сбросил SSH-соединение или соединение нестабильно (лимит одновременных сессий):\n" + msg)
	case strings.Contains(lowerMsg, "context deadline exceeded") || strings.Contains(lowerMsg, "operation timed out") || strings.Contains(lowerMsg, "i/o timeout"):
		msg = "таймаут обращения к удаленному репозиторию: сервер недоступен или отвечает слишком долго\n" + msg
	case strings.Contains(lowerMsg, "connection refused"):
		msg = "удаленный репозиторий недоступен: соединение отклонено\n" + msg
	case strings.Contains(lowerMsg, "no such host") || strings.Contains(lowerMsg, "could not resolve host"):
		msg = "удаленный репозиторий недоступен: не удалось разрешить хост\n" + msg
	case strings.Contains(lowerMsg, "no route to host"):
		msg = "удаленный репозиторий недоступен: нет маршрута до хоста\n" + msg
	case strings.Contains(lowerMsg, "permission denied"):
		msg = "доступ к удаленному репозиторию запрещен (проверьте SSH-ключи/права)\n" + msg
	}

	if strings.HasPrefix(msg, "git ") {
		msg = "ошибка git: " + msg
	}

	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "неизвестная ошибка"
	}

	return errors.New(msg)
}

func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.TrimSpace(s) == "" {
		return []string{s}
	}

	var lines []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapParagraph(para, width)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func wrapParagraph(s string, width int) []string {
	runes := []rune(s)
	if len(runes) <= width {
		return []string{s}
	}

	var lines []string
	for len(runes) > width {
		cut := width
		if space := lastSpaceIndex(runes[:width]); space >= width/3 {
			cut = space
		}
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return lines
}

func lastSpaceIndex(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == ' ' {
			return i
		}
	}
	return -1
}

func wrapPrefixed(s, prefix string, width int) []string {
	inner := width - len([]rune(prefix))
	if inner < 8 {
		inner = 8
	}
	raw := wrapText(s, inner)
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		out = append(out, prefix+line)
	}
	return out
}

func truncate(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

// fitCell усекает строку до заданной ширины и дополняет пробелами для выравнивания в ячейке.
func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	trimmed := truncate(s, width)
	return fmt.Sprintf("%-*s", width, trimmed)
}
