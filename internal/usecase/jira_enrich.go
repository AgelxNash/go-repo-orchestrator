package usecase

import (
	"strings"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/jira"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"

	"go.uber.org/zap"
)

func collectJiraStatusRequests(repo config.RepoConfig, branches []model.BranchInfo) []jira.StatusBatchRequest {
	requests := make([]jira.StatusBatchRequest, 0, len(branches))
	for _, branch := range branches {
		jiraMatch, ok, _ := repo.ExtractJiraMatchDetailed(branch.Name)
		if !ok {
			continue
		}
		if strings.TrimSpace(jiraMatch.Group) == "" || strings.TrimSpace(jiraMatch.TicketURL) == "" {
			continue
		}
		requests = append(requests, jira.StatusBatchRequest{
			Group:       jiraMatch.Group,
			TicketURL:   jiraMatch.TicketURL,
			JiraBaseURL: jiraMatch.URL,
			Key:         jiraMatch.Key,
		})
	}

	return requests
}

func mapJiraStatusState(state jira.StatusState) model.JiraStatusState {
	switch state {
	case jira.StatusStateReady:
		return model.JiraStatusStateReady
	case jira.StatusStateLoading:
		return model.JiraStatusStateLoading
	case jira.StatusStateTransient:
		return model.JiraStatusStateTransient
	case jira.StatusStateAuth:
		return model.JiraStatusStateAuth
	case jira.StatusStateUnmapped:
		return model.JiraStatusStateUnmapped
	default:
		return model.JiraStatusStateError
	}
}

func mapJiraStatusReason(reason jira.StatusReason) model.JiraStatusReason {
	switch reason {
	case jira.StatusReasonNone:
		return model.JiraStatusReasonNone
	case jira.StatusReasonNoMapping:
		return model.JiraStatusReasonNoMapping
	case jira.StatusReasonNoGroupConfig:
		return model.JiraStatusReasonNoGroupConfig
	case jira.StatusReasonInvalidRequest:
		return model.JiraStatusReasonInvalidRequest
	case jira.StatusReasonTemporarilyDown:
		return model.JiraStatusReasonTemporarilyDown
	case jira.StatusReasonAuthRequired:
		return model.JiraStatusReasonAuthRequired
	case jira.StatusReasonForbidden:
		return model.JiraStatusReasonForbidden
	case jira.StatusReasonLoginRequired:
		return model.JiraStatusReasonLoginRequired
	case jira.StatusReasonIssueNotFound:
		return model.JiraStatusReasonIssueNotFound
	case jira.StatusReasonClientError:
		return model.JiraStatusReasonClientError
	case jira.StatusReasonHTTPError:
		return model.JiraStatusReasonHTTPError
	case jira.StatusReasonTransportError:
		return model.JiraStatusReasonTransportError
	case jira.StatusReasonResponseParseErr:
		return model.JiraStatusReasonResponseParseErr
	case jira.StatusReasonBrowserUnavailableHTTPFallback:
		return model.JiraStatusReasonBrowserUnavailableHTTPFallback
	case jira.StatusReasonBrowserUnavailableHTTPAuthRequired:
		return model.JiraStatusReasonBrowserUnavailableHTTPAuthRequired
	case jira.StatusReasonBrowserUnavailableHTTPError:
		return model.JiraStatusReasonBrowserUnavailableHTTPError
	default:
		return model.JiraStatusReasonHTTPError
	}
}

func valueOrDash(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}

	return raw
}

type jiraMappingStats struct {
	total  int
	counts map[config.JiraMatchReason]int
}

func newJiraMappingStats() jiraMappingStats {
	return jiraMappingStats{counts: make(map[config.JiraMatchReason]int)}
}

func (s *jiraMappingStats) add(diag config.JiraMatchDiagnostics) {
	if s == nil {
		return
	}
	s.total++
	s.counts[diag.Reason]++
}

func (c *Cleaner) logJiraMappingSummary(repoName string, stats jiraMappingStats) {
	if c == nil || c.log == nil || !c.log.Core().Enabled(zap.DebugLevel) {
		return
	}

	fields := []zap.Field{
		zap.String("repo", strings.TrimSpace(repoName)),
		zap.Int("total_branches", stats.total),
		zap.Int("mapped_named_group", stats.counts[config.JiraMatchReasonMappedNamedGroup]),
		zap.Int("named_group_no_group_config", stats.counts[config.JiraMatchReasonNamedGroupNoGroup]),
		zap.Int("fallback_jira", stats.counts[config.JiraMatchReasonFallbackJIRA]),
		zap.Int("fallback_full_match", stats.counts[config.JiraMatchReasonFallbackFullMatch]),
		zap.Int("no_regex_match", stats.counts[config.JiraMatchReasonNoRegexMatch]),
	}

	c.log.Debug("jira mapping summary", fields...)
}
