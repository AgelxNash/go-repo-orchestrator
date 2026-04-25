package tui

import (
	"context"
	"time"

	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"

	"github.com/agelxnash/go-repo-orchestrator/internal/model"
)

type branchesLoadedMsg struct {
	requestID            int
	actionKey            string
	actionID             int
	repoName             string
	rb                   model.RepoBranches
	err                  error
	startup              bool
	jiraResolved         int
	jiraBatchProgress    []usecase.RepoLoadProgress
	jiraProgressStreamed bool
	syncNote             string
}

// startupLogMsg позволяет фоновым задачам отправлять записи в лог загрузки TUI.
type startupLogMsg struct{ text string }

type startupTimerTickMsg struct{ at time.Time }

type repoLoadJiraProgressMsg struct {
	repoName string
	startup  bool
	progress usecase.RepoLoadProgress
	stream   <-chan usecase.RepoLoadProgress
}

type playwrightStartupCompletedMsg struct{ err error }

type scriptGeneratedMsg struct {
	result model.ScriptResult
	err    error
}

type initialLoadMsg struct{}

type repoStatLoadedMsg struct {
	actionKey string
	actionID  int
	repoName  string
	stat      model.RepoStat
	err       error
	startup   bool
}

type checkoutCompletedMsg struct {
	actionKey string
	actionID  int
	repoName  string
	err       error
}

type localCopyCompletedMsg struct {
	actionKey string
	actionID  int
	repoName  string
	branch    string
	err       error
}

type repoFetchPullCompletedMsg struct {
	actionKey string
	actionID  int
	repoName  string
	err       error
}

type releaseOptionsLoadedMsg struct {
	actionKey string
	actionID  int
	repoName  string
	options   []usecase.RepoRelease
	err       error
}

type releaseAutocheckAppliedMsg struct {
	actionKey  string
	actionID   int
	repoName   string
	summary    usecase.ReleaseAutocheckResult
	branches   []model.BranchInfo
	selectedID string
	err        error
}

type actionCancelRef struct {
	id     int
	cancel context.CancelFunc
}
