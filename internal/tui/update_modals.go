package tui

import (
	"fmt"
	"log/slog"

	"menace/internal/engine"
	mlog "menace/internal/log"
	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) handleModalClose() (tea.Model, tea.Cmd) {
	m.activeModal = nil
	return m, nil
}

func (m model) handleSettingsCfgChanged(msg settingsCfgChangedMsg) (tea.Model, tea.Cmd) {
	m.ts.cfg = msg.Cfg
	return m, nil
}

func (m model) handleSettingsThemeChanged(msg settingsThemeChangedMsg) (tea.Model, tea.Cmd) {
	m.ts.theme = msg.Theme
	m.ts.cfg.Theme = msg.ThemeName
	for i, name := range m.ts.names {
		if name == msg.ThemeName {
			m.ts.idx = i
			break
		}
	}
	applyThemeColors(m.ts.theme.Colors)
	return m, nil
}

func (m model) handleSettingsModelChanged() (tea.Model, tea.Cmd) {
	// Kill architect so it restarts with new model on next prompt.
	// Chat history stays — user sees continuity.
	if m.chat.proc != nil {
		m.chat.proc.Stop()
		m.chat.proc = nil
	}
	// Recreate orchestrator with new worker model.
	if m.orchestrator != nil {
		m.orchestrator.Stop()
	}
	auth, _ := m.store.GetAuth()
	if auth != nil {
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
	m.architectModel = auth.ArchitectModel
	m.workerModel = auth.WorkerModel
	m.chat.appendMessage("architect", fmt.Sprintf("↻ Models updated. architect: %s, worker: %s", auth.ArchitectModel, auth.WorkerModel))
	m.updateChatViewport()
	return m, nil
}

func (m model) handleSettingsLogout() (tea.Model, tea.Cmd) {
	m.activeModal = nil
	if m.chat.proc != nil {
		m.chat.proc.Stop()
		m.chat.proc = nil
	}
	if m.orchestrator != nil {
		m.orchestrator.Stop()
		m.orchestrator = nil
	}
	if err := m.store.ClearAllAuth(); err != nil {
		mlog.Error("ClearAllAuth", slog.String("err", err.Error()))
	}
	m.screen = screenSetup
	m.setup = newSetupModel(m.store)
	m.chat.history = nil
	m.chat.resetStream()
	m.updateChatViewport()
	return m, nil
}

func (m model) handleSettingsCustomize() (tea.Model, tea.Cmd) {
	m.activeModal = nil
	return m, nil
}

func (m model) handleSessionSelected(msg sessionSelectedMsg) (tea.Model, tea.Cmd) {
	m.activeModal = nil
	m.sess.current = msg.Session
	m.chat.history = msg.Session.Chat
	m.updateChatViewport()

	sessionProjID := msg.ProjectID
	if sessionProjID == "" {
		sessionProjID = m.project.id
	}
	allP, err := m.store.LoadProposals(sessionProjID)
	if err != nil {
		mlog.Error("LoadProposals", slog.String("err", err.Error()))
	}
	m.propPanel.proposals = nil
	for _, p := range allP {
		for _, pid := range msg.Session.ProposalIDs {
			if p.ID == pid {
				m.propPanel.proposals = append(m.propPanel.proposals, p)
				break
			}
		}
	}
	m.propPanel.sel = 0

	allT := toUITasks(engine.SyncTasks(m.store, sessionProjID))
	m.queuePanel.tasks = nil
	for _, t := range allT {
		for _, tid := range msg.Session.TaskIDs {
			if t.id == tid {
				m.queuePanel.tasks = append(m.queuePanel.tasks, t)
				break
			}
		}
	}
	m.queuePanel.sel = 0
	m.recalcProgress()
	return m, nil
}

func (m model) handleProposalApproved(msg proposalApprovedMsg) (tea.Model, tea.Cmd) {
	idx := msg.Index
	p := msg.Proposal
	sessionID := ""
	if m.sess.current != nil {
		sessionID = m.sess.current.ID
	}
	t, err := engine.AddTask(m.store, m.project.id, sessionID, p.Description, p.Instruction, p.Subtasks)
	if err != nil {
		mlog.Error("AddTask", slog.String("err", err.Error()))
	}
	if m.sess.current != nil {
		m.sess.current.TaskIDs = append(m.sess.current.TaskIDs, t.ID)
		m.markDirty()
	}
	m.propPanel.proposals = append(m.propPanel.proposals[:idx], m.propPanel.proposals[idx+1:]...)
	if err := m.store.DeleteProposal(p.ID); err != nil {
		mlog.Error("DeleteProposal", slog.String("err", err.Error()))
	}
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.recalcProgress()
	if m.propPanel.sel >= len(m.propPanel.proposals) && m.propPanel.sel > 0 {
		m.propPanel.sel--
	}
	m.activeModal = nil
	m.kickScheduler()
	return m, nil
}

func (m model) handleProposalRejected(msg proposalRejectedMsg) (tea.Model, tea.Cmd) {
	idx := msg.Index
	if idx >= 0 && idx < len(m.propPanel.proposals) {
		m.propPanel.proposals = append(m.propPanel.proposals[:idx], m.propPanel.proposals[idx+1:]...)
		if err := m.store.DeleteProposal(msg.ProposalID); err != nil {
			mlog.Error("DeleteProposal", slog.String("err", err.Error()))
		}
		if m.propPanel.sel >= len(m.propPanel.proposals) && m.propPanel.sel > 0 {
			m.propPanel.sel--
		}
	}
	m.activeModal = nil
	return m, nil
}

func (m model) handleReviewCancelTask(msg reviewCancelTaskMsg) (tea.Model, tea.Cmd) {
	if m.orchestrator != nil {
		m.orchestrator.CancelTask(msg.TaskID)
	}
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.recalcProgress()
	if rm, ok := m.activeModal.(*ReviewModal); ok {
		rm.RefreshTasks(m.queuePanel.tasks)
	}
	return m, nil
}

func (m model) handleReviewDeleteTask(msg reviewDeleteTaskMsg) (tea.Model, tea.Cmd) {
	if err := m.store.RemoveTask(msg.TaskID); err != nil {
		mlog.Error("RemoveTask", slog.String("err", err.Error()))
	}
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	if m.queuePanel.sel >= len(m.queuePanel.tasks) && m.queuePanel.sel > 0 {
		m.queuePanel.sel--
	}
	m.recalcProgress()
	m.activeModal = nil
	return m, nil
}

func (m model) handleReviewRetryTask(msg reviewRetryTaskMsg) (tea.Model, tea.Cmd) {
	_ = m.store.UpdateTaskStatus(msg.TaskID, store.StatusPending)
	m.queuePanel.tasks = toUITasks(engine.SyncTasks(m.store, m.project.id))
	m.recalcProgress()
	if rm, ok := m.activeModal.(*ReviewModal); ok {
		rm.RefreshTasks(m.queuePanel.tasks)
	}
	m.kickScheduler()
	return m, nil
}
