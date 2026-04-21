package tui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"menace/internal/config"
	"menace/internal/engine"
	mlog "menace/internal/log"
	"menace/internal/store"
	"menace/internal/workspace"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case engine.TasksChangedMsg:
		return m.handleTasksChanged()
	case engine.TaskCompletedMsg:
		return m.handleTaskCompleted(msg)
	case spinner.TickMsg:
		return m.handleTick(msg)
	case loginDoneMsg:
		return m.handleLoginDone(msg)
	case startDashboardMsg:
		return m.handleStartDashboard()
	case projectSelectedMsg:
		return m.handleProjectSelected(msg)
	case modelsFetchedMsg:
		m.setup = m.setup.HandleModelsFetched(msg)
		return m, nil
	case customizeEditDoneMsg:
		// Reload custom theme after editor closes
		m.ts.theme = config.LoadTheme("custom", m.ts.dir)
		applyThemeColors(m.ts.theme.Colors)
		m.ts.cfg.Theme = "custom"
		config.Save(m.ts.dir, m.ts.cfg)
		_ = m.store.SetProjectTheme(m.project.id, "custom")
		m.ts.names = config.ListThemes(m.ts.dir)
		m.chat.appendMessage("architect", "theme: custom")
		m.updateChatViewport()
		return m, nil
	case engine.ArchChunkMsg:
		m.chat.HandleChunk(msg.Delta, m.ts.theme, m.frame)
		return m, nil
	case engine.ArchToolMsg:
		m.chat.HandleToolMsg(msg.Display, m.ts.theme, m.frame)
		return m, nil
	case engine.ArchDoneMsg:
		cmd := m.chat.HandleDone(msg, m.ts.theme, m.frame)
		return m, cmd
	case engine.ArchCrashedMsg:
		m.chat.HandleCrashed(msg.Err, m.ts.theme, m.frame)
		return m, nil
	case chatExitInsertMsg:
		m.insertMode = false
		return m, nil
	case chatMarkDirtyMsg:
		m.markDirty()
		return m, nil
	case chatEnsureSessionMsg:
		if m.sess.current == nil {
			m.sess.current = engine.NewSession()
		}
		return m, nil
	case chatResultsSentMsg:
		m.markResultsSent()
		return m, nil
	// ── Proposal panel messages ──
	case propNavDownMsg:
		m.activePanel = panelQueue
		if m.queuePanel.sel < 0 && len(m.queuePanel.tasks) > 0 {
			m.queuePanel.sel = 0
		}
		return m, nil
	case propClearAllMsg:
		if err := m.store.ClearProposals(m.project.id); err != nil {
			mlog.Error("ClearProposals", slog.String("err", err.Error()))
		}
		return m, nil
	case propOpenMsg:
		m.openProposalModal(msg.Index)
		return m, nil
	// ── Queue panel messages ──
	case queueNavUpMsg:
		m.activePanel = panelProposals
		if len(m.propPanel.proposals) > 0 {
			m.propPanel.sel = len(m.propPanel.proposals) - 1
		}
		return m, nil
	case queueClearDoneMsg:
		if err := m.store.ClearFinishedTasks(m.project.id); err != nil {
			mlog.Error("ClearFinishedTasks", slog.String("err", err.Error()))
		}
		m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
		m.queuePanel.ClampSel()
		m.recalcProgress()
		return m, nil
	case queueOpenMsg:
		m.openReviewModal()
		return m, nil
	// ── Chat panel messages ──
	case chatProposalsMsg:
		for _, p := range msg.proposals {
			m.propPanel.proposals = append(m.propPanel.proposals, p)
			if m.sess.current != nil {
				m.sess.current.ProposalIDs = append(m.sess.current.ProposalIDs, p.ID)
			}
			_ = m.store.SaveProposal(m.project.id, m.sessionID(), p)
		}
		return m, nil
	case modalCloseMsg:
		return m.handleModalClose()
	case settingsCfgChangedMsg:
		return m.handleSettingsCfgChanged(msg)
	case settingsThemeChangedMsg:
		return m.handleSettingsThemeChanged(msg)
	case settingsModelChangedMsg:
		return m.handleSettingsModelChanged()
	case settingsLogoutMsg:
		return m.handleSettingsLogout()
	case settingsCustomizeMsg:
		return m.handleSettingsCustomize()
	case sessionSelectedMsg:
		return m.handleSessionSelected(msg)
	case proposalApprovedMsg:
		return m.handleProposalApproved(msg)
	case proposalRejectedMsg:
		return m.handleProposalRejected(msg)
	case reviewCancelTaskMsg:
		return m.handleReviewCancelTask(msg)
	case reviewDeleteTaskMsg:
		return m.handleReviewDeleteTask(msg)
	case reviewRetryTaskMsg:
		return m.handleReviewRetryTask(msg)
	case tea.KeyMsg:
		if m.screen == screenSetup {
			return m.handleSetupKey(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.resizeComponents()
	return m, nil
}

func (m model) handleTasksChanged() (tea.Model, tea.Cmd) {
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.recalcProgress()
	return m, nil
}

func (m model) handleTaskCompleted(msg engine.TaskCompletedMsg) (tea.Model, tea.Cmd) {
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.recalcProgress()

	result := store.TaskResult{
		TaskID:      msg.TaskID,
		Description: msg.Description,
		Status:      msg.Status,
		Error:       msg.ErrLine,
	}

	if m.sess.current != nil {
		for _, tid := range m.sess.current.TaskIDs {
			if tid == msg.TaskID {
				m.sess.current.Results = append(m.sess.current.Results, result)
				desc := msg.Description
				if desc == "" {
					desc = msg.TaskID
				}
				icon := "✓ done"
				if msg.Status == store.StatusFailed {
					icon = "✗ failed"
				}
				m.chat.appendMessage("architect", fmt.Sprintf("[%s] %s", icon, desc))
				m.updateChatViewport()
				m.markDirty()
				break
			}
		}
	}
	return m, nil
}

func (m model) handleTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	m.frame++
	if m.frame%60 == 0 {
		m.flushSession()
	}
	cmd := m.chat.TickSpinner(msg)
	return m, cmd
}

func (m model) handleLoginDone(msg loginDoneMsg) (tea.Model, tea.Cmd) {
	auth, err := m.store.GetAuth()
	if err != nil {
		m.chat.appendMessage("architect", "Auth error: "+err.Error())
		m.updateChatViewport()
		return m, nil
	}
	if auth == nil {
		m.chat.appendMessage("architect", "No auth configured.")
		m.updateChatViewport()
		return m, nil
	}
	m.screen = screenDashboard
	m.architectModel = auth.ArchitectModel
	m.workerModel = auth.WorkerModel
	m.orchestrator = engine.NewOrchestrator(engine.OrchestratorConfig{
		CWD:           m.project.cwd,
		MenaceDir:     m.ts.dir,
		ProjectID:     m.project.id,
		ProviderName:  auth.Provider,
		WorkerModel:   auth.WorkerModel,
		APIKey:        auth.APIKey,
		MaxConcurrent: m.ts.cfg.Concurrency,
		MaxRetry:      m.ts.cfg.MaxRetry,
	}, m.store, m.programRef.p)
	m.chat.appendMessage("architect", m.ts.theme.Personality.Welcome)
	m.updateChatViewport()
	return m, nil
}

func (m model) handleStartDashboard() (tea.Model, tea.Cmd) {
	auth, err := m.store.GetAuth()
	if err != nil {
		mlog.Error("handleStartDashboard", slog.String("err", err.Error()))
		return m, nil
	}
	if auth == nil {
		return m, nil
	}
	m.architectModel = auth.ArchitectModel
	m.workerModel = auth.WorkerModel
	m.orchestrator = engine.NewOrchestrator(engine.OrchestratorConfig{
		CWD:           m.project.cwd,
		MenaceDir:     m.ts.dir,
		ProjectID:     m.project.id,
		ProviderName:  auth.Provider,
		WorkerModel:   auth.WorkerModel,
		APIKey:        auth.APIKey,
		MaxConcurrent: m.ts.cfg.Concurrency,
		MaxRetry:      m.ts.cfg.MaxRetry,
	}, m.store, m.programRef.p)

	if m.sess.pending {
		m.sess.current = engine.NewSession()
		m.markDirty()
		m.sess.pending = false
	}

	m.chat.appendMessage("architect", m.ts.theme.Personality.Welcome)
	if !m.project.gitAvailable {
		m.chat.appendMessage("architect", "Warning: git not available in this directory. Task diffs will not be captured.")
	}
	m.updateChatViewport()
	return m, nil
}

func (m model) handleProjectSelected(msg projectSelectedMsg) (tea.Model, tea.Cmd) {
	data, err := os.ReadFile(m.project.pickerTmp)
	os.Remove(m.project.pickerTmp)

	newCwd := strings.TrimSpace(string(data))
	if newCwd == "" || err != nil || msg.err != nil {
		return m, nil
	}

	// Register the new project (may be first time)
	newID := workspace.ProjectHash(newCwd)
	_ = m.store.RegisterProject(newID, newCwd)

	return m.switchToProject(store.ProjectEntry{
		ID:   newID,
		Path: newCwd,
		Name: filepath.Base(newCwd),
	})
}

// switchToProject handles all state transitions when changing the active project.
func (m model) switchToProject(p store.ProjectEntry) (tea.Model, tea.Cmd) {
	if p.ID == m.project.id {
		return m, nil
	}

	m.markDirty()
	m.flushSession()

	if m.orchestrator != nil {
		m.orchestrator.Stop()
	}
	if m.chat.proc != nil {
		m.chat.proc.Stop()
		m.chat.proc = nil
	}

	m.project.cwd = p.Path
	m.project.id = p.ID
	m.project.gitAvailable = engine.GitAvailable(p.Path)
	_ = m.store.RegisterProject(p.ID, p.Path)

	// Load per-project theme
	projTheme := m.store.GetProjectTheme(p.ID)
	if projTheme != "" {
		m.ts.theme = config.LoadTheme(projTheme, m.ts.dir)
	} else {
		m.ts.theme = config.LoadTheme(m.ts.cfg.Theme, m.ts.dir)
	}
	applyThemeColors(m.ts.theme.Colors)
	// Sync theme index
	for i, name := range m.ts.names {
		if name == m.ts.theme.Meta.Name {
			m.ts.idx = i
			break
		}
	}

	m.sess.current = nil
	m.chat.history = nil
	loadedProposals, err := m.store.LoadProposals(m.project.id)
	if err != nil {
		mlog.Error("LoadProposals", slog.String("err", err.Error()))
	}
	m.propPanel.proposals = loadedProposals
	m.propPanel.sel = 0
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.queuePanel.sel = 0
	m.recalcProgress()

	auth, err := m.store.GetAuth()
	if err != nil {
		mlog.Error("switchToProject auth", slog.String("err", err.Error()))
	}
	if auth != nil {
		m.architectModel = auth.ArchitectModel
		m.workerModel = auth.WorkerModel
		m.orchestrator = engine.NewOrchestrator(engine.OrchestratorConfig{
			CWD:           m.project.cwd,
			MenaceDir:     m.ts.dir,
			ProjectID:     m.project.id,
			ProviderName:  auth.Provider,
			WorkerModel:   auth.WorkerModel,
			APIKey:        auth.APIKey,
			MaxConcurrent: m.ts.cfg.Concurrency,
			MaxRetry:      m.ts.cfg.MaxRetry,
		}, m.store, m.programRef.p)
	}

	m.chat.appendMessage("architect", fmt.Sprintf(m.ts.theme.Personality.Switched, filepath.Base(m.project.cwd)))
	m.updateChatViewport()

	if m.sess.pending {
		m.sess.current = engine.NewSession()
		if len(m.chat.history) > 0 {
			m.chat.history[len(m.chat.history)-1].Content = fmt.Sprintf(m.ts.theme.Personality.NewSess, filepath.Base(m.project.cwd))
		}
		m.markDirty()
		m.sess.pending = false
	}

	// Refresh project list and find new index
	refreshedProjects, err := m.store.ListProjects()
	if err != nil {
		mlog.Error("ListProjects", slog.String("err", err.Error()))
	}
	m.project.list = refreshedProjects
	for i, proj := range m.project.list {
		if proj.ID == m.project.id {
			m.project.idx = i
			break
		}
	}

	return m, nil
}


func (m model) handleSetupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.setup, cmd = m.setup.Update(msg)
	return m, cmd
}

func (m *model) resizeComponents() {
	bodyH := m.height - m.chromeHeight()
	leftW := m.width/2 - layoutBorderPadW
	chatH := bodyH - layoutChatPadH
	if chatH < 3 {
		chatH = 3
	}
	leftContentH := bodyH - layoutPanelPadH
	maxInputH := leftContentH / 2
	if maxInputH < 3 {
		maxInputH = 3
	}
	m.chat.Resize(leftW-2, chatH, leftW, maxInputH)

	if m.activeModal != nil {
		m.activeModal.Resize(m.width, m.height)
	}
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.activeModal != nil {
		if m.activeModal.WantsRawKeys() {
			return m, m.activeModal.HandleRawKey(key)
		}
		return m, m.activeModal.Update(resolveModal(key))
	}
	if m.insertMode {
		act := resolveInsert(key)
		if act != actNone {
			return m.handleInsert(act)
		}

		maxInputH := (m.height - m.chromeHeight() - layoutPanelPadH) / 2
		if maxInputH < 3 {
			maxInputH = 3
		}
		m.chat.input.SetHeight(maxInputH)

		var cmd tea.Cmd
		m.chat.input, cmd = m.chat.input.Update(msg)

		lines := visualLineCount(m.chat.input.Value(), m.chat.input.Width())
		if lines < 1 {
			lines = 1
		}
		if lines > maxInputH {
			lines = maxInputH
		}
		m.chat.input.SetHeight(lines)
		m.chat.input.MaxHeight = maxInputH

		return m, cmd
	}
	return m.handleNormal(resolveNormal(key))
}

func (m *model) sessionID() string {
	if m.sess.current != nil {
		return m.sess.current.ID
	}
	return ""
}

func (m *model) sessionUnseenResults() []store.TaskResult {
	if m.sess.current == nil {
		return nil
	}
	if m.sess.current.ResultsSent >= len(m.sess.current.Results) {
		return nil
	}
	return m.sess.current.Results[m.sess.current.ResultsSent:]
}

func (m *model) markResultsSent() {
	if m.sess.current != nil {
		m.sess.current.ResultsSent = len(m.sess.current.Results)
	}
}




func (m *model) chatCtx() chatContext {
	maxInputH := (m.height - m.chromeHeight() - layoutPanelPadH) / 2
	if maxInputH < 3 {
		maxInputH = 3
	}
	ctx := chatContext{
		theme:      m.ts.theme,
		frame:      m.frame,
		menaceDir:  m.ts.dir,
		cwd:        m.project.cwd,
		programRef: m.programRef.p,
		store:      m.store,
		hasSession: m.sess.current != nil,
		maxInputH:  maxInputH,
	}
	if m.sess.current != nil {
		ctx.sessionID = m.sess.current.ID
		ctx.results = m.sessionUnseenResults()
	}
	return ctx
}

func (m *model) kickScheduler() {
	if m.orchestrator != nil {
		go m.orchestrator.Schedule()
	}
}

func (m *model) updateChatViewport() {
	m.chat.updateViewport(m.ts.theme, m.frame)
}
