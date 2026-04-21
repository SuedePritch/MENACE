package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Concurrency != 3 {
		t.Fatalf("expected concurrency 3, got %d", cfg.Concurrency)
	}
	if cfg.MaxRetry != 2 {
		t.Fatalf("expected max_retry 2, got %d", cfg.MaxRetry)
	}
	if cfg.Theme != "menace" {
		t.Fatalf("expected theme 'menace', got '%s'", cfg.Theme)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	cfg := Load(t.TempDir())
	if cfg.Concurrency != 3 {
		t.Fatal("missing config should return defaults")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.Theme = "midnight"

	Save(dir, cfg)

	loaded := Load(dir)
	if loaded.Theme != "midnight" {
		t.Fatalf("expected theme 'midnight', got '%s'", loaded.Theme)
	}
}

func TestDefaultTheme(t *testing.T) {
	theme := DefaultTheme()
	if theme.Meta.Name != "menace" {
		t.Fatalf("expected name 'menace', got '%s'", theme.Meta.Name)
	}
	if theme.Colors.Active == "" {
		t.Fatal("expected non-empty active color")
	}
	if theme.Personality.Welcome == "" {
		t.Fatal("expected non-empty welcome")
	}
	if theme.Personality.PanelArchitect == "" {
		t.Fatal("expected non-empty panel title")
	}
}

func TestDefaultPersonalityBannerNoBadWhitespace(t *testing.T) {
	p := DefaultPersonality()
	if p.Banner == "" {
		t.Fatal("expected non-empty banner")
	}
	// Should not start with whitespace
	if p.Banner[0] == '\t' || p.Banner[0] == '\n' || p.Banner[0] == ' ' {
		t.Fatalf("banner should not start with whitespace, starts with %q", p.Banner[0])
	}
}

func TestSystemTheme(t *testing.T) {
	theme := SystemTheme()
	if theme.Meta.Name != "system" {
		t.Fatalf("expected name 'system', got '%s'", theme.Meta.Name)
	}
	// ANSI colors are numeric strings
	if theme.Colors.Active != "10" {
		t.Fatalf("expected ANSI color '10', got '%s'", theme.Colors.Active)
	}
}

func TestLoadThemeBuiltins(t *testing.T) {
	dir := t.TempDir()

	menace := LoadTheme("menace", dir)
	if menace.Meta.Name != "menace" {
		t.Fatal("expected menace theme")
	}

	system := LoadTheme("system", dir)
	if system.Meta.Name != "system" {
		t.Fatal("expected system theme")
	}

	// Empty name should default to menace
	def := LoadTheme("", dir)
	if def.Meta.Name != "menace" {
		t.Fatal("expected default to menace")
	}
}

func TestLoadThemeFromFile(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	os.MkdirAll(themesDir, 0755)

	toml := `
[meta]
name = "custom"
author = "test"

[colors]
active = "#ff0000"
accent = "#00ff00"

[personality]
welcome = "hello custom"
panel_architect = "brain"
`
	os.WriteFile(filepath.Join(themesDir, "custom.toml"), []byte(toml), 0644)

	theme := LoadTheme("custom", dir)
	if theme.Meta.Name != "custom" {
		t.Fatalf("expected 'custom', got '%s'", theme.Meta.Name)
	}
	if theme.Colors.Active != "#ff0000" {
		t.Fatalf("expected custom color, got '%s'", theme.Colors.Active)
	}
	if theme.Personality.Welcome != "hello custom" {
		t.Fatalf("expected custom welcome, got '%s'", theme.Personality.Welcome)
	}
	if theme.Personality.PanelArchitect != "brain" {
		t.Fatalf("expected 'brain', got '%s'", theme.Personality.PanelArchitect)
	}
	// Missing fields should fall back to defaults
	if theme.Personality.Thinking == "" {
		t.Fatal("missing thinking should fall back to default")
	}
	if theme.Colors.Text == "" {
		t.Fatal("missing text color should fall back to default")
	}
}

func TestLoadThemeUnknownFallsToDefault(t *testing.T) {
	theme := LoadTheme("nonexistent_theme_xyz", t.TempDir())
	if theme.Meta.Name != "menace" {
		t.Fatalf("unknown theme should fall back to menace, got '%s'", theme.Meta.Name)
	}
}

func TestListThemes(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	os.MkdirAll(themesDir, 0755)

	os.WriteFile(filepath.Join(themesDir, "custom.toml"), []byte("[meta]\nname=\"custom\""), 0644)
	os.WriteFile(filepath.Join(themesDir, "another.toml"), []byte("[meta]\nname=\"another\""), 0644)

	themes := ListThemes(dir)

	// Should always have menace and system
	has := map[string]bool{}
	for _, name := range themes {
		has[name] = true
	}
	if !has["menace"] {
		t.Fatal("expected 'menace' in theme list")
	}
	if !has["system"] {
		t.Fatal("expected 'system' in theme list")
	}
	if !has["custom"] {
		t.Fatal("expected 'custom' in theme list")
	}
	if !has["another"] {
		t.Fatal("expected 'another' in theme list")
	}
}
