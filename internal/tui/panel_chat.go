package tui

import (
	"fmt"
	"strings"

	"menace/internal/config"
	"menace/internal/engine"
	"menace/internal/store"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages the chat panel sends to the parent ────────────────────────────

// chatExitInsertMsg asks the parent to leave insert mode.
type chatExitInsertMsg struct{}

// chatMarkDirtyMsg asks the parent to mark the session dirty.
type chatMarkDirtyMsg struct{}

// chatProposalsMsg delivers proposals parsed from the architect response.
type chatProposalsMsg struct {
	proposals []store.Proposal
}

// chatEnsureSessionMsg asks the parent to create a session if none exists.
type chatEnsureSessionMsg struct{}

// chatResultsSentMsg tells the parent that unseen results were steered to the architect.
type chatResultsSentMsg struct{}

// ── Read-only context the parent passes in ─────────────────────────────────

// chatContext carries the shared state the chat panel needs but must not own.
type chatContext struct {
	theme      config.Theme
	frame      int
	menaceDir  string
	cwd        string
	programRef *tea.Program
	store      *store.Store
	sessionID  string
	hasSession bool
	results    []store.TaskResult // unseen task results
	maxInputH  int
}

// ── Chat panel ─────────────────────────────────────────────────────────────

const maxHistoryMessages = 500

// chatPanel owns the architect chat: history, input, viewport, streaming state.
type chatPanel struct {
	history  []store.ChatMessage
	trimmed  bool // true if older messages were dropped from memory
	input    textarea.Model
	view     viewport.Model
	busy     bool
	proc     *engine.ArchProcess
	stream   string
	tools    []string
	spin     spinner.Model
}

func newChatPanel(cfg config.MenaceConfig) chatPanel {
	ti := textarea.New()
	ti.Placeholder = "Talk to the architect..."
	ti.CharLimit = cfg.ChatCharLimit
	ti.SetHeight(1)
	ti.MaxHeight = cfg.ChatMaxHeight
	ti.ShowLineNumbers = false
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Base = lipgloss.NewStyle().Foreground(ColorText)
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(ColorSubtle)
	ti.BlurredStyle.Base = lipgloss.NewStyle().Foreground(ColorDim)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(ColorSubtle)
	ti.Blur()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ColorAccent)

	return chatPanel{
		history: []store.ChatMessage{},
		input:   ti,
		view:    viewport.New(80, 20),
		spin:    sp,
	}
}

func (c *chatPanel) appendMessage(role, content string) {
	c.history = append(c.history, store.ChatMessage{Role: role, Content: content})
	if len(c.history) > maxHistoryMessages {
		// Drop the oldest 20% to avoid trimming on every append.
		drop := maxHistoryMessages / 5
		c.history = c.history[drop:]
		c.trimmed = true
	}
}

func (c *chatPanel) resetStream() {
	c.busy = false
	c.stream = ""
	c.tools = nil
}

func (c *chatPanel) updateViewport(theme config.Theme, frame int) {
	atBottom := c.view.AtBottom()
	c.view.SetContent(c.buildContent(c.view.Width, theme, frame))
	if atBottom {
		c.view.GotoBottom()
	}
}

// ── Insert mode ────────────────────────────────────────────────────────────

// HandleInsert processes insert-mode actions. Returns tea.Cmds that produce
// messages for the parent (chatExitInsertMsg, chatMarkDirtyMsg, etc.)
// instead of mutating the parent model directly.
func (c *chatPanel) HandleInsert(act action, ctx chatContext) tea.Cmd {
	switch act {
	case actEscape:
		if c.busy {
			if c.proc != nil {
				_ = c.proc.Abort()
			}
			c.resetStream()
			c.appendMessage("architect", ctx.theme.Personality.Cancelled)
			c.updateViewport(ctx.theme, ctx.frame)
		}
		c.input.Blur()
		return msgCmd(chatExitInsertMsg{})

	case actSend:
		val := strings.TrimSpace(c.input.Value())
		if val == "" {
			return nil
		}

		c.appendMessage("user", val)
		c.input.Reset()
		c.input.SetHeight(1)
		c.input.Blur()
		c.busy = true
		c.stream = ""
		c.tools = nil
		c.updateViewport(ctx.theme, ctx.frame)

		var cmds []tea.Cmd
		cmds = append(cmds, msgCmd(chatExitInsertMsg{}))

		if !ctx.hasSession {
			cmds = append(cmds, msgCmd(chatEnsureSessionMsg{}))
		}
		cmds = append(cmds, msgCmd(chatMarkDirtyMsg{}))

		if c.proc == nil || !c.proc.IsAlive() {
			auth, err := ctx.store.GetAuth()
			if err != nil {
				c.busy = false
				c.appendMessage("architect", "⚠ Auth error: "+err.Error())
				c.updateViewport(ctx.theme, ctx.frame)
				return tea.Batch(cmds...)
			}
			if auth == nil {
				c.busy = false
				c.appendMessage("architect", "⚠ No auth configured")
				c.updateViewport(ctx.theme, ctx.frame)
				return tea.Batch(cmds...)
			}
			proc, err := engine.StartArchProcess(ctx.menaceDir, ctx.cwd, ctx.programRef, auth.Provider, auth.ArchitectModel, auth.APIKey)
			if err != nil {
				c.busy = false
				c.appendMessage("architect", "⚠ "+err.Error())
				c.updateViewport(ctx.theme, ctx.frame)
				return tea.Batch(cmds...)
			}
			c.proc = proc
		}

		if len(ctx.results) > 0 {
			_ = c.proc.Steer(engine.FormatTaskResults(ctx.results))
			cmds = append(cmds, msgCmd(chatResultsSentMsg{}))
		}

		if err := c.proc.Prompt(val); err != nil {
			c.busy = false
			c.appendMessage("architect", "⚠ Send failed: "+err.Error())
			c.updateViewport(ctx.theme, ctx.frame)
		}
		return tea.Batch(cmds...)

	case actClearChat:
		c.history = nil
		c.updateViewport(ctx.theme, ctx.frame)
		return nil

	case actClearInput:
		c.input.Reset()
		c.input.SetHeight(1)
		return nil

	case actNewline:
		c.input.InsertString("\n")
		maxInputH := ctx.maxInputH
		lines := visualLineCount(c.input.Value(), c.input.Width())
		if lines < 1 {
			lines = 1
		}
		if lines > maxInputH {
			lines = maxInputH
		}
		c.input.SetHeight(lines)
		c.input.MaxHeight = maxInputH
		return nil

	case actPageUp:
		c.view.HalfViewUp()
		return nil
	case actPageDown:
		c.view.HalfViewDown()
		return nil
	}
	return nil
}

// ── Normal mode ────────────────────────────────────────────────────────────

// HandleNormal processes normal-mode architect panel actions.
func (c *chatPanel) HandleNormal(act action, theme config.Theme, frame int) {
	switch act {
	case actDown:
		c.view.LineDown(3)
	case actUp:
		c.view.LineUp(3)
	case actHalfDown:
		c.view.HalfViewDown()
	case actHalfUp:
		c.view.HalfViewUp()
	case actBottom:
		c.view.GotoBottom()
	case actTop:
		c.view.GotoTop()
	case actCancel:
		if c.busy {
			if c.proc != nil {
				_ = c.proc.Abort()
			}
			c.resetStream()
			c.appendMessage("architect", theme.Personality.Cancelled)
			c.updateViewport(theme, frame)
		}
	case actClearAll:
		c.history = nil
		c.updateViewport(theme, frame)
	}
}

// ── Streaming event handlers ───────────────────────────────────────────────

// HandleChunk processes a streaming text delta from the architect.
func (c *chatPanel) HandleChunk(delta string, theme config.Theme, frame int) {
	c.stream += delta
	c.updateViewport(theme, frame)
}

// HandleToolMsg processes a tool call notification.
func (c *chatPanel) HandleToolMsg(display string, theme config.Theme, frame int) {
	c.tools = append(c.tools, display)
	c.updateViewport(theme, frame)
}

// HandleDone processes the architect's completed response. Returns proposals
// for the parent to persist, rather than reaching into the parent model.
func (c *chatPanel) HandleDone(msg engine.ArchDoneMsg, theme config.Theme, frame int) tea.Cmd {
	c.resetStream()
	if msg.Err != nil {
		c.appendMessage("architect", "⚠ Error: "+msg.Err.Error())
		c.updateViewport(theme, frame)
		return msgCmd(chatMarkDirtyMsg{})
	}

	if msg.Response != "" {
		c.appendMessage("architect", msg.Response)
	}

	var proposals []store.Proposal
	for _, p := range msg.Proposals {
		pid := engine.GenID()
		var subtasks []store.ProposalSubtask
		for i, ps := range p.Subtasks {
			subtasks = append(subtasks, store.ProposalSubtask{
				ID:          engine.GenID(),
				Seq:         i + 1,
				Description: ps.Description,
				Instruction: ps.Instruction,
			})
		}
		proposals = append(proposals, store.Proposal{
			ID:          pid,
			Description: p.Description,
			Instruction: p.Instruction,
			Subtasks:    subtasks,
		})
	}

	if len(proposals) > 0 {
		c.appendMessage("architect", fmt.Sprintf("◆ %d proposal(s) ready for review", len(proposals)))
	}

	c.updateViewport(theme, frame)
	return tea.Batch(
		msgCmd(chatProposalsMsg{proposals: proposals}),
		msgCmd(chatMarkDirtyMsg{}),
	)
}

// HandleCrashed processes an architect process crash.
func (c *chatPanel) HandleCrashed(err error, theme config.Theme, frame int) {
	c.resetStream()
	if c.proc != nil {
		c.proc.SetDead()
	}
	c.appendMessage("architect", "⚠ Process crashed: "+err.Error()+"\nPress r to restart.")
	c.updateViewport(theme, frame)
}

// ── Rendering ──────────────────────────────────────────────────────────────

// Render renders the architect panel with chat viewport and input.
func (c *chatPanel) Render(w, h int, insertMode bool, theme config.Theme, frame int) string {
	sep := lipgloss.NewStyle().Foreground(ColorSubtle).Render(strings.Repeat("─", w))

	var input string
	inputH := 1
	if insertMode {
		c.input.SetWidth(w)
		visualLines := visualLineCount(c.input.Value(), c.input.Width())
		if visualLines < 1 {
			visualLines = 1
		}
		if visualLines > c.input.MaxHeight {
			visualLines = c.input.MaxHeight
		}
		c.input.SetHeight(visualLines)
		input = c.input.View()
		inputH = lipgloss.Height(input)
	} else {
		input = lipgloss.NewStyle().Foreground(ColorDim).Render(" " + theme.Personality.InputHint)
	}

	chatH := h - inputH - 1
	if chatH < 3 {
		chatH = 3
	}
	c.view.Width = w
	c.view.Height = chatH

	return lipgloss.JoinVertical(lipgloss.Left, c.view.View(), sep, input)
}

// buildContent builds the chat viewport content string.
func (c *chatPanel) buildContent(w int, theme config.Theme, frame int) string {
	var lines []string
	muted := lipgloss.NewStyle().Foreground(ColorMuted)
	maxBubble := w - 4
	if maxBubble < 20 {
		maxBubble = 20
	}

	if c.trimmed {
		lines = append(lines, muted.Render(" ── older messages trimmed ──"), "")
	}

	for _, msg := range c.history {
		if msg.Role == "user" {
			wrapped := wordWrap(msg.Content, maxBubble-2)
			for _, line := range wrapped {
				pad := w - lipgloss.Width(line) - 1
				if pad < 0 {
					pad = 0
				}
				lines = append(lines, strings.Repeat(" ", pad)+
					lipgloss.NewStyle().Foreground(ColorSuccess).Render(line))
			}
		} else {
			rendered := renderMarkdown(msg.Content, maxBubble)
			for _, line := range strings.Split(rendered, "\n") {
				lines = append(lines, " "+line)
			}
		}
		lines = append(lines, "")
	}

	if c.busy {
		if len(c.stream) > 0 {
			if len(c.tools) > 0 {
				lines = append(lines, "    "+muted.Render(fmt.Sprintf("⚙ %d tool calls", len(c.tools))))
			}
			lines = append(lines, "")
			rendered := renderMarkdown(engine.StripThinking(c.stream), maxBubble)
			for _, line := range strings.Split(rendered, "\n") {
				lines = append(lines, " "+line)
			}
		} else if len(c.tools) > 0 {
			lines = append(lines, formatToolGrid(c.tools, w, muted)...)
		} else {
			lines = append(lines, " "+c.spin.View()+" "+muted.Render(theme.Personality.Thinking))
		}
	}

	if len(lines) == 0 {
		empty := muted
		lines = append(lines, "")
		for _, line := range theme.Personality.Empty {
			if line == "" {
				lines = append(lines, "")
			} else {
				lines = append(lines, empty.Render(" "+line))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// ── Lifecycle ──────────────────────────────────────────────────────────────

// Resize updates the chat panel dimensions.
func (c *chatPanel) Resize(viewW, viewH, inputW, maxInputH int) {
	c.view.Width = viewW
	c.view.Height = viewH
	c.input.SetWidth(inputW)
	c.input.MaxHeight = maxInputH
	lines := visualLineCount(c.input.Value(), c.input.Width())
	if lines < 1 {
		lines = 1
	}
	if lines > maxInputH {
		lines = maxInputH
	}
	c.input.SetHeight(lines)
}

// Stop gracefully stops the architect process.
func (c *chatPanel) Stop() {
	if c.proc != nil {
		c.proc.Stop()
		c.proc = nil
	}
}

// TickSpinner updates the spinner and returns a cmd for the next tick.
func (c *chatPanel) TickSpinner(msg spinner.TickMsg) tea.Cmd {
	var cmd tea.Cmd
	c.spin, cmd = c.spin.Update(msg)
	return cmd
}

// ── Helpers ────────────────────────────────────────────────────────────────

// msgCmd wraps a message in a tea.Cmd for returning from panel methods.
func msgCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
