package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"menace/internal/engine"
	"menace/internal/store"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ReviewModal encapsulates the task review modal state.
type ReviewModal struct {
	store        *store.Store
	orchestrator *engine.Orchestrator
	projectCwd   string

	tasks    []task
	taskID   string
	focus    modalFocus
	showLogs bool
	scope    int
	files    []diffFile
	fileSel  int
	raw      string
	logView  viewport.Model
	diffView viewport.Model
}

// NewReviewModal creates a review modal for the given task.
func NewReviewModal(s *store.Store, orch *engine.Orchestrator, cwd string, tasks []task, taskIdx int) *ReviewModal {
	t := tasks[taskIdx]
	rm := &ReviewModal{
		store:        s,
		orchestrator: orch,
		projectCwd:   cwd,
		tasks:        tasks,
		taskID:       t.id,
		focus:        modalFocusFiles,
		showLogs:     false,
		scope:        diffScopeAll,
		logView:      viewport.New(40, 20),
		diffView:     viewport.New(40, 20),
	}

	logContent := s.GetTaskLog(t.id)
	if logContent == "" {
		rm.logView.SetContent("No logs yet.")
	} else {
		rm.logView.SetContent(logContent)
	}
	rm.logView.GotoTop()
	rm.loadDiffForScope()
	return rm
}

func (rm *ReviewModal) WantsRawKeys() bool        { return false }
func (rm *ReviewModal) HandleRawKey(string) tea.Cmd { return nil }

// Resize updates viewport dimensions.
func (rm *ReviewModal) Resize(w, h int) {
	modalW := w/2 - 3
	modalH := h - 5
	rm.logView.Width = modalW
	rm.logView.Height = modalH
	rm.diffView.Width = modalW
	rm.diffView.Height = modalH
}

// Update processes an action and returns a command for the parent.
func (rm *ReviewModal) Update(act action) tea.Cmd {
	switch act {
	case actEscape:
		return func() tea.Msg { return modalCloseMsg{} }

	case actSwitchPane:
		if rm.showLogs {
			if rm.focus == modalFocusLogs {
				rm.focus = modalFocusDiff
			} else {
				rm.focus = modalFocusLogs
			}
		} else {
			if rm.focus == modalFocusFiles {
				rm.focus = modalFocusDiff
			} else {
				rm.focus = modalFocusFiles
			}
		}

	case actCycleScope:
		rm.scope = (rm.scope + 1) % 3
		rm.loadDiffForScope()

	case actToggleLogs:
		rm.showLogs = !rm.showLogs
		if rm.showLogs {
			rm.focus = modalFocusLogs
		} else {
			rm.focus = modalFocusFiles
		}

	case actDown:
		switch rm.focus {
		case modalFocusFiles:
			if rm.fileSel < len(rm.files)-1 {
				rm.fileSel++
				rm.updateDiffViewForFile()
			}
		case modalFocusLogs:
			rm.logView.LineDown(1)
		case modalFocusDiff:
			rm.diffView.LineDown(1)
		}
	case actUp:
		switch rm.focus {
		case modalFocusFiles:
			if rm.fileSel > 0 {
				rm.fileSel--
				rm.updateDiffViewForFile()
			}
		case modalFocusLogs:
			rm.logView.LineUp(1)
		case modalFocusDiff:
			rm.diffView.LineUp(1)
		}
	case actHalfDown:
		if rm.focus == modalFocusDiff {
			rm.diffView.HalfViewDown()
		} else if rm.focus == modalFocusLogs {
			rm.logView.HalfViewDown()
		}
	case actHalfUp:
		if rm.focus == modalFocusDiff {
			rm.diffView.HalfViewUp()
		} else if rm.focus == modalFocusLogs {
			rm.logView.HalfViewUp()
		}
	case actTop:
		if rm.focus == modalFocusDiff {
			rm.diffView.GotoTop()
		} else if rm.focus == modalFocusLogs {
			rm.logView.GotoTop()
		}
	case actBottom:
		if rm.focus == modalFocusDiff {
			rm.diffView.GotoBottom()
		} else if rm.focus == modalFocusLogs {
			rm.logView.GotoBottom()
		}
	}

	t := rm.findTask()
	if t == nil {
		return nil
	}

	switch act {
	case actCancel:
		if t.status == store.StatusRunning || t.status == store.StatusPending || t.status == store.StatusQueued {
			return func() tea.Msg { return reviewCancelTaskMsg{TaskID: t.id} }
		}
	case actDelete:
		if t.status == store.StatusDone || t.status == store.StatusFailed || t.status == store.StatusCancelled {
			return func() tea.Msg { return reviewDeleteTaskMsg{TaskID: t.id} }
		}
	case actRetry:
		if t.status == store.StatusFailed || t.status == store.StatusCancelled {
			return func() tea.Msg { return reviewRetryTaskMsg{TaskID: t.id} }
		}
	}

	return nil
}

// RefreshTasks updates the task snapshot and reloads logs if needed.
func (rm *ReviewModal) RefreshTasks(tasks []task) {
	rm.tasks = tasks
	t := rm.findTask()
	if t == nil {
		return
	}
	logContent := rm.store.GetTaskLog(t.id)
	if logContent == "" {
		rm.logView.SetContent("No logs yet.")
	} else {
		rm.logView.SetContent(logContent)
	}
}

func (rm *ReviewModal) findTask() *task {
	for i := range rm.tasks {
		if rm.tasks[i].id == rm.taskID {
			return &rm.tasks[i]
		}
	}
	return nil
}

func (rm *ReviewModal) loadDiffForScope() {
	t := rm.findTask()
	if t == nil {
		return
	}

	var raw string
	switch rm.scope {
	case diffScopeSubtask:
		if len(t.subtasks) > 0 {
			for i := len(t.subtasks) - 1; i >= 0; i-- {
				d := rm.store.GetSubtaskDiff(t.subtasks[i].id)
				if d != "" {
					raw = d
					break
				}
			}
		}
		if raw == "" {
			raw = rm.store.GetTaskDiff(t.id)
		}
	case diffScopeTask:
		raw = rm.store.GetTaskDiff(t.id)
	case diffScopeAll:
		cmd := exec.Command("git", "diff", "HEAD")
		cmd.Dir = rm.projectCwd
		out, err := cmd.Output()
		if err != nil || len(out) == 0 {
			cmd2 := exec.Command("git", "diff", "--cached")
			cmd2.Dir = rm.projectCwd
			out, _ = cmd2.Output()
		}
		raw = string(out)
	}

	rm.raw = raw
	rm.files = parseDiffFiles(raw)
	rm.fileSel = 0
	rm.updateDiffViewForFile()
}

func (rm *ReviewModal) updateDiffViewForFile() {
	if len(rm.files) == 0 {
		rm.diffView.SetContent(lipgloss.NewStyle().Foreground(ColorSubtle).Render("No changes."))
		rm.diffView.GotoTop()
		return
	}
	if rm.fileSel >= len(rm.files) {
		rm.fileSel = len(rm.files) - 1
	}
	f := rm.files[rm.fileSel]
	rm.diffView.SetContent(colorizeDiff(f.hunks))
	rm.diffView.GotoTop()
}

// View renders the review modal.
func (rm *ReviewModal) View(w, h int) string {
	title := "review"
	if t := rm.findTask(); t != nil {
		title = truncate(t.description, w-20)
	}

	bodyH := h - 3
	leftW := w / 4
	if leftW < 25 {
		leftW = 25
	}
	rightW := w - leftW - 1
	contentH := bodyH - 2

	var leftContent string
	var leftTitle string

	if rm.showLogs {
		leftTitle = "logs"
		rm.logView.Width = leftW - 2
		rm.logView.Height = contentH
		leftContent = rm.logView.View()
	} else {
		leftTitle = "files"
		leftContent = rm.renderFileList(leftW-4, contentH)
	}

	leftBorder := ColorInactive
	if rm.focus == modalFocusFiles || rm.focus == modalFocusLogs {
		leftBorder = ColorActive
	}
	leftBox := bentoBox.BorderForeground(leftBorder).Width(leftW).Height(contentH).
		Render(leftContent)
	leftBox = injectPanelTitle(leftBox, leftTitle, leftBorder == ColorActive)

	rm.diffView.Width = rightW - 2
	rm.diffView.Height = contentH

	diffBorder := ColorInactive
	if rm.focus == modalFocusDiff {
		diffBorder = ColorActive
	}
	diffBox := bentoBox.BorderForeground(diffBorder).Width(rightW).Height(contentH).
		Render(rm.diffView.View())

	scopeLabel := "all"
	if rm.scope < len(diffScopeNames) {
		scopeLabel = diffScopeNames[rm.scope]
	}
	diffBox = injectPanelTitle(diffBox, "diff ("+scopeLabel+")", rm.focus == modalFocusDiff)

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, diffBox)
	header := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Padding(0, 1).Render("◈ " + title)

	helpEntries := []helpEntry{
		helpPair(modalKeys, actDown, actUp, "scroll"),
		helpKey(modalKeys, actSwitchPane),
		helpKey(modalKeys, actCycleScope),
		helpKey(modalKeys, actToggleLogs),
	}

	if t := rm.findTask(); t != nil {
		switch t.status {
		case store.StatusRunning, store.StatusPending, store.StatusQueued:
			helpEntries = append(helpEntries, helpKey(modalKeys, actCancel))
		case store.StatusFailed, store.StatusCancelled:
			helpEntries = append(helpEntries, helpKey(modalKeys, actRetry), helpKey(modalKeys, actDelete))
		case store.StatusDone:
			helpEntries = append(helpEntries, helpKey(modalKeys, actDelete))
		}
	}

	helpEntries = append(helpEntries, helpKeyLabel(modalKeys, actEscape, "close"))
	help := " " + renderHelp(helpEntries...)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, help)
}

func (rm *ReviewModal) renderFileList(w, h int) string {
	if len(rm.files) == 0 {
		return lipgloss.NewStyle().Foreground(ColorSubtle).Render("  No changes.")
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(ColorFail)
	selStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	normalStyle := lipgloss.NewStyle().Foreground(ColorText)

	var lines []string
	for i, f := range rm.files {
		stats := addStyle.Render(fmt.Sprintf("+%d", f.added)) + " " +
			delStyle.Render(fmt.Sprintf("-%d", f.removed))

		name := f.name
		if len(name) > w-12 {
			name = "…" + name[len(name)-(w-13):]
		}

		if i == rm.fileSel {
			lines = append(lines, selStyle.Render("▸ "+name)+" "+stats)
		} else {
			lines = append(lines, normalStyle.Render("  "+name)+" "+stats)
		}
	}
	return strings.Join(lines, "\n")
}

// ── Diff Helpers ────────────────────────────────────────────────────────────

func colorizeDiff(diff string) string {
	var lines []string
	for _, line := range strings.Split(diff, "\n") {
		lines = append(lines, colorizeDiffLine(line))
	}
	return strings.Join(lines, "\n")
}

func colorizeDiffLine(line string) string {
	if len(line) == 0 {
		return ""
	}
	if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index ") {
		return lipgloss.NewStyle().Foreground(ColorDim).Render(line)
	}
	if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
		return lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render(line)
	}
	if strings.HasPrefix(line, "@@") {
		return lipgloss.NewStyle().Foreground(ColorInfo).Render(line)
	}
	switch line[0] {
	case '+':
		return lipgloss.NewStyle().Foreground(ColorSuccess).Render(line)
	case '-':
		return lipgloss.NewStyle().Foreground(ColorFail).Render(line)
	default:
		return lipgloss.NewStyle().Foreground(ColorText).Render(line)
	}
}

func parseDiffFiles(raw string) []diffFile {
	if raw == "" {
		return nil
	}

	var files []diffFile
	lines := strings.Split(raw, "\n")
	var current *diffFile

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			if current != nil {
				files = append(files, *current)
			}
			name := parseDiffFileName(line)
			current = &diffFile{name: name}
			current.hunks = line + "\n"
			continue
		}
		if current != nil {
			current.hunks += line + "\n"
			if len(line) > 0 {
				switch line[0] {
				case '+':
					if !strings.HasPrefix(line, "+++") {
						current.added++
					}
				case '-':
					if !strings.HasPrefix(line, "---") {
						current.removed++
					}
				}
			}
		}
	}
	if current != nil {
		files = append(files, *current)
	}
	return files
}

func parseDiffFileName(line string) string {
	parts := strings.Fields(line)
	if len(parts) >= 4 {
		name := parts[len(parts)-1]
		if strings.HasPrefix(name, "b/") {
			return name[2:]
		}
		return name
	}
	return "unknown"
}
