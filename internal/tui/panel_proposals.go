package tui

import (
	"fmt"
	"strings"

	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages the proposal panel sends to the parent ────────────────────────

// propNavDownMsg signals overflow navigation to the next panel.
type propNavDownMsg struct{}

// propClearAllMsg asks the parent to clear all proposals from the store.
type propClearAllMsg struct{}

// propOpenMsg asks the parent to open the proposal modal for the selected item.
type propOpenMsg struct{ Index int }

// ── Proposal panel ─────────────────────────────────────────────────────────

// proposalPanel owns the proposal list, selection, and rendering.
type proposalPanel struct {
	proposals []store.Proposal
	sel       int
}

// HandleNormal processes normal-mode actions. Returns a tea.Cmd for the parent.
func (pp *proposalPanel) HandleNormal(act action) tea.Cmd {
	switch act {
	case actDown:
		if len(pp.proposals) == 0 || pp.sel >= len(pp.proposals)-1 {
			return msgCmd(propNavDownMsg{})
		}
		pp.sel++
	case actUp:
		if pp.sel > 0 {
			pp.sel--
		}
	case actConfirm:
		if pp.sel >= 0 && pp.sel < len(pp.proposals) {
			return msgCmd(propOpenMsg{Index: pp.sel})
		}
	case actClearAll:
		pp.proposals = nil
		pp.sel = 0
		return msgCmd(propClearAllMsg{})
	}
	return nil
}

// ClampSel ensures the selection index is in bounds.
func (pp *proposalPanel) ClampSel() {
	if len(pp.proposals) == 0 {
		pp.sel = 0
		return
	}
	if pp.sel >= len(pp.proposals) {
		pp.sel = len(pp.proposals) - 1
	}
}

// ── Rendering ──────────────────────────────────────────────────────────────

// Render renders the proposal list.
func (pp *proposalPanel) Render(w, h int, active bool) string {
	if len(pp.proposals) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("No proposals yet.")
	}

	var lines []string
	for i, p := range pp.proposals {
		sel := i == pp.sel && active
		arrow := "  "
		if sel {
			arrow = lipgloss.NewStyle().Foreground(ColorActive).Render("▸ ")
		}
		diamond := lipgloss.NewStyle().Foreground(ColorAccent).Render("◆ ")
		desc := truncate(p.Description, w-5)
		if sel {
			desc = lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render(desc)
		} else {
			desc = lipgloss.NewStyle().Foreground(ColorMuted).Render(desc)
		}
		lines = append(lines, arrow+diamond+desc)
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// RenderBox renders the proposal panel with its bento box border and title.
func (pp *proposalPanel) RenderBox(w, h int, active bool, title string, boxStyle lipgloss.Style) string {
	contentW := w - 2
	displayTitle := title
	if len(pp.proposals) > 0 {
		displayTitle = fmt.Sprintf("%s (%d)", title, len(pp.proposals))
	}
	box := boxStyle.
		Width(contentW).Height(h).Padding(0, 1).
		Render(pp.Render(contentW-2, h, active))
	return injectPanelTitle(box, displayTitle, active)
}
