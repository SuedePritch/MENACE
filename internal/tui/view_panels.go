package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	lgtable "github.com/charmbracelet/lipgloss/table"
)

// ── Architect Panel ────────────────────────────────────────────────────────────

func (m model) renderArchitectPanel(w, h int) string {
	return m.chat.Render(w, h, m.insertMode, m.ts.theme, m.frame)
}

func renderTable(lines []string, w int) string {
	if len(lines) == 0 {
		return ""
	}

	var allRows [][]string
	sepRowIdx := -1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, "|")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
			parts = parts[1:]
		}
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
			parts = parts[:len(parts)-1]
		}
		cells := make([]string, len(parts))
		for i, part := range parts {
			cells[i] = strings.TrimSpace(part)
		}
		if len(cells) == 0 {
			continue
		}
		rowIdx := len(allRows)
		allRows = append(allRows, cells)
		if sepRowIdx == -1 {
			isSep := true
			for _, cell := range cells {
				stripped := strings.ReplaceAll(strings.ReplaceAll(cell, "-", ""), ":", "")
				stripped = strings.TrimSpace(stripped)
				if stripped != "" {
					isSep = false
					break
				}
			}
			if isSep {
				sepRowIdx = rowIdx
			}
		}
	}

	if len(allRows) < 2 {
		return ""
	}

	header := allRows[0]
	startData := 1
	if sepRowIdx > 0 {
		startData = sepRowIdx + 1
	}

	t := lgtable.New().
		Headers(header...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorDim)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == lgtable.HeaderRow {
				return lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Padding(0, 1)
			}
			return lipgloss.NewStyle().Foreground(ColorText).Padding(0, 1)
		})

	for i := startData; i < len(allRows); i++ {
		t = t.Row(allRows[i]...)
	}

	return t.Render()
}

func renderMarkdown(text string, w int) string {
	bold := lipgloss.NewStyle().Bold(true).Foreground(ColorText)
	code := lipgloss.NewStyle().Foreground(ColorWarn)
	header := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	dim := lipgloss.NewStyle().Foreground(ColorDim)

	lines := strings.Split(text, "\n")
	var out []string
	var tableBuf []string
	inCodeBlock := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if len(tableBuf) > 0 {
				tableOut := renderTable(tableBuf, w)
				out = append(out, strings.Split(tableOut, "\n")...)
				tableBuf = nil
			}
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				out = append(out, dim.Render("─── code ───"))
			} else {
				out = append(out, dim.Render("────────────"))
			}
			continue
		}
		if inCodeBlock {
			out = append(out, code.Render(line))
			continue
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") {
			tableBuf = append(tableBuf, line)
			continue
		}

		if len(tableBuf) > 0 {
			tableOut := renderTable(tableBuf, w)
			out = append(out, strings.Split(tableOut, "\n")...)
			tableBuf = nil
		}

		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, header.Render(trimmed[4:]))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, header.Render(trimmed[3:]))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, header.Render(trimmed[2:]))
			continue
		}

		rendered := renderInlineMarkdown(line, bold, code)
		out = append(out, wordWrap(rendered, w)...)
	}

	if len(tableBuf) > 0 {
		tableOut := renderTable(tableBuf, w)
		out = append(out, strings.Split(tableOut, "\n")...)
	}

	return strings.Join(out, "\n")
}

func renderInlineMarkdown(line string, bold, code lipgloss.Style) string {
	var result strings.Builder
	i := 0
	for i < len(line) {
		if i+1 < len(line) && line[i] == '*' && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "**")
			if end >= 0 {
				result.WriteString(bold.Render(line[i+2 : i+2+end]))
				i = i + 2 + end + 2
				continue
			}
		}
		if line[i] == '`' {
			end := strings.Index(line[i+1:], "`")
			if end >= 0 {
				result.WriteString(code.Render(line[i+1 : i+1+end]))
				i = i + 1 + end + 1
				continue
			}
		}
		result.WriteByte(line[i])
		i++
	}
	return result.String()
}


func formatToolGrid(tools []string, w int, style lipgloss.Style) []string {
	maxLen := 0
	for _, t := range tools {
		if len(t) > maxLen {
			maxLen = len(t)
		}
	}
	cellW := maxLen + 3
	if cellW < 12 {
		cellW = 12
	}

	usable := w - 4
	cols := usable / cellW
	if cols < 1 {
		cols = 1
	}
	if cols > 5 {
		cols = 5
	}

	var lines []string
	for i := 0; i < len(tools); i += cols {
		var row strings.Builder
		row.WriteString("    ")
		for j := 0; j < cols && i+j < len(tools); j++ {
			cell := "· " + tools[i+j]
			for len(cell) < cellW {
				cell += " "
			}
			row.WriteString(cell)
		}
		lines = append(lines, style.Render(row.String()))
	}
	return lines
}

// ── Right Column ─────────────────────────────────────────────────────────────

func (m model) renderRightColumn(w, h int) string {
	props := m.sessionProposalList()
	propRows := max(len(props), 1) + 1
	propContentH := min(propRows, h/3-2)
	if propContentH < 2 {
		propContentH = 2
	}
	propRenderedH := propContentH + 2
	taskContentH := h - propRenderedH - 2
	if taskContentH < 3 {
		taskContentH = 3
	}

	// Build a temporary proposalPanel with session-filtered proposals for rendering
	pp := proposalPanel{proposals: props, sel: m.propPanel.sel}
	propBox := pp.RenderBox(w, propContentH, m.activePanel == panelProposals,
		m.ts.theme.Personality.PanelProposals, m.rightBoxStyle(panelProposals))

	tasks := m.sessionTaskList()
	qp := queuePanel{tasks: tasks, sel: m.queuePanel.sel}
	taskTitle := m.ts.theme.Personality.PanelTasks
	if m.workerModel != "" {
		taskTitle += " · " + m.workerModel
	}
	queueBox := qp.RenderBox(w, taskContentH, m.activePanel == panelQueue,
		taskTitle, m.rightBoxStyle(panelQueue), m.frame)

	return lipgloss.JoinVertical(lipgloss.Left, propBox, queueBox)
}
