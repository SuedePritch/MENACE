package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/flitsinc/go-llms/anthropic"
	"github.com/flitsinc/go-llms/content"
	"github.com/flitsinc/go-llms/google"
	"github.com/flitsinc/go-llms/llms"
	"github.com/flitsinc/go-llms/openai"
	"github.com/flitsinc/go-llms/tools"
)

// Event is emitted during agent execution for UI streaming.
type Event struct {
	Type string // "text_delta", "tool_start", "tool_done", "done", "error"

	Delta      string // text_delta
	ToolCallID string // tool_start, tool_done
	ToolName   string // tool_start, tool_done
	ToolInput  string // tool_done
	Error      error  // error
}

// Usage tracks cumulative token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

// Agent wraps go-llms to provide a persistent conversational agent.
type Agent struct {
	llm      *llms.LLM
	OnEvent  func(Event)
}

// Usage returns the cumulative token usage across all turns.
func (a *Agent) Usage() Usage {
	u := a.llm.TotalUsage
	return Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CachedTokens: u.CachedInputTokens,
	}
}

// NewAgent creates an agent with the given provider, model, system prompt, and tools.
func NewAgent(providerName, model, apiKey, systemPrompt string, agentTools []tools.Tool, maxTurns int) (*Agent, error) {
	provider, err := newProvider(providerName, model, apiKey)
	if err != nil {
		return nil, err
	}

	l := llms.New(provider, agentTools...)
	l.SystemPrompt = func() content.Content {
		return content.FromText(systemPrompt)
	}
	if maxTurns > 0 {
		l.WithMaxTurns(maxTurns)
	}

	return &Agent{llm: l}, nil
}

// maxResponseSize caps accumulated text to prevent OOM from runaway models.
const maxResponseSize = 10 * 1024 * 1024 // 10MB

// Run sends a user message and runs the agent loop until completion.
// Returns the final text response.
func (a *Agent) Run(ctx context.Context, message string) (string, error) {
	var buf strings.Builder

	for update := range a.llm.ChatWithContext(ctx, message) {
		switch u := update.(type) {
		case llms.TextUpdate:
			if buf.Len()+len(u.Text) > maxResponseSize {
				return buf.String(), fmt.Errorf("response exceeded %d bytes", maxResponseSize)
			}
			buf.WriteString(u.Text)
			if a.OnEvent != nil {
				a.OnEvent(Event{Type: "text_delta", Delta: u.Text})
			}
		case llms.ToolStartUpdate:
			if a.OnEvent != nil {
				a.OnEvent(Event{
					Type:       "tool_start",
					ToolCallID: u.ToolCallID,
					ToolName:   u.Tool.FuncName(),
				})
			}
		case llms.ToolDoneUpdate:
			if a.OnEvent != nil {
				a.OnEvent(Event{
					Type:       "tool_done",
					ToolCallID: u.ToolCallID,
					ToolName:   u.Tool.FuncName(),
				})
			}
		}
	}

	fullText := buf.String()

	if err := a.llm.Err(); err != nil {
		return fullText, err
	}

	if a.OnEvent != nil {
		a.OnEvent(Event{Type: "done"})
	}

	return fullText, nil
}

// NewProviderWithBase creates a go-llms provider, optionally with a custom base URL (for Ollama etc).
func newProvider(providerName, model, apiKey string) (llms.Provider, error) {
	return NewProviderWithBase(providerName, model, apiKey, "")
}

func NewProviderWithBase(providerName, model, apiKey, baseURL string) (llms.Provider, error) {
	switch providerName {
	case "anthropic":
		return anthropic.New(apiKey, model), nil
	case "google":
		return google.New(model).WithGeminiAPI(apiKey), nil
	case "openai":
		return openai.New(apiKey, model), nil
	case "ollama":
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1/chat/completions"
		}
		return openai.New("ollama", model).WithEndpoint(baseURL, "Ollama"), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
}
