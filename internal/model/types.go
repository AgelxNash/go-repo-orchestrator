package model

import "time"

type MergeStatus string

const (
	MergeStatusMerged   MergeStatus = "merged"
	MergeStatusUnmerged MergeStatus = "unmerged"
	MergeStatusUnknown  MergeStatus = "unknown"
)

type ScriptFormat string

const (
	ScriptFormatSH  ScriptFormat = "sh"
	ScriptFormatBAT ScriptFormat = "bat"
)

type BranchScope string

const (
	BranchScopeLocal  BranchScope = "local"
	BranchScopeRemote BranchScope = "remote"
)

// DirtyStats хранит списки измененных файлов в репозитории.
type DirtyStats struct {
	Modified  []string
	Added     []string
	Deleted   []string
	Untracked []string
}

// HasChanges возвращает true, если в репозитории есть какие-либо изменения.
func (s DirtyStats) HasChanges() bool {
	return len(s.Modified) > 0 || len(s.Added) > 0 || len(s.Deleted) > 0 || len(s.Untracked) > 0
}

// BranchInfo описывает состояние локальной ветки в репозитории.
type BranchInfo struct {
	Key           string
	Name          string
	QualifiedName string
	FullRef       string
	Scope         BranchScope
	RemoteName    string
	LastCommitAt  time.Time
	LastCommitSHA string
	JiraKey       string
	JiraGroup     string
	JiraURL       string
	JiraTicketURL string
	JiraStatus    string
	JiraState     JiraStatusState
	JiraReason    JiraStatusReason
	BaseBranch    string
	MergeStatus   MergeStatus
	Protected     bool
	Reason        string
	Autocheck     bool
}

type JiraStatusState string

const (
	JiraStatusStateReady     JiraStatusState = "ready"
	JiraStatusStateLoading   JiraStatusState = "loading"
	JiraStatusStateTransient JiraStatusState = "transient"
	JiraStatusStateAuth      JiraStatusState = "auth"
	JiraStatusStateUnmapped  JiraStatusState = "unmapped"
	JiraStatusStateError     JiraStatusState = "error"
)

type JiraStatusReason string

const (
	JiraStatusReasonNone                               JiraStatusReason = "none"
	JiraStatusReasonNoMapping                          JiraStatusReason = "no_mapping"
	JiraStatusReasonNoRegexMatch                       JiraStatusReason = "no_regex_match"
	JiraStatusReasonRegexKeyOnly                       JiraStatusReason = "regex_key_only"
	JiraStatusReasonNoGroupConfig                      JiraStatusReason = "no_group_config"
	JiraStatusReasonInvalidRequest                     JiraStatusReason = "invalid_request"
	JiraStatusReasonTemporarilyDown                    JiraStatusReason = "temporarily_unavailable"
	JiraStatusReasonAuthRequired                       JiraStatusReason = "auth_required"
	JiraStatusReasonForbidden                          JiraStatusReason = "forbidden"
	JiraStatusReasonLoginRequired                      JiraStatusReason = "login_required"
	JiraStatusReasonIssueNotFound                      JiraStatusReason = "issue_not_found"
	JiraStatusReasonClientError                        JiraStatusReason = "client_error"
	JiraStatusReasonHTTPError                          JiraStatusReason = "http_error"
	JiraStatusReasonTransportError                     JiraStatusReason = "transport_error"
	JiraStatusReasonResponseParseErr                   JiraStatusReason = "response_parse_error"
	JiraStatusReasonBrowserUnavailableHTTPFallback     JiraStatusReason = "browser_unavailable_http_fallback"
	JiraStatusReasonBrowserUnavailableHTTPAuthRequired JiraStatusReason = "browser_unavailable_http_auth_required"
	JiraStatusReasonBrowserUnavailableHTTPError        JiraStatusReason = "browser_unavailable_http_error"
)

func (b BranchInfo) IsRemote() bool {
	return b.Scope == BranchScopeRemote
}

func (b BranchInfo) IsLocal() bool {
	return b.Scope == BranchScopeLocal
}

// RepoWarning описывает non-fatal предупреждение по репозиторию.
type RepoWarning struct {
	Code    string
	Message string
}

// Text возвращает человекочитаемое сообщение предупреждения.
func (w RepoWarning) Text() string {
	return w.Message
}

// RepoBranches объединяет информацию о ветках конкретного репозитория.
type RepoBranches struct {
	RepoName      string
	RepoURL       string
	RepoSource    string
	RepoPath      string
	SyncWarning   string
	Warning       RepoWarning
	DefaultBranch string
	CurrentBranch string
	DirtyStats    DirtyStats
	Branches      []BranchInfo
}

// RepoStat хранит базовую информацию о статусе репозитория (для списка).
type RepoStat struct {
	CurrentBranch string
	DirtyStats    DirtyStats
	LoadError     string
	SyncWarning   string
	Warning       RepoWarning
	Loaded        bool
}

// HasError возвращает true, если при загрузке состояния репозитория была ошибка.
func (s RepoStat) HasError() bool {
	return s.LoadError != ""
}

// HasSyncWarning возвращает true, если синхронизация remote не удалась, но локальные данные доступны.
func (s RepoStat) HasSyncWarning() bool {
	return s.SyncWarning != "" || s.Warning.Message != ""
}

// ScriptResult хранит результат генерации скрипта удаления веток.
type ScriptResult struct {
	RepoName      string
	RepoPath      string
	ScriptPath    string
	Format        ScriptFormat
	BranchesCount int
}
