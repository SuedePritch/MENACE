package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"menace/internal/store"

	"github.com/charmbracelet/lipgloss"
)

// Layout constants for dashboard geometry.
const (
	layoutChatPadH   = 6
	layoutBorderPadW = 1
	layoutPanelPadH  = 2
)

// chromeHeight returns the total vertical space used by banner + help bar.
// Adapts to the theme's banner size.
func (m model) chromeHeight() int {
	bannerH := len(m.bannerLines()) // banner text
	bannerH += 1                    // project name line
	bannerH += 1                    // status line (may be empty but reserved)
	bannerH += 1                    // help bar
	return bannerH
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m model) bannerLines() []string {
	return strings.Split(m.ts.theme.Personality.Banner, "\n")
}

func (m model) rightBoxStyle(panelIdx focusPanel) lipgloss.Style {
	if m.activePanel == panelIdx {
		return bentoBox.BorderForeground(ColorActive)
	}
	return bentoBox
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.screen == screenSetup {
		return m.setup.View(m.width, m.height, m.bannerLines(), themeRef{})
	}
	if m.activeModal != nil {
		return m.activeModal.View(m.width, m.height)
	}

	w := m.width
	h := m.height
	bodyH := h - m.chromeHeight()

	leftContentW := w/2 - layoutBorderPadW
	leftRenderedW := leftContentW + 2
	rightW := w - leftRenderedW
	leftContentH := bodyH - layoutPanelPadH

	leftBorder := ColorInactive
	if m.activePanel == panelArchitect {
		leftBorder = ColorActive
	}

	left := baseStyle.
		Width(leftContentW).Height(leftContentH).BorderForeground(leftBorder).
		Render(m.renderArchitectPanel(leftContentW, leftContentH))
	archTitle := m.ts.theme.Personality.PanelArchitect
	if m.architectModel != "" {
		archTitle += " · " + m.architectModel
	}
	left = injectPanelTitle(left, archTitle, m.activePanel == panelArchitect)

	right := m.renderRightColumn(rightW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, m.renderBanner(w), body, m.renderHelpBar(w))
}


// ── Banner ───────────────────────────────────────────────────────────────────

func (m model) renderBanner(w int) string {
	style := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	var styled []string
	for _, line := range m.bannerLines() {
		styled = append(styled, style.Render(line))
	}

	sessionTasks := m.sessionTaskList()
	running := 0
	tasksDone, tasksTotal := 0, len(sessionTasks)
	subsDone, subsTotal := 0, 0
	for _, t := range sessionTasks {
		if t.status == store.StatusRunning {
			running++
		}
		if t.status == store.StatusDone {
			tasksDone++
		}
		for _, s := range t.subtasks {
			subsTotal++
			if s.status == store.StatusDone {
				subsDone++
			}
		}
	}

	var parts []string
	if running > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorAccent).Render(
			fmt.Sprintf(m.ts.theme.Personality.Havoc, spinnerFrames[m.frame%len(spinnerFrames)], running)))
	}
	if tasksTotal > 0 {
		progress := lipgloss.NewStyle().Foreground(ColorSuccess)
		if subsTotal > 0 {
			parts = append(parts, progress.Render(
				fmt.Sprintf("✓ %d/%d tasks · %d/%d steps", tasksDone, tasksTotal, subsDone, subsTotal)))
		} else {
			parts = append(parts, progress.Render(
				fmt.Sprintf("✓ %d/%d tasks", tasksDone, tasksTotal)))
		}
	}
	if len(m.sessionProposalList()) > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorWarn).Render(
			fmt.Sprintf("◆ %d proposals", len(m.sessionProposalList()))))
	}
	if cost := m.estimateCost(); cost != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorDim).Render(cost))
	}
	allDone := tasksTotal > 0 && running == 0 && tasksDone == tasksTotal
	if allDone {
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render(
			m.ts.theme.Personality.Done))
	}

	// Center banner art as a block (preserving internal spacing)
	bannerBlock := lipgloss.PlaceHorizontal(w, lipgloss.Center, strings.Join(styled, "\n"))

	// Center status lines independently
	var statusLines []string
	projectLabel := filepath.Base(m.project.cwd)
	if len(m.project.list) > 1 {
		projectLabel = fmt.Sprintf("%s [%d/%d]", projectLabel, m.project.idx+1, len(m.project.list))
	}
	statusLines = append(statusLines, lipgloss.NewStyle().Foreground(ColorMuted).Render(projectLabel))

	if len(parts) > 0 {
		sep := lipgloss.NewStyle().Foreground(ColorSubtle).Render("  ·  ")
		statusLines = append(statusLines, strings.Join(parts, sep))
	}

	statusBlock := lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(strings.Join(statusLines, "\n"))
	return bannerBlock + "\n" + statusBlock
}

// ── Help Bar ─────────────────────────────────────────────────────────────────

func (m model) renderHelpBar(w int) string {
	var entries []helpEntry

	if m.insertMode {
		entries = append(entries,
			helpKey(insertKeys, actSend),
			helpKey(insertKeys, actClearInput),
			helpKey(insertKeys, actClearChat),
		)
		if m.chat.busy {
			entries = append(entries, helpKeyLabel(insertKeys, actEscape, "cancel"))
		} else {
			entries = append(entries, helpKeyLabel(insertKeys, actEscape, "normal"))
		}
		mode := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("INSERT")
		return " " + mode + " " + renderHelp(entries...)
	}

	entries = append(entries,
		helpKey(normalKeys, actInsert),
		helpKey(normalKeys, actNextPanel),
		helpPair(normalKeys, actDown, actUp, "nav"),
	)

	if m.activePanel == panelProposals || m.activePanel == panelQueue {
		entries = append(entries, helpKeyLabel(normalKeys, actConfirm, "open"))
	}

	switch m.activePanel {
	case panelArchitect:
		entries = append(entries, helpKeyLabel(normalKeys, actClearAll, "clear chat"))
		if m.chat.busy {
			entries = append(entries, helpKey(normalKeys, actCancel))
		}
	case panelProposals:
		if len(m.propPanel.proposals) > 0 {
			entries = append(entries, helpKeyLabel(normalKeys, actClearAll, "clear all"))
		}
	case panelQueue:
		entries = append(entries, helpKeyLabel(normalKeys, actClearAll, "clear done"))
	}

	entries = append(entries, helpKey(normalKeys, actSettings))
	entries = append(entries, helpKey(normalKeys, actTheme))
	entries = append(entries, helpKey(normalKeys, actProjectCycle))
	entries = append(entries, helpKey(normalKeys, actProjectAdd))
	entries = append(entries, helpKey(normalKeys, actSessions))
	entries = append(entries, helpKey(normalKeys, actNewSession))
	entries = append(entries, helpKeyLabel(normalKeys, actQuit, "quit"))
	mode := lipgloss.NewStyle().Foreground(ColorInfo).Bold(true).Render("NORMAL")
	return " " + mode + " " + renderHelp(entries...)
}

// ── Title Injection ──────────────────────────────────────────────────────────

func injectPanelTitle(rendered, title string, active bool) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	titleColor := ColorMuted
	if active {
		titleColor = ColorActive
	}
	styled := lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(" " + title + " ")

	top := lines[0]
	runes := []rune(stripAnsi(top))
	if len(runes) < 4 {
		return rendered
	}

	borderColor := ColorInactive
	if active {
		borderColor = ColorActive
	}
	bStyle := lipgloss.NewStyle().Foreground(borderColor)

	titleWidth := lipgloss.Width(styled)
	suffixStart := 2 + titleWidth
	if suffixStart >= len(runes)-1 {
		suffixStart = len(runes) - 1
	}

	lines[0] = bStyle.Render(string(runes[:2])) + styled + bStyle.Render(string(runes[suffixStart:]))
	return strings.Join(lines, "\n")
}

// ── Help Rendering ──────────────────────────────────────────────────────

// estimateCost returns a formatted cost string based on architect token usage,
// or empty string if no usage yet. Uses chars/4 estimate for workers.
func (m model) estimateCost() string {
	if m.chat.proc == nil {
		return ""
	}
	u := m.chat.proc.Usage()
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return ""
	}
	totalTokens := u.InputTokens + u.OutputTokens
	if totalTokens < 1000 {
		return fmt.Sprintf("~%dk tokens", totalTokens/1000+1)
	}
	return fmt.Sprintf("~%dk tokens", totalTokens/1000)
}

func renderHelp(entries ...helpEntry) string {
	keyStyle := lipgloss.NewStyle().Foreground(ColorActive)
	valStyle := lipgloss.NewStyle().Foreground(ColorInfo)
	sep := lipgloss.NewStyle().Foreground(ColorSubtle).Render(" │ ")

	var parts []string
	for _, e := range entries {
		parts = append(parts, keyStyle.Render(e.Key)+valStyle.Render(" "+e.Label))
	}
	return strings.Join(parts, sep)
}

