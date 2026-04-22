package tui

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"menace/internal/config"
	"menace/internal/engine"
	mlog "menace/internal/log"
	"menace/internal/workspace"

	tea "github.com/charmbracelet/bubbletea"
)

type projectSelectedMsg struct{ err error }

func (m model) handleInsert(act action) (tea.Model, tea.Cmd) {
	cmd := m.chat.HandleInsert(act, m.chatCtx())
	return m, cmd
}

func (m model) handleNormal(act action) (tea.Model, tea.Cmd) {
	switch act {
	case actQuit:
		m.markDirty()
		m.flushSession()
		if m.chat.proc != nil {
			m.chat.proc.Stop()
		}
		return m, tea.Quit
	case actInsert:
		m.activePanel = panelArchitect
		m.insertMode = true
		m.chat.input.Focus()
		return m, nil
	case actNextPanel:
		m.activePanel = (m.activePanel + 1) % 3
	
		return m, nil
	case actPrevPanel:
		m.activePanel = (m.activePanel + 2) % 3
	
		return m, nil
	case actLeft:
		if m.activePanel != panelArchitect {
			m.activePanel = panelArchitect
	
		}
		return m, nil
	case actRight:
		if m.activePanel == panelArchitect {
			m.activePanel = panelProposals
	
		}
		return m, nil
	case actClearChat:
		m.chat.history = nil
		m.updateChatViewport()
		return m, nil
	case actNewSession:
		m.markDirty()
		m.flushSession()
		if m.chat.proc != nil {
			m.chat.proc.Stop()
			m.chat.proc = nil
		}
		m.sess.current = engine.NewSession()
		m.chat.history = nil
		m.chat.appendMessage("architect", fmt.Sprintf(m.ts.theme.Personality.NewSess, filepath.Base(m.project.cwd)))
		m.updateChatViewport()
		m.markDirty()
		return m, nil
	case actSessions:
		sm := NewSessionsModal(m.store)
		sm.Resize(m.width, m.height)
		m.activeModal = sm
		return m, nil
	case actProjectCycle:
		cycleProjects, err := m.store.ListProjects()
		if err != nil {
			mlog.Error("ListProjects", slog.String("err", err.Error()))
		}
		m.project.list = cycleProjects
		if len(m.project.list) <= 1 {
			return m, nil
		}
		// Find current index
		for i, p := range m.project.list {
			if p.ID == m.project.id {
				m.project.idx = i
				break
			}
		}
		m.project.idx = (m.project.idx + 1) % len(m.project.list)
		return m.switchToProject(m.project.list[m.project.idx])
	case actProjectAdd:
		m.sess.pending = false
		cmd, tmpPath, err := workspace.PickerCmd()
		if err != nil {
			m.chat.appendMessage("architect", "⚠ " + err.Error())
			m.updateChatViewport()
			return m, nil
		}
		m.project.pickerTmp = tmpPath
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return projectSelectedMsg{err: err} })
	case actTheme:
		if len(m.ts.names) > 0 {
			m.ts.idx = (m.ts.idx + 1) % len(m.ts.names)
			name := m.ts.names[m.ts.idx]
			m.ts.theme = config.LoadTheme(name, m.ts.dir)
			applyThemeColors(m.ts.theme.Colors)
			_ = m.store.SetProjectTheme(m.project.id, name)
			m.chat.appendMessage("architect", fmt.Sprintf("theme: %s", m.ts.theme.Meta.Name))
			m.updateChatViewport()
		}
		return m, nil
	case actSettings:
		m.ts.names = config.ListThemes(m.ts.dir)
		m.activeModal = NewSettingsModal(m.store, m.ts.cfg, m.ts.theme, m.ts.names, m.ts.dir, m.project.id)
		return m, nil
	}

	switch m.activePanel {
	case panelArchitect:
		return m.normalArchitect(act)
	case panelProposals:
		return m.normalProposals(act)
	case panelQueue:
		return m.normalQueue(act)
	}
	return m, nil
}

func (m model) normalArchitect(act action) (tea.Model, tea.Cmd) {
	if act == actRestart {
		if m.chat.proc != nil {
			m.chat.proc.Stop()
			m.chat.proc = nil
		}
		m.chat.resetStream()
		m.chat.appendMessage("architect", m.ts.theme.Personality.Restarted)
		m.updateChatViewport()
		return m, nil
	}
	m.chat.HandleNormal(act, m.ts.theme, m.frame)
	return m, nil
}

func (m model) normalProposals(act action) (tea.Model, tea.Cmd) {
	cmd := m.propPanel.HandleNormal(act)
	return m, cmd
}

func (m model) normalQueue(act action) (tea.Model, tea.Cmd) {
	cmd := m.queuePanel.HandleNormal(act)
	return m, cmd
}

func (m *model) openProposalModal(idx int) {
	pm := NewProposalModal(m.propPanel.proposals, idx, m.width)
	pm.Resize(m.width, m.height)
	m.activeModal = pm
}

func (m *model) openReviewModal() {
	rm := NewReviewModal(m.store, m.orchestrator, m.project.cwd, m.queuePanel.tasks, m.queuePanel.sel)
	rm.Resize(m.width, m.height)
	m.activeModal = rm
}
