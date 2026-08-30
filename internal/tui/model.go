package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agelxnash/go-repo-orchestrator/internal/config"
	"github.com/agelxnash/go-repo-orchestrator/internal/model"
	"github.com/agelxnash/go-repo-orchestrator/internal/usecase"
)

type panelFocus int

const (
	focusRepos panelFocus = iota
	focusBranches
)

type confirmMode int

const (
	confirmNone confirmMode = iota
	confirmGenerate
	confirmCheckout
	confirmReleaseSelect
)

type branchScopeFilter int

const (
	branchScopeLocal branchScopeFilter = iota
	branchScopeRemote
	branchScopeAll
)

type branchSortMode int

const (
	branchSortByName branchSortMode = iota
	branchSortByCommitDate
	branchSortByMergeStatus
	branchSortByJiraStatus
)

type repoSortMode int

const (
	repoSortByName repoSortMode = iota
	repoSortByActiveBranch
)

type startupPlaywrightState int

const (
	startupPlaywrightSkipped startupPlaywrightState = iota
	startupPlaywrightPending
	startupPlaywrightReady
	startupPlaywrightFailed
)

// Model хранит состояние TUI в двухпанельном режиме.
type Model struct {
	cfg   *config.Config
	clean *usecase.Cleaner

	focus        panelFocus
	repoIdx      int
	repoOffset   int
	selected     map[string]map[string]bool
	branchCursor map[string]int
	branchOffset map[string]int

	repoStats   map[string]model.RepoStat
	repoData    map[string]model.RepoBranches
	repoLoading map[string]bool
	repoLoadReq map[string]int
	searchMode  bool
	searchInput textinput.Model

	activeRepo model.RepoBranches
	showInfo   bool

	hideProtected  bool
	branchScope    branchScopeFilter
	branchSort     branchSortMode
	repoSort       repoSortMode
	confirmType    confirmMode
	checkoutTarget string
	scriptFormat   model.ScriptFormat

	releaseLoading         bool
	releaseOptions         []usecase.RepoRelease
	releaseOptionIdx       int
	releaseSelectionByRepo map[string]string

	spinner spinner.Model

	startupLoading   bool
	startupPending   int
	startupTotal     int
	startupURLTotal  int
	startupURLDone   int
	startupRepoTotal int
	startupRepoDone  int

	startupCurrentRepo  string
	startupCurrentStage string
	startupRepoDoneSet  map[string]bool

	startupCurrentOp string

	startupStartedAt      time.Time
	startupElapsed        time.Duration
	startupStageStartedAt time.Time
	startupStageElapsed   time.Duration

	startupPlaywrightStartFn   func() error
	startupPlaywrightState     startupPlaywrightState
	startupPlaywrightScheduled bool

	refreshLocked  bool
	refreshAll     bool
	refreshPending map[string]bool
	refreshRepo    string
	refreshReqID   int

	lastGenerated *model.ScriptResult
	err           error
	statusLine    string
	startupWarn   string

	eventLog []string

	width  int
	height int

	appCtx        context.Context
	appCancel     context.CancelFunc
	actionSeq     int
	actionCancels map[string]actionCancelRef
}

// NewModel создает корневую модель интерфейса.
func NewModel(cfg *config.Config, cleaner *usecase.Cleaner, _ bool) Model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = warnStyle

	ti := textinput.New()
	ti.Placeholder = "Поиск..."
	ti.Prompt = "F3> "
	ti.CharLimit = 100

	appCtx, appCancel := context.WithCancel(context.Background())

	return Model{
		cfg:                    cfg,
		clean:                  cleaner,
		focus:                  focusRepos,
		selected:               make(map[string]map[string]bool),
		branchCursor:           make(map[string]int),
		branchOffset:           make(map[string]int),
		repoStats:              make(map[string]model.RepoStat),
		repoData:               make(map[string]model.RepoBranches),
		repoLoading:            make(map[string]bool),
		repoLoadReq:            make(map[string]int),
		refreshPending:         make(map[string]bool),
		branchScope:            branchScopeAll,
		repoSort:               repoSortByName,
		branchSort:             branchSortByName,
		scriptFormat:           model.ScriptFormatSH,
		searchInput:            ti,
		spinner:                s,
		showInfo:               true,
		width:                  120,
		height:                 36,
		eventLog:               make([]string, 0, 50),
		startupPlaywrightState: startupPlaywrightSkipped,
		startupRepoDoneSet:     make(map[string]bool),
		appCtx:                 appCtx,
		appCancel:              appCancel,
		actionCancels:          make(map[string]actionCancelRef),
		releaseSelectionByRepo: make(map[string]string),
	}
}

func (m *Model) SetStartupWarning(message string) {
	if m == nil {
		return
	}

	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	m.statusLine = message
	m.startupWarn = message
}

func (m *Model) SetPlaywrightStartupStartFn(startFn func() error) {
	if m == nil {
		return
	}

	m.startupPlaywrightStartFn = startFn
	if startFn == nil {
		m.startupPlaywrightState = startupPlaywrightSkipped
		m.startupPlaywrightScheduled = false
		return
	}

	m.startupPlaywrightState = startupPlaywrightPending
	m.startupPlaywrightScheduled = false
}

// Init запускает первичную загрузку данных репозиториев.
func (m Model) Init() tea.Cmd {
	if len(m.cfg.Repos) == 0 {
		return nil
	}

	return tea.Batch(
		textinput.Blink,
		func() tea.Msg {
			return initialLoadMsg{}
		},
	)
}

// Update обновляет состояние интерфейса: распределяет сообщения по обработчикам.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case startupLogMsg:
		return m.handleStartupLog(msg)

	case startupTimerTickMsg:
		return m.handleStartupTimerTick(msg)

	case repoLoadJiraProgressMsg:
		return m.handleRepoLoadJiraProgress(msg)

	case playwrightStartupCompletedMsg:
		return m.handlePlaywrightStartupCompleted(msg)

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)

	case branchesLoadedMsg:
		return m.handleBranchesLoaded(msg)

	case initialLoadMsg:
		return m.handleInitialLoad(msg)

	case scriptGeneratedMsg:
		return m.handleScriptGenerated(msg)

	case repoStatLoadedMsg:
		return m.handleRepoStatLoaded(msg)

	case checkoutCompletedMsg:
		return m.handleCheckoutCompleted(msg)

	case localCopyCompletedMsg:
		return m.handleLocalCopyCompleted(msg)

	case repoFetchPullCompletedMsg:
		return m.handleRepoFetchPullCompleted(msg)

	case releaseOptionsLoadedMsg:
		return m.handleReleaseOptionsLoaded(msg)

	case releaseAutocheckAppliedMsg:
		return m.handleReleaseAutocheckApplied(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	return m, nil
}
