package tui

import (
	"fmt"
	"strings"

	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages the queue panel sends to the parent ───────────────────────────

// queueNavUpMsg signals overflow navigation to the previous panel.
type queueNavUpMsg struct{}

// queueClearDoneMsg asks the parent to clear finished tasks from the store.
type queueClearDoneMsg struct{}

// queueOpenMsg asks the parent to open the review modal for the selected task.
type queueOpenMsg struct{ Index int }

// ── Queue panel ────────────────────────────────────────────────────────────

// queuePanel owns the task list, selection, and rendering.
type queuePanel struct {
	tasks []task
	sel   int
}

// HandleNormal processes normal-mode actions. Returns a tea.Cmd for the parent.
func (qp *queuePanel) HandleNormal(act action) tea.Cmd {
	switch act {
	case actUp:
		if qp.sel > 0 {
			qp.sel--
		} else {
			return msgCmd(queueNavUpMsg{})
		}
	case actDown:
		if qp.sel < len(qp.tasks)-1 {
			qp.sel++
		}
	case actConfirm:
		if qp.sel >= 0 && qp.sel < len(qp.tasks) {
			return msgCmd(queueOpenMsg{Index: qp.sel})
		}
	case actClearAll:
		qp.sel = 0
		return msgCmd(queueClearDoneMsg{})
	}
	return nil
}

// ClampSel ensures the selection index is in bounds.
func (qp *queuePanel) ClampSel() {
	if len(qp.tasks) == 0 {
		qp.sel = 0
		return
	}
	if qp.sel >= len(qp.tasks) {
		qp.sel = len(qp.tasks) - 1
	}
}

// ── Rendering ──────────────────────────────────────────────────────────────

// Render renders the task queue.
func (qp *queuePanel) Render(w, h int, active bool, frame int) string {
	if len(qp.tasks) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("No tasks yet.")
	}

	var lines []string
	for i, t := range qp.tasks {
		sel := i == qp.sel && active
		arrow := "  "
		if sel {
			arrow = lipgloss.NewStyle().Foreground(ColorActive).Render("▸ ")
		}

		var icon string
		switch t.status {
		case store.StatusDone:
			icon = lipgloss.NewStyle().Foreground(ColorSuccess).Render("✓ ")
		case store.StatusFailed:
			icon = lipgloss.NewStyle().Foreground(ColorFail).Render("✗ ")
		case store.StatusRunning:
			icon = lipgloss.NewStyle().Foreground(ColorAccent).Render(spinnerFrames[frame%len(spinnerFrames)] + " ")
		case store.StatusCancelled:
			icon = lipgloss.NewStyle().Foreground(ColorMuted).Render("⊘ ")
		case store.StatusStalled:
			icon = lipgloss.NewStyle().Foreground(ColorWarn).Render("⏸ ")
		default:
			icon = lipgloss.NewStyle().Foreground(ColorMuted).Render("○ ")
		}

		desc := truncate(t.description, w-16)
		descStyle := lipgloss.NewStyle().Foreground(ColorText)
		if sel {
			descStyle = descStyle.Bold(true)
		} else if t.status == store.StatusRunning {
			descStyle = descStyle.Foreground(ColorAccent)
		} else if t.status == store.StatusDone {
			descStyle = descStyle.Foreground(ColorDim)
		} else if t.status == store.StatusFailed {
			descStyle = descStyle.Foreground(ColorFail)
		} else if t.status == store.StatusCancelled {
			descStyle = descStyle.Foreground(ColorDim)
		}

		var meta string
		switch t.status {
		case store.StatusRunning:
			meta = lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf(" %ds", t.elapsed))
		case store.StatusFailed:
			meta = lipgloss.NewStyle().Foreground(ColorFail).Bold(true).Render(" failed")
		case store.StatusDone:
			meta = lipgloss.NewStyle().Foreground(ColorSuccess).Render(" done")
		case store.StatusCancelled:
			meta = lipgloss.NewStyle().Foreground(ColorMuted).Render(" cancelled")
		}

		lines = append(lines, arrow+icon+descStyle.Render(desc)+meta)

		if sel && len(t.subtasks) > 0 {
			for _, s := range t.subtasks {
				si := lipgloss.NewStyle().Foreground(ColorSubtle).Render("  ○ ")
				if s.status == store.StatusDone {
					si = lipgloss.NewStyle().Foreground(ColorSuccess).Render("  ✓ ")
				} else if s.status == store.StatusRunning {
					si = lipgloss.NewStyle().Foreground(ColorAccent).Render("  " + spinnerFrames[frame%len(spinnerFrames)] + " ")
				} else if s.status == store.StatusFailed {
					si = lipgloss.NewStyle().Foreground(ColorFail).Render("  ✗ ")
				}
				st := truncate(s.description, w-10)
				stStyle := lipgloss.NewStyle().Foreground(ColorMuted)
				if s.status == store.StatusDone {
					stStyle = stStyle.Foreground(ColorDim)
				}
				lines = append(lines, "    "+si+stStyle.Render(st))
			}
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// RenderBox renders the queue panel with its bento box border and title.
func (qp *queuePanel) RenderBox(w, h int, active bool, title string, boxStyle lipgloss.Style, frame int) string {
	contentW := w - 2
	box := boxStyle.
		Width(contentW).Height(h).Padding(0, 1).
		Render(qp.Render(contentW-2, h, active, frame))
	return injectPanelTitle(box, title, active)
}
