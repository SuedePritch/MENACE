package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Theme holds all customizable visual and personality properties.
type Theme struct {
	Meta        ThemeMeta        `toml:"meta"`
	Colors      ThemeColors      `toml:"colors"`
	Personality ThemePersonality `toml:"personality"`
}

type ThemeMeta struct {
	Name   string `toml:"name"`
	Author string `toml:"author"`
}

type ThemeColors struct {
	Active   string `toml:"active"`
	Inactive string `toml:"inactive"`
	Accent   string `toml:"accent"`
	Text     string `toml:"text"`
	Subtle   string `toml:"subtle"`
	Muted    string `toml:"muted"`
	Warn     string `toml:"warn"`
	Fail     string `toml:"fail"`
	Success  string `toml:"success"`
	Dim      string `toml:"dim"`
	Info     string `toml:"info"`
}

type ThemePersonality struct {
	Banner    string   `toml:"banner"`
	Welcome   string   `toml:"welcome"`
	Thinking  string   `toml:"thinking"`
	Done      string   `toml:"done"`
	Cancelled string   `toml:"cancelled"`
	Restarted string   `toml:"restarted"`
	Switched  string   `toml:"switched"`
	NewSess   string   `toml:"new_session"`
	Havoc     string   `toml:"havoc"`
	InputHint string   `toml:"input_hint"`
	Empty     []string `toml:"empty"`

	// Panel titles
	PanelArchitect string `toml:"panel_architect"`
	PanelProposals string `toml:"panel_proposals"`
	PanelTasks     string `toml:"panel_tasks"`
}

// OmarchyColors maps the 22 variables from ~/.config/omarchy/current/theme/colors.toml
type OmarchyColors struct {
	Accent              string `toml:"accent"`
	Cursor              string `toml:"cursor"`
	Foreground          string `toml:"foreground"`
	Background          string `toml:"background"`
	SelectionForeground string `toml:"selection_foreground"`
	SelectionBackground string `toml:"selection_background"`
	Color0              string `toml:"color0"`
	Color1              string `toml:"color1"`
	Color2              string `toml:"color2"`
	Color3              string `toml:"color3"`
	Color4              string `toml:"color4"`
	Color5              string `toml:"color5"`
	Color6              string `toml:"color6"`
	Color7              string `toml:"color7"`
	Color8              string `toml:"color8"`
	Color9              string `toml:"color9"`
	Color10             string `toml:"color10"`
	Color11             string `toml:"color11"`
	Color12             string `toml:"color12"`
	Color13             string `toml:"color13"`
	Color14             string `toml:"color14"`
	Color15             string `toml:"color15"`
}

// DefaultTheme returns the built-in MENACE theme.
func DefaultTheme() Theme {
	return Theme{
		Meta: ThemeMeta{Name: "menace", Author: "MENACE"},
		Colors: ThemeColors{
			Active:   "#3aff37",
			Inactive: "#585858",
			Accent:   "#ff3dbe",
			Text:     "#e4e4e4",
			Subtle:   "#4e4e4e",
			Muted:    "#9a9a9a",
			Warn:     "#d29922",
			Fail:     "#f85149",
			Success:  "#3aff37",
			Dim:      "#6e6e6e",
			Info:     "#5de4c7",
		},
		Personality: DefaultPersonality(),
	}
}

// DefaultPersonality returns the default MENACE personality strings.
func DefaultPersonality() ThemePersonality {
	p := ThemePersonality{
		Banner: `
			███╗   ███╗███████╗███╗   ██╗ █████╗  ██████╗███████╗
			████╗ ████║██╔════╝████╗  ██║██╔══██╗██╔════╝██╔════╝
			██╔████╔██║█████╗  ██╔██╗ ██║███████║██║     █████╗
			██║╚██╔╝██║██╔══╝  ██║╚██╗██║██╔══██║██║     ██╔══╝
			██║ ╚═╝ ██║███████╗██║ ╚████║██║  ██║╚██████╗███████╗
			╚═╝     ╚═╝╚══════╝╚═╝  ╚═══╝╚═╝  ╚═╝ ╚═════╝╚══════╝`,
		Welcome:   "MENACE is loose. Press i to chat.",
		Thinking:  "thinking...",
		Done:      "nothing left to destroy.",
		Cancelled: "⏹ Cancelled",
		Restarted: "↻ Architect restarted. Press i to chat.",
		Switched:  "Switched to %s. Press i to chat.",
		NewSess:   "New session in %s. Press i to chat.",
		Havoc:     "%s %d wreaking havoc",
		InputHint: "press i or / to chat",
		Empty:     []string{"Chat with the architect.", "Research, brainstorm, propose.", "", "Press i to start."},

		PanelArchitect: "architect",
		PanelProposals: "proposals",
		PanelTasks:     "tasks",
	}
	p.Banner = padBanner(strings.TrimSpace(dedent(p.Banner)))
	return p
}

// SystemTheme returns a theme that uses ANSI color indices so it inherits
// from the terminal's palette (Ghostty, Alacritty, kitty, etc.).
func SystemTheme() Theme {
	t := DefaultTheme()
	t.Meta = ThemeMeta{Name: "system", Author: "terminal"}
	t.Colors = ThemeColors{
		Active:   "10",  // ANSI bright green
		Inactive: "8",   // ANSI bright black (gray)
		Accent:   "13",  // ANSI bright magenta
		Text:     "15",  // ANSI bright white
		Subtle:   "8",   // ANSI bright black
		Muted:    "7",   // ANSI white (gray)
		Warn:     "3",   // ANSI yellow
		Fail:     "1",   // ANSI red
		Success:  "2",   // ANSI green
		Dim:      "8",   // ANSI bright black
		Info:     "14",  // ANSI bright cyan
	}
	return t
}

// LoadTheme resolves a theme by name. Resolution order:
//  1. "menace" → built-in default
//  2. "system" → ANSI terminal palette
//  3. "omarchy" → reads ~/.config/omarchy/current/theme/colors.toml
//  4. Check ~/.config/menace/themes/<name>.toml
//  5. Check <menaceDir>/themes/<name>.toml
func LoadTheme(name, menaceDir string) Theme {
	switch strings.ToLower(name) {
	case "", "menace":
		return DefaultTheme()
	case "system":
		return SystemTheme()
	case "omarchy":
		if t, ok := loadOmarchyTheme(); ok {
			return t
		}
		return DefaultTheme()
	}

	// Try user config dir
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".config", "menace", "themes", name+".toml")
		if t, ok := loadThemeFile(path); ok {
			return t
		}
	}

	// Try menace dir
	path := filepath.Join(menaceDir, "themes", name+".toml")
	if t, ok := loadThemeFile(path); ok {
		return t
	}

	return DefaultTheme()
}

func loadThemeFile(path string) (Theme, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, false
	}

	t := DefaultTheme()
	if err := toml.Unmarshal(data, &t); err != nil {
		return Theme{}, false
	}

	// Clean up banner from TOML
	t.Personality.Banner = padBanner(strings.TrimSpace(dedent(t.Personality.Banner)))

	// Fill in any missing personality strings from defaults
	defaults := DefaultPersonality()
	if t.Personality.Banner == "" {
		t.Personality.Banner = defaults.Banner
	}
	if t.Personality.Welcome == "" {
		t.Personality.Welcome = defaults.Welcome
	}
	if t.Personality.Thinking == "" {
		t.Personality.Thinking = defaults.Thinking
	}
	if t.Personality.Done == "" {
		t.Personality.Done = defaults.Done
	}
	if t.Personality.Cancelled == "" {
		t.Personality.Cancelled = defaults.Cancelled
	}
	if t.Personality.Restarted == "" {
		t.Personality.Restarted = defaults.Restarted
	}
	if t.Personality.Switched == "" {
		t.Personality.Switched = defaults.Switched
	}
	if t.Personality.NewSess == "" {
		t.Personality.NewSess = defaults.NewSess
	}
	if t.Personality.Havoc == "" {
		t.Personality.Havoc = defaults.Havoc
	}
	if t.Personality.InputHint == "" {
		t.Personality.InputHint = defaults.InputHint
	}
	if len(t.Personality.Empty) == 0 {
		t.Personality.Empty = defaults.Empty
	}
	if t.Personality.PanelArchitect == "" {
		t.Personality.PanelArchitect = defaults.PanelArchitect
	}
	if t.Personality.PanelProposals == "" {
		t.Personality.PanelProposals = defaults.PanelProposals
	}
	if t.Personality.PanelTasks == "" {
		t.Personality.PanelTasks = defaults.PanelTasks
	}

	return t, true
}

func loadOmarchyTheme() (Theme, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Theme{}, false
	}

	path := filepath.Join(home, ".config", "omarchy", "current", "theme", "colors.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Theme{}, false
	}

	var oc OmarchyColors
	if err := toml.Unmarshal(data, &oc); err != nil {
		return Theme{}, false
	}

	t := DefaultTheme()
	t.Meta = ThemeMeta{Name: "omarchy", Author: "omarchy"}

	// Map Omarchy's palette to MENACE color roles:
	// color2 = green (success/active), color1 = red (fail),
	// color3 = yellow (warn), color5 = magenta (accent),
	// color6 = cyan (info), color8 = bright black (dim/subtle)
	t.Colors = ThemeColors{
		Active:   or(oc.Color2, t.Colors.Active),
		Inactive: or(oc.Color8, t.Colors.Inactive),
		Accent:   or(oc.Accent, or(oc.Color5, t.Colors.Accent)),
		Text:     or(oc.Foreground, t.Colors.Text),
		Subtle:   or(oc.Color8, t.Colors.Subtle),
		Muted:    or(oc.Color7, t.Colors.Muted),
		Warn:     or(oc.Color3, t.Colors.Warn),
		Fail:     or(oc.Color1, t.Colors.Fail),
		Success:  or(oc.Color2, t.Colors.Success),
		Dim:      or(oc.Color8, t.Colors.Dim),
		Info:     or(oc.Color6, t.Colors.Info),
	}

	return t, true
}

// ListThemes returns all available theme names in cycle order.
// Built-ins first, then user themes from ~/.config/menace/themes/ and <menaceDir>/themes/.
func ListThemes(menaceDir string) []string {
	seen := map[string]bool{}
	var names []string

	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}

	// Built-ins
	add("menace")
	add("system")

	// Check for omarchy
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".config", "omarchy", "current", "theme", "colors.toml")); err == nil {
			add("omarchy")
		}
	}

	// Scan menaceDir/themes/
	scanDir := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".toml")
			add(name)
		}
	}

	scanDir(filepath.Join(menaceDir, "themes"))

	if home, err := os.UserHomeDir(); err == nil {
		scanDir(filepath.Join(home, ".config", "menace", "themes"))
	}

	return names
}

// dedent strips common leading whitespace (tabs/spaces) from all lines.
func dedent(s string) string {
	lines := strings.Split(s, "\n")

	// Find minimum indentation (in runes) across non-empty lines
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := 0
		for _, r := range line {
			if r == '\t' || r == ' ' {
				indent++
			} else {
				break
			}
		}
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}

	if minIndent <= 0 {
		return s
	}

	for i, line := range lines {
		runes := []rune(line)
		if len(runes) >= minIndent {
			lines[i] = string(runes[minIndent:])
		}
	}
	return strings.Join(lines, "\n")
}

// padBanner pads all lines to the same rune width so center-alignment is consistent.
func padBanner(s string) string {
	lines := strings.Split(s, "\n")
	maxW := 0
	for _, line := range lines {
		w := len([]rune(line))
		if w > maxW {
			maxW = w
		}
	}
	for i, line := range lines {
		w := len([]rune(line))
		if w < maxW {
			lines[i] = line + strings.Repeat(" ", maxW-w)
		}
	}
	return strings.Join(lines, "\n")
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ExportCustomTheme duplicates a theme to <menaceDir>/themes/custom.toml.
// Returns the path to the file. If custom.toml already exists, returns its path without overwriting.
func ExportCustomTheme(menaceDir string, base Theme) (string, error) {
	themesDir := filepath.Join(menaceDir, "themes")
	if err := os.MkdirAll(themesDir, 0755); err != nil {
		return "", fmt.Errorf("create themes directory: %w", err)
	}
	path := filepath.Join(themesDir, "custom.toml")

	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}

	base.Meta.Name = "custom"
	base.Meta.Author = "you"

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(base); err != nil {
		return "", err
	}
	return path, nil
}
