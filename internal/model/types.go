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
	JiraStatusReasonResponseTooLarge                   JiraStatusReason = "response_too_large"
	JiraStatusReasonBrowserUnavailableHTTPFallback     JiraStatusReason = "browser_unavailable_http_fallback"
	JiraStatusReasonBrowserUnavailableHTTPAuthRequired JiraStatusReason = "browser_unavailable_http_auth_required"
	JiraStatusReasonBrowserUnavailableHTTPError        JiraStatusReason = "browser_unavailable_http_error"
)

// IsRemote сообщает, что ветка относится к удаленному scope.
func (b BranchInfo) IsRemote() bool {
	return b.Scope == BranchScopeRemote
}

// IsLocal сообщает, что ветка относится к локальному scope.
func (b BranchInfo) IsLocal() bool {
	return b.Scope == BranchScopeLocal
}

// RepoWarning описывает non-fatal предупреждение по репозиторию.
type RepoWarning struct {
	Code    string
	Message string
}

const (
	// RepoWarningRemoteSyncFailed — синхронизация remote не удалась, локальные данные доступны.
	RepoWarningRemoteSyncFailed = "remote_sync_failed"
	// RepoWarningEmptyClone — клон без checkout: HEAD не резолвится и нет локальных веток.
	RepoWarningEmptyClone = "empty_clone"
)

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

// RepoLoadErrorKind различает причину LoadError для сводки старта TUI.
type RepoLoadErrorKind string

const (
	// RepoLoadErrorKindUnknown — неизвестная категория, в сводке «ошибка загрузки».
	RepoLoadErrorKindUnknown RepoLoadErrorKind = ""
	// RepoLoadErrorKindNetwork — сбой сети или SSH handshake.
	RepoLoadErrorKindNetwork RepoLoadErrorKind = "network"
	// RepoLoadErrorKindLocal — локальный путь или не git-каталог.
	RepoLoadErrorKindLocal RepoLoadErrorKind = "local"
	// RepoLoadErrorKindCorrupt — повреждённые git-метаданные.
	RepoLoadErrorKindCorrupt RepoLoadErrorKind = "corrupt"
)

// RepoStat хранит базовую информацию о статусе репозитория (для списка).
type RepoStat struct {
	CurrentBranch string
	DirtyStats    DirtyStats
	LoadError     string
	LoadErrorKind RepoLoadErrorKind
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
	return s.SyncWarning != "" || s.Warning.Code == RepoWarningRemoteSyncFailed
}

// HasEmptyCloneWarning возвращает true, если репозиторий прочитан как оболочка без checkout.
func (s RepoStat) HasEmptyCloneWarning() bool {
	return s.Warning.Code == RepoWarningEmptyClone
}

// ScriptResult хранит результат генерации скрипта удаления веток.
type ScriptResult struct {
	RepoName      string
	RepoPath      string
	ScriptPath    string
	Format        ScriptFormat
	BranchesCount int
}

// StatusReport содержит сводный отчет о состоянии репозиториев для команды status.
type StatusReport struct {
	Total  int             `json:"total"`
	Clean  int             `json:"clean"`
	Dirty  int             `json:"dirty"`
	Errors int             `json:"errors"`
	Repos  []RepoStatusDTO `json:"repos"`
}

// RepoStatusDTO описывает состояние отдельного репозитория в отчете команды status.
type RepoStatusDTO struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Dirty       bool   `json:"dirty"`
	SyncWarning string `json:"sync_warning,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ExecReport содержит сводный отчет пакетного выполнения команд.
type ExecReport struct {
	Total   int             `json:"total"`
	Success int             `json:"success"`
	Failed  int             `json:"failed"`
	Results []ExecResultDTO `json:"results"`
}

// ExecResultDTO описывает результат выполнения команды в одном репозитории.
type ExecResultDTO struct {
	Repo       string `json:"repo"`
	Path       string `json:"path"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}
