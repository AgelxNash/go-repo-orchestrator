package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

func (m Model) renderJiraStatusCell(branch model.BranchInfo, width int, highlighted bool) string {
	if width < 3 {
		width = 3
	}

	text := fitCell(m.jiraStatusDisplay(branch), width)
	if highlighted {
		return text
	}

	return m.jiraStatusStyle(branch).Render(text)
}

func (m Model) jiraStatusDisplay(branch model.BranchInfo) string {
	state := m.jiraState(branch)
	switch state {
	case model.JiraStatusStateTransient:
		return "недост."
	case model.JiraStatusStateLoading:
		return "недост."
	case model.JiraStatusStateAuth:
		return "auth"
	case model.JiraStatusStateError:
		return "ошибка"
	case model.JiraStatusStateUnmapped:
		return "-"
	default:
		return valueOrDash(branch.JiraStatus)
	}
}

func (m Model) jiraStatusStyle(branch model.BranchInfo) lipgloss.Style {
	state := m.jiraState(branch)
	switch state {
	case model.JiraStatusStateLoading:
		return jiraMutedStyle
	case model.JiraStatusStateTransient:
		return jiraWarningStyle
	case model.JiraStatusStateAuth:
		return jiraAuthStyle
	case model.JiraStatusStateError:
		return errorStyle
	case model.JiraStatusStateUnmapped:
		return jiraMutedStyle
	}

	switch jiraStatusSortBucket(branch.JiraStatus) {
	case 0:
		return jiraDoneStyle
	case 1:
		return jiraTestingStyle
	case 2:
		return jiraActiveStyle
	case 3:
		return jiraOpenStyle
	default:
		return jiraMutedStyle
	}
}

func (m Model) jiraStatusWithIndicator(branch model.BranchInfo) string {
	return fmt.Sprintf("%s [%s]", m.jiraStatusDisplay(branch), m.jiraStateIndicator(branch))
}

func (m Model) jiraStateLabel(branch model.BranchInfo) string {
	switch m.jiraState(branch) {
	case model.JiraStatusStateTransient:
		return "временно недоступно"
	case model.JiraStatusStateLoading:
		return "временно недоступно"
	case model.JiraStatusStateAuth:
		switch branch.JiraReason {
		case model.JiraStatusReasonForbidden:
			return "нет доступа"
		case model.JiraStatusReasonLoginRequired:
			return "требуется вход"
		default:
			return "нужна авторизация"
		}
	case model.JiraStatusStateError:
		switch branch.JiraReason {
		case model.JiraStatusReasonIssueNotFound:
			return "тикет не найден"
		case model.JiraStatusReasonClientError:
			return "ошибка запроса"
		default:
			return "ошибка Jira"
		}
	case model.JiraStatusStateUnmapped:
		return "нет mapping"
	default:
		return "ok"
	}
}

func (m Model) jiraStateIndicator(branch model.BranchInfo) string {
	switch m.jiraState(branch) {
	case model.JiraStatusStateTransient:
		return "!"
	case model.JiraStatusStateLoading:
		return "!"
	case model.JiraStatusStateAuth:
		return "A"
	case model.JiraStatusStateError:
		return "E"
	case model.JiraStatusStateUnmapped:
		return "-"
	default:
		return "OK"
	}
}

func (m Model) jiraStateText(branch model.BranchInfo) string {
	return fmt.Sprintf("Jira-состояние: %s (%s)", m.jiraStateLabel(branch), m.jiraReasonLabel(branch))
}

func (m Model) jiraReasonLabel(branch model.BranchInfo) string {
	switch branch.JiraReason {
	case model.JiraStatusReasonRegexKeyOnly:
		return "ключ извлечен только из regex"
	case model.JiraStatusReasonNoRegexMatch:
		return "regex не совпал"
	case model.JiraStatusReasonNoMapping:
		return "нет link mapping"
	case model.JiraStatusReasonNoGroupConfig:
		return "group не настроена"
	case model.JiraStatusReasonInvalidRequest:
		return "некорректный запрос"
	case model.JiraStatusReasonTemporarilyDown:
		return "временный сбой"
	case model.JiraStatusReasonAuthRequired:
		return "требуется авторизация (401)"
	case model.JiraStatusReasonForbidden:
		return "доступ запрещен (403)"
	case model.JiraStatusReasonLoginRequired:
		return "нужен вход (redirect/login/html)"
	case model.JiraStatusReasonIssueNotFound:
		return "тикет не найден (404)"
	case model.JiraStatusReasonClientError:
		return "клиентская ошибка HTTP (4xx)"
	case model.JiraStatusReasonHTTPError:
		return "ошибка HTTP ответа"
	case model.JiraStatusReasonTransportError:
		return "ошибка транспорта"
	case model.JiraStatusReasonResponseParseErr:
		return "некорректный JSON-ответ Jira"
	case model.JiraStatusReasonResponseTooLarge:
		return "ответ Jira превышает лимит размера"
	case model.JiraStatusReasonBrowserUnavailableHTTPFallback:
		return "browser недоступен, использован HTTP fallback"
	case model.JiraStatusReasonBrowserUnavailableHTTPAuthRequired:
		return "browser недоступен, HTTP требует авторизацию"
	case model.JiraStatusReasonBrowserUnavailableHTTPError:
		return "browser недоступен, ошибка HTTP/сети"
	case model.JiraStatusReasonNone:
		return "нет"
	default:
		return "нет"
	}
}

func (m Model) jiraState(branch model.BranchInfo) model.JiraStatusState {
	if branch.JiraState != "" {
		return branch.JiraState
	}

	if valueOrDash(branch.JiraKey) == "-" || valueOrDash(branch.JiraTicketURL) == "-" {
		return model.JiraStatusStateUnmapped
	}

	return model.JiraStatusStateReady
}

func (m Model) jiraInfoStyle(branch model.BranchInfo) lipgloss.Style {
	return m.jiraStatusStyle(branch)
}

func (m Model) jiraContextStyle(branch model.BranchInfo) lipgloss.Style {
	state := m.jiraState(branch)
	base := lipgloss.NewStyle().Background(mcCyan).Foreground(mcBlack).Padding(0, 1)

	switch state {
	case model.JiraStatusStateAuth:
		return base.Foreground(lipgloss.Color("196")).Bold(true)
	case model.JiraStatusStateTransient:
		return base.Foreground(mcYellow).Bold(true)
	case model.JiraStatusStateError:
		return base.Foreground(lipgloss.Color("196"))
	case model.JiraStatusStateUnmapped:
		return base.Foreground(mcGray)
	case model.JiraStatusStateLoading:
		return base.Foreground(mcGray)
	default:
		switch jiraStatusSortBucket(branch.JiraStatus) {
		case 0:
			return base.Foreground(lipgloss.Color("46"))
		case 1:
			return base.Foreground(mcYellow)
		case 2:
			return base.Foreground(mcBrightCyan)
		case 3:
			return base.Foreground(mcLightGray)
		default:
			return base.Foreground(mcGray)
		}
	}
}
