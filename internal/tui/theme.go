package tui

import (
	"menace/internal/config"

	"github.com/charmbracelet/lipgloss"
)

// Colors — mutable package-level vars, overridden by theme.
var (
	ColorActive   = lipgloss.Color("#3aff37")
	ColorInactive = lipgloss.Color("#585858")
	ColorAccent   = lipgloss.Color("#ff3dbe")
	ColorMuted    = lipgloss.Color("#9a9a9a")
	ColorText     = lipgloss.Color("#e4e4e4")
	ColorSubtle   = lipgloss.Color("#4e4e4e")
	ColorWarn     = lipgloss.Color("#d29922")
	ColorFail     = lipgloss.Color("#f85149")
	ColorSuccess  = lipgloss.Color("#3aff37")
	ColorDim      = lipgloss.Color("#6e6e6e")
	ColorInfo     = lipgloss.Color("#5de4c7")
)

// Base styles
var (
	baseStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder())

	bentoBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorInactive)
)

// applyThemeColors overrides the package-level color vars from a resolved theme.
func applyThemeColors(c config.ThemeColors) {
	if c.Active != "" {
		ColorActive = lipgloss.Color(c.Active)
	}
	if c.Inactive != "" {
		ColorInactive = lipgloss.Color(c.Inactive)
	}
	if c.Accent != "" {
		ColorAccent = lipgloss.Color(c.Accent)
	}
	if c.Muted != "" {
		ColorMuted = lipgloss.Color(c.Muted)
	}
	if c.Text != "" {
		ColorText = lipgloss.Color(c.Text)
	}
	if c.Subtle != "" {
		ColorSubtle = lipgloss.Color(c.Subtle)
	}
	if c.Warn != "" {
		ColorWarn = lipgloss.Color(c.Warn)
	}
	if c.Fail != "" {
		ColorFail = lipgloss.Color(c.Fail)
	}
	if c.Success != "" {
		ColorSuccess = lipgloss.Color(c.Success)
	}
	if c.Dim != "" {
		ColorDim = lipgloss.Color(c.Dim)
	}
	if c.Info != "" {
		ColorInfo = lipgloss.Color(c.Info)
	}

	// Rebuild base styles with new colors
	bentoBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorInactive)
}
