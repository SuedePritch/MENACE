package engine

import (
	"testing"
)

func TestClassifyModel(t *testing.T) {
	tests := []struct {
		provider string
		id       string
		want     string
	}{
		// Anthropic
		{"anthropic", "claude-opus-4-6", "strong"},
		{"anthropic", "claude-sonnet-4-20250514", "mid"},
		{"anthropic", "claude-haiku-4-5-20251001", "cheap"},
		{"anthropic", "claude-unknown-9", "mid"},

		// Google
		{"google", "gemini-2.5-pro", "strong"},
		{"google", "gemini-2.5-flash", "mid"},
		{"google", "gemini-2.5-flash-lite", "cheap"},
		{"google", "gemini-2.5-flash-8b", "cheap"},
		{"google", "gemma-3-27b", "cheap"},
		{"google", "gemini-unknown", "mid"},

		// OpenAI
		{"openai", "o4-mini", "strong"},
		{"openai", "o3-mini", "strong"},
		{"openai", "gpt-4.1", "strong"},
		{"openai", "gpt-4.1-mini", "mid"},
		{"openai", "gpt-4.1-nano", "cheap"},
		{"openai", "gpt-3.5-turbo", "mid"},

		// Ollama
		{"ollama", "llama3:70b", "strong"},
		{"ollama", "qwen3:1.7b", "cheap"},
		{"ollama", "qwen3:8b", "mid"},

		// Unknown provider
		{"unknown", "anything", "mid"},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.id, func(t *testing.T) {
			got := classifyModel(tt.provider, tt.id)
			if got != tt.want {
				t.Errorf("classifyModel(%q, %q) = %q, want %q", tt.provider, tt.id, got, tt.want)
			}
		})
	}
}

func TestParseAnthropicModels(t *testing.T) {
	body := []byte(`{"data":[{"id":"claude-sonnet-4","display_name":"Claude Sonnet 4"},{"id":"claude-haiku-4.5","display_name":""}]}`)
	models, err := parseAnthropicModels(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	// Sorted by ID
	if models[0].ID != "claude-haiku-4.5" {
		t.Errorf("expected first model claude-haiku-4.5, got %s", models[0].ID)
	}
	// Empty display name falls back to ID
	if models[0].Desc != "claude-haiku-4.5" {
		t.Errorf("expected desc to fallback to ID, got %s", models[0].Desc)
	}
}

func TestParseGoogleModels(t *testing.T) {
	body := []byte(`{"models":[
		{"name":"models/gemini-2.5-flash","displayName":"Gemini 2.5 Flash","supportedGenerationMethods":["generateContent"]},
		{"name":"models/veo-2","displayName":"Veo 2","supportedGenerationMethods":["generateContent"]},
		{"name":"models/embedding-001","displayName":"Embedding","supportedGenerationMethods":["embedContent"]}
	]}`)
	models, err := parseGoogleModels(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model (flash only), got %d", len(models))
	}
	if models[0].ID != "gemini-2.5-flash" {
		t.Errorf("expected gemini-2.5-flash, got %s", models[0].ID)
	}
}

func TestParseOpenAIModels(t *testing.T) {
	body := []byte(`{"data":[
		{"id":"gpt-4.1"},
		{"id":"dall-e-3"},
		{"id":"whisper-1"},
		{"id":"text-embedding-3-small"},
		{"id":"gpt-4.1-mini"}
	]}`)
	models, err := parseOpenAIModels(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 chat models, got %d", len(models))
	}
}

func TestParseOllamaModels(t *testing.T) {
	body := []byte(`{"models":[{"name":"qwen3:8b","size":4500000000},{"name":"llama3:70b","size":40000000000}]}`)
	models, err := parseOllamaModels(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
}
