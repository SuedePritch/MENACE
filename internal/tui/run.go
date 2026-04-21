package tui

import (
	"fmt"

	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the MENACE TUI. Returns nil on clean exit.
func Run(cwd, menaceDir string, s *store.Store) error {
	m := initialModel(cwd, menaceDir, s)
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.programRef.p = p

	if _, err := p.Run(); err != nil {
		return err
	}

	if m.orchestrator != nil {
		m.orchestrator.Stop()
	}
	if m.chat.proc != nil {
		m.chat.proc.Stop()
	}
	fmt.Println("MENACE contained.")
	return nil
}
