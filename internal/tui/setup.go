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
// Steps:
//
//	0 = architect provider selection
//	1 = architect API key entry
//	2 = architect model selection
//	3 = worker provider selection
//	4 = worker API key entry (skipped if same provider as architect or Ollama)
//	5 = worker model selection
type setupModel struct {
	// architect
	archProviderSel int
	architectModels []engine.ModelOption
	architectSel    int
	archKey         string

	// worker
	workerProviderSel int
	workerModels      []engine.ModelOption
	workerSel         int
	workerKey         string

	step     int
	fetching bool
	store    *store.Store
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

type workerModelsFetchedMsg struct {
	models []engine.ModelOption
}

func (sm setupModel) Update(msg tea.KeyMsg) (setupModel, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		return sm, tea.Quit
	}

	fetchArchModelsCmd := func(provider, apiKey string) tea.Cmd {
		return func() tea.Msg {
			a, w := engine.FetchModels(provider, apiKey)
			return modelsFetchedMsg{architect: a, worker: w}
		}
	}

	fetchWorkerModelsCmd := func(provider, apiKey string) tea.Cmd {
		return func() tea.Msg {
			_, w := engine.FetchModels(provider, apiKey)
			return workerModelsFetchedMsg{models: w}
		}
	}

	switch sm.step {
	case 0: // Architect provider selection
		switch key {
		case "j", "down":
			if sm.archProviderSel < len(engine.ProviderPresets)-1 {
				sm.archProviderSel++
			}
		case "k", "up":
			if sm.archProviderSel > 0 {
				sm.archProviderSel--
			}
		case "enter":
			preset := &engine.ProviderPresets[sm.archProviderSel]
			if preset.ArchitectProvider == "ollama" {
				if err := sm.store.SaveAPIKey("ollama", "ollama"); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchArchModelsCmd("ollama", "")
			}
			if envKey := engine.ResolveAPIKeyFromEnv(preset.ArchitectProvider); envKey != "" {
				if err := sm.store.SaveAPIKey(preset.ArchitectProvider, envKey); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchArchModelsCmd(preset.ArchitectProvider, envKey)
			}
			if sm.store.HasAPIKey(preset.ArchitectProvider) {
				apiKey := sm.store.GetAPIKey(preset.ArchitectProvider)
				sm.fetching = true
				return sm, fetchArchModelsCmd(preset.ArchitectProvider, apiKey)
			}
			sm.step = 1
			sm.archKey = ""
		}
		return sm, nil

	case 1: // Architect API key entry
		switch key {
		case "esc", "escape":
			sm.step = 0
			sm.archKey = ""
		case "enter":
			apiKey := strings.TrimSpace(sm.archKey)
			if apiKey == "" {
				return sm, nil
			}
			preset := &engine.ProviderPresets[sm.archProviderSel]
			if err := sm.store.SaveAPIKey(preset.ArchitectProvider, apiKey); err != nil {
				mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
			}
			sm.fetching = true
			return sm, fetchArchModelsCmd(preset.ArchitectProvider, apiKey)
		case "backspace":
			if len(sm.archKey) > 0 {
				sm.archKey = sm.archKey[:len(sm.archKey)-1]
			}
		case "ctrl+u":
			sm.archKey = ""
		default:
			if len(msg.Runes) > 0 {
				sm.archKey += string(msg.Runes)
			}
		}
		return sm, nil

	case 2: // Architect model selection
		switch key {
		case "esc", "escape":
			sm.step = 0
		case "j", "down":
			if sm.architectSel < len(sm.architectModels)-1 {
				sm.architectSel++
			}
		case "k", "up":
			if sm.architectSel > 0 {
				sm.architectSel--
			}
		case "enter":
			sm.step = 3
			sm.workerProviderSel = sm.archProviderSel // default same as architect
		}
		return sm, nil

	case 3: // Worker provider selection
		switch key {
		case "esc", "escape":
			sm.step = 2
		case "j", "down":
			if sm.workerProviderSel < len(engine.ProviderPresets)-1 {
				sm.workerProviderSel++
			}
		case "k", "up":
			if sm.workerProviderSel > 0 {
				sm.workerProviderSel--
			}
		case "enter":
			workerPreset := &engine.ProviderPresets[sm.workerProviderSel]
			archPreset := &engine.ProviderPresets[sm.archProviderSel]
			sameProvider := workerPreset.ArchitectProvider == archPreset.ArchitectProvider

			if workerPreset.ArchitectProvider == "ollama" {
				if err := sm.store.SaveAPIKey("ollama", "ollama"); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchWorkerModelsCmd("ollama", "")
			}
			if sameProvider {
				// Already have the key — go straight to model selection.
				apiKey := sm.store.GetAPIKey(archPreset.ArchitectProvider)
				sm.fetching = true
				return sm, fetchWorkerModelsCmd(workerPreset.ArchitectProvider, apiKey)
			}
			if envKey := engine.ResolveAPIKeyFromEnv(workerPreset.ArchitectProvider); envKey != "" {
				if err := sm.store.SaveAPIKey(workerPreset.ArchitectProvider, envKey); err != nil {
					mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
				}
				sm.fetching = true
				return sm, fetchWorkerModelsCmd(workerPreset.ArchitectProvider, envKey)
			}
			if sm.store.HasAPIKey(workerPreset.ArchitectProvider) {
				apiKey := sm.store.GetAPIKey(workerPreset.ArchitectProvider)
				sm.fetching = true
				return sm, fetchWorkerModelsCmd(workerPreset.ArchitectProvider, apiKey)
			}
			// Need a key for the different provider.
			sm.step = 4
			sm.workerKey = ""
		}
		return sm, nil

	case 4: // Worker API key entry (different provider only)
		switch key {
		case "esc", "escape":
			sm.step = 3
			sm.workerKey = ""
		case "enter":
			apiKey := strings.TrimSpace(sm.workerKey)
			if apiKey == "" {
				return sm, nil
			}
			preset := &engine.ProviderPresets[sm.workerProviderSel]
			if err := sm.store.SaveAPIKey(preset.ArchitectProvider, apiKey); err != nil {
				mlog.Error("SaveAPIKey", slog.String("err", err.Error()))
			}
			sm.fetching = true
			return sm, fetchWorkerModelsCmd(preset.ArchitectProvider, apiKey)
		case "backspace":
			if len(sm.workerKey) > 0 {
				sm.workerKey = sm.workerKey[:len(sm.workerKey)-1]
			}
		case "ctrl+u":
			sm.workerKey = ""
		default:
			if len(msg.Runes) > 0 {
				sm.workerKey += string(msg.Runes)
			}
		}
		return sm, nil

	case 5: // Worker model selection
		switch key {
		case "esc", "escape":
			sm.step = 3
		case "j", "down":
			if sm.workerSel < len(sm.workerModels)-1 {
				sm.workerSel++
			}
		case "k", "up":
			if sm.workerSel > 0 {
				sm.workerSel--
			}
		case "enter":
			archPreset := &engine.ProviderPresets[sm.archProviderSel]
			workerPreset := &engine.ProviderPresets[sm.workerProviderSel]
			architectModel := ""
			if len(sm.architectModels) > 0 {
				architectModel = sm.architectModels[sm.architectSel].ID
			}
			workerModel := ""
			if len(sm.workerModels) > 0 {
				workerModel = sm.workerModels[sm.workerSel].ID
			}
			archAPIKey := sm.store.GetAPIKey(archPreset.ArchitectProvider)
			workerAPIKey := sm.store.GetAPIKey(workerPreset.ArchitectProvider)
			if err := sm.store.SaveAuth(
				archPreset.ArchitectProvider, archAPIKey, architectModel,
				workerPreset.ArchitectProvider, workerAPIKey, workerModel,
			); err != nil {
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

func (sm setupModel) HandleWorkerModelsFetched(msg workerModelsFetchedMsg) setupModel {
	sm.fetching = false
	sm.workerModels = msg.models
	sort.Slice(sm.workerModels, func(i, j int) bool { return sm.workerModels[i].ID < sm.workerModels[j].ID })
	sm.workerSel = 0
	sm.step = 5
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

	// API key entry (steps 1 and 4)
	if sm.step == 1 || sm.step == 4 {
		var providerName string
		var inputKey string
		var envVarHint string
		if sm.step == 1 {
			preset := engine.ProviderPresets[sm.archProviderSel]
			providerName = preset.Name
			inputKey = sm.archKey
			if envVars, ok := engine.ProviderEnvVars[preset.ArchitectProvider]; ok && len(envVars) > 0 {
				envVarHint = envVars[0]
			}
		} else {
			preset := engine.ProviderPresets[sm.workerProviderSel]
			providerName = preset.Name
			inputKey = sm.workerKey
			if envVars, ok := engine.ProviderEnvVars[preset.ArchitectProvider]; ok && len(envVars) > 0 {
				envVarHint = envVars[0]
			}
		}

		title := lipgloss.NewStyle().Foreground(ColorMuted).Render("enter your " + providerName + " API key")
		display := strings.Repeat("•", len(inputKey))
		if len(inputKey) > 0 && len(inputKey) <= 8 {
			display = inputKey
		} else if len(inputKey) > 8 {
			display = inputKey[:4] + strings.Repeat("•", len(inputKey)-8) + inputKey[len(inputKey)-4:]
		}
		cursor := lipgloss.NewStyle().Foreground(ColorActive).Render("█")
		inputLine := lipgloss.NewStyle().Foreground(ColorText).Render(display) + cursor

		envHint := ""
		if envVarHint != "" {
			envHint = lipgloss.NewStyle().Foreground(ColorDim).Italic(true).Render("or set " + envVarHint + " env var")
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

	// Model selection (steps 2 and 5)
	if sm.step == 2 || sm.step == 5 {
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

		help := strings.Join([]string{
			keyStyle.Render("j/k") + valStyle.Render(" select"),
			keyStyle.Render("enter") + valStyle.Render(" confirm"),
			keyStyle.Render("esc") + valStyle.Render(" back"),
		}, sep)

		header := lipgloss.JoinVertical(lipgloss.Center, append(banner, "", subtitle)...)
		content := lipgloss.JoinVertical(lipgloss.Left, "", strings.Join(modelLines, "\n"), "")
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

	// Provider selection (steps 0 and 3)
	var subtitle string
	var providerSel int
	if sm.step == 0 {
		subtitle = lipgloss.NewStyle().Foreground(ColorMuted).Render("select architect provider")
		providerSel = sm.archProviderSel
	} else {
		subtitle = lipgloss.NewStyle().Foreground(ColorMuted).Render("select worker provider")
		providerSel = sm.workerProviderSel
	}

	var providerLines []string
	for i, p := range engine.ProviderPresets {
		sel := i == providerSel
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
