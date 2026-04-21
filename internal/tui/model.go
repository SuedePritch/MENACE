package tui

import (
	"log/slog"

	"menace/internal/config"
	"menace/internal/engine"
	mlog "menace/internal/log"
	"menace/internal/store"
	"menace/internal/workspace"

	tea "github.com/charmbracelet/bubbletea"
)

type appScreen int

const (
	screenSetup appScreen = iota
	screenDashboard
)

type focusPanel int

const (
	panelArchitect focusPanel = iota
	panelProposals
	panelQueue
)


type modalFocus int

const (
	modalFocusFiles modalFocus = iota
	modalFocusDiff
	modalFocusLogs
)

const (
	diffScopeSubtask = 0
	diffScopeTask    = 1
	diffScopeAll     = 2
)

var diffScopeNames = []string{"subtask", "task", "all"}

type diffFile struct {
	name    string
	added   int
	removed int
	hunks   string
}

// chatState is an alias kept for the model struct field name.
// The actual implementation is in panel_chat.go as chatPanel.

// setupState is an alias for the extracted setupModel.
// Kept as a field name for minimal diff in model struct.

// projectState groups multi-project management fields.
type projectState struct {
	list         []store.ProjectEntry
	idx          int
	id           string
	cwd          string
	pickerTmp    string
	gitAvailable bool
}

// themeState groups theme/config fields that travel together.
type themeState struct {
	cfg    config.MenaceConfig
	theme  config.Theme
	names  []string
	idx    int
	dir    string // menaceDir
}

// sessionState groups the active session and its dirty tracking.
type sessionState struct {
	current *store.Session
	dirty   bool
	pending bool // newSessionPending — deferred session creation
}

// task is the UI-facing representation of a task (lighter than store.TaskData).
type task struct {
	id          string
	description string
	status      store.TaskStatus
	elapsed     int
	subtasks    []subtask
}

type subtask struct {
	id          string
	description string
	status      store.TaskStatus
}

// toUITasks converts store.TaskData to UI task structs.
func toUITasks(dbTasks []store.TaskData) []task {
	var tasks []task
	for _, t := range dbTasks {
		var subs []subtask
		for _, s := range t.Subtasks {
			subs = append(subs, subtask{
				id:          s.ID,
				description: s.Description,
				status:      s.Status,
			})
		}
		tasks = append(tasks, task{
			id:          t.ID,
			description: t.Description,
			status:      t.Status,
			elapsed:     t.Elapsed,
			subtasks:    subs,
		})
	}
	return tasks
}

type model struct {
	width  int
	height int

	screen      appScreen
	activePanel focusPanel
	insertMode  bool

	chat    chatPanel
	setup   setupModel
	project projectState
	ts      themeState
	sess    sessionState

	// Right panels
	propPanel  proposalPanel
	queuePanel queuePanel

	// Modal — at most one active at a time.
	activeModal Modal

	// Animation
	progress float64
	frame    int

	// Infrastructure
	store          *store.Store
	orchestrator   *engine.Orchestrator
	programRef     *programRef
	architectModel string
	workerModel    string
}

type programRef struct {
	p *tea.Program
}

type startDashboardMsg struct{}

func initialModel(cwd, menaceDir string, s *store.Store) model {
	cfg := config.Load(menaceDir)
	themeNames := config.ListThemes(menaceDir)
	theme := config.LoadTheme(cfg.Theme, menaceDir)
	applyThemeColors(theme.Colors)
	applyKeyOverrides(cfg.Keys)

	// Find current theme index
	themeIdx := 0
	for i, name := range themeNames {
		if name == cfg.Theme {
			themeIdx = i
			break
		}
	}

	startScreen := screenSetup
	if auth, _ := s.GetAuth(); auth != nil && auth.APIKey != "" {
		startScreen = screenDashboard
	}

	projectID := workspace.ProjectHash(cwd)

	m := model{
		screen:      startScreen,
		activePanel: panelArchitect,
		store:       s,
		programRef:  &programRef{},
		setup:       newSetupModel(s),
		chat:        newChatPanel(cfg),
		ts: themeState{
			cfg:   cfg,
			theme: theme,
			names: themeNames,
			idx:   themeIdx,
			dir:   menaceDir,
		},
		project: projectState{
			id:           projectID,
			cwd:          cwd,
			gitAvailable: engine.GitAvailable(cwd),
		},
		propPanel:  proposalPanel{},
		queuePanel: queuePanel{},
	}

	proposals, err := s.LoadProposals(projectID)
	if err != nil {
		mlog.Error("LoadProposals", slog.String("err", err.Error()))
	}
	m.propPanel.proposals = proposals

	_ = s.RegisterProject(projectID, cwd)

	// Load project list and find current index
	projects, err := s.ListProjects()
	if err != nil {
		mlog.Error("ListProjects", slog.String("err", err.Error()))
	}
	m.project.list = projects
	for i, p := range m.project.list {
		if p.ID == projectID {
			m.project.idx = i
			break
		}
	}

	// Per-project theme overrides global
	if projTheme := s.GetProjectTheme(projectID); projTheme != "" {
		m.ts.theme = config.LoadTheme(projTheme, menaceDir)
		applyThemeColors(m.ts.theme.Colors)
		for i, name := range m.ts.names {
			if name == projTheme {
				m.ts.idx = i
				break
			}
		}
	}

	m.queuePanel.tasks = toUITasks(engine.SyncTasks(s, m.project.id))
	m.recalcProgress()
	return m
}

func (m *model) recalcProgress() {
	sessionTasks := m.sessionTaskList()
	if len(sessionTasks) == 0 {
		m.progress = 0
		return
	}
	done := 0
	for _, t := range sessionTasks {
		if t.status == store.StatusDone {
			done++
		}
	}
	m.progress = float64(done) / float64(len(sessionTasks))
}

func (m *model) sessionTaskList() []task {
	if m.sess.current == nil || len(m.sess.current.TaskIDs) == 0 {
		return m.queuePanel.tasks
	}
	var result []task
	taskIDSet := make(map[string]bool)
	for _, id := range m.sess.current.TaskIDs {
		taskIDSet[id] = true
	}
	for _, t := range m.queuePanel.tasks {
		if taskIDSet[t.id] {
			result = append(result, t)
		}
	}
	return result
}

func (m *model) sessionProposalList() []store.Proposal {
	if m.sess.current == nil || len(m.sess.current.ProposalIDs) == 0 {
		return m.propPanel.proposals
	}
	var result []store.Proposal
	proposalIDSet := make(map[string]bool)
	for _, id := range m.sess.current.ProposalIDs {
		proposalIDSet[id] = true
	}
	for _, p := range m.propPanel.proposals {
		if proposalIDSet[p.ID] {
			result = append(result, p)
		}
	}
	return result
}

func (m *model) markDirty() {
	if m.sess.current != nil {
		m.sess.current.Chat = m.chat.history
		m.sess.dirty = true
	}
}

func (m *model) flushSession() {
	if m.sess.dirty && m.sess.current != nil {
		_ = m.store.SaveSession(m.project.id, m.sess.current)
		m.sess.dirty = false
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.EnterAltScreen,
		m.chat.spin.Tick,
	}
	if m.screen == screenDashboard {
		cmds = append(cmds, func() tea.Msg {
			return startDashboardMsg{}
		})
	}
	return tea.Batch(cmds...)
}
