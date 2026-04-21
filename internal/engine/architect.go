package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"menace/internal/agent"
	mlog "menace/internal/log"
	"menace/internal/store"
	"gopkg.in/yaml.v3"
)

// ── Bubble Tea messages ─────────────────────────────────────────────────────

type ArchChunkMsg struct{ Delta string }
type ArchToolMsg struct{ Display string }
type ArchDoneMsg struct {
	Response  string
	Proposals []ParsedProposal
	Err       error
}
type ArchCrashedMsg struct{ Err error }

// ── Proposal types ──────────────────────────────────────────────────────────

type ParsedProposal struct {
	Description string          `json:"description" yaml:"description"`
	Instruction string          `json:"instruction" yaml:"instruction"`
	Subtasks    []ParsedSubtask `json:"subtasks" yaml:"subtasks"`
}

type ParsedSubtask struct {
	Description string `json:"description" yaml:"description"`
	Instruction string `json:"instruction" yaml:"instruction"`
}

func (ps *ParsedSubtask) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		ps.Description = value.Value
		return nil
	}
	type plain ParsedSubtask
	return value.Decode((*plain)(ps))
}

// ── Persistent architect process ────────────────────────────────────────────

type ArchProcess struct {
	ag       *agent.Agent
	mu       sync.Mutex
	alive    bool
	prog     *tea.Program
	cancel   context.CancelFunc
	ctx      context.Context
	gen      int64      // incremented on Abort; used to discard stale results
	msgCh    chan string // serializes Prompt/Steer to avoid concurrent agent calls
	stopOnce sync.Once  // ensures msgCh is closed exactly once
}

func StartArchProcess(menaceDir, cwd string, prog *tea.Program, providerName, modelName, apiKey string) (*ArchProcess, error) {
	systemPrompt := LoadSystemPrompt(menaceDir, "architect")

	if apiKey == "" {
		return nil, fmt.Errorf("no API key for provider %q", providerName)
	}

	ag, err := agent.NewAgent(providerName, modelName, apiKey, systemPrompt, agent.ReadTools(cwd), MaxArchitectIterations)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	ap := &ArchProcess{
		ag:     ag,
		alive:  true,
		prog:   prog,
		cancel: cancel,
		ctx:    ctx,
		msgCh:  make(chan string, 8),
	}

	// Single goroutine drains the message queue, ensuring only one runPrompt
	// executes at a time. This prevents concurrent access to the underlying
	// go-llms agent which is not goroutine-safe.
	go ap.processLoop()

	mlog.Info("architect process started", slog.String("provider", providerName), slog.String("model", modelName))
	return ap, nil
}

// Usage returns the cumulative token usage from the architect agent.
func (ap *ArchProcess) Usage() agent.Usage {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.ag == nil {
		return agent.Usage{}
	}
	return ap.ag.Usage()
}

func (ap *ArchProcess) IsAlive() bool {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.alive
}

func (ap *ArchProcess) SetDead() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.alive = false
}

func (ap *ArchProcess) Prompt(msg string) error {
	return ap.send(msg)
}

func (ap *ArchProcess) Steer(msg string) error {
	return ap.send("[System] " + msg)
}

// send enqueues a message for the process loop. The alive check and channel
// send happen under the same lock to prevent a TOCTOU race where Stop() could
// close msgCh between the check and the send.
func (ap *ArchProcess) send(msg string) error {
	ap.mu.Lock()
	if !ap.alive {
		ap.mu.Unlock()
		return fmt.Errorf("process not alive")
	}
	// Non-blocking send under lock — channel is buffered (cap 8).
	// If the buffer is full, drop the lock and use select with ctx.
	select {
	case ap.msgCh <- msg:
		ap.mu.Unlock()
		return nil
	default:
		ap.mu.Unlock()
	}
	// Buffer was full; block outside the lock but use ctx for cancellation.
	select {
	case ap.msgCh <- msg:
		return nil
	case <-ap.ctx.Done():
		return fmt.Errorf("process stopped")
	}
}

// processLoop drains the message channel sequentially, ensuring only one
// runPrompt executes at a time against the underlying agent.
func (ap *ArchProcess) processLoop() {
	for {
		select {
		case msg, ok := <-ap.msgCh:
			if !ok {
				return
			}
			ap.mu.Lock()
			alive := ap.alive
			ap.mu.Unlock()
			if !alive {
				return
			}
			ap.runPrompt(msg)
		case <-ap.ctx.Done():
			return
		}
	}
}

func (ap *ArchProcess) Abort() error {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if !ap.alive {
		return nil
	}
	ap.cancel()
	ap.gen++
	ap.ctx, ap.cancel = context.WithCancel(context.Background())
	return nil
}

func (ap *ArchProcess) Stop() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if !ap.alive {
		return
	}
	ap.alive = false
	ap.cancel()
	ap.stopOnce.Do(func() { close(ap.msgCh) })
	mlog.Info("architect process stopped")
}

func (ap *ArchProcess) runPrompt(msg string) {
	ap.mu.Lock()
	ctx := ap.ctx
	gen := ap.gen
	ap.mu.Unlock()

	ap.ag.OnEvent = func(ev agent.Event) {
		switch ev.Type {
		case "text_delta":
			ap.prog.Send(ArchChunkMsg{Delta: ev.Delta})
		case "tool_start":
			ap.prog.Send(ArchToolMsg{Display: ev.ToolName})
		case "tool_done":
			ap.prog.Send(ArchToolMsg{Display: ev.ToolName})
		}
	}

	fullText, err := ap.ag.Run(ctx, msg)

	if err != nil {
		ap.mu.Lock()
		wasAlive := ap.alive
		ap.mu.Unlock()
		if wasAlive {
			if ctx.Err() != nil {
				return // aborted
			}
			ap.prog.Send(ArchCrashedMsg{Err: err})
		}
		return
	}

	// If the generation changed (Abort was called), discard stale results.
	ap.mu.Lock()
	stale := ap.gen != gen
	ap.mu.Unlock()
	if stale {
		return
	}

	mlog.Debug("architect fullText", slog.Int("len", len(fullText)))
	proposals := ParseProposalBlocks(fullText)
	mlog.Info("architect parsed proposals", slog.Int("count", len(proposals)))
	clean := CleanResponse(fullText)
	ap.prog.Send(ArchDoneMsg{Response: clean, Proposals: proposals})
}

// ── Tool call formatting ────────────────────────────────────────────────────

func FormatToolCall(name string, args map[string]interface{}) string {
	var key string
	switch name {
	case "list_dir", "read_file", "file_outline", "get_imports", "file_stats", "project_outline":
		key = shortArg(args, "path")
	case "search_code", "find_files":
		key = shortArg(args, "pattern")
	case "get_function", "get_type", "find_references":
		key = shortArg(args, "name")
	default:
		for _, k := range []string{"path", "name", "pattern", "file_path"} {
			if v := shortArg(args, k); v != "" {
				key = v
				break
			}
		}
	}
	if key != "" {
		return name + "(" + key + ")"
	}
	return name
}

func shortArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	if len(s) > 30 {
		s = s[:27] + "..."
	}
	return s
}

// ── Task results formatting ─────────────────────────────────────────────────

func FormatTaskResults(results []store.TaskResult) string {
	var ctx strings.Builder
	ctx.WriteString("=== Task Results ===\n")
	for _, r := range results {
		desc := r.Description
		if desc == "" {
			desc = r.TaskID
		}
		if r.Status == store.StatusFailed && r.Error != "" {
			ctx.WriteString(fmt.Sprintf("- [%s] %s — %s\n", r.Status, desc, r.Error))
		} else {
			ctx.WriteString(fmt.Sprintf("- [%s] %s\n", r.Status, desc))
		}
	}
	return ctx.String()
}

func BuildArchitectPrompt(history []store.ChatMessage, results []store.TaskResult) string {
	var b strings.Builder
	for _, msg := range history {
		b.WriteString(msg.Role + ": " + msg.Content + "\n")
	}
	if len(results) > 0 {
		b.WriteString(FormatTaskResults(results))
	}
	return b.String()
}

// ── Proposal parsing ────────────────────────────────────────────────────────

func ParseProposalBlocks(response string) []ParsedProposal {
	var proposals []ParsedProposal
	parts := strings.Split(response, "```proposal")
	for i := 1; i < len(parts); i++ {
		endIdx := strings.Index(parts[i], "```")
		if endIdx == -1 {
			continue
		}
		content := parts[i][:endIdx]
		p := ParsedProposal{}
		err := json.Unmarshal([]byte(content), &p)
		if err != nil {
			err = yaml.Unmarshal([]byte(content), &p)
			if err != nil {
				continue
			}
		}
		if p.Description != "" {
			proposals = append(proposals, p)
		}
	}
	return proposals
}

// cleanNewlinesRe collapses runs of 3+ newlines down to 2.
var cleanNewlinesRe = regexp.MustCompile(`\n{3,}`)

// thinkingRe matches model thinking blocks that should not be shown to the user.
// Covers ∞thought...∞thought, <thought>...</thought>, <thinking>...</thinking>,
// lines starting with "CRITICAL INSTRUCTION", and raw JSON event blobs
// that some providers emit as text (e.g. {"name": "model", "text": "thought..."}).
var thinkingRe = regexp.MustCompile(`(?s)(∞thought.*?∞thought|<thought>.*?</thought>|<thinking>.*?</thinking>)`)
var instructionLineRe = regexp.MustCompile(`(?m)^CRITICAL INSTRUCTION[^\n]*\n?`)
var rawJSONEventRe = regexp.MustCompile(`(?s)\{"name":\s*"model",\s*"text":\s*"[^"]*"\}`)

func CleanResponse(response string) string {
	result := response
	result = StripThinking(result)
	for {
		start := strings.Index(result, "```proposal")
		if start == -1 {
			break
		}
		end := strings.Index(result[start+11:], "```")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+11+end+3:]
	}
	result = cleanNewlinesRe.ReplaceAllString(result, "\n\n")
	return strings.TrimSpace(result)
}

// StripThinking removes model thinking/reasoning blocks from text.
func StripThinking(s string) string {
	s = thinkingRe.ReplaceAllString(s, "")
	s = instructionLineRe.ReplaceAllString(s, "")
	s = rawJSONEventRe.ReplaceAllString(s, "")
	return s
}
