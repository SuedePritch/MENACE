package tui

import (
	"log/slog"
	"sort"
	"strings"

	"menace/internal/engine"
	mlog "menace/internal/log"
	"menace/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// setupModel encapsulates the provider/key/model setup flow.
// Steps: 0=provider, 1=api key, 2=architect model, 3=worker model
type setupModel struct {
	providerSel     int
	step            int
	key             string
	architectSel    int
	workerSel       int
	architectModels []engine.ModelOption
	workerModels    []engine.ModelOption
	fetching        bool
	store           *store.Store
}

func newSetupModel(s *store.Store) setupModel {
	return setupModel{store: s}
}

// loginDoneMsg signals setup is complete and dashboard should start.
type loginDoneMsg struct{}

type modelsFetchedMsg struct {
	architect []engine.ModelOption
	worker    []engine.ModelOption
}

func (sm setupModel) Update(msg tea.KeyMsg) (setupModel, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		return sm, tea.Quit
	}

	fetchModelsCmd := func(provider, apiKey string) tea.Cmd {
		return func() tea.Msg {
			a, w := engine.FetchModels(provider, apiKey)
			return modelsFetchedMsg{architect: a, worker: w}
		}
	}

	switch sm.step {
	case 0: // Provider selection
		switch key {
		case "j", "down":
			if sm.providerSel < len(engine.ProviderPresets)-1 {
				sm.providerSel++
			}
		case "k", "up":
			if sm.providerSel > 0 {
				sm.providerSel--
			}
		case "enter":
			preset := &engine.ProviderPresets[sm.providerSel]
			if preset.ArchitectProvider == "ollama" {
				if err := sm.store.SaveAPIKey("ollama", "ollama"); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchModelsCmd("ollama", "")
			}
			if envKey := engine.ResolveAPIKeyFromEnv(preset.ArchitectProvider); envKey != "" {
				if err := sm.store.SaveAPIKey(preset.ArchitectProvider, envKey); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchModelsCmd(preset.ArchitectProvider, envKey)
			}
			if sm.store.HasAPIKey(preset.ArchitectProvider) {
				apiKey := sm.store.GetAPIKey(preset.ArchitectProvider)
				sm.fetching = true
				return sm, fetchModelsCmd(preset.ArchitectProvider, apiKey)
			}
			sm.step = 1
			sm.key = ""
		}
		return sm, nil

	case 1: // API key entry
		switch key {
		case "esc", "escape":
			sm.step = 0
			sm.key = ""
		case "enter":
			apiKey := strings.TrimSpace(sm.key)
			if apiKey == "" {
				return sm, nil
			}
			preset := &engine.ProviderPresets[sm.providerSel]
			if err := sm.store.SaveAPIKey(preset.ArchitectProvider, apiKey); err != nil {
				mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
			}
			sm.fetching = true
			return sm, fetchModelsCmd(preset.ArchitectProvider, apiKey)
		case "backspace":
			if len(sm.key) > 0 {
				sm.key = sm.key[:len(sm.key)-1]
			}
		case "ctrl+u":
			sm.key = ""
		default:
			if len(msg.Runes) > 0 {
				sm.key += string(msg.Runes)
			}
		}
		return sm, nil

	case 2: // Architect model selection
		models := sm.architectModels
		switch key {
		case "esc", "escape":
			sm.step = 0
		case "j", "down":
			if sm.architectSel < len(models)-1 {
				sm.architectSel++
			}
		case "k", "up":
			if sm.architectSel > 0 {
				sm.architectSel--
			}
		case "enter":
			sm.step = 3
			sm.workerSel = 0
		}
		return sm, nil

	case 3: // Worker model selection
		models := sm.workerModels
		switch key {
		case "esc", "escape":
			sm.step = 2
		case "j", "down":
			if sm.workerSel < len(models)-1 {
				sm.workerSel++
			}
		case "k", "up":
			if sm.workerSel > 0 {
				sm.workerSel--
			}
		case "enter":
			workerModel := ""
			if len(models) > 0 {
				workerModel = models[sm.workerSel].ID
			}
			preset := &engine.ProviderPresets[sm.providerSel]
			architectModel := ""
			if len(sm.architectModels) > 0 {
				architectModel = sm.architectModels[sm.architectSel].ID
			}
			apiKey := sm.store.GetAPIKey(preset.ArchitectProvider)
			if err := sm.store.SaveAuth(preset.ArchitectProvider, apiKey, architectModel, workerModel); err != nil {
				mlog.Error("SaveAuth", slog.String("err", err.Error()))
			}
			return sm, func() tea.Msg { return loginDoneMsg{} }
		}
		return sm, nil
	}
	return sm, nil
}

func (sm setupModel) HandleModelsFetched(msg modelsFetchedMsg) setupModel {
	sm.fetching = false
	sm.architectModels = msg.architect
	sm.workerModels = msg.worker
	sort.Slice(sm.architectModels, func(i, j int) bool { return sm.architectModels[i].ID < sm.architectModels[j].ID })
	sort.Slice(sm.workerModels, func(i, j int) bool { return sm.workerModels[i].ID < sm.workerModels[j].ID })
	sm.architectSel = 0
	sm.workerSel = 0
	sm.step = 2
	return sm
}

func (sm setupModel) View(w, h int, bannerLines []string, theme themeRef) string {
	bannerStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	var banner []string
	for _, line := range bannerLines {
		banner = append(banner, bannerStyle.Render(line))
	}

	keyStyle := lipgloss.NewStyle().Foreground(ColorActive)
	valStyle := lipgloss.NewStyle().Foreground(ColorInfo)
	sep := lipgloss.NewStyle().Foreground(ColorSubtle).Render(" │ ")

	if sm.step == 1 {
		preset := engine.ProviderPresets[sm.providerSel]
		title := lipgloss.NewStyle().Foreground(ColorMuted).Render(
			"enter your " + preset.Name + " API key")

		display := strings.Repeat("•", len(sm.key))
		if len(sm.key) > 0 && len(sm.key) <= 8 {
			display = sm.key
		} else if len(sm.key) > 8 {
			display = sm.key[:4] + strings.Repeat("•", len(sm.key)-8) + sm.key[len(sm.key)-4:]
		}
		cursor := lipgloss.NewStyle().Foreground(ColorActive).Render("█")
		inputLine := lipgloss.NewStyle().Foreground(ColorText).Render(display) + cursor

		envHint := ""
		if envVars, ok := engine.ProviderEnvVars[preset.ArchitectProvider]; ok {
			envHint = lipgloss.NewStyle().Foreground(ColorDim).Italic(true).Render(
				"or set " + envVars[0] + " env var")
		}

		help := strings.Join([]string{
			keyStyle.Render("enter") + valStyle.Render(" confirm"),
			keyStyle.Render("esc") + valStyle.Render(" back"),
			keyStyle.Render("ctrl+c") + valStyle.Render(" quit"),
		}, sep)

		var lines []string
		lines = append(lines, banner...)
		lines = append(lines, "", title, "", "  "+inputLine, "")
		if envHint != "" {
			lines = append(lines, envHint, "")
		}
		lines = append(lines, help)
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
	}

	if sm.step == 2 || sm.step == 3 {
		var subtitle string
		var models []engine.ModelOption
		var sel int
		if sm.step == 2 {
			subtitle = lipgloss.NewStyle().Foreground(ColorMuted).Render("architect model — pick a powerful model for planning")
			models = sm.architectModels
			sel = sm.architectSel
		} else {
			subtitle = lipgloss.NewStyle().Foreground(ColorMuted).Render("worker model — pick a fast/cheap model for execution")
			models = sm.workerModels
			sel = sm.workerSel
		}


		var modelLines []string
		for i, mo := range models {
			isSel := i == sel
			arrow := "  "
			if isSel {
				arrow = lipgloss.NewStyle().Foreground(ColorActive).Render("▸ ")
			}
			id := mo.ID
			if isSel {
				id = lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render(id)
			} else {
				id = lipgloss.NewStyle().Foreground(ColorMuted).Render(id)
			}
			desc := ""
			if mo.Desc != "" && mo.Desc != mo.ID {
				desc = lipgloss.NewStyle().Foreground(ColorInfo).Render("  " + mo.Desc)
			}
			modelLines = append(modelLines, arrow+id+desc)
		}

		modelBlock := strings.Join(modelLines, "\n")

		help := strings.Join([]string{
			keyStyle.Render("j/k") + valStyle.Render(" select"),
			keyStyle.Render("enter") + valStyle.Render(" confirm"),
			keyStyle.Render("esc") + valStyle.Render(" back"),
		}, sep)

		header := lipgloss.JoinVertical(lipgloss.Center, append(banner, "", subtitle)...)
		content := lipgloss.JoinVertical(lipgloss.Left, "", modelBlock, "")
		block := lipgloss.JoinVertical(lipgloss.Center, header, content, help)
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, block)
	}

	if sm.fetching {
		subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("fetching models...")
		var lines []string
		lines = append(lines, banner...)
		lines = append(lines, "", subtitle)
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
	}

	// Step 0: provider selection
	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("select your provider")

	var providerLines []string
	for i, p := range engine.ProviderPresets {
		sel := i == sm.providerSel
		arrow := "  "
		if sel {
			arrow = lipgloss.NewStyle().Foreground(ColorActive).Render("▸ ")
		}
		name := p.Name
		if sel {
			name = lipgloss.NewStyle().Foreground(ColorText).Bold(true).Render(name)
		} else {
			name = lipgloss.NewStyle().Foreground(ColorMuted).Render(name)
		}
		providerLines = append(providerLines, arrow+name)
	}

	help := strings.Join([]string{
		keyStyle.Render("j/k") + valStyle.Render(" select"),
		keyStyle.Render("enter") + valStyle.Render(" continue"),
		keyStyle.Render("ctrl+c") + valStyle.Render(" quit"),
	}, sep)

	var lines []string
	lines = append(lines, banner...)
	lines = append(lines, "", subtitle, "")
	lines = append(lines, providerLines...)
	lines = append(lines, "", help)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

// themeRef carries just the data the setup view needs from the theme.
type themeRef struct {
	BannerLines []string
}
