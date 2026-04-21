package engine

import (
	"log/slog"
	"os"
	"path/filepath"

	mlog "menace/internal/log"
)

// ProviderPreset defines a preconfigured AI provider.
type ProviderPreset struct {
	Name              string
	ArchitectProvider string
	ArchitectModel    string
}

var ProviderPresets = []ProviderPreset{
	{Name: "Anthropic", ArchitectProvider: "anthropic", ArchitectModel: "claude-sonnet-4-20250514"},
	{Name: "Google", ArchitectProvider: "google", ArchitectModel: "gemini-2.5-pro"},
	{Name: "OpenAI", ArchitectProvider: "openai", ArchitectModel: "gpt-4.1"},
	{Name: "Ollama", ArchitectProvider: "ollama", ArchitectModel: "qwen3:8b"},
}

const DefaultWorkerModel = "gemini-2.5-flash-lite"

// ModelOption is a model choice shown during setup.
type ModelOption struct {
	ID   string
	Desc string // short label shown in selector
}

// ArchitectModels returns recommended architect models for a provider.
func ArchitectModels(provider string) []ModelOption {
	switch provider {
	case "anthropic":
		return []ModelOption{
			{"claude-sonnet-4-20250514", "Sonnet 4 — fast, strong"},
			{"claude-opus-4-6", "Opus 4.6 — smartest, slower"},
		}
	case "google":
		return []ModelOption{
			{"gemini-2.5-pro", "Gemini 2.5 Pro — strong reasoning"},
			{"gemini-2.5-flash", "Gemini 2.5 Flash — fast"},
		}
	case "openai":
		return []ModelOption{
			{"gpt-4.1", "GPT-4.1 — strong all-rounder"},
			{"gpt-4.1-mini", "GPT-4.1 Mini — cheaper"},
			{"o4-mini", "o4-mini — reasoning"},
		}
	case "ollama":
		return []ModelOption{
			{"qwen3:8b", "Qwen3 8B — strong for size"},
			{"llama3.3:latest", "Llama 3.3 — solid all-rounder"},
			{"deepseek-coder-v2:latest", "DeepSeek Coder V2 — code-focused"},
			{"codellama:latest", "Code Llama — code generation"},
		}
	}
	return nil
}

// WorkerModels returns recommended worker models for a provider.
func WorkerModels(provider string) []ModelOption {
	switch provider {
	case "anthropic":
		return []ModelOption{
			{"claude-sonnet-4-20250514", "Sonnet 4 — fast, capable"},
			{"claude-haiku-4-5-20251001", "Haiku 4.5 — cheapest"},
		}
	case "google":
		return []ModelOption{
			{"gemini-2.5-flash-lite", "Flash Lite — cheapest"},
			{"gemini-2.5-flash", "Flash — more capable"},
		}
	case "openai":
		return []ModelOption{
			{"gpt-4.1-mini", "GPT-4.1 Mini — cheap, fast"},
			{"gpt-4.1-nano", "GPT-4.1 Nano — cheapest"},
		}
	case "ollama":
		return []ModelOption{
			{"qwen3:1.7b", "Qwen3 1.7B — tiny, fast"},
			{"llama3.2:3b", "Llama 3.2 3B — small, capable"},
			{"qwen3:8b", "Qwen3 8B — strong for size"},
		}
	}
	return nil
}

func ResolveWorkerModel(worker string) string {
	if worker != "" {
		return worker
	}
	return DefaultWorkerModel
}

func ApplyModelOverrides(preset *ProviderPreset, architect string) {
	if architect != "" {
		preset.ArchitectModel = architect
	}
}

func PresetByName(name string) *ProviderPreset {
	for i := range ProviderPresets {
		if ProviderPresets[i].Name == name {
			return &ProviderPresets[i]
		}
	}
	return nil
}

// ─── Auth ──────────────────────────────────────────────────────────────

var ProviderEnvVars = map[string][]string{
	"anthropic": {"ANTHROPIC_API_KEY"},
	"google":    {"GOOGLE_API_KEY", "GEMINI_API_KEY"},
	"openai":    {"OPENAI_API_KEY"},
	"ollama":    {}, // no key needed
}

func ResolveAPIKeyFromEnv(provider string) string {
	for _, envVar := range ProviderEnvVars[provider] {
		if key := os.Getenv(envVar); key != "" {
			return key
		}
	}
	return ""
}


// ─── System Prompts ───────────────────────────────────────────────────

// LoadSystemPrompt reads a system prompt from menaceDir/prompts/{name}.md.
// Falls back to the embedded default if not found.
func LoadSystemPrompt(menaceDir, name string) string {
	data, err := os.ReadFile(filepath.Join(menaceDir, "prompts", name+".md"))
	if err != nil {
		mlog.Info("LoadSystemPrompt fallback", slog.String("name", name), slog.String("err", err.Error()))
		return ""
	}
	return string(data)
}
