package tui

import (
	"fmt"
	"log/slog"
	"strings"

	mlog "menace/internal/log"
	"menace/internal/store"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionsModal encapsulates the sessions picker modal state.
type SessionsModal struct {
	store       *store.Store
	sessionList []store.SessionSummary
	sel         int
	sessionsView viewport.Model
}

// NewSessionsModal creates a sessions modal, loading all sessions from the store.
func NewSessionsModal(s *store.Store) *SessionsModal {
	sessions, err := s.ListAllSessions()
	if err != nil {
		mlog.Error("ListAllSessions", slog.String("err", err.Error()))
	}
	sm := &SessionsModal{
		store:        s,
		sessionList:  sessions,
		sel:          0,
		sessionsView: viewport.New(40, 20),
	}
	sm.renderSessionList()
	return sm
}

func (sm *SessionsModal) WantsRawKeys() bool        { return false }
func (sm *SessionsModal) HandleRawKey(string) tea.Cmd { return nil }

// Resize updates viewport dimensions.
func (sm *SessionsModal) Resize(w, h int) {
	sm.sessionsView.Width = w - 4
	sm.sessionsView.Height = h - 3
}

// Update processes an action and returns a command for the parent.
func (sm *SessionsModal) Update(act action) tea.Cmd {
	switch act {
	case actEscape:
		return func() tea.Msg { return modalCloseMsg{} }
	case actDown:
		if sm.sel < len(sm.sessionList)-1 {
			sm.sel++
		}
		sm.renderSessionList()
	case actUp:
		if sm.sel > 0 {
			sm.sel--
		}
		sm.renderSessionList()
	case actConfirm:
		if len(sm.sessionList) == 0 {
			return nil
		}
		sel := sm.sessionList[sm.sel]
		s, err := sm.store.LoadSession(sel.ID)
		if err != nil || s == nil {
			return nil
		}
		projectID := sel.ProjectID
		return func() tea.Msg {
			return sessionSelectedMsg{Session: s, ProjectID: projectID}
		}
	}
	return nil
}

// View renders the sessions modal.
func (sm *SessionsModal) View(w, h int) string {
	contentH := h - 3
	contentW := w - 4

	sm.sessionsView.Width = contentW
	sm.sessionsView.Height = contentH

	box := bentoBox.BorderForeground(ColorAccent).Width(contentW).Height(contentH).
		Render(sm.sessionsView.View())
	box = injectPanelTitle(box, "sessions", true)

	header := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Padding(0, 1).Render("◇ sessions")

	help := " " + renderHelp(
		helpPair(modalKeys, actDown, actUp, "scroll"),
		helpKeyLabel(modalKeys, actConfirm, "load"),
		helpKeyLabel(modalKeys, actEscape, "close"),
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, box, help)
}

func (sm *SessionsModal) renderSessionList() {
	if len(sm.sessionList) == 0 {
		content := lipgloss.NewStyle().Foreground(ColorSubtle).Render("  No sessions yet. Start chatting to create one.")
		sm.sessionsView.SetContent(content)
		sm.sessionsView.GotoTop()
		return
	}

	var lines []string
	for i, s := range sm.sessionList {
		var line string
		formatted := fmt.Sprintf("%s  %s  (%d tasks)", s.StartedAt.Format("Jan 02 15:04"), s.Summary, s.Tasks)

		if i == sm.sel {
			line = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("▸ " + formatted)
		} else {
			line = lipgloss.NewStyle().Foreground(ColorText).Render("  " + formatted)
		}
		lines = append(lines, line)
	}

	sm.sessionsView.SetContent(strings.Join(lines, "\n"))
	sm.sessionsView.GotoTop()
}
