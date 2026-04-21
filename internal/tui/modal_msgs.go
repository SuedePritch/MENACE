package tui

import (
	"menace/internal/config"
	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// Modal is the interface all modal types implement.
// The parent model holds at most one active Modal at a time.
type Modal interface {
	// Update handles a resolved key action and returns an optional command.
	Update(act action) tea.Cmd
	// View renders the modal at the given dimensions.
	View(w, h int) string
	// Resize updates internal viewport dimensions.
	Resize(w, h int)
	// WantsRawKeys returns true when the modal needs unresolved key input
	// (e.g. numeric editing in settings).
	WantsRawKeys() bool
	// HandleRawKey processes a raw key string. Only called when WantsRawKeys() is true.
	HandleRawKey(key string) tea.Cmd
}

// modalCloseMsg tells the parent to close the active modal.
type modalCloseMsg struct{}

// ── Review modal messages ───────────────────────────────────────────────────

type reviewCancelTaskMsg struct{ TaskID string }
type reviewDeleteTaskMsg struct{ TaskID string }
type reviewRetryTaskMsg struct{ TaskID string }

// ── Proposal modal messages ─────────────────────────────────────────────────

type proposalApprovedMsg struct {
	Index    int
	Proposal store.Proposal
}
type proposalRejectedMsg struct {
	Index      int
	ProposalID string
}

// ── Sessions modal messages ─────────────────────────────────────────────────

type sessionSelectedMsg struct {
	Session   *store.Session
	ProjectID string
}

// ── Settings modal messages ─────────────────────────────────────────────────

type settingsCfgChangedMsg struct{ Cfg config.MenaceConfig }
type settingsThemeChangedMsg struct {
	ThemeName string
	Theme     config.Theme
}
type settingsModelChangedMsg struct{}
type settingsLogoutMsg struct{}
type settingsCustomizeMsg struct{}

// customizeEditDoneMsg is sent when the external editor closes after theme customization.
type customizeEditDoneMsg struct{ err error }
