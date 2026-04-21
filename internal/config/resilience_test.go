package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClampExtremeValues(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"concurrency": 999,
		"max_retry": -50,
		"chat_char_limit": 999999999,
		"chat_max_height": 0,
		"ollama_base_url": "not-a-url"
	}`), 0644)

	cfg := Load(dir)

	if cfg.Concurrency != 20 {
		t.Fatalf("expected concurrency clamped to 20, got %d", cfg.Concurrency)
	}
	if cfg.MaxRetry != 0 {
		t.Fatalf("expected max_retry clamped to 0, got %d", cfg.MaxRetry)
	}
	if cfg.ChatCharLimit != 50000 {
		t.Fatalf("expected chat_char_limit clamped to 50000, got %d", cfg.ChatCharLimit)
	}
	if cfg.ChatMaxHeight != 3 {
		t.Fatalf("expected chat_max_height clamped to 3, got %d", cfg.ChatMaxHeight)
	}
	if cfg.OllamaBaseURL != Default().OllamaBaseURL {
		t.Fatalf("expected invalid URL reset to default, got %s", cfg.OllamaBaseURL)
	}
}

func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{corrupt json!!!`), 0644)

	cfg := Load(dir)

	// Should fall back to defaults
	if cfg.Concurrency != 3 {
		t.Fatalf("corrupt config should use default concurrency, got %d", cfg.Concurrency)
	}
}

func TestLoadPartialJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"theme": "custom"}`), 0644)

	cfg := Load(dir)

	if cfg.Theme != "custom" {
		t.Fatalf("expected theme 'custom', got '%s'", cfg.Theme)
	}
	// Unset fields should be defaults
	if cfg.Concurrency != 3 {
		t.Fatalf("expected default concurrency, got %d", cfg.Concurrency)
	}
}
