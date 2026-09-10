package jira

import "context"

const unknownStatus = "-"

type StatusState string

const (
	StatusStateReady     StatusState = "ready"
	StatusStateLoading   StatusState = "loading"
	StatusStateTransient StatusState = "transient"
	StatusStateAuth      StatusState = "auth"
	StatusStateUnmapped  StatusState = "unmapped"
	StatusStateError     StatusState = "error"
)

type StatusReason string

const (
	StatusReasonNone                               StatusReason = "none"
	StatusReasonNoMapping                          StatusReason = "no_mapping"
	StatusReasonNoGroupConfig                      StatusReason = "no_group_config"
	StatusReasonInvalidRequest                     StatusReason = "invalid_request"
	StatusReasonTemporarilyDown                    StatusReason = "temporarily_unavailable"
	StatusReasonAuthRequired                       StatusReason = "auth_required"
	StatusReasonForbidden                          StatusReason = "forbidden"
	StatusReasonLoginRequired                      StatusReason = "login_required"
	StatusReasonIssueNotFound                      StatusReason = "issue_not_found"
	StatusReasonClientError                        StatusReason = "client_error"
	StatusReasonHTTPError                          StatusReason = "http_error"
	StatusReasonTransportError                     StatusReason = "transport_error"
	StatusReasonResponseParseErr                   StatusReason = "response_parse_error"
	StatusReasonResponseTooLarge                   StatusReason = "response_too_large"
	StatusReasonBrowserUnavailableHTTPFallback     StatusReason = "browser_unavailable_http_fallback"
	StatusReasonBrowserUnavailableHTTPAuthRequired StatusReason = "browser_unavailable_http_auth_required"
	StatusReasonBrowserUnavailableHTTPError        StatusReason = "browser_unavailable_http_error"
)

type StatusResult struct {
	Status string
	State  StatusState
	Reason StatusReason
}

func (r StatusResult) StatusOrDash() string {
	status := r.Status
	if status == "" {
		return unknownStatus
	}

	return status
}

type StatusResolver interface {
	ResolveStatus(group, ticketURL, jiraBaseURL, key string) StatusResult
}

type ReleaseVersion struct {
	ID          string
	Name        string
	ReleaseDate string
}

type ReleaseService interface {
	ListReleasedFixVersions(ctx context.Context, group string) ([]ReleaseVersion, error)
	ListDoneIssueKeysByRelease(ctx context.Context, group, releaseID string) ([]string, error)
}

// Noop — заглушка Jira-адаптера для MVP без внешней интеграции.
type Noop struct{}

// NewNoop возвращает no-op реализацию Jira-адаптера.
func NewNoop() Noop { return Noop{} }

func (Noop) ResolveStatus(_, _, _, _ string) StatusResult {
	return StatusResult{Status: unknownStatus, State: StatusStateUnmapped, Reason: StatusReasonNoMapping}
}

func (Noop) ListReleasedFixVersions(_ context.Context, _ string) ([]ReleaseVersion, error) {
	return nil, nil
}

func (Noop) ListDoneIssueKeysByRelease(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
